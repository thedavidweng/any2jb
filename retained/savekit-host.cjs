'use strict';

// SaveKit host. Go owns the safety gate: preview, per-operation confirmation,
// target validation, the verified backup and post-write verification. This
// module joins the pinned upstream tool scripts to JSON lines. A mutation runs
// only with an explicit confirm, re-verifies the backup Go prepared before any
// console change, stages files locally and requires the pinned tool's own
// post-commit verification line. Nothing is retried: a failure is reported
// and the verified backup stays where Go put it.
const fs = require('node:fs');
const fsp = require('node:fs/promises');
const path = require('node:path');
const http = require('node:http');
const crypto = require('node:crypto');
const {spawn} = require('node:child_process');

const TOOLS = ['list-apps.cjs', 'save-inventory.cjs', 'save-pull-root.cjs',
  'save-write-stage.cjs', 'save-delete-root.cjs'];
const READ_ACTIONS = new Set(['accounts', 'titles', 'inventory', 'inspect', 'backup', 'filetime']);
const WRITE_ACTIONS = new Set(['write', 'restore', 'delete']);
const titlePattern = /^[0-9a-f]{16}$/;
const uidPattern = /^[0-9a-f]{32}$/;
const check = (value, message) => { if (!value) throw Error(message); };
const spaces = {0: 'System', 1: 'User', 2: 'SdSystem', 3: 'Temporary', 4: 'SdUser',
  100: 'ProperSystem', 101: 'SafeMode', 102: 'ProperUser'};
const types = {0: 'System', 1: 'Account', 2: 'Bcat', 3: 'Device', 4: 'Temporary',
  5: 'Cache', 6: 'SystemBcat', 7: 'StreamPlay'};

let sequence = 0;
const emit = (type, fields = {}) =>
  process.stdout.write(JSON.stringify({type, sequence: ++sequence, ...fields}) + '\n');

function request(value) {
  check(value && typeof value === 'object' && !Array.isArray(value), 'Invalid savekit request.');
  check(READ_ACTIONS.has(value.action) || WRITE_ACTIONS.has(value.action),
    'Unsupported savekit action.');
  const fields = {
    accounts: ['action'], titles: ['action'], inventory: ['action'],
    inspect: ['action', 'titleId', 'uid', 'archive', 'all'],
    backup: ['action', 'titleId', 'uid', 'archive', 'all'],
    filetime: ['action', 'titleId', 'uid', 'archive', 'all'],
    write: ['action', 'titleId', 'uid', 'sourceFolder', 'allowReplace', 'confirm', 'previewDigest'],
    restore: ['action', 'titleId', 'uid', 'archive', 'sourceFolder', 'confirm', 'previewDigest'],
    delete: ['action', 'titleId', 'uid', 'fileName', 'confirm', 'previewDigest'],
  };
  check(Object.keys(value).every(key => fields[value.action].includes(key)),
    'Unknown field in savekit request.');
  const parsed = {action: value.action};
  if (['accounts', 'titles', 'inventory'].includes(value.action)) return parsed;
  const titleId = typeof value.titleId === 'string' ? value.titleId.toLowerCase() : '';
  const uid = typeof value.uid === 'string' ? value.uid.toLowerCase() : '';
  check(titlePattern.test(titleId), 'Use a 16-digit hexadecimal title ID.');
  check(uidPattern.test(uid), 'Choose a 32-digit hexadecimal account ID.');
  parsed.titleId = titleId;
  parsed.uid = uid;
  if (READ_ACTIONS.has(value.action)) {
    parsed.all = value.all === true;
    check(!parsed.all || value.action !== 'filetime', 'Timestamp reads need one archive.');
    // An all-archive scan keeps only the keys the user gave; a single-archive
    // target gets the documented defaults.
    if (parsed.all) {
      parsed.archive = value.archive === undefined ? undefined : archiveSelection(value.archive);
    } else {
      parsed.archive = archive(value.archive);
    }
    return parsed;
  }
  check(value.confirm === true, 'Confirm this exact save operation before running it.');
  check(typeof value.previewDigest === 'string' && /^[0-9a-f]{64}$/.test(value.previewDigest),
    'Preview the affected files before confirming this save operation.');
  parsed.previewDigest = value.previewDigest;
  if (value.action === 'write') {
    check(typeof value.sourceFolder === 'string' && path.isAbsolute(value.sourceFolder),
      'Choose an absolute local save folder.');
    check(value.allowReplace === undefined || typeof value.allowReplace === 'boolean',
      'Invalid replacement option.');
    parsed.sourceFolder = value.sourceFolder;
    parsed.allowReplace = value.allowReplace === true;
  }
  if (value.action === 'restore') {
    check(typeof value.sourceFolder === 'string' && path.isAbsolute(value.sourceFolder),
      'Choose an absolute verified backup folder.');
    parsed.sourceFolder = value.sourceFolder;
    parsed.archive = archive(value.archive);
    check([1, 3].includes(parsed.archive.type) && parsed.archive.rank === 0 &&
      parsed.archive.index === 0 && parsed.archive.staticId === 0,
      'Restore supports Account or Device saves at rank 0 and index 0 only.');
  }
  if (value.action === 'delete') {
    check(safeRootName(value.fileName), 'Choose one file at the save root to delete.');
    parsed.fileName = value.fileName;
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
      return ['save-pull-root.cjs', run, '0000000000000000', '.', '--list-uids'];
    case 'titles':
      return ['list-apps.cjs', run, '--no-names'];
    case 'inventory':
      return ['save-inventory.cjs', run, '--json', reportPath];
    case 'inspect':
      return ['save-pull-root.cjs', run, parsed.titleId, '.', parsed.uid,
        ...archiveArgs, '--list-only', ...(parsed.all ? ['--all'] : [])];
    case 'filetime':
      return ['save-pull-root.cjs', run, parsed.titleId, '.', parsed.uid,
        ...archiveArgs, '--filetime'];
    case 'backup':
      check(destination !== '-' && path.isAbsolute(destination),
        'A backup needs an absolute export destination.');
      return ['save-pull-root.cjs', run, parsed.titleId, destination, parsed.uid,
        ...archiveArgs, ...(parsed.all ? ['--all'] : [])];
    default:
      throw Error('Unsupported read-only savekit action.');
  }
}

