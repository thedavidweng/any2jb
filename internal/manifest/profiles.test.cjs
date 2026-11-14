"use strict";
const assert = require("node:assert/strict");
const { test } = require("node:test");
const { manifest, profileById } = require("./profiles.cjs");

test("Node reads the canonical five profiles and their distinct launch contracts", () => {
	assert.equal(manifest.default, "kurumuz");
	assert.deepEqual(
		manifest.profiles.map((p) => p.id),
		["pepsipu", "kurumuz", "cellenseres", "BermTheSequel", "astral"],
	);
	const pepsipu = profileById("pepsipu");
	assert.equal(pepsipu.startPort, 39380);
	assert.equal(pepsipu.saveName, "NS2");
	assert.equal(pepsipu.startArgumentCount, 9);
	assert.equal(pepsipu.supportsHeldRPC, false);
	assert.equal(profileById("kurumuz").supportsSaveKit, true);
	assert.equal(profileById("astral").supportsModloader, false);
	assert.throws(() => profileById("unknown"), /Unknown host profile/);
});

test("distribution decisions cannot be changed through the Node reader", () => {
	assert.equal(profileById("kurumuz").distribution.decision, "may-ship");
	assert.equal(profileById("kurumuz").distribution.bundled, true);
	const ouncecake = manifest.components.find((c) => c.id === "ouncecake");
	assert.equal(ouncecake.distribution.decision, "may-ship");
	assert.equal(ouncecake.distribution.bundled, false);
	const research = manifest.components.find((c) => c.id === "browser-research");
	assert.equal(research.distribution.decision, "excluded");
	assert.equal(research.distribution.bundled, false);
	assert.throws(() => {
		research.distribution.decision = "may-ship";
	}, TypeError);
	assert.throws(() => {
		ouncecake.distribution.bundled = true;
	}, TypeError);
});
