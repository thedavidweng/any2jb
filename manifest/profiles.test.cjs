'use strict';
const assert = require('node:assert/strict');
const {test} = require('node:test');
const {manifest, profileById} = require('./profiles.cjs');

test('Node reads the canonical six profiles and their distinct launch contracts', () => {
  assert.equal(manifest.default, 'kurumuz');
  assert.deepEqual(manifest.profiles.map(p => p.id),
    ['pepsipu', 'kurumuz', 'cellenseres', 'BermTheSequel', 'izmysh', 'astral']);
  const izmysh = profileById('izmysh');
  assert.equal(izmysh.startPort, 39395);
  assert.equal(izmysh.saveName, 'NS2I');
  assert.equal(izmysh.startArgumentCount, 9);
  assert.equal(izmysh.supportsHeldRPC, false);
  assert.equal(profileById('kurumuz').supportsSaveKit, true);
  assert.equal(profileById('astral').supportsModloader, false);
  assert.throws(() => profileById('unknown'), /Unknown host profile/);
});