// argv for the one pinned mutation entrypoint. The backup is the export Go
// took and verified immediately before this operation; it is re-verified
// against the request target before anything changes on the console.
function buildMutationArgs(parsed, run, backup, manifestPath) {
  check(typeof backup === 'string' && path.isAbsolute(backup),
    'A mutation needs the absolute verified backup folder.');
  switch (parsed.action) {
    case 'write':
      return ['save-write-stage.cjs', run, parsed.titleId, parsed.uid, backup, manifestPath];
    case 'restore':
      return ['save-write-stage.cjs', run, parsed.titleId, parsed.uid, backup, manifestPath,
        '--space', String(parsed.archive.space), '--type', String(parsed.archive.type),
        '--restore', '--confirm-restore'];
    case 'delete':
      return ['save-delete-root.cjs', run, parsed.titleId, parsed.uid,
        '/' + parsed.fileName, backup];
    default:
      throw Error('Unsupported savekit mutation.');
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
    result.rpc?.state === 'listening' && !result.completed && !result.error &&
    (!result.cleanupErrors || Array.isArray(result.cleanupErrors) && result.cleanupErrors.length === 0),
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
        reject(Error('The save operation timed out. Restart the console before any further save operation.')); }
    }, timeoutMs);
    timer.unref();
    child.stdout.setEncoding('utf8');
    child.stdout.on('data', chunk => {
      stdout += chunk;
      if (stdout.length > 1024 * 1024 && !settled) {
        settled = true; stop('SIGTERM');
        reject(Error('The save operation produced too much output.'));
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
      if (code !== 0) reject(Error('The underlying save tool did not finish.'));
      else resolve(stdout);
    });
  });
}

async function shaFile(file) {
  const hash = crypto.createHash('sha256');
  for await (const chunk of fs.createReadStream(file)) hash.update(chunk);
  return hash.digest('hex');
}

function safeRootName(name) {
  return typeof name === 'string' && name !== '' && name !== '.' && name !== '..' &&
    !name.includes('/') && !name.includes('\\') && !/[\x00-\x1f\x7f]/.test(name) &&
    Buffer.byteLength(name, 'utf8') < 0x300;
}

function safeSavePath(value) {
  check(typeof value === 'string' && value.startsWith('/') && !value.includes('\\') &&
    !/[\x00-\x1f\x7f]/.test(value) && value === path.posix.normalize(value) &&
    value.split('/').slice(1).every(part => part && part !== '.' && part !== '..'),
    'The save manifest contains an unsafe path.');
  return value.slice(1);
}

