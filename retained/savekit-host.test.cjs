'use strict';
const assert = require('node:assert/strict');
const {test} = require('node:test');
const {request, buildArgs, TOOLS} = require('./savekit-host.cjs');

const TITLE = '010012900d38a000';
const UID = '1'.repeat(32);

test('only read-only operations validate', () => {
  for (const action of ['accounts', 'titles', 'inventory']) {
    assert.deepEqual(request({action}), {action});
  }
  for (const action of ['inspect', 'backup', 'filetime']) {
    assert.equal(request({action, titleId: TITLE, uid: UID}).action, action);
  }
  for (const action of ['write', 'restore', 'delete', 'verify', 'unknown']) {
    assert.throws(() => request({action, titleId: TITLE, uid: UID}), /Unsupported|unsupported/);
  }
  assert.throws(() => request({action: 'inspect', titleId: TITLE, uid: UID, sourceFolder: '/tmp/x'}),
    /Unknown field/);
  assert.throws(() => request({action: 'inspect', titleId: TITLE, uid: UID, all: true, extra: 1}),
    /Unknown field/);
  assert.throws(() => request({action: 'filetime', titleId: TITLE, uid: UID, all: true}),
    /one archive/);
  assert.throws(() => request({action: 'inspect', titleId: 'zz' + TITLE.slice(2), uid: UID}),
    /16-digit/);
  assert.throws(() => request({action: 'accounts', titleId: TITLE}), /Unknown field/);
});

test('the staged tool set contains no write entrypoint', () => {
  for (const tool of TOOLS) assert.match(tool, /\.cjs$/);
  assert.ok(!TOOLS.some(name => /write|delete/.test(name)), TOOLS.join(','));
});

test('read-only argv reaches only the pinned tools', () => {
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
  assert.deepEqual(buildArgs(request({action: 'inspect', titleId: TITLE, uid: UID, archive: {space: 4}, all: true}),
    run, '-', '/tmp/report.json'),
    ['save-pull-root.cjs', run, TITLE, '.', UID, '--space', '4', '--list-only', '--all']);
  assert.deepEqual(buildArgs(request({action: 'filetime', titleId: TITLE, uid: UID}), run, '-', '/tmp/report.json'),
    ['save-pull-root.cjs', run, TITLE, '.', UID,
      '--space', '1', '--type', '1', '--rank', '0', '--index', '0', '--static-id', '0', '--filetime']);
  assert.deepEqual(buildArgs(request({action: 'backup', titleId: TITLE, uid: UID}), run, '/exports/a', '/tmp/report.json'),
    ['save-pull-root.cjs', run, TITLE, '/exports/a', UID,
      '--space', '1', '--type', '1', '--rank', '0', '--index', '0', '--static-id', '0']);
});
