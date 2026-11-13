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

test('distribution decisions cannot be changed through the Node reader', () => {
  assert.equal(profileById('kurumuz').distribution.decision, 'may-ship');
  assert.equal(profileById('kurumuz').distribution.bundled, true);
  assert.equal(profileById('izmysh').distribution.decision, 'blocked');
  assert.equal(profileById('izmysh').distribution.bundled, false);
  const research = manifest.components.find(c => c.id === 'browser-research');
  assert.equal(research.distribution.decision, 'excluded');
  assert.equal(research.distribution.bundled, false);
  assert.throws(() => { research.distribution.decision = 'may-ship'; }, TypeError);
  assert.throws(() => { profileById('izmysh').distribution.bundled = true; }, TypeError);
});
