"use strict";
// Structured-process fixture only. No console protocol or exploit is emulated.
const fs = require("node:fs");
const path = require("node:path");
const http = require("node:http");
const readline = require("node:readline");
const [scenario, runID, workspace] = process.argv.slice(2);
let sequence = 0;
const emit = (type, fields = {}) =>
	process.stdout.write(JSON.stringify({ type, sequence: ++sequence, runID, ...fields }) + "\n");
const save = (name, data) =>
	fs.writeFileSync(path.join(workspace, name), JSON.stringify(data), { mode: 0o600 });
async function main() {
	if (scenario === "waiting") {
		process.stdin.resume();
		return;
	}
	if (scenario === "bind-failure") {
		emit("failed");
		process.exitCode = 1;
		return;
	}
	if (scenario === "grandchild") {
		const child = require("node:child_process").spawn(
			process.execPath,
			[
				"-e",
				`
      process.on('SIGTERM',()=>{});
      setInterval(()=>{},1000);
      process.send('ready');
    `,
			],
			{ stdio: ["ignore", "ignore", "ignore", "ipc"] },
		);
		await new Promise((resolve) => child.once("message", resolve));
		save("grandchild.json", { pid: child.pid });
		emit("ready", { command: "V=START(39380,192,0,2,1)" });
		process.stdin.resume();
		return;
	}
	emit("ready", { command: "V=START(39380,192,0,2,1)" });
	if (scenario === "ready-wait") {
		process.stdin.resume();
		return;
	}
	emit("connected");
	if (scenario === "crash" || scenario === "disconnect") {
		process.exitCode = 1;
		return;
	}
	if (scenario === "protocol-error") {
		emit("unknown");
		return;
	}
	if (scenario === "truncated") {
		process.stdout.write('{"type":"stage"');
		return;
	}
	if (scenario === "out-of-order") {
		emit("stage", { stage: 4 });
		return;
	}
	for (let stage = 1; stage <= 4; stage++) emit("stage", { stage });
	const result = {
		verified: true,
		fs: { verified: true, restored: true, cleanup: { cleanedUp: true } },
	};
	if (scenario === "partial") result.fs.restored = false;
	if (scenario === "cleanup-error")
		result.cleanupErrors = [{ name: "fixture cleanup", error: "fixture" }];
	if (scenario.startsWith("held-")) {
		const server = http.createServer((req, res) => {
			req.resume();
			res.setHeader("Content-Type", "application/json");
			res.end(JSON.stringify({ ready: scenario !== "held-dead-rpc" }));
		});
		await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
		const session = path.basename(workspace);
		save("connection.json", {
			port: server.address().port,
			addresses: { session, target: "switch2" },
		});
		result.fs.retainedEndpoint = { ready: true, session };
		result.fs.cleanup = { deferred: true, donor: { cleanedUp: true } };
		result.rpc = { mode: "held", state: "listening" };
		if (scenario === "held-wrong-session") result.fs.retainedEndpoint.session = "another-run";
		save("result.json", result);
		if (scenario === "held-public-artifact")
			fs.chmodSync(path.join(workspace, "result.json"), 0o644);
		emit("held");
		const lines = readline.createInterface({ input: process.stdin });
		await new Promise((resolve) =>
			lines.once("line", (line) => {
				if (JSON.parse(line).type !== "finish") throw Error("unexpected fixture command");
				resolve();
			}),
		);
		lines.close();
		process.stdin.pause();
		server.close();
		result.fs.cleanup = { cleanedUp: true };
		result.rpc.state = "closed";
	}
	result.completed = "fixture";
	save("result.json", result);
	emit("result");
	if (scenario === "dirty-exit") process.exitCode = 1;
}
main().catch(() => {
	emit("failed");
	process.exitCode = 1;
});