// The identity and integrity rules of a save-pull export, from the pinned
// reference workflow. A backup that fails them is never a mutation base.
function validateArchive(manifest, expected = {}) {
  const {titleId: expectedTitle, uid: expectedUid, selection = {}, trustedStaticId = 0} = expected;
  const id = manifest.titleId ?? manifest.programId;
  check(titlePattern.test(id) && (!expectedTitle || id === expectedTitle),
    'The pulled save belongs to a different program ID.');
  const selected = manifest.selection ?? {uid: manifest.uid, space: manifest.space,
    type: manifest.type, rank: manifest.rank, index: manifest.index,
    staticId: trustedStaticId};
  check(selected && typeof selected === 'object' && !Array.isArray(selected) &&
    Number.isInteger(selected.space) && Number.isInteger(selected.type) &&
    Number.isInteger(selected.rank) && Number.isInteger(selected.index) &&
    Number.isSafeInteger(selected.staticId) && uidPattern.test(selected.uid),
    'The pulled save has incomplete archive identity.');
  if (Object.hasOwn(manifest, 'programId') && Object.hasOwn(manifest, 'titleId'))
    check(manifest.programId === manifest.titleId,
      'The pulled save has conflicting program IDs.');
  for (const key of ['uid', 'space', 'type', 'rank', 'index'])
    if (Object.hasOwn(manifest, key))
      check(manifest[key] === selected[key],
        'The pulled save has conflicting ' + key + ' fields.');
  for (const [key, number] of Object.entries(selection))
    check(selected[key] === number, 'The pulled save differs from the selected ' + key + '.');
  if (expectedUid) check(selected.uid === expectedUid ||
    selected.uid === '0'.repeat(32), 'The pulled save belongs to another account.');
  const archiveLabel = `space=${spaces[selected.space] ?? selected.space} ` +
    `type=${types[selected.type] ?? selected.type} rank=${selected.rank} ` +
    `index=${selected.index} uid=${selected.uid.slice(0, 16)}`;
  check(manifest.archive === archiveLabel, 'The archive identity differs from its manifest.');
  return {titleId: id, selection: selected};
}

async function verifyPull(directory, expected = {}) {
  const manifest = JSON.parse(await fsp.readFile(path.join(directory, 'manifest.json'), 'utf8'));
  const identity = validateArchive(manifest, expected);
  check(manifest.partial !== true && (!manifest.failed || manifest.failed.length === 0) &&
    Array.isArray(manifest.entries) && manifest.entries.length === manifest.files &&
    manifest.files >= 0 && manifest.files <= 65536 && typeof manifest.archive === 'string',
    'The pulled save has an incomplete manifest.');
  const canonical = await fsp.realpath(directory);
  let bytesTotal = 0;
  const seen = new Set();
  for (const entry of manifest.entries) {
    const relative = safeSavePath(entry.path);
    check(!seen.has(relative), 'The save manifest repeats a file: ' + relative);
    seen.add(relative);
    const file = path.join(directory, relative);
    const info = await fsp.lstat(file);
    const resolved = await fsp.realpath(file);
    check(info.isFile() && !info.isSymbolicLink() && resolved.startsWith(canonical + path.sep) &&
      info.size === entry.size && Number.isSafeInteger(entry.size) && entry.size >= 0,
      'The pulled save contains an invalid file: ' + relative);
    check(await shaFile(file) === entry.sha256,
      'The pulled save failed SHA-256 verification: ' + relative);
    bytesTotal += info.size;
  }
  check(bytesTotal === manifest.bytes, 'The pulled save byte count differs from its manifest.');
  const actual = [];
  async function walk(folder, prefix = '') {
    for (const item of await fsp.readdir(folder, {withFileTypes: true})) {
      const name = prefix ? prefix + '/' + item.name : item.name;
      if (item.isDirectory()) await walk(path.join(folder, item.name), name);
      else {
        check(item.isFile() && !item.isSymbolicLink(), 'The save export contains a link or special file.');
        if (name !== 'manifest.json') actual.push(name);
      }
    }
  }
  await walk(directory);
  check(actual.length === seen.size && actual.every(name => seen.has(name)),
    'The save export contains files missing from its manifest.');
  return {...manifest, ...identity};
}

