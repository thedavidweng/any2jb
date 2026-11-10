'use strict';
// Structured fixture for the Go SaveKit client. No console, no RPC: it answers
// the read-only protocol from argv and writes a valid export for backups.
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

function writeExport(dir) {
  fs.mkdirSync(dir, {recursive: true, mode: 0o700});
  const body = Buffer.from('save data\n');
  fs.writeFileSync(path.join(dir, 'savegame.bin'), body, {mode: 0o600});
  const manifest = {
    archive: 'space=User type=Account rank=0 index=0 uid=1111111111111111',
    programId: TITLE, uid: UID1, space: 1, type: 1, rank: 0, index: 0,
    files: 1, bytes: body.length, partial: false, failed: [],
    entries: [{path: '/savegame.bin', size: body.length,
      sha256: crypto.createHash('sha256').update(body).digest('hex')}],
  };
  fs.writeFileSync(path.join(dir, 'manifest.json'), JSON.stringify(manifest, null, 2) + '\n', {mode: 0o600});
}

async function main() {
  let input = '';
  for await (const chunk of process.stdin) input += chunk;
  const request = JSON.parse(input.trim());
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
      const target = scenario === 'foreign-path' ? '/tmp/any2jb-foreign-export' : destination;
      writeExport(destination);
      emit('result', {action: 'backup', path: target});
      return;
    }
    default:
      emit('failed', {message: 'unsupported fixture action'});
  }
}

main().catch(error => { emit('failed', {message: error.message}); process.exitCode = 1; });
