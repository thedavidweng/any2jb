'use strict';

// Read-only SaveKit host. Go owns policy, staging, integrity and verification;
// this module joins the pinned upstream read-only tool scripts to JSON lines.
// No write, restore or delete path exists here: the request schema has no such
// action, the staged tool set has no write entrypoint, and child argv is built
// only by buildArgs. The save itself is only ever opened read-only by the
// upstream tools (fs read-only command 53); exports go to a folder Go chose
// outside every session workspace.
const fs = require('node:fs');
const fsp = require('node:fs/promises');
const path = require('node:path');
const http = require('node:http');
const crypto = require('node:crypto');
const {spawn} = require('node:child_process');

const TOOLS = ['list-apps.cjs', 'save-inventory.cjs', 'save-pull-root.cjs'];
const ACTIONS = new Set(['accounts', 'titles', 'inventory', 'inspect', 'backup', 'filetime']);
const titlePattern = /^[0-9a-f]{16}$/;
const uidPattern = /^[0-9a-f]{32}$/;
const check = (value, message) => { if (!value) throw Error(message); };

let sequence = 0;
const emit = (type, fields = {}) =>
  process.stdout.write(JSON.stringify({type, sequence: ++sequence, ...fields}) + '\n');

function request(value) {
  check(value && typeof value === 'object' && !Array.isArray(value), 'Invalid savekit request.');
  check(ACTIONS.has(value.action), 'Unsupported read-only savekit action.');
  const fields = {
    accounts: ['action'], titles: ['action'], inventory: ['action'],
    inspect: ['action', 'titleId', 'uid', 'archive', 'all'],
    backup: ['action', 'titleId', 'uid', 'archive', 'all'],
    filetime: ['action', 'titleId', 'uid', 'archive', 'all'],
  };
  check(Object.keys(value).every(key => fields[value.action].includes(key)),
    'Unknown field in savekit request.');
  const parsed = {action: value.action};
  if (!['accounts', 'titles', 'inventory'].includes(value.action)) {
    const titleId = typeof value.titleId === 'string' ? value.titleId.toLowerCase() : '';
    const uid = typeof value.uid === 'string' ? value.uid.toLowerCase() : '';
    check(titlePattern.test(titleId), 'Use a 16-digit hexadecimal title ID.');
    check(uidPattern.test(uid), 'Choose a 32-digit hexadecimal account ID.');
    parsed.titleId = titleId;
    parsed.uid = uid;
    parsed.all = value.all === true;
    check(!parsed.all || value.action !== 'filetime', 'Timestamp reads need one archive.');
    // An all-archive scan keeps only the keys the user gave; a single-archive
    // target gets the documented defaults.
    if (parsed.all) {
      parsed.archive = value.archive === undefined ? undefined : archiveSelection(value.archive);
    } else {
      parsed.archive = archive(value.archive);
    }
  }
  return parsed;
}

function archive(value) {
  const defaults = {space: 1, type: 1, rank: 0, index: 0, staticId: 0};
  if (value === undefined) return {...defaults};
  check(value && typeof value === 'object' && !Array.isArray(value), 'Invalid save archive options.');
  const keys = ['space', 'type', 'rank', 'index', 'staticId'];
  check(Object.keys(value).every(key => keys.includes(key)), 'Unknown save archive option.');
  const result = {...defaults, ...value};
  for (const [name, limit] of [['space', 255], ['type', 7], ['rank', 1],
    ['index', 65535], ['staticId', Number.MAX_SAFE_INTEGER]])
    check(Number.isSafeInteger(result[name]) && result[name] >= 0 && result[name] <= limit,
      'Invalid save archive ' + name + '.');
  return result;
}

function archiveSelection(value) {
  check(value && typeof value === 'object' && !Array.isArray(value), 'Invalid save archive options.');
  const keys = ['space', 'type', 'rank', 'index', 'staticId'];
  check(Object.keys(value).every(key => keys.includes(key)), 'Unknown save archive option.');
  for (const [name, number] of Object.entries(value)) {
    const limit = {space: 255, type: 7, rank: 1, index: 65535, staticId: Number.MAX_SAFE_INTEGER}[name];
    check(Number.isSafeInteger(number) && number >= 0 && number <= limit,
      'Invalid save archive ' + name + '.');
  }
  return {...value};
}

