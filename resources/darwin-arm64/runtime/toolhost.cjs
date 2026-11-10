'use strict';

// Go owns policy, integrity, staging, process groups and verification. This
// adapter joins the pinned read-only upstream tool APIs to JSON lines. It never
// writes console data: no save, mod, launcher or filesystem write path exists here.
const fs = require('node:fs');
const fsp = require('node:fs/promises');
const path = require('node:path');
const http = require('node:http');
const net = require('node:net');
const crypto = require('node:crypto');
const readline = require('node:readline');

const TOOLS = ['rpc-status', 'system-code-dump', 'ouncecake-webdav', 'ouncecake-ftp', 'sd-browser'];
const MINIMUM_FREE_BYTES = 512n * 1024n * 1024n;
const check = (value, message) => { if (!value) throw Error(message); };

let sequence = 0, tool = '';
const emit = (type, fields = {}) =>
  process.stdout.write(JSON.stringify({type, sequence: ++sequence, tool, ...fields}) + '\n');

// Callers canonicalize a root once, then join fixed names under it, so lstat
// alone keeps every checked path free of symlinks.
async function owned(file, kind = 'file', privateMode = true) {
  check(path.isAbsolute(file) && path.resolve(file) === file, 'Use absolute canonical tool paths.');
  const info = await fsp.lstat(file);
  check(!info.isSymbolicLink() && info.uid === process.getuid() &&
    (kind === 'directory' ? info.isDirectory() : info.isFile()) &&
    (info.mode & (privateMode ? 0o077 : 0o022)) === 0,
  'Expected an owned ' + (privateMode ? 'private ' : '') + kind + ' without symlinks: ' + file);
  return info;
}

async function privateJSON(workspace, name) {
  const file = path.join(workspace, name);
  check(file.startsWith(workspace + path.sep), 'A session proof pointed outside its run folder.');
  const info = await owned(file);
  check(info.size <= 8 * 1024 * 1024, 'A session proof exceeds its limit.');
  return JSON.parse(await fsp.readFile(file, 'utf8'));
}

// The same held contract Go verifies, re-checked inside the tool process before
// any upstream module is required.
async function heldProof(requested) {
  const workspace = await fsp.realpath(requested);
  await owned(workspace, 'directory');
  const session = path.basename(workspace);
  const result = await privateJSON(workspace, 'result.json');
  check(result.verified === true && result.fs?.verified === true && result.fs?.restored === true &&
    result.fs?.cleanup?.deferred === true && result.fs?.cleanup?.cleanedUp !== true &&
    result.fs?.cleanup?.donor?.cleanedUp === true && result.fs?.retainedEndpoint?.ready === true &&
    result.fs.retainedEndpoint.session === session && result.rpc?.mode === 'held' &&
    result.rpc?.state === 'listening' && !result.completed && !result.error,
  'A verified session with a live retained RPC lease is required.');
  const connection = await privateJSON(workspace, 'connection.json');
  check(connection.addresses?.session === session && connection.addresses?.target === 'switch2' &&
    Number.isSafeInteger(connection.port) && connection.port > 0 && connection.port <= 65535,
  'The retained endpoint does not belong to this session.');
  return {workspace, session, result, connection};
}

// A fresh loopback status request. A stale result file is never accepted as
// proof, and a failed request is reported, never replayed.
function rpcStatus(connection, timeoutMs = 10000) {
  const body = JSON.stringify({command: 'status', ...(connection.token ? {token: connection.token} : {})});
  return new Promise((resolve, reject) => {
    const request = http.request({host: '127.0.0.1', port: connection.port, path: '/', method: 'POST',
      headers: {'content-type': 'application/json'}}, response => {
      const parts = []; let length = 0;
      response.on('error', reject);
      response.on('data', part => {
        length += part.length;
        if (length > 65536) response.destroy(Error('The RPC status response exceeded its limit.'));
        else parts.push(part);
      });
      response.on('end', () => {
        try {
          const value = JSON.parse(Buffer.concat(parts));
          check(response.statusCode === 200 && value.ready === true && !value.error,
            'The retained RPC endpoint is not ready.');
          resolve(value);
        } catch (error) { reject(error); }
      });
    });
    request.setTimeout(timeoutMs, () => request.destroy(Error('The RPC status request timed out; it was not replayed.')));
    request.on('error', reject);
    request.end(body);
  });
}

