'use strict';
const assert = require('node:assert/strict');
const {test} = require('node:test');
const {request, buildArgs, buildMutationArgs, TOOLS} = require('./savekit-host.cjs');

const TITLE = '010012900d38a000';
const UID = '1'.repeat(32);
const DIGEST = 'a'.repeat(64);

test('read-only operations validate without gate fields', () => {
  for (const action of ['accounts', 'titles', 'inventory']) {
    assert.deepEqual(request({action}), {action});
  }
  for (const action of ['inspect', 'backup', 'filetime']) {
    assert.equal(request({action, titleId: TITLE, uid: UID}).action, action);
  }
  assert.throws(() => request({action: 'inspect', titleId: TITLE, uid: UID, sourceFolder: '/tmp/x'}),
    /Unknown field/);
  assert.throws(() => request({action: 'backup', titleId: TITLE, uid: UID, confirm: true}),
    /Unknown field/);
  assert.throws(() => request({action: 'filetime', titleId: TITLE, uid: UID, all: true}),
    /one archive/);
  assert.throws(() => request({action: 'accounts', titleId: TITLE}), /Unknown field/);
  // The live comparison is orchestrated in Go over the read-only backup; the
  // host has no verify action.
  assert.throws(() => request({action: 'verify', titleId: TITLE, uid: UID,
    expectedFolder: '/tmp/x'}), /Unsupported/);
});

test('mutations validate only with confirmation', () => {
  const write = request({action: 'write', titleId: TITLE, uid: UID,
    sourceFolder: '/tmp/saves', allowReplace: true, confirm: true});
  assert.deepEqual(write, {action: 'write', titleId: TITLE, uid: UID,
    sourceFolder: '/tmp/saves', allowReplace: true});
  assert.throws(() => request({action: 'write', titleId: TITLE, uid: UID,
    sourceFolder: '/tmp/saves'}), /[Cc]onfirm/);
  assert.throws(() => request({action: 'write', titleId: TITLE, uid: UID,
    sourceFolder: 'relative', confirm: true}), /absolute/);
  assert.throws(() => request({action: 'write', titleId: TITLE, uid: UID,
    sourceFolder: '/tmp/saves', confirm: true, archive: {space: 1}}), /Unknown field/);
  assert.throws(() => request({action: 'write', titleId: TITLE, uid: UID,
    sourceFolder: '/tmp/saves', confirm: true, previewDigest: DIGEST}), /Unknown field/);

  const restore = request({action: 'restore', titleId: TITLE, uid: UID,
    sourceFolder: '/tmp/backup', confirm: true});
  assert.equal(restore.action, 'restore');
  assert.deepEqual(restore.archive, {space: 1, type: 1, rank: 0, index: 0, staticId: 0});
  assert.throws(() => request({action: 'restore', titleId: TITLE, uid: UID,
    sourceFolder: '/tmp/backup', confirm: true,
    archive: {space: 1, type: 2}}), /Account or Device/);
  assert.throws(() => request({action: 'restore', titleId: TITLE, uid: UID,
    sourceFolder: '/tmp/backup', confirm: true,
    archive: {rank: 1}}), /Account or Device/);
  assert.throws(() => request({action: 'restore', titleId: TITLE, uid: UID,
    sourceFolder: '/tmp/backup'}), /[Cc]onfirm/);

  const del = request({action: 'delete', titleId: TITLE, uid: UID,
    fileName: 'savegame.bin', confirm: true});
  assert.equal(del.fileName, 'savegame.bin');
  assert.throws(() => request({action: 'delete', titleId: TITLE, uid: UID,
    fileName: 'savegame.bin'}), /[Cc]onfirm/);
  for (const name of ['', '.', '..', 'a/b', String.raw`a\b`, 'bad\nname']) {
    assert.throws(() => request({action: 'delete', titleId: TITLE, uid: UID,
      fileName: name, confirm: true}), /save root/);
  }
  assert.throws(() => request({action: 'unknown', titleId: TITLE, uid: UID}),
    /Unsupported/);
});

test('the staged tool set holds the pinned read and write entrypoints', () => {
  assert.deepEqual([...TOOLS].sort(), ['list-apps.cjs', 'save-delete-root.cjs',
    'save-inventory.cjs', 'save-pull-root.cjs', 'save-write-stage.cjs']);
});

test('read-only argv reaches only the pinned read tools', () => {
  const run = '/runs/run-1';
  assert.deepEqual(buildArgs(request({action: 'accounts'}), run, '-', '/tmp/report.json'),
    ['save-pull-root.cjs', run, '0000000000000000', '.', '--list-uids']);
  assert.deepEqual(buildArgs(request({action: 'titles'}), run, '-', '/tmp/report.json'),
    ['list-apps.cjs', run, '--no-names']);
  assert.deepEqual(buildArgs(request({action: 'inventory'}), run, '-', '/tmp/report.json'),
    ['save-inventory.cjs', run, '--json', '/tmp/report.json']);
  assert.deepEqual(buildArgs(request({action: 'inspect', titleId: TITLE, uid: UID}), run, '-', '/tmp/report.json'),
    ['save-pull-root.cjs', run, TITLE, '.', UID,
      '--space', '1', '--type', '1', '--rank', '0', '--index', '0', '--static-id', '0', '--list-only']);
  assert.deepEqual(buildArgs(request({action: 'backup', titleId: TITLE, uid: UID}), run, '/exports/a', '/tmp/report.json'),
    ['save-pull-root.cjs', run, TITLE, '/exports/a', UID,
      '--space', '1', '--type', '1', '--rank', '0', '--index', '0', '--static-id', '0']);
});

test('mutation argv carries the re-verified backup', () => {
  const run = '/runs/run-1';
  const write = request({action: 'write', titleId: TITLE, uid: UID,
    sourceFolder: '/tmp/saves', confirm: true});
  assert.deepEqual(buildMutationArgs(write, run, '/exports/backup', '/tmp/stage/manifest.json'),
    ['save-write-stage.cjs', run, TITLE, UID, '/exports/backup', '/tmp/stage/manifest.json']);
  const restore = request({action: 'restore', titleId: TITLE, uid: UID,
    sourceFolder: '/tmp/backup', confirm: true, archive: {space: 4, type: 3}});
  assert.deepEqual(buildMutationArgs(restore, run, '/exports/backup', '/tmp/stage/restore.json'),
    ['save-write-stage.cjs', run, TITLE, UID, '/exports/backup', '/tmp/stage/restore.json',
      '--space', '4', '--type', '3', '--restore', '--confirm-restore']);
  const del = request({action: 'delete', titleId: TITLE, uid: UID,
    fileName: 'savegame.bin', confirm: true});
  assert.deepEqual(buildMutationArgs(del, run, '/exports/backup'),
    ['save-delete-root.cjs', run, TITLE, UID, '/savegame.bin', '/exports/backup']);
  assert.throws(() => buildMutationArgs(request({action: 'write', titleId: TITLE, uid: UID,
    sourceFolder: '/tmp/saves', confirm: true}), run, 'relative'),
    /absolute/);
});