// argv for the one upstream tool a read-only action needs. Nothing else runs.
function buildArgs(parsed, run, destination, reportPath) {
  const tool = name => name;
  const selection = parsed.archive || archive(undefined);
  const archiveArgs = parsed.all
    ? Object.entries(parsed.archive || {}).flatMap(([name, number]) =>
      [{space: '--space', type: '--type', rank: '--rank', index: '--index',
        staticId: '--static-id'}[name], String(number)])
    : ['--space', String(selection.space), '--type', String(selection.type),
      '--rank', String(selection.rank), '--index', String(selection.index),
      '--static-id', String(selection.staticId)];
  switch (parsed.action) {
    case 'accounts':
      return [tool('save-pull-root.cjs'), run, '0000000000000000', '.', '--list-uids'];
    case 'titles':
      return [tool('list-apps.cjs'), run, '--no-names'];
    case 'inventory':
      return [tool('save-inventory.cjs'), run, '--json', reportPath];
    case 'inspect':
      return [tool('save-pull-root.cjs'), run, parsed.titleId, '.', parsed.uid,
        ...archiveArgs, '--list-only', ...(parsed.all ? ['--all'] : [])];
    case 'filetime':
      return [tool('save-pull-root.cjs'), run, parsed.titleId, '.', parsed.uid,
        ...archiveArgs, '--filetime'];
    case 'backup':
      check(destination !== '-' && path.isAbsolute(destination),
        'A backup needs an absolute export destination.');
      return [tool('save-pull-root.cjs'), run, parsed.titleId, destination, parsed.uid,
        ...archiveArgs, ...(parsed.all ? ['--all'] : [])];
    default:
      throw Error('Unsupported read-only savekit action.');
  }
}

// The same held contract the tool host verifies, re-checked before any
// upstream module is required: private evidence plus a fresh loopback status.
async function owned(file, kind = 'file') {
  check(path.isAbsolute(file) && path.resolve(file) === file, 'Use absolute canonical paths.');
  const info = await fsp.lstat(file);
  check(!info.isSymbolicLink() && info.uid === process.getuid() &&
    (kind === 'directory' ? info.isDirectory() : info.isFile()) &&
    (info.mode & 0o077) === 0, 'Expected an owned private ' + kind + ': ' + file);
  return info;
}

async function privateJSON(workspace, name) {
  const file = path.join(workspace, name);
  check(file.startsWith(workspace + path.sep), 'A session proof pointed outside its run folder.');
  const info = await owned(file);
  check(info.size <= 8 * 1024 * 1024, 'A session proof exceeds its limit.');
  return JSON.parse(await fsp.readFile(file, 'utf8'));
}

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
  return {workspace, connection};
}

