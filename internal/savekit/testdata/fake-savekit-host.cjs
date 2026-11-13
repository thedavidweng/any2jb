'use strict';
// Structured fixture for the Go SaveKit client and manager. No console, no
// RPC: it answers the protocol from argv and models one live save as a JSON
// state file inside the run directory, so exports, writes, restores, deletes
// and read-back verification compose like the real flow. With
// ANY2JB_SAVEKIT_FIXTURE_LOG set it appends one JSON line per action so tests
// can count and order operations.
const fs = require('node:fs');
const path = require('node:path');
const crypto = require('node:crypto');

const [runDir, destination, scenario = 'ok'] = process.argv.slice(2);
let sequence = 0;
const emit = (type, fields = {}) =>
  process.stdout.write(JSON.stringify({type, sequence: ++sequence, ...fields}) + '\n');

const UID1 = '11111111111111111111111111111111';
const UID2 = '22222222222222222222222222222222';
const TITLE = '010012900d38a000';
const OTHER = '0100000000001000';

const stateFile = path.join(runDir, 'fixture-save.json');
function liveSave() {
  try {
    return JSON.parse(fs.readFileSync(stateFile, 'utf8'));
  } catch {
    return {'/savegame.bin': 'save data\n'};
  }
}
function saveLive(files) {
  fs.writeFileSync(stateFile, JSON.stringify(files), {mode: 0o600});
}
function sha(body) {
  return crypto.createHash('sha256').update(body).digest('hex');
}
function log(request) {
  if (process.env.ANY2JB_SAVEKIT_FIXTURE_LOG) {
    fs.appendFileSync(process.env.ANY2JB_SAVEKIT_FIXTURE_LOG,
      JSON.stringify({action: request.action, titleId: request.titleId, uid: request.uid,
        fileName: request.fileName, sourceFolder: request.sourceFolder}) + '\n');
  }
}

function writeExport(dir) {
  const files = liveSave();
  const entries = [];
  let bytes = 0;
  fs.mkdirSync(dir, {recursive: true, mode: 0o700});
  for (const [name, body] of Object.entries(files)) {
    const relative = name.slice(1);
    fs.mkdirSync(path.dirname(path.join(dir, relative)), {recursive: true});
    fs.writeFileSync(path.join(dir, relative), body, {mode: 0o600});
    entries.push({path: name, size: Buffer.byteLength(body), sha256: sha(body)});
    bytes += Buffer.byteLength(body);
  }
  const manifest = {
    archive: 'space=User type=Account rank=0 index=0 uid=1111111111111111',
    programId: TITLE, uid: UID1, space: 1, type: 1, rank: 0, index: 0,
    files: entries.length, bytes, partial: false, failed: [], entries,
  };
  fs.writeFileSync(path.join(dir, 'manifest.json'), JSON.stringify(manifest, null, 2) + '\n', {mode: 0o600});
}

// Emulates the host-side re-verification: the backup must be a complete
// verified export of this fixture's identity before any mutation proceeds.
function verifiedBackup(dir, uid) {
  const manifest = JSON.parse(fs.readFileSync(path.join(dir, 'manifest.json'), 'utf8'));
  if (manifest.partial || manifest.uid !== uid || manifest.programId !== TITLE) {
    throw Error('the backup does not prove the selected title and account');
  }
  for (const entry of manifest.entries) {
    const body = fs.readFileSync(path.join(dir, entry.path.slice(1)), 'utf8');
    if (sha(body) !== entry.sha256) throw Error('the backup failed verification');
  }
  return manifest;
}

function sourceFiles(folder) {
  const files = {};
  (function walk(dir, prefix) {
    for (const entry of fs.readdirSync(dir, {withFileTypes: true}).sort((a, b) => a.name.localeCompare(b.name))) {
      if (entry.name === 'manifest.json' && !prefix) continue;
      const relative = prefix ? prefix + '/' + entry.name : entry.name;
      if (entry.isDirectory()) walk(path.join(dir, entry.name), relative);
      else files['/' + relative] = fs.readFileSync(path.join(dir, entry.name), 'utf8');
    }
  })(folder, '');
  return files;
}

