"use strict";
const { test } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const os = require("node:os");
const { spawn } = require("node:child_process");

test("bridge executes pipeline copies only inside the owned workspace", async () => {
	// Fake upstream APIs, not a binary console/exploit emulator.
	const root = fs.mkdtempSync(path.join(os.tmpdir(), "any2jb-launch-"));
	const staged = path.join(root, "staged");
	const workspace = path.join(root, "runs", "run-fixture");
	const source = path.join(staged, "profile");
	const write = (name, body) => {
		fs.mkdirSync(path.dirname(name), { recursive: true, mode: 0o700 });
		fs.writeFileSync(name, body);
	};
	const profile = {
		id: "fixture",
		sourceDirectory: "profile",
		revision: "pinned",
		sourceSha256: "fixture",
		startPort: 39380,
		startArgumentCount: 5,
		supportsHeldRPC: false,
	};
	write(path.join(staged, "profiles.json"), JSON.stringify({ profiles: [profile] }));
	write(
		path.join(staged, "stage.json"),
		JSON.stringify({
			source: "developer-staged",
			redistributable: false,
			profileID: "fixture",
			revision: "pinned",
			sourceSha256: "fixture",
		}),
	);
	write(
		path.join(source, "engine/lib/connection.cjs"),
		`exports.listen = async ({onWaiting}) => {onWaiting(); return {connection:{}, addresses:{}};};`,
	);
	write(
		path.join(source, "engine/lib/native.cjs"),
		`exports.NativeClient = class {async stop() {}};`,
	);
	write(
		path.join(source, "engine/lib/qualify.cjs"),
		`exports.qualify = async () => ({activity:{}});`,
	);
	write(
		path.join(source, "engine/lib/bridge.cjs"),
		`exports.serve = async () => ({port:1234, keepAwake(){}, async close(){}});`,
	);
	write(path.join(source, "engine/lib/atomic-write.cjs"), `exports.marker = 'copied-lib';`);
	write(path.join(source, "engine/shared/fixture"), "fixture");
	write(
		path.join(source, "engine/switch2/memory/run-control.cjs"),
		`module.exports = require('../../lib/atomic-write.cjs');`,
	);
	write(
		path.join(source, "engine/switch2/pipeline.cjs"),
		`
    const fs=require('node:fs'), path=require('node:path');
    if (process.cwd() !== path.dirname(__dirname)) throw Error('not in workspace');
    if (require('./memory/run-control.cjs').marker !== 'copied-lib') throw Error('missing lib copy');
    fs.writeFileSync(path.join(__dirname,'../shared/fixture'),'working copy');
    for(let stage=1;stage<=4;stage++) console.log(stage+'/4 fixture');
    fs.writeFileSync(path.join(__dirname,'fs-execution-current.json'),
      JSON.stringify({verified:true,restored:true,cleanup:{cleanedUp:true}}));
  `,
	);
	const seal = (directory) => {
		for (const entry of fs.readdirSync(directory, { withFileTypes: true })) {
			const name = path.join(directory, entry.name);
			if (entry.isDirectory()) seal(name);
			else fs.chmodSync(name, 0o400);
		}
		fs.chmodSync(directory, 0o500);
	};
	seal(staged);
	fs.mkdirSync(workspace, { recursive: true, mode: 0o700 });
	try {
		const bridge = path.join(__dirname, "../../resources/runtime/bridge.cjs");
		const child = spawn(
			process.execPath,
			[
				bridge,
				path.join(staged, "profiles.json"),
				"fixture",
				"--launch",
				workspace,
				"fixture-run",
				"192.0.2.1",
			],
			{ cwd: workspace, detached: true },
		);
		let stdout = "",
			stderr = "";
		child.stdout.on("data", (data) => {
			stdout += data;
		});
		child.stderr.on("data", (data) => {
			stderr += data;
		});
		const code = await new Promise((resolve, reject) => {
			child.once("error", reject);
			child.once("close", resolve);
		});
		assert.equal(code, 0, stderr);
		const events = stdout.trim().split("\n").map(JSON.parse);
		assert.deepEqual(
			events.map((e) => e.type),
			["ready", "connected", "stage", "stage", "stage", "stage", "result"],
		);
		assert.equal(
			JSON.parse(fs.readFileSync(path.join(workspace, "result.json"))).fs.cleanup.cleanedUp,
			true,
		);
		assert.equal(fs.existsSync(path.join(staged, "runs")), false);
		assert.equal(
			fs.existsSync(path.join(source, "engine/switch2/fs-execution-current.json")),
			false,
		);
		assert.equal(fs.readFileSync(path.join(source, "engine/shared/fixture"), "utf8"), "fixture");
	} finally {
		const unseal = (directory) => {
			fs.chmodSync(directory, 0o700);
			for (const entry of fs.readdirSync(directory, { withFileTypes: true }))
				if (entry.isDirectory()) unseal(path.join(directory, entry.name));
		};
		unseal(root);
		fs.rmSync(root, { recursive: true });
	}
});