function rpcStatus(connection, timeoutMs = 10000) {
  const body = JSON.stringify({command: 'status', ...(connection.token ? {token: connection.token} : {})});
  return new Promise((resolve, reject) => {
    const req = http.request({host: '127.0.0.1', port: connection.port, path: '/', method: 'POST',
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
    req.setTimeout(timeoutMs, () => req.destroy(Error('The RPC status request timed out; it was not replayed.')));
    req.on('error', reject);
    req.end(body);
  });
}

// One bounded child. Raw tool stderr can contain console details, so it is
// capped and never relayed; a failure carries only a fixed message.
function runTool(script, args, timeoutMs = 600000) {
  return new Promise((resolve, reject) => {
    const child = spawn(process.execPath, [script, ...args],
      {stdio: ['ignore', 'pipe', 'pipe'], detached: true});
    let stdout = '', settled = false;
    const stop = signal => { try { process.kill(-child.pid, signal); } catch {} };
    const timer = setTimeout(() => {
      if (!settled) { settled = true; stop('SIGTERM'); setTimeout(() => stop('SIGKILL'), 3000).unref();
        reject(Error('The read-only save operation timed out. Restart the console before retrying.')); }
    }, timeoutMs);
    timer.unref();
    child.stdout.setEncoding('utf8');
    child.stdout.on('data', chunk => {
      stdout += chunk;
      if (stdout.length > 1024 * 1024 && !settled) {
        settled = true; stop('SIGTERM');
        reject(Error('The read-only save operation produced too much output.'));
      }
    });
    let stderrLength = 0;
    child.stderr.on('data', chunk => {
      stderrLength += chunk.length;
      if (stderrLength > 65536) child.stderr.destroy();
    });
    child.once('error', error => { if (!settled) { settled = true; clearTimeout(timer); reject(error); } });
    child.once('close', code => {
      if (settled) return;
      settled = true; clearTimeout(timer);
      if (code !== 0) reject(Error('The underlying read-only save tool did not finish.'));
      else resolve(stdout);
    });
  });
}

function parseUsers(output) {
  const users = [...new Set(output.split('\n').map(line => line.trim().toLowerCase())
    .filter(line => uidPattern.test(line)))];
  check(users.length > 0 && users.length <= 8, 'No console accounts were returned.');
  return users;
}

function parseTitles(output) {
  const titles = [...new Set(output.split('\n')
    .map(line => line.trim().replace(/^0x/i, '').toLowerCase())
    .filter(line => titlePattern.test(line)))];
  check(titles.length > 0 && titles.length <= 2048, 'No installed application IDs were returned.');
  return titles;
}

function parseInventory(report) {
  check(report && report.rootPid === 1 && Array.isArray(report.entries) &&
    report.entries.length <= 32768 && report.spaces && typeof report.spaces === 'object',
  'The save archive inventory is incomplete.');
  return report.entries.map(entry => {
    check(entry && typeof entry.id === 'string' && /^\d{1,20}$/.test(entry.id) &&
      typeof entry.applicationId === 'string' && /^0x[0-9a-f]{16}$/.test(entry.applicationId) &&
      uidPattern.test(entry.uid) && Number.isInteger(entry.space) && entry.space >= 0 && entry.space <= 255 &&
      Number.isInteger(entry.type) && entry.type >= 0 && entry.type <= 7 &&
      Number.isInteger(entry.rank) && entry.rank >= 0 && entry.rank <= 1 &&
      Number.isSafeInteger(entry.index) && entry.index >= 0 && entry.index <= 65535 &&
      typeof entry.size === 'string' && /^\d{1,20}$/.test(entry.size),
    'The save archive inventory contains an invalid record.');
    return {id: entry.id, titleId: entry.applicationId.slice(2), uid: entry.uid,
      space: entry.space, type: entry.type, rank: entry.rank, index: entry.index, size: entry.size};
  });
}

async function readRequest() {
  const chunks = [];
  let size = 0;
  for await (const chunk of process.stdin) {
    size += chunk.length;
    check(size <= 8192, 'The savekit request is too large.');
    chunks.push(chunk);
  }
  const input = Buffer.concat(chunks).toString('utf8').trim();
  check(input && !input.includes('\n'), 'Send exactly one savekit request.');
  return JSON.parse(input);
}

async function main(args = process.argv.slice(2)) {
  const [stagedRoot, runDirectory, destination = '-'] = args;
  check(args.length === 3, 'usage: savekit-host.cjs <staged-root> <run-directory> <destination|->');
  const record = JSON.parse(await fsp.readFile(path.join(stagedRoot, 'stage.json'), 'utf8'));
  check(record.source === 'developer-staged' && record.redistributable === false,
    'Read-only SaveKit tools require an explicit developer stage of their pinned sources.');
  const tools = path.join(stagedRoot, 'tools', 'savekit', 'vendor', 'ns2-savekit', 'tools');
  for (const name of TOOLS) await owned(path.join(tools, name), 'file');
  const parsed = request(await readRequest());
  const proof = await heldProof(path.resolve(runDirectory));
  await rpcStatus(proof.connection);
  emit('started', {message: 'Read-only save operation started. Keep the console session open.'});
  const reportPath = path.join(stagedRoot, 'tools', 'savekit',
    'inventory-' + crypto.randomBytes(6).toString('hex') + '.json');
  try {
    const argv = buildArgs(parsed, proof.workspace, destination, reportPath);
    check(TOOLS.includes(argv[0]), 'Only a pinned read-only tool may run.');
    argv[0] = path.join(tools, argv[0]);
    const output = await runTool(argv[0], argv.slice(1));
    switch (parsed.action) {
      case 'accounts':
        emit('result', {action: 'accounts', users: parseUsers(output)});
        return;
      case 'titles':
        emit('result', {action: 'titles', titles: parseTitles(output)});
        return;
      case 'inventory':
        emit('result', {action: 'inventory',
          inventory: parseInventory(JSON.parse(await fsp.readFile(reportPath, 'utf8')))});
        return;
      case 'inspect':
      case 'filetime':
        check(output.length > 0 && output.length <= 256 * 1024,
          'The read-only save inspection returned no bounded listing.');
        emit('result', {action: parsed.action, output});
        return;
      case 'backup':
        emit('result', {action: 'backup', path: destination});
        return;
    }
  } finally {
    await fsp.rm(reportPath, {force: true});
  }
}

module.exports = {request, archive, buildArgs, parseUsers, parseTitles, parseInventory,
  heldProof, main, TOOLS};
if (require.main === module) main().catch(error => {
  emit('failed', {message: String(error.message).replace(/[\x00-\x1f\x7f]+/g, ' ').slice(0, 512)});
  process.exitCode = 1;
});
