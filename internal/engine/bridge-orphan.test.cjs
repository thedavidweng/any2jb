"use strict";
const { test } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const os = require("node:os");
const { spawn } = require("node:child_process");
const readline = require("node:readline");

test("killing the owner closes bridge stdin and removes its owned lease/workspace", {
	timeout: 60000,
}, async () => {
	const root = fs.mkdtempSync(path.join(os.tmpdir(), "any2jb-orphan-"));
	const staged = path.join(root, "stage");
	const workspace = path.join(root, "runs", "run-orphan");
	const source = path.join(staged, "profile/engine/lib");
	fs.mkdirSync(source, { recursive: true });
	fs.mkdirSync(workspace, { recursive: true, mode: 0o700 });
	for (const name of ["backups", "savekit-exports", "verified-exports", "dumps"]) {
		fs.mkdirSync(path.join(workspace, name));
		fs.writeFileSync(path.join(workspace, name, "retained.bin"), "retained private data");
	}
	fs.writeFileSync(path.join(root, "session.lock"), "orphan-run", { mode: 0o600 });
	const profile = {
		id: "fixture",
		revision: "pinned",
		sourceSha256: "fixture",
		sourceDirectory: "profile",
		startArgumentCount: 5,
		startPort: 39380,
	};
	fs.writeFileSync(path.join(staged, "profiles.json"), JSON.stringify({ profiles: [profile] }));
	fs.writeFileSync(
		path.join(staged, "stage.json"),
		JSON.stringify({
			source: "developer-staged",
			redistributable: false,
			profileID: profile.id,
			revision: profile.revision,
			sourceSha256: profile.sourceSha256,
		}),
	);
	fs.writeFileSync(
		path.join(source, "connection.cjs"),
		`exports.listen = ({onWaiting}) => new Promise(() => {onWaiting(); setInterval(()=>{},1000);});`,
	);
	for (const name of ["native", "qualify", "bridge"])
		fs.writeFileSync(path.join(source, name + ".cjs"), "");
	const bridge = path.join(__dirname, "../../resources/runtime/bridge.cjs");
	// The child inherits the parent's output pipe. Its closure therefore proves
	// both the killed owner and the bridge released their handles.
	const parent = spawn(process.execPath, [
		"-e",
		`
    const {spawn}=require('node:child_process');
    const [bridge,manifest,workspace]=process.argv.slice(1);
    const child=spawn(process.execPath,[bridge,manifest,'fixture','--launch',workspace,'orphan-run','192.0.2.1'],
      {cwd:workspace,detached:true,stdio:['pipe','inherit','inherit']});
    console.log(JSON.stringify({type:'child',pid:child.pid}));
    process.stdin.resume();
  `,
		bridge,
		path.join(staged, "profiles.json"),
		workspace,
	]);
	let pid;
	const lines = readline.createInterface({ input: parent.stdout });
	const closed = new Promise((resolve, reject) => {
		parent.once("error", reject);
		parent.once("close", resolve);
	});
	let cleanupError;
	try {
		await new Promise((resolve, reject) => {
			lines.on("line", (line) => {
				const e = JSON.parse(line);
				if (e.type === "child") pid = e.pid;
				if (e.type === "ready") resolve();
			});
			parent.once("exit", () => {
				if (!pid) reject(Error("owner exited before starting bridge"));
			});
		});
		parent.kill("SIGKILL");
		await closed;
		assert.equal(fs.existsSync(workspace), false);
		assert.equal(fs.existsSync(path.join(root, "session.lock")), false);
		assert.equal(fs.readFileSync(path.join(root, "session-interrupted"), "utf8"), "orphan-run");
		for (const name of ["backups", "savekit-exports", "verified-exports", "dumps"]) {
			assert.equal(
				fs.readFileSync(path.join(root, "past-runs", "run-orphan", name, "retained.bin"), "utf8"),
				"retained private data",
			);
		}
		// On Linux, do_exit() closes inherited pipes before PID 1 / the subreaper
		// reaps the reparented child's zombie entry from the process table.
		const deadline = Date.now() + 5000;
		while (Date.now() < deadline) {
			try {
				process.kill(pid, 0);
			} catch (error) {
				if (error.code === "ESRCH") break;
				throw error;
			}
			await new Promise((resolve) => setTimeout(resolve, 20));
		}
		assert.throws(() => process.kill(pid, 0), { code: "ESRCH" });
	} finally {
		parent.kill("SIGKILL");
		if (pid) {
			try {
				process.kill(-pid, "SIGKILL");
			} catch (error) {
				if (error.code !== "ESRCH") cleanupError = error;
			}
		}
		fs.rmSync(root, { recursive: true });
	}
	if (cleanupError) throw cleanupError;
});