function control() {
  let stopRequested = false, selected, offer = new Set(), resolveStop, resolveSelection;
  const stopped = new Promise(resolve => { resolveStop = resolve; });
  const selection = new Promise(resolve => { resolveSelection = resolve; });
  const stop = () => { stopRequested = true; resolveStop(); resolveSelection(null); };
  const lines = readline.createInterface({input: process.stdin});
  lines.on('line', line => {
    let command;
    try {
      check(Buffer.byteLength(line) <= 8192, 'control line too large');
      command = JSON.parse(line);
      check(command && typeof command === 'object' && !Array.isArray(command), 'invalid control line');
    } catch { emit('progress', {message: 'An invalid control line was ignored.'}); return; }
    if (command.type === 'stop') stop();
    else if (command.type === 'select' && selected === undefined && offer.has(command.id)) {
      selected = command.id;
      resolveSelection(command.id);
    } else emit('progress', {message: 'Only a stop request or one offered selection is accepted.'});
  });
  lines.on('close', stop);
  process.on('SIGTERM', stop);
  process.on('SIGINT', stop);
  return {
    get stopping() { return stopRequested; },
    shouldStop: () => stopRequested,
    stopped, selection,
    offer: ids => { offer = new Set(ids); },
    close: () => { lines.close(); process.stdin.pause(); }
  };
}

async function digest(file, expected, size) {
  const hash = crypto.createHash('sha256');
  let bytes = 0;
  for await (const chunk of fs.createReadStream(file, {highWaterMark: 1024 * 1024})) {
    bytes += chunk.length;
    check(size === undefined || bytes <= size, 'A captured file exceeds its recorded size.');
    hash.update(chunk);
  }
  check((size === undefined || bytes === size) && hash.digest('hex') === expected,
    'A captured file failed its SHA-256 readback: ' + file);
}

// The capture module is the engine's own, already staged and digest-verified as
// part of the selected host profile and copied into this run by the bridge.
async function systemCodeDump(workspace, proof, destination, modulePath) {
  const current = await privateJSON(workspace, 'switch2/fs-execution-current.json');
  check(current.session === proof.session && current.verified === true && current.restored === true &&
    current.cleanup?.deferred === true && typeof current.file === 'string',
  'The live FS execution proof does not match this retained session.');
  await owned(modulePath, 'file', false);
  await owned(path.dirname(destination), 'directory');
  await fsp.mkdir(destination, {mode: 0o700, recursive: true});
  await owned(destination, 'directory');
  check((await fsp.readdir(destination)).length === 0,
    'The dump destination must be empty; existing captures are never overwritten.');
  const space = await fsp.statfs(destination, {bigint: true});
  check(BigInt(space.bavail) * BigInt(space.bsize) >= MINIMUM_FREE_BYTES,
    'The system-code dump needs at least 512 MiB free on this Mac.');
  emit('started', {message: 'Capturing installed system code. Keep the console session open.'});
  process.env.NS2_FIRMWARE_DUMP_DIR = destination;
  const capture = require(modulePath);
  check(typeof capture.main === 'function', 'The staged engine has no system-code capture entrypoint.');
  await capture.main(path.resolve(workspace, current.file));
  const manifest = JSON.parse(await fsp.readFile(path.join(destination, 'firmware-dump.json'), 'utf8'));
  const total = manifest.inventory?.total;
  check(manifest.complete === true && Number.isSafeInteger(total) && total > 0 && total <= 1024 &&
    Array.isArray(manifest.entries) && manifest.entries.length === total,
  'The capture did not produce a complete verified inventory.');
  for (const entry of manifest.entries) {
    if (entry.status === 'codefs-unavailable') continue;
    for (const file of entry.files || []) {
      check(typeof file.file === 'string' && file.file.startsWith(destination + path.sep),
        'A captured file escaped the dump destination.');
      await digest(file.file, file.sha256, file.size);
    }
  }
  emit('complete', {path: destination, total, message: 'System-code capture verified.'});
}