async function stageFiles(sourceFolder, stage) {
  const sourceInfo = await fsp.lstat(sourceFolder);
  check(sourceInfo.isDirectory() && !sourceInfo.isSymbolicLink(),
    'Choose a regular folder of save files.');
  const source = await fsp.realpath(sourceFolder);
  const files = [];
  let total = 0;
  async function walk(folder, prefix = '') {
    for (const entry of (await fsp.readdir(folder, {withFileTypes: true}))
      .sort((a, b) => a.name.localeCompare(b.name))) {
      const name = entry.name;
      if (name === '.DS_Store' || name === '.nx_save_meta.bin' || name.startsWith('._') ||
          (!prefix && name === 'manifest.json')) continue;
      check(safeRootName(name), 'The source contains an unsafe name: ' + name);
      const relative = prefix ? prefix + '/' + name : name;
      safeSavePath('/' + relative);
      check(Buffer.byteLength('/' + relative, 'utf8') < 0x301,
        'The source contains a path that is too long: ' + relative);
      const file = path.join(folder, name);
      const info = await fsp.lstat(file);
      check(!info.isSymbolicLink(), 'The source contains a symbolic link: ' + relative);
      if (info.isDirectory()) {
        await walk(file, relative);
        continue;
      }
      check(info.isFile() && info.size <= 64 * 1024 * 1024,
        'The source contains an unsupported file: ' + relative);
      total += info.size;
      check(total <= 256 * 1024 * 1024 && files.length < 2048,
        'The selected save folder is too large for a single write.');
      const target = path.join(stage, relative);
      await fsp.mkdir(path.dirname(target), {recursive: true});
      await fsp.copyFile(file, target, fs.constants.COPYFILE_EXCL);
      files.push({name: relative, path: target, sha256: await shaFile(target)});
    }
  }
  await walk(source);
  check(files.length > 0, 'The selected folder contains no save files.');
  return files;
}

async function stageRestoredFiles(sourceFolder, stage, manifest) {
  check(manifest.entries.length > 0 && manifest.entries.length <= 2048,
    'The selected backup has too many files for one restore.');
  let total = 0;
  const files = [];
  for (const entry of manifest.entries) {
    const relative = safeSavePath(entry.path);
    check(entry.size <= 64 * 1024 * 1024, 'The selected backup has a file larger than 64 MiB.');
    total += entry.size;
    check(total <= 256 * 1024 * 1024, 'The selected backup is too large for one restore.');
    const target = path.join(stage, relative);
    await fsp.mkdir(path.dirname(target), {recursive: true});
    await fsp.copyFile(path.join(sourceFolder, relative), target, fs.constants.COPYFILE_EXCL);
    check(await shaFile(target) === entry.sha256,
      'The selected backup changed while preparing the restore: ' + entry.path);
    files.push({path: entry.path, local: target, sha256: entry.sha256, size: entry.size});
  }
  return files;
}

