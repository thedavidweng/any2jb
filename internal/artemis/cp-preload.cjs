"use strict";
// any2jb Node preload for the pinned Artemis Seedsow filesystem host.
//
// The verified plugin stage is read-only by design: retained.StageTools writes
// files at 0400 and directories at 0500, and the pinned runtime-host.cjs copies
// that tree with fs.cp, which preserves those modes. The copy then cannot take
// the SaveKit overlays or the engine's own reports. Widening only the copy keeps
// the verified plugin bytes, and their SHA-256, untouched.
//
// Only destinations inside ANY2JB_RUNS_ROOT are widened, only directories and
// regular files are touched, and a symlink is never followed.

const fs = require("node:fs/promises");
const path = require("node:path");

const runsRoot = process.env.ANY2JB_RUNS_ROOT;
const copy = fs.cp;

async function widen(target) {
	const info = await fs.lstat(target);
	if (info.isSymbolicLink()) return;
	if (info.isDirectory()) {
		await fs.chmod(target, 0o700);
		for (const entry of await fs.readdir(target, { withFileTypes: true })) {
			if (entry.isDirectory() || entry.isFile()) await widen(path.join(target, entry.name));
		}
	} else if (info.isFile()) {
		await fs.chmod(target, 0o600);
	}
}

fs.cp = async function copyIntoRuns(source, destination, options) {
	const result = await copy.call(this, source, destination, options);
	if (!runsRoot) return result;
	const root = path.resolve(runsRoot);
	const target = path.resolve(String(destination));
	if (target === root || target.startsWith(root + path.sep)) await widen(target);
	return result;
};