function readOnlySD(storage) {
  const directory = {type: 'dir', size: 0};
  const inside = value => {
    check(typeof value === 'string' && (value === '/sd' || value.startsWith('/sd/')),
      'Only the SD filesystem is exported.');
    return value;
  };
  return {
    list: value => value === '/' ? [{name: 'sd', ...directory}] : storage.list(inside(value)),
    stat: value => value === '/' ? directory : storage.stat(inside(value)),
    openRead: value => storage.openRead(inside(value)),
    read: (file, offset, count) => storage.read(file, offset, count),
    close: file => storage.close(file)
  };
}

async function sdBrowser(toolRoot, workspace, proof, consoleAddress, run) {
  const upstream = path.join(toolRoot, 'integrations/super-file-bros/upstream');
  const load = name => require(path.join(upstream, name));
  let attachment, server, pumping, stopping = false, failure;
  try {
    emit('started', {message: 'Opening the read-only SD export.'});
    attachment = await load('modules/iz-exchange/server/ouncecake/engine/session.cjs').attachGame(workspace);
    check(attachment.live.session === proof.session, 'The export attached to a different native session.');
    const storage = await load('tools/dolphin-fs-rpc/storage.cjs').createStorage(attachment);
    check(storage.mounts.includes('sd'), 'The SD filesystem is unavailable in this retained session.');
    const library = load('tools/dolphin-fs-rpc/library.cjs');
    const port = await library.availablePort(consoleAddress);
    const native = await load('core/console/host/ftp-native.cjs')
      .create(require(path.join(workspace, 'shared/rpc.cjs')), workspace);
    server = load('core/console/host/ftp-server.cjs').create({...native, storage: readOnlySD(storage),
      readOnly: true, greeting: 'any2jb SD files, read-only'});
    await server.start(port);
    pumping = (async () => { while (!stopping) { await server.pump(); await new Promise(r => setTimeout(r, 10)); } })();
    pumping.catch(error => { failure ||= error; });
    const listing = await library.probeFtp(consoleAddress, port);
    check(listing.some(line => /(?:^|\s)sd$/.test(line)), 'The SD listing did not verify.');
    check(!run.stopping, 'The export stopped before verification.');
    emit('running', {endpoint: 'ftp://' + consoleAddress + ':' + port + '/sd/', readOnly: true,
      message: 'Read-only SD export is listening on the console.'});
    await run.stopped;
  } catch (error) { failure = error; }
  finally {
    stopping = true;
    for (const close of [() => pumping, () => server?.stop(), () => attachment?.close()]) {
      try { await close(); } catch (error) { failure ||= error; }
    }
  }
  if (failure) throw failure;
}

