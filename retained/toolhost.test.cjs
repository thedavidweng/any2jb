'use strict';
const assert = require('node:assert/strict');
const {test} = require('node:test');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const {heldProof, readOnlySD, TOOLS} = require('../resources/darwin-arm64/runtime/toolhost.cjs');

function heldWorkspace(changes = {}) {
  const support = fs.mkdtempSync(path.join(os.tmpdir(), 'any2jb-toolhost-'));
  const workspace = fs.mkdtempSync(path.join(support, 'run-'));
  const session = path.basename(workspace);
  const result = {verified: true, fs: {verified: true, restored: true,
    retainedEndpoint: {ready: true, session}, cleanup: {deferred: true, donor: {cleanedUp: true}}},
    rpc: {mode: 'held', state: 'listening'}, ...changes.result};
  fs.writeFileSync(path.join(workspace, 'result.json'), JSON.stringify(result), {mode: changes.mode ?? 0o600});
  fs.writeFileSync(path.join(workspace, 'connection.json'),
    JSON.stringify({port: 39999, addresses: {session: changes.session ?? session, target: 'switch2'}}), {mode: 0o600});
  return workspace;
}

test('only a complete private held proof is accepted', async () => {
  const workspace = heldWorkspace();
  const proof = await heldProof(workspace);
  assert.equal(proof.session, path.basename(workspace));
  assert.equal(proof.connection.port, 39999);
});

test('a finished, public or foreign session is refused', async () => {
  await assert.rejects(heldProof(heldWorkspace({result: {completed: '2026-01-01T00:00:00Z'}})),
    /verified session with a live retained RPC lease/);
  await assert.rejects(heldProof(heldWorkspace({mode: 0o644})), /owned private file/);
  await assert.rejects(heldProof(heldWorkspace({session: 'another-run'})),
    /retained endpoint does not belong to this session/);
  await assert.rejects(heldProof(fs.mkdtempSync(path.join(os.tmpdir(), 'any2jb-empty-'))), /ENOENT/);
});

test('the SD export exposes only read operations under /sd', () => {
  const calls = [];
  const storage = {list: value => (calls.push(value), []), stat: value => (calls.push(value), {type: 'dir'}),
    openRead: value => (calls.push(value), 1), read: () => Buffer.alloc(0), close: () => {}};
  const readOnly = readOnlySD(storage);
  assert.deepEqual(readOnly.list('/'), [{name: 'sd', type: 'dir', size: 0}]);
  readOnly.list('/sd/games');
  assert.deepEqual(calls, ['/sd/games']);
  for (const method of ['list', 'stat', 'openRead']) {
    assert.throws(() => readOnly[method]('/save'), /Only the SD filesystem is exported/);
    assert.throws(() => readOnly[method]('/sdcard'), /Only the SD filesystem is exported/);
  }
  for (const write of ['write', 'remove', 'mkdir', 'rename', 'openWrite', 'truncate']) {
    assert.equal(readOnly[write], undefined, write + ' must not exist in a read-only export');
  }
});

test('the catalog of tools has no write path', () => {
  assert.deepEqual(TOOLS, ['rpc-status', 'system-code-dump', 'ouncecake-webdav', 'ouncecake-ftp', 'sd-browser']);
});