async function main() {
  let input = '';
  for await (const chunk of process.stdin) input += chunk;
  const request = JSON.parse(input.trim());
  if (scenario === 'protocol-hang') {
    sequence = 5;
    emit('result', {action: request.action, users: [UID1]});
    setInterval(() => emit('progress'), 1);
    return;
  }
  if (scenario === 'slow') await new Promise(resolve => setTimeout(resolve, 400));
  if (scenario === 'bad-sequence') {
    sequence = 5;
    emit('result', {action: request.action, users: [UID1]});
    return;
  }
  emit('started', {message: 'fixture started in ' + runDir});
  if (scenario === 'write-action') {
    emit('result', {action: 'write', output: 'wrote'});
    return;
  }
  log(request);
  switch (request.action) {
    case 'accounts':
      emit('result', {action: 'accounts', users: [UID1, UID2]});
      return;
    case 'titles':
      emit('result', {action: 'titles', titles: [TITLE, OTHER]});
      return;
    case 'inventory':
      emit('result', {action: 'inventory', inventory: [
        {id: '1', titleId: TITLE, uid: UID1, space: 1, type: 1, rank: 0, index: 0, size: '10'},
        {id: '2', titleId: OTHER, uid: UID2, space: 1, type: 3, rank: 0, index: 0, size: '20'},
      ]});
      return;
    case 'inspect':
      emit('result', {action: 'inspect', output: '       10  /savegame.bin\nlisted 1 files, 10 bytes'});
      return;
    case 'filetime':
      emit('result', {action: 'filetime', output: '12 out=16 path=/ -> 0x0  0102'});
      return;
    case 'backup': {
      if (scenario === 'backup-fails') {
        emit('failed', {message: 'the save pull did not finish'});
        return;
      }
      const target = scenario === 'foreign-path' ? '/tmp/any2jb-foreign-export' : destination;
      writeExport(destination);
      emit('result', {action: 'backup', path: target});
      return;
    }
    case 'write': {
      const backup = verifiedBackup(destination, request.uid);
      const incoming = sourceFiles(request.sourceFolder);
      const present = new Set(backup.entries.map(entry => entry.path));
      const replaced = Object.keys(incoming).filter(name => present.has(name));
      if (replaced.length > 0 && request.allowReplace !== true) {
        emit('failed', {message: 'the write would replace existing save files'});
        return;
      }
      if (scenario === 'write-unverified') {
        emit('failed', {message: 'save writing was not fully verified'});
        return;
      }
      const live = liveSave();
      for (const [name, body] of Object.entries(incoming)) {
        live[name] = scenario === 'verify-mismatch' ? body + 'tampered\n' : body;
      }
      saveLive(live);
      emit('result', {action: 'write', files: Object.keys(incoming).length,
        replaced: replaced.length});
      return;
    }
    case 'restore': {
      verifiedBackup(destination, request.uid);
      const source = JSON.parse(fs.readFileSync(path.join(request.sourceFolder, 'manifest.json'), 'utf8'));
      const live = liveSave();
      let replaced = 0;
      for (const entry of source.entries) {
        const body = fs.readFileSync(path.join(request.sourceFolder, entry.path.slice(1)), 'utf8');
        if (Object.hasOwn(live, entry.path)) replaced++;
        live[entry.path] = scenario === 'verify-mismatch' ? body + 'tampered\n' : body;
      }
      if (scenario === 'write-unverified') {
        emit('failed', {message: 'the restore was not fully verified'});
        return;
      }
      saveLive(live);
      emit('result', {action: 'restore', files: source.entries.length, replaced});
      return;
    }
    case 'delete': {
      verifiedBackup(destination, request.uid);
      const live = liveSave();
      const target = '/' + request.fileName;
      if (!Object.hasOwn(live, target)) {
        emit('failed', {message: 'the selected file is not in the live save'});
        return;
      }
      if (scenario === 'write-unverified') {
        emit('failed', {message: 'save deletion was not fully verified'});
        return;
      }
      if (scenario !== 'verify-mismatch') delete live[target];
      saveLive(live);
      emit('result', {action: 'delete', files: 1, replaced: 0});
      return;
    }
    default:
      emit('failed', {message: 'unsupported fixture action'});
  }
}

main().catch(error => { emit('failed', {message: error.message}); process.exitCode = 1; });