async function ounceCake(toolRoot, workspace, proof, consoleAddress, mode, run) {
  const vendor = path.join(toolRoot, 'vendor/OunceCake');
  const load = name => require(path.join(vendor, 'engine', name + '.cjs'));
  const image = await fsp.readFile(path.join(vendor, 'engine/oz-code/native.bin'));
  let attachment, server, bridge, failure, cleanupBlocked = false;
  try {
    emit('started', {message: 'Preparing the read-only game export.'});
    attachment = await load('session').attachGame(workspace);
    check(attachment.live.session === proof.session, 'The export attached to a different native session.');
    const found = await load('content').discoverGames({...attachment, shouldStop: run.shouldStop});
    check(Array.isArray(found) && found.length > 0 && found.length <= 1000,
      'No installed game or inserted game card was found.');
    const games = [], ids = new Set();
    for (const game of found) {
      check(/^[0-9a-f]{16}$/.test(game.title) && !ids.has(game.title), 'The console returned invalid game metadata.');
      ids.add(game.title);
      games.push({titleId: game.title, name: String(game.name || 'Unknown title').replace(/[^\x20-\x7e]/g, ' ').trim().slice(0, 80) || 'Unknown title'});
    }
    run.offer(ids);
    emit('catalog', {games, message: 'Choose one game to export read-only.'});
    const title = await run.selection;
    check(title && !run.stopping, 'No game was selected.');
    emit('progress', {message: 'Preparing the selected game. This can take several minutes.'});
    const prepared = await load('mounts').prepareGame({attachment, title, shouldStop: run.shouldStop});
    check(!run.stopping, 'The export stopped before its server started.');
    server = await load('server').deployServer({attachment, image, ...prepared, port: 2121, zipPort: 0});
    check(!server.address || server.address === consoleAddress, 'The console reported a different address.');
    let endpoint = 'http://' + consoleAddress + ':2121/';
    if (mode === 'ftp') {
      bridge = await require(path.join(toolRoot, 'integrations/mintbrew-ouncecake/ftp-bridge.cjs'))
        .start({index: prepared.index, sourceAddress: consoleAddress, sourcePort: 2121});
      check(bridge.address === '127.0.0.1' && Number.isInteger(bridge.port) && bridge.port > 0 &&
        bridge.port < 65536 && typeof bridge.close === 'function',
      'The FTP bridge did not bind this Mac only.');
      endpoint = 'ftp://127.0.0.1:' + bridge.port + '/';
    }
    emit('running', {endpoint, readOnly: true, message: 'Read-only export is running.'});
    await run.stopped;
  } catch (error) { failure = error; cleanupBlocked = Boolean(error?.cleanupBlocked); }
  finally {
    if (bridge) { try { await bridge.close(); } catch (error) { failure ||= error; } }
    if (server && !cleanupBlocked) {
      try {
        const shutdown = await server.close();
        check(shutdown?.nativeStopped === true && shutdown?.joined === true && shutdown?.mappingsCleared === true,
          'The export did not prove native cleanup.');
      } catch (error) { failure ||= error; cleanupBlocked = true; }
    }
    if (attachment && !cleanupBlocked) {
      try { await attachment.close(); } catch (error) { failure ||= error; cleanupBlocked = true; }
    }
  }
  if (cleanupBlocked) {
    const error = failure || Error('Export cleanup was blocked.');
    error.cleanupBlocked = true;
    throw error;
  }
  if (failure) throw failure;
}

async function main(args = process.argv.slice(2)) {
  if (args.length === 1 && args[0] === '--self-test') {
    process.stdout.write(JSON.stringify({type: 'self-test', tools: TOOLS}) + '\n');
    return;
  }
  const [stagedRoot, selected, workspace, consoleAddress, destination] = args;
  check(args.length === 5, 'usage: toolhost.cjs <staged-root> <tool> <workspace> <console-IPv4|-> <destination|->');
  check(TOOLS.includes(selected), 'Unknown read-only tool.');
  tool = selected;
  const record = JSON.parse(await fsp.readFile(path.join(stagedRoot, 'stage.json'), 'utf8'));
  check(record.source === 'developer-staged' && record.redistributable === false,
    'Read-only tools require an explicit developer stage of their pinned sources.');
  const proof = await heldProof(path.resolve(workspace));
  await rpcStatus(proof.connection);
  const owned = proof.workspace;
  if (selected === 'rpc-status') {
    emit('status', {ready: true, message: 'The retained RPC lease answered a fresh status request.'});
    return;
  }
  const run = control();
  try {
    if (selected === 'system-code-dump') {
      check(destination !== '-' && path.isAbsolute(destination), 'Choose a dedicated dump destination.');
      await systemCodeDump(owned, proof, path.resolve(destination),
        path.join(owned, 'switch2/firmware-dump.cjs'));
    } else {
      check(net.isIPv4(consoleAddress), 'Enter the console IPv4 address.');
      const toolRoot = path.join(stagedRoot, 'tools', selected === 'sd-browser' ? 'sd-browser' : 'ouncecake');
      if (selected === 'sd-browser') await sdBrowser(toolRoot, owned, proof, consoleAddress, run);
      else await ounceCake(toolRoot, owned, proof, consoleAddress,
        selected === 'ouncecake-ftp' ? 'ftp' : 'webdav', run);
    }
    emit('stopped', {message: 'The read-only tool released its own resources.'});
  } finally {
    run.close();
  }
}

if (require.main === module) main().catch(error => {
  emit('failed', {cleanupBlocked: Boolean(error?.cleanupBlocked),
    message: error.message + ' This tool was not retried.'});
  process.exitCode = 1;
});
module.exports = {main, heldProof, readOnlySD, TOOLS};
