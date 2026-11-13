'use strict';
const { test } = require('node:test');
const assert = require('node:assert/strict');
const { spawnSync } = require('node:child_process');
const { mkdtempSync, writeFileSync, rmSync } = require('node:fs');
const { tmpdir } = require('node:os');
const path = require('node:path');

test('bridge emits selected canonical profile and rejects blocked profiles', () => {
  const root = mkdtempSync(path.join(tmpdir(), 'any2jb-bridge-'));
  const manifest = path.join(root, 'profiles.json');
  const bridge = path.join(__dirname, '../resources/runtime/bridge.cjs');
  try {
    writeFileSync(manifest, JSON.stringify({profiles: [
      {id: 'fixture', revision: 'pinned', distribution: {decision: 'may-ship', bundled: true}},
      {id: 'blocked', distribution: {decision: 'blocked', bundled: false, reason: 'permission unresolved'}},
    ]}));
    const invoke = id => spawnSync(process.execPath, [bridge, manifest, id], {encoding: 'utf8'});
    const result = invoke('fixture');
    assert.equal(result.status, 0);
    assert.equal(JSON.parse(result.stdout).profile.revision, 'pinned');
    for (const id of ['blocked', 'unknown']) {
      const result = invoke(id);
      assert.equal(result.status, 1);
      assert.equal(result.stdout, '');
      assert.equal(JSON.parse(result.stderr).type, 'error');
    }
  } finally {
    rmSync(root, {recursive: true});
  }
});