// checkPreview proves the files about to be written are exactly the ones the
// user confirmed. The digest format matches savekit.Preview in Go: sorted
// "/path\tsize\tsha256\n" lines, or "delete\n/name\n" for a delete. It runs on
// the private staged copies, so a source edited after the preview is refused.
function checkPreview(files, digest, deleteName) {
  const hash = crypto.createHash('sha256');
  if (deleteName !== undefined) hash.update('delete\n/' + deleteName + '\n');
  else for (const file of [...files].sort((a, b) => Buffer.compare(Buffer.from(a.path), Buffer.from(b.path))))
    hash.update(file.path + '\t' + file.size + '\t' + file.sha256 + '\n');
  check(hash.digest('hex') === digest,
    'The affected files changed since its preview. No save was changed; preview it again.');
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

// One gated mutation: re-verify the backup, stage locally, run the single
// pinned entrypoint and require its own post-commit verification line.
async function mutate(parsed, proof, tools, backup, stageParent) {
  const selection = parsed.action === 'restore' ? parsed.archive : archive(undefined);
  const saved = await verifyPull(backup, {titleId: parsed.titleId, uid: parsed.uid,
    selection, trustedStaticId: 0});
  check(saved.selection.uid === parsed.uid,
    'The backup belongs to another account. No save was changed.');
  if (parsed.action === 'delete') {
    checkPreview(null, parsed.previewDigest, parsed.fileName);
    check(saved.entries.some(entry => entry.path === '/' + parsed.fileName),
      'The selected file is not in the live save. No file was deleted.');
    const output = await runTool(path.join(tools, 'save-delete-root.cjs'),
      buildMutationArgs(parsed, proof.workspace, backup).slice(1));
    check(output.includes('verified deleted /' + parsed.fileName + ' after commit'),
      'Save deletion was not fully verified. Keep the backup and restart the console.');
    return {files: 1, replaced: 0};
  }
  const stage = await fsp.mkdtemp(path.join(stageParent, 'stage-'));
  try {
    const present = new Set(saved.entries.map(entry => entry.path));
    let files, manifestPath;
    if (parsed.action === 'write') {
      files = await stageFiles(parsed.sourceFolder, stage);
      checkPreview(await Promise.all(files.map(async file => ({path: '/' + file.name,
        size: (await fsp.lstat(file.path)).size, sha256: file.sha256}))), parsed.previewDigest);
      const replacements = files.filter(file => present.has('/' + file.name));
      check(parsed.allowReplace || replacements.length === 0,
        'The selected folder would replace existing save files. Enable replacement explicitly.');
      manifestPath = path.join(stage, 'manifest.json');
      await fsp.writeFile(manifestPath, JSON.stringify({title: parsed.titleId, backup,
        files: files.map(file => ({path: '/' + file.name, local: file.path,
          ...(present.has('/' + file.name) ? {replace: true} : {})}))}, null, 2) + '\n',
        {mode: 0o600});
      const output = await runTool(path.join(tools, 'save-write-stage.cjs'),
        buildMutationArgs(parsed, proof.workspace, backup, manifestPath).slice(1), 1800000);
      check(output.includes('verified ' + files.length + ' written files after commit'),
        'Save writing was not fully verified. Keep the backup and restart the console.');
      return {files: files.length, replaced: replacements.length};
    }
    const source = await verifyPull(parsed.sourceFolder, {titleId: parsed.titleId,
      uid: parsed.uid, selection, trustedStaticId: 0});
    check(source.selection.uid === parsed.uid,
      'The selected backup belongs to another account. No save was written.');
    files = await stageRestoredFiles(parsed.sourceFolder, stage, source);
    checkPreview(files, parsed.previewDigest);
    manifestPath = path.join(stage, 'restore.json');
    await fsp.writeFile(manifestPath, JSON.stringify({files: files.map(file => ({
      path: file.path, local: file.local, replace: present.has(file.path)}))}, null, 2) + '\n',
      {mode: 0o600});
    const output = await runTool(path.join(tools, 'save-write-stage.cjs'),
      buildMutationArgs(parsed, proof.workspace, backup, manifestPath).slice(1), 1800000);
    check(output.includes('verified ' + files.length + ' written files after commit'),
      'The restore was not fully verified. Keep the current-save backup and restart the console.');
    return {files: files.length, replaced: files.filter(file => present.has(file.path)).length};
  } finally {
    await fsp.rm(stage, {recursive: true, force: true});
  }
}

async function main(args = process.argv.slice(2)) {
  const [stagedRoot, runDirectory, destination = '-'] = args;
  check(args.length === 3, 'usage: savekit-host.cjs <staged-root> <run-directory> <destination|->');
  const record = JSON.parse(await fsp.readFile(path.join(stagedRoot, 'stage.json'), 'utf8'));
  check((record.source === 'developer-staged' && record.redistributable === false) ||
    (record.source === 'bundled' && record.redistributable === true),
    'SaveKit tools require a verified stage of their pinned sources.');
  const tools = path.join(stagedRoot, 'tools', 'savekit', 'vendor', 'ns2-savekit', 'tools');
  for (const name of TOOLS) await owned(path.join(tools, name), 'file');
  const parsed = request(await readRequest());
  const proof = await heldProof(path.resolve(runDirectory));
  await rpcStatus(proof.connection);
  emit('started', {message: WRITE_ACTIONS.has(parsed.action)
    ? 'Save operation started. Keep the console session open.'
    : 'Read-only save operation started. Keep the console session open.'});
  const reportPath = path.join(stagedRoot, 'tools', 'savekit',
    'inventory-' + crypto.randomBytes(6).toString('hex') + '.json');
  try {
    if (WRITE_ACTIONS.has(parsed.action)) {
      check(destination !== '-' && path.isAbsolute(destination),
        'A mutation needs the absolute verified backup folder.');
      const outcome = await mutate(parsed, proof, tools, destination,
        path.join(stagedRoot, 'tools', 'savekit'));
      emit('result', {action: parsed.action, files: outcome.files, replaced: outcome.replaced});
      return;
    }
    const argv = buildArgs(parsed, proof.workspace, destination, reportPath);
    check(TOOLS.includes(argv[0]), 'Only a pinned tool may run.');
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

module.exports = {request, archive, buildArgs, buildMutationArgs, parseUsers, parseTitles,
  parseInventory, verifyPull, heldProof, checkPreview, main, TOOLS};
if (require.main === module) main().catch(error => {
  emit('failed', {message: String(error.message).replace(/[\x00-\x1f\x7f]+/g, ' ').slice(0, 512)});
  process.exitCode = 1;
});
