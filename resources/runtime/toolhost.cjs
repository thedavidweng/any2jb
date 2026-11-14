"use strict";

// Go owns policy, integrity, staging, process groups and verification. This
// adapter joins the pinned read-only upstream tool APIs to JSON lines, and
// supervises the pinned homebrew launchers against their staged sources. The
// read-only tools never write console data. The one write path is the
// modloader, run as a session's final action from a layout-checked private
// stage of the user's mod folder; no save or console filesystem write exists
// here. The homebrew supervisors drive only the pinned launcher
// contract (display, audio and the loopback RPC proxy) and must prove their
// cleanup with the attempt-marker protocol before Go allows another launch.
const fs = require("node:fs");
const fsp = require("node:fs/promises");
const path = require("node:path");
const http = require("node:http");
const net = require("node:net");
const crypto = require("node:crypto");
const readline = require("node:readline");
const { spawn } = require("node:child_process");
const { EventEmitter } = require("node:events");

const TOOLS = [
	"rpc-status",
	"system-code-dump",
	"ouncecake-webdav",
	"ouncecake-ftp",
	"mintbrew",
	"native-hblauncher",
	"custom-homebrew",
	"modloader",
];
const HOMEBREW = ["mintbrew", "native-hblauncher", "custom-homebrew"];
const HELD_TARGETS = ["switch2", "civ7", "artemis-seedsow"];
const ATTEMPT_MARKER = ".mac2jb-hblauncher-attempt.json";
const RPC_COMMANDS = new Set([
	"status",
	"call",
	"verify-function",
	"call-address",
	"pair",
	"read",
	"write",
	"query",
]);
const NATIVE_TRUSTED_PROFILES = ["kurumuz", "cellenseres", "astral"];
const MINIMUM_FREE_BYTES = 512n * 1024n * 1024n;
const check = (value, message) => {
	if (!value) throw Error(message);
};

let sequence = 0,
	tool = "";
const emit = (type, fields = {}) =>
	process.stdout.write(JSON.stringify({ type, sequence: ++sequence, tool, ...fields }) + "\n");

// Callers canonicalize a root once, then join fixed names under it, so lstat
// alone keeps every checked path free of symlinks.
async function owned(file, kind = "file", privateMode = true) {
	check(path.isAbsolute(file) && path.resolve(file) === file, "Use absolute canonical tool paths.");
	const info = await fsp.lstat(file);
	check(
		!info.isSymbolicLink() &&
			info.uid === process.getuid() &&
			(kind === "directory" ? info.isDirectory() : info.isFile()) &&
			(info.mode & (privateMode ? 0o077 : 0o022)) === 0,
		"Expected an owned " + (privateMode ? "private " : "") + kind + " without symlinks: " + file,
	);
	return info;
}

async function privateJSON(workspace, name) {
	const file = path.join(workspace, name);
	check(file.startsWith(workspace + path.sep), "A session proof pointed outside its run folder.");
	const info = await owned(file);
	check(info.size <= 8 * 1024 * 1024, "A session proof exceeds its limit.");
	return JSON.parse(await fsp.readFile(file, "utf8"));
}

// The same held contract Go verifies, re-checked inside the tool process before
// any upstream module is required.
async function heldProof(requested) {
	const workspace = await fsp.realpath(requested);
	await owned(workspace, "directory");
	const session = path.basename(workspace);
	const result = await privateJSON(workspace, "result.json");
	check(
		result.verified === true &&
			result.fs?.verified === true &&
			result.fs?.restored === true &&
			result.fs?.cleanup?.deferred === true &&
			result.fs?.cleanup?.cleanedUp !== true &&
			result.fs?.cleanup?.donor?.cleanedUp === true &&
			result.fs?.retainedEndpoint?.ready === true &&
			result.fs.retainedEndpoint.session === session &&
			result.rpc?.mode === "held" &&
			result.rpc?.state === "listening" &&
			!result.completed &&
			!result.error &&
			(!result.cleanupErrors ||
				(Array.isArray(result.cleanupErrors) && result.cleanupErrors.length === 0)),
		"A verified session with a live retained RPC lease is required.",
	);
	const connection = await privateJSON(workspace, "connection.json");
	const target = connection.addresses?.target;
	check(
		connection.addresses?.session === session &&
			HELD_TARGETS.includes(target) &&
			(target === "switch2"
				? !result.target || result.target === "switch2"
				: result.target === target) &&
			Number.isSafeInteger(connection.port) &&
			connection.port > 0 &&
			connection.port <= 65535,
		"The retained endpoint does not belong to this session.",
	);
	return { workspace, session, target, result, connection };
}

// A fresh loopback status request. A stale result file is never accepted as
// proof, and a failed request is reported, never replayed.
function rpcStatus(connection, timeoutMs = 10000) {
	const body = JSON.stringify({
		command: "status",
		...(connection.token ? { token: connection.token } : {}),
	});
	return new Promise((resolve, reject) => {
		const request = http.request(
			{
				host: "127.0.0.1",
				port: connection.port,
				path: "/",
				method: "POST",
				headers: { "content-type": "application/json" },
			},
			(response) => {
				const parts = [];
				let length = 0;
				response.on("error", reject);
				response.on("data", (part) => {
					length += part.length;
					if (length > 65536)
						response.destroy(Error("The RPC status response exceeded its limit."));
					else parts.push(part);
				});
				response.on("end", () => {
					try {
						const value = JSON.parse(Buffer.concat(parts));
						check(
							response.statusCode === 200 && value.ready === true && !value.error,
							"The retained RPC endpoint is not ready.",
						);
						resolve(value);
					} catch (error) {
						reject(error);
					}
				});
			},
		);
		request.setTimeout(timeoutMs, () =>
			request.destroy(Error("The RPC status request timed out; it was not replayed.")),
		);
		request.on("error", reject);
		request.end(body);
	});
}

function control() {
	let stopRequested = false,
		selected,
		offer = new Set(),
		resolveStop,
		resolveSelection;
	const stopped = new Promise((resolve) => {
		resolveStop = resolve;
	});
	const selection = new Promise((resolve) => {
		resolveSelection = resolve;
	});
	const stop = () => {
		stopRequested = true;
		resolveStop();
		resolveSelection(null);
	};
	const lines = readline.createInterface({ input: process.stdin });
	lines.on("line", (line) => {
		let command;
		try {
			check(Buffer.byteLength(line) <= 8192, "control line too large");
			command = JSON.parse(line);
			check(
				command && typeof command === "object" && !Array.isArray(command),
				"invalid control line",
			);
		} catch {
			emit("progress", { message: "An invalid control line was ignored." });
			return;
		}
		if (command.type === "stop") stop();
		else if (command.type === "select" && selected === undefined && offer.has(command.id)) {
			selected = command.id;
			resolveSelection(command.id);
		} else
			emit("progress", { message: "Only a stop request or one offered selection is accepted." });
	});
	lines.on("close", stop);
	process.on("SIGTERM", stop);
	process.on("SIGINT", stop);
	return {
		get stopping() {
			return stopRequested;
		},
		shouldStop: () => stopRequested,
		stopped,
		selection,
		offer: (ids) => {
			offer = new Set(ids);
		},
		close: () => {
			lines.close();
			process.stdin.pause();
		},
	};
}

async function digest(file, expected, size) {
	const hash = crypto.createHash("sha256");
	let bytes = 0;
	for await (const chunk of fs.createReadStream(file, { highWaterMark: 1024 * 1024 })) {
		bytes += chunk.length;
		check(size === undefined || bytes <= size, "A captured file exceeds its recorded size.");
		hash.update(chunk);
	}
	check(
		(size === undefined || bytes === size) && hash.digest("hex") === expected,
		"A captured file failed its SHA-256 readback: " + file,
	);
}

// The capture module is the engine's own, already staged and digest-verified as
// part of the selected host profile and copied into this run by the bridge.
async function systemCodeDump(workspace, proof, destination, modulePath) {
	const current = await privateJSON(workspace, "switch2/fs-execution-current.json");
	check(
		current.session === proof.session &&
			current.verified === true &&
			current.restored === true &&
			current.cleanup?.deferred === true &&
			typeof current.file === "string",
		"The live FS execution proof does not match this retained session.",
	);
	await owned(modulePath, "file", false);
	await owned(path.dirname(destination), "directory");
	await fsp.mkdir(destination, { mode: 0o700, recursive: true });
	await owned(destination, "directory");
	check(
		(await fsp.readdir(destination)).length === 0,
		"The dump destination must be empty; existing captures are never overwritten.",
	);
	const space = await fsp.statfs(destination, { bigint: true });
	check(
		BigInt(space.bavail) * BigInt(space.bsize) >= MINIMUM_FREE_BYTES,
		"The system-code dump needs at least 512 MiB free on this Mac.",
	);
	emit("started", { message: "Capturing installed system code. Keep the console session open." });
	process.env.NS2_FIRMWARE_DUMP_DIR = destination;
	const capture = require(modulePath);
	check(
		typeof capture.main === "function",
		"The staged engine has no system-code capture entrypoint.",
	);
	await capture.main(path.resolve(workspace, current.file));
	const manifest = JSON.parse(
		await fsp.readFile(path.join(destination, "firmware-dump.json"), "utf8"),
	);
	const total = manifest.inventory?.total;
	check(
		manifest.complete === true &&
			Number.isSafeInteger(total) &&
			total > 0 &&
			total <= 1024 &&
			Array.isArray(manifest.entries) &&
			manifest.entries.length === total,
		"The capture did not produce a complete verified inventory.",
	);
	for (const entry of manifest.entries) {
		if (entry.status === "codefs-unavailable") continue;
		for (const file of entry.files || []) {
			check(
				typeof file.file === "string" && file.file.startsWith(destination + path.sep),
				"A captured file escaped the dump destination.",
			);
			await digest(file.file, file.sha256, file.size);
		}
	}
	emit("complete", { path: destination, total, message: "System-code capture verified." });
}

async function ounceCake(toolRoot, workspace, proof, consoleAddress, mode, run) {
	const vendor = path.join(toolRoot, "vendor/OunceCake");
	const load = (name) => require(path.join(vendor, "engine", name + ".cjs"));
	const image = await fsp.readFile(path.join(vendor, "engine/oz-code/native.bin"));
	let attachment,
		server,
		bridge,
		failure,
		cleanupBlocked = false;
	try {
		emit("started", { message: "Preparing the read-only game export." });
		attachment = await load("session").attachGame(workspace);
		check(
			attachment.live.session === proof.session,
			"The export attached to a different native session.",
		);
		const found = await load("content").discoverGames({
			...attachment,
			shouldStop: run.shouldStop,
		});
		check(
			Array.isArray(found) && found.length > 0 && found.length <= 1000,
			"No installed game or inserted game card was found.",
		);
		const games = [],
			ids = new Set();
		for (const game of found) {
			check(
				/^[0-9a-f]{16}$/.test(game.title) && !ids.has(game.title),
				"The console returned invalid game metadata.",
			);
			ids.add(game.title);
			games.push({
				titleId: game.title,
				name:
					String(game.name || "Unknown title")
						.replace(/[^\x20-\x7e]/g, " ")
						.trim()
						.slice(0, 80) || "Unknown title",
			});
		}
		run.offer(ids);
		emit("catalog", { games, message: "Choose one game to export read-only." });
		const title = await run.selection;
		check(title && !run.stopping, "No game was selected.");
		emit("progress", { message: "Preparing the selected game. This can take several minutes." });
		const prepared = await load("mounts").prepareGame({
			attachment,
			title,
			shouldStop: run.shouldStop,
		});
		check(!run.stopping, "The export stopped before its server started.");
		server = await load("server").deployServer({
			attachment,
			image,
			...prepared,
			port: 2121,
			zipPort: 0,
		});
		check(
			!server.address || server.address === consoleAddress,
			"The console reported a different address.",
		);
		let endpoint = "http://" + consoleAddress + ":2121/";
		if (mode === "ftp") {
			bridge = await require(
				path.join(toolRoot, "integrations/mintbrew-ouncecake/ftp-bridge.cjs"),
			).start({ index: prepared.index, sourceAddress: consoleAddress, sourcePort: 2121 });
			check(
				bridge.address === "127.0.0.1" &&
					Number.isInteger(bridge.port) &&
					bridge.port > 0 &&
					bridge.port < 65536 &&
					typeof bridge.close === "function",
				"The FTP bridge did not bind this Mac only.",
			);
			endpoint = "ftp://127.0.0.1:" + bridge.port + "/";
		}
		emit("running", { endpoint, readOnly: true, message: "Read-only export is running." });
		await run.stopped;
	} catch (error) {
		failure = error;
		cleanupBlocked = Boolean(error?.cleanupBlocked);
	} finally {
		if (bridge) {
			try {
				await bridge.close();
			} catch (error) {
				failure ||= error;
			}
		}
		if (server && !cleanupBlocked) {
			try {
				const shutdown = await server.close();
				check(
					shutdown?.nativeStopped === true &&
						shutdown?.joined === true &&
						shutdown?.mappingsCleared === true,
					"The export did not prove native cleanup.",
				);
			} catch (error) {
				failure ||= error;
				cleanupBlocked = true;
			}
		}
		if (attachment && !cleanupBlocked) {
			try {
				await attachment.close();
			} catch (error) {
				failure ||= error;
				cleanupBlocked = true;
			}
		}
	}
	if (cleanupBlocked) {
		const error = failure || Error("Export cleanup was blocked.");
		error.cleanupBlocked = true;
		throw error;
	}
	if (failure) throw failure;
}

// ---------- homebrew supervisors ----------
// The supervisors below are original any2jb code. They drive the staged,
// digest-pinned upstream lower-level modules (prepare stages, owner scripts,
// the native host) exactly the way the mac2jb GUI supervisors do, preserving
// the same readiness lines, shutdown proofs and attempt-marker protocol. Go
// requires those proofs before it allows another homebrew launch.

// A generic request to the held engine bridge. The proxy passes replies
// through unchanged; the children enforce their own limits.
function rpcRequest(connection, body, timeoutMs = 300000) {
	const requestBody = JSON.stringify({
		...body,
		...(connection.token ? { token: connection.token } : {}),
	});
	return new Promise((resolve, reject) => {
		const request = http.request(
			{
				host: "127.0.0.1",
				port: connection.port,
				path: "/",
				method: "POST",
				headers: { "content-type": "application/json" },
			},
			(response) => {
				const parts = [];
				let length = 0;
				response.on("error", reject);
				response.on("data", (part) => {
					length += part.length;
					if (length > 64 * 1024 * 1024)
						response.destroy(Error("The RPC response exceeded its limit."));
					else parts.push(part);
				});
				response.on("end", () => {
					try {
						check(response.statusCode === 200, "The retained RPC endpoint refused a request.");
						resolve(JSON.parse(Buffer.concat(parts)));
					} catch (error) {
						reject(error);
					}
				});
			},
		);
		request.setTimeout(timeoutMs, () =>
			request.destroy(Error("The RPC request timed out; it was not replayed.")),
		);
		request.on("error", reject);
		request.end(requestBody);
	});
}

// The live-lease predicate re-checked on every bridge request and before any
// cleanup proof is accepted.
function liveCheck(proof) {
	try {
		const result = JSON.parse(fs.readFileSync(path.join(proof.workspace, "result.json"), "utf8"));
		const connection = JSON.parse(
			fs.readFileSync(path.join(proof.workspace, "connection.json"), "utf8"),
		);
		return (
			result.verified === true &&
			!result.error &&
			!result.completed &&
			result.rpc?.mode === "held" &&
			result.rpc?.state === "listening" &&
			result.fs?.verified === true &&
			result.fs?.restored === true &&
			result.fs?.retainedEndpoint?.ready === true &&
			result.fs?.cleanup?.deferred === true &&
			connection.port === proof.connection.port &&
			connection.addresses?.session === proof.session
		);
	} catch {
		return false;
	}
}

// The portable runtime environment every homebrew child inherits, built from
// the verified prepared runtime root.
function homebrewEnvironment(homebrewRoot, inherited = process.env, extra = {}) {
	const env = {
		...inherited,
		PATH: [
			path.join(homebrewRoot, "python/bin"),
			path.join(homebrewRoot, "bin"),
			inherited.PATH || "",
		].join(path.delimiter),
		SPIRV_VAL: path.join(homebrewRoot, "bin/spirv-val"),
		PYTHONPATH: path.join(homebrewRoot, "python-custom"),
		PYTHONNOUSERSITE: "1",
		PYTHONDONTWRITEBYTECODE: "1",
		MAC2JB_HBLAUNCHER_FONT: path.join(homebrewRoot, "fonts/DejaVuSans.ttf"),
		...extra,
	};
	delete env.PYTHONHOME;
	return env;
}

async function checkHomebrewRuntime(homebrewRoot) {
	for (const file of ["python/bin/python3", "bin/spirv-val"]) {
		await fsp.access(path.join(homebrewRoot, file), fs.constants.X_OK).catch(() => {
			throw Error("The staged homebrew runtime is not executable: " + file);
		});
	}
	for (const file of ["fonts/DejaVuSans.ttf", "python-custom/sitecustomize.py"]) {
		check(
			(await fsp.lstat(path.join(homebrewRoot, file))).isFile(),
			"The staged homebrew runtime is incomplete: " + file,
		);
	}
}

function readOutputLines(stream, callback) {
	let pending = "";
	stream.on("data", (bytes) => {
		pending += bytes.toString();
		let at = pending.search(/[\r\n]/);
		while (at >= 0) {
			const line = pending.slice(0, at);
			pending = pending.slice(at + 1);
			if (line) callback(line);
			at = pending.search(/[\r\n]/);
		}
		if (pending.length > 65536) {
			callback(pending.slice(0, 65536));
			pending = "";
		}
	});
	stream.on("end", () => {
		if (pending) callback(pending);
	});
}

// The loopback RPC proxy every homebrew child talks to. It re-checks the live
// lease on every request and forwards only the documented bridge commands.
async function rpcProxy(proof, onRequest) {
	const token = crypto.randomBytes(32).toString("hex");
	const server = http.createServer(async (req, res) => {
		if (
			req.method !== "POST" ||
			req.url !== "/" ||
			req.headers.authorization !== "Bearer " + token
		) {
			res.writeHead(403).end();
			return;
		}
		try {
			check(liveCheck(proof), "The original held RPC session is no longer available.");
			const parts = [];
			let length = 0;
			for await (const part of req) {
				length += part.length;
				check(length <= 2 * 1024 * 1024, "Bridge request is too large");
				parts.push(part);
			}
			const body = JSON.parse(Buffer.concat(parts));
			check(
				body && typeof body === "object" && !Array.isArray(body) && RPC_COMMANDS.has(body.command),
				"Unknown homebrew bridge command",
			);
			const reply = await onRequest(body);
			if (!res.destroyed) {
				res.setHeader("content-type", "application/json");
				res.end(
					JSON.stringify(reply, (_, value) =>
						typeof value === "bigint" ? value.toString() : value,
					),
				);
			}
		} catch (error) {
			if (!res.destroyed) {
				res
					.writeHead(400, { "content-type": "application/json" })
					.end(JSON.stringify({ error: error.message }));
			}
		}
	});
	await new Promise((resolve, reject) => {
		server.once("error", reject);
		server.listen(0, "127.0.0.1", resolve);
	});
	return {
		token,
		port: server.address().port,
		async close() {
			server.closeAllConnections();
			await new Promise((resolve) => server.close(resolve));
		},
	};
}

async function writePrivateJSON(file, value, exclusive = false) {
	await fsp.writeFile(file, JSON.stringify(value, null, 2) + "\n", {
		mode: 0o600,
		flag: exclusive ? "wx" : "w",
	});
}

async function writeAttemptMarker(workspace, attempt) {
	try {
		await fsp.writeFile(
			path.join(workspace, ATTEMPT_MARKER),
			JSON.stringify(attempt, null, 2) + "\n",
			{ mode: 0o600, flag: "wx" },
		);
	} catch (error) {
		if (error.code === "EEXIST") {
			throw Error(
				"Another homebrew launcher already owns this console session; stop it and prove its cleanup before switching.",
			);
		}
		throw error;
	}
}

// The attempt-marker release the mac2jb homebrew-reentry module documents: the
// proof shape is checked, then the marker itself (no symlinks, owned, private,
// one link, unchanged digest) before it is unlinked.
function releaseAttemptMarker(directory, proof) {
	check(
		proof?.cleanupVerified === true &&
			/^[a-f0-9]{64}$/.test(proof.attemptMarkerSha256 || "") &&
			JSON.stringify(proof.attemptMarkers) === JSON.stringify([ATTEMPT_MARKER]),
		"The homebrew app did not prove its launch marker cleanup.",
	);
	const file = path.join(directory, ATTEMPT_MARKER);
	const fd = fs.openSync(file, fs.constants.O_RDONLY | fs.constants.O_NOFOLLOW);
	try {
		const info = fs.fstatSync(fd);
		check(
			info.isFile() &&
				info.uid === process.getuid() &&
				info.nlink === 1 &&
				(info.mode & 0o077) === 0,
			"The homebrew launch marker is not a private owned file.",
		);
		const bytes = fs.readFileSync(fd);
		check(
			crypto.createHash("sha256").update(bytes).digest("hex") === proof.attemptMarkerSha256,
			"The homebrew launch marker changed.",
		);
	} finally {
		fs.closeSync(fd);
	}
	fs.unlinkSync(file);
}

// superviseHomebrew runs one pinned launcher lifecycle: session directory,
// loopback proxy, prepare stages, owner children with readiness lines, frame
// readiness, then the stated shutdown and attempt-marker proofs.
async function superviseHomebrew(app, workspace, proof, run) {
	const sessionDir = await fsp.mkdtemp(path.join(path.dirname(workspace), app.id + "-"));
	const session = path.basename(sessionDir);
	const markerFile = path.join(workspace, ATTEMPT_MARKER);
	let proxy,
		running = false,
		failure,
		nativeMutation = false,
		cleanupUnsafe = false;
	const children = new Set(),
		activeOwners = new Map(),
		pendingRPC = new Set();
	let stopping = false,
		stopResolve;
	const stopRequested = new Promise((resolve) => {
		stopResolve = resolve;
	});
	const stop = () => {
		if (!stopping) {
			stopping = true;
			stopResolve();
		}
	};
	run.stopped.then(stop);
	const fail = (error) => {
		failure ||= error;
		stop();
	};
	const live = () => liveCheck(proof);
	function forward(body) {
		if (!["status", "read", "query"].includes(body.command)) nativeMutation = true;
		const operation = Promise.resolve().then(() => rpcRequest(proof.connection, body));
		pendingRPC.add(operation);
		operation.finally(() => pendingRPC.delete(operation)).catch(() => {});
		return operation;
	}
	function startChild(script, extra = [], readyText, role = "prepare") {
		check(!stopping, app.name + " launch was stopped.");
		const child = spawn(
			process.execPath,
			[path.join(app.vendorDir, script), sessionDir, ...extra],
			{
				cwd: app.vendorDir,
				env: app.env,
				stdio: ["ignore", "pipe", "pipe"],
			},
		);
		const state = { child, script, role, closed: false, frameReady: false, output: "" };
		children.add(state);
		let readyResolve, readyReject;
		state.readiness = new Promise((resolve, reject) => {
			readyResolve = resolve;
			readyReject = reject;
		});
		state.readiness.catch(() => {});
		const observe = (line) => {
			state.output = (state.output + line + "\n").slice(-32768);
			if (readyText && line.trim() === readyText) readyResolve();
			if (line.trim() === app.frameReady) state.frameReady = true;
			emit("progress", { message: script + ": " + line });
		};
		readOutputLines(child.stdout, observe);
		readOutputLines(child.stderr, observe);
		state.done = new Promise((resolve) => {
			child.once("error", (error) => {
				state.closed = true;
				readyReject(error);
				resolve({ code: null, error });
			});
			child.once("close", (code, signal) => {
				state.closed = true;
				state.outcome = { code, signal };
				children.delete(state);
				readyReject(Error(script + " ended before readiness."));
				if (role !== "prepare" && !stopping)
					fail(Error(script + " ended unexpectedly: " + (signal || code)));
				resolve(state.outcome);
			});
		});
		return state;
	}
	async function waitFor(promise, label, timeoutMs = 300000) {
		let timer;
		try {
			return await Promise.race([
				promise,
				stopRequested.then(() => {
					throw Error(app.name + " stopped during " + label);
				}),
				new Promise((_, reject) => {
					timer = setTimeout(() => reject(Error(label + " timed out.")), timeoutMs);
				}),
			]);
		} finally {
			clearTimeout(timer);
		}
	}
	async function endChild(state) {
		if (!state || state.closed) return;
		state.child.kill("SIGTERM");
		let timer,
			timedOut = false;
		const outcome = await Promise.race([
			state.done,
			new Promise((resolve) => {
				timer = setTimeout(() => {
					timedOut = true;
					state.child.kill("SIGKILL");
					resolve({ code: null });
				}, 30000);
			}),
		]);
		clearTimeout(timer);
		if (timedOut || outcome.error || outcome.code !== 0) {
			cleanupUnsafe = true;
			failure ||= Error(
				app.name + " " + state.role + " did not confirm a clean stop. Restart the console.",
			);
		}
		if (timedOut) await state.done;
	}
	const ctx = {
		sessionDir,
		session,
		activeOwners,
		live,
		markMutation: () => {
			nativeMutation = true;
		},
		stage: async (script, extra) => {
			emit("progress", { message: "Preparing " + script });
			return waitFor(startChild(script, extra).done, script);
		},
		startOwner: async (owner) => {
			const state = startChild(owner.script, owner.args, owner.ready, owner.role);
			activeOwners.set(owner.role, state);
			await waitFor(state.readiness, owner.role + " readiness");
			return state;
		},
		frameReady: async (state) => {
			await waitFor(
				(async () => {
					while (!state.frameReady) {
						check(!state.closed, "The graphics worker ended before its frame loop started.");
						await new Promise((resolve) => setTimeout(resolve, 20));
					}
				})(),
				"console graphics readiness",
			);
		},
	};
	const monitor = setInterval(() => {
		if (!live()) fail(Error("The original held RPC session ended. Restart the console."));
	}, 500);
	monitor.unref();
	try {
		if (app.prepareSession) await app.prepareSession(sessionDir);
		proxy = await rpcProxy(proof, forward);
		await fsp.mkdir(path.join(sessionDir, "shared"), { mode: 0o700 });
		await fsp.copyFile(app.sharedClient, path.join(sessionDir, "shared/rpc.cjs"));
		await fsp.chmod(path.join(sessionDir, "shared/rpc.cjs"), 0o600);
		await writePrivateJSON(path.join(sessionDir, "connection.json"), {
			port: proxy.port,
			token: proxy.token,
			addresses: { ...proof.connection.addresses, session },
		});
		emit("started", { message: "Attaching " + app.name + " to the existing RPC session." });
		await app.launch(ctx);
		check(live(), "The original RPC session ended during launcher preparation.");
		running = true;
		emit("running", { message: app.name + " is running. Keep SmileBASIC and this app open." });
		await stopRequested;
	} catch (error) {
		failure ||= error;
	}
	stop();
	clearInterval(monitor);
	for (const role of ["ftp", "audio", "graphics"]) await endChild(activeOwners.get(role));
	for (const state of [...children]) await endChild(state);
	if (app.drain && pendingRPC.size) {
		try {
			const outcomes = await Promise.allSettled([...pendingRPC]);
			check(
				outcomes.every((outcome) => outcome.status === "fulfilled"),
				app.name + " RPC shutdown could not be confirmed.",
			);
		} catch (error) {
			failure ||= error;
		}
	}
	if (running && !failure) {
		try {
			await app.shutdownProof(ctx);
		} catch (error) {
			failure ||= error;
		}
	}
	try {
		await proxy?.close();
	} catch (error) {
		failure ||= error;
	}
	let attemptProof;
	if (!failure && running) {
		try {
			check(live(), "The held RPC session ended before launcher shutdown was verified.");
			const marker = await fsp.readFile(markerFile);
			check(
				JSON.stringify(JSON.parse(marker)) === JSON.stringify(app.attempt),
				"The launch attempt marker changed.",
			);
			attemptProof = {
				cleanupVerified: true,
				attemptMarkerSha256: crypto.createHash("sha256").update(marker).digest("hex"),
				attemptMarkers: [ATTEMPT_MARKER],
			};
			releaseAttemptMarker(workspace, attemptProof);
		} catch (error) {
			failure ||= error;
		}
	}
	if (failure) {
		// Retry-safe means: no native mutation happened, every child confirmed a
		// clean stop, the lease is live and the marker is provably unchanged.
		let retrySafe = !nativeMutation && !cleanupUnsafe && live();
		if (retrySafe && app.markerWritten) {
			try {
				const current = JSON.parse(await fsp.readFile(markerFile));
				check(
					JSON.stringify(current) === JSON.stringify(app.attempt),
					"The launch attempt marker changed.",
				);
				await fsp.unlink(markerFile);
			} catch {
				retrySafe = false;
			}
		}
		failure.cleanupBlocked = !retrySafe;
		failure.homebrewReported = true;
		emit("failed", {
			cleanupBlocked: !retrySafe,
			message:
				failure.message +
				(retrySafe
					? " The held RPC session is still available; choose another app or retry."
					: " This launch was not retried. Restart the console before launching homebrew again."),
		});
		throw failure;
	}
	emit("stopped", {
		message: app.name + " stopped. Graphics allocations remain owned by this console session.",
		...attemptProof,
	});
}

const MINTBREW_STAGES = [
	["homebrew-launcher/inspect-display.cjs"],
	["homebrew-launcher/inspect-compatibility.cjs"],
	["homebrew-launcher/inspect-input.cjs"],
	["homebrew-launcher/vulkan-copy.cjs", "display"],
	["homebrew-launcher/launcher.cjs", "auto"],
	["homebrew-launcher/launcher-check.cjs", "auto"],
];
const MINTBREW_OWNERS = [
	{
		role: "graphics",
		script: "homebrew-launcher/launcher-loop.cjs",
		args: ["auto"],
		ready: "MintBrew game event source ready.",
	},
	{ role: "ftp", script: "sdk/host/ftp-service.cjs", args: [], ready: "SDK FTP service ready." },
	{
		role: "audio",
		script: "homebrew-launcher/game-audio.cjs",
		args: [],
		ready: "Game sound effects ready.",
	},
];

// The MintBrew descriptor: pinned manifest, frozen RPC client and the default
// Stremio configuration (no add-ons; add-on URLs stay gated with Stremio's
// unresolved provenance).
async function mintbrewApp(toolRoot, workspace, proof, config, custom) {
	const vendorDir = path.join(toolRoot, "mintbrew", "vendor/MintBrew");
	const manifest = JSON.parse(await fsp.readFile(path.join(vendorDir, "manifest.json"), "utf8"));
	check(
		manifest.name === "MintBrew" &&
			typeof manifest.version === "string" &&
			/^\d+\.\d+\.\d+$/.test(manifest.version),
		"Invalid MintBrew manifest.",
	);
	await checkHomebrewRuntime(config.homebrewRuntime);
	const attempt = {
		application: custom ? "custom-homebrew" : "mintbrew",
		source: "MintBrew",
		format: "mintbrew-v1",
		started: new Date().toISOString(),
	};
	check(
		(await rpcRequest(proof.connection, { command: "status" }))?.ready === true,
		"The existing native RPC connection is not ready.",
	);
	await writeAttemptMarker(workspace, attempt);
	const app = {
		id: "mintbrew",
		name: custom ? "Custom homebrew" : "MintBrew",
		vendorDir,
		attempt,
		markerWritten: true,
		frameReady: "MintBrew continuous native frame loop running.",
		sharedClient: path.join(vendorDir, "smilebasic/engine/shared/rpc.cjs"),
		env: homebrewEnvironment(config.homebrewRuntime, process.env, {
			HB_APPS_DIR: path.join(vendorDir, "apps"),
		}),
		prepareSession: async (sessionDir) => {
			const quality = ["480p", "720p", "1080p"].includes(config.stremioQuality)
				? config.stremioQuality
				: "720p";
			const addonUrls = Array.isArray(config.stremioAddonUrls)
				? config.stremioAddonUrls
						.filter((u) => typeof u === "string" && /^https?:\/\//.test(u))
						.slice(0, 16)
				: [];
			await writePrivateJSON(
				path.join(sessionDir, "stremio-config.json"),
				{ addonUrls, libraryItems: [], quality },
				true,
			);
			await writePrivateJSON(
				path.join(sessionDir, "mintbrew-host.json"),
				{ runDirectory: workspace, sourceSession: proof.session },
				true,
			);
			if (custom) {
				const library = path.join(sessionDir, "custom-apps");
				fs.mkdirSync(library, { mode: 0o700 });
				copyRegularTree(custom.folder, path.join(library, custom.id));
				app.env = { ...app.env, HB_APPS_DIR: library };
			} else if (config.appFolder) {
				app.env = {
					...app.env,
					HB_APPS_DIR: ownedFolderSync(config.appFolder, "MintBrew apps folder"),
				};
			}
		},
		launch: async (ctx) => {
			for (const [script, ...extra] of MINTBREW_STAGES) {
				if (script === "homebrew-launcher/vulkan-copy.cjs") ctx.markMutation();
				const outcome = await ctx.stage(script, extra);
				check(
					!outcome.error && outcome.code === 0 && !outcome.signal,
					"MintBrew preparation failed: " + script,
				);
			}
			let graphics;
			for (const owner of MINTBREW_OWNERS) {
				const state = await ctx.startOwner(owner);
				if (owner.role === "graphics") graphics = state;
			}
			await ctx.frameReady(graphics);
			return graphics;
		},
		shutdownProof: async (ctx) => {
			let loop, sound;
			try {
				loop = await privateJSON(ctx.sessionDir, "launcher-auto-loop.json");
				sound = await privateJSON(ctx.sessionDir, "game-audio.json");
			} catch {
				throw Error("MintBrew did not verify its graphics and audio shutdown.");
			}
			check(
				loop.session === ctx.session &&
					loop.completed &&
					!loop.error &&
					loop.threadRunning === false &&
					sound.session === ctx.session &&
					sound.completed &&
					!sound.error &&
					!sound.stopError,
				"MintBrew did not verify its graphics and audio shutdown.",
			);
		},
	};
	return app;
}

// nativeHBLauncher delegates to the staged MIT fork's host with injected
// adapters: the any2jb held proof, the loopback RPC request, the developer
// engine sources for its ABI check and the any2jb-owned MintNES ROM folder.
// Its upstream attempt-marker contract is verified and released here.
async function nativeHBLauncher(toolRoot, workspace, proof, config, run) {
	check(
		NATIVE_TRUSTED_PROFILES.includes(config.profileID),
		"Native HBLauncher requires a supported held RPC host profile.",
	);
	check(
		typeof config.engineSourceDir === "string" &&
			path.isAbsolute(config.engineSourceDir) &&
			typeof config.sourceDirectory === "string" &&
			config.sourceDirectory !== "" &&
			!path.isAbsolute(config.sourceDirectory) &&
			!config.sourceDirectory.split(/[\\/]/).includes(".."),
		"Native HBLauncher requires the verified staged engine sources.",
	);
	const component = fs.existsSync(path.join(toolRoot, "mintbrew-native"))
		? path.join(toolRoot, "mintbrew-native")
		: path.join(toolRoot, "native-hblauncher");
	const host = require(path.join(component, "integrations/mintbrew-native/host.cjs"));
	const signals = new EventEmitter();
	run.stopped.then(() => signals.emit("SIGTERM"));
	const args = [workspace];
	if (config.appFolder) args.push("--app-folder", config.appFolder);
	if (config.sdRomFolder) args.push("--sd-rom-folder", config.sdRomFolder);
	const ouncecakeSession = path.join(toolRoot, "ouncecake", "vendor/OunceCake/engine/session.cjs");
	let reported = false,
		stoppedProof;
	const forward = (type, fields = {}) => {
		if (type === "stopped") {
			stoppedProof = fields;
			return;
		}
		if (type === "failed") {
			reported = true;
			emit("failed", { cleanupBlocked: Boolean(fields.cleanupBlocked), message: fields.message });
			return;
		}
		if (type === "running") {
			emit("running", { message: fields.message });
			return;
		}
		if (type === "started") {
			emit("started", { message: fields.message });
			return;
		}
		emit("progress", { message: fields.message });
	};
	try {
		await host.main(args, {
			root: path.join(toolRoot, "mintbrew"),
			emit: forward,
			signals,
			validateRun: async () => ({
				directory: workspace,
				proof: { session: proof.session },
				connection: proof.connection,
			}),
			verifySession: async () => ({ root: workspace, session: proof.session }),
			request: (_directory, body) => rpcRequest(proof.connection, body),
			trustedProfileForDigest: () => ({
				profile: { id: config.profileID },
				sourceDirectory: path.join(config.engineSourceDir, config.sourceDirectory),
			}),
			macRomDirectory: path.join(config.supportDir, "MintNES", "roms"),
			readSDRoms: fs.existsSync(ouncecakeSession)
				? (directory, options) =>
						require(path.join(component, "integrations/mintbrew-native/sd-roms.cjs")).readSDRoms(
							directory,
							{ ...options, attachGame: require(ouncecakeSession).attachGame },
						)
				: undefined,
		});
	} catch (error) {
		if (reported || error.homebrewReported) return;
		throw error;
	}
	check(stoppedProof?.cleanupVerified === true, "The native launcher did not prove its cleanup.");
	releaseAttemptMarker(workspace, stoppedProof);
	emit("stopped", {
		cleanupVerified: true,
		attemptMarkerSha256: stoppedProof.attemptMarkerSha256,
		attemptMarkers: stoppedProof.attemptMarkers,
		message: "Native HBLauncher stopped. Its display resources remain in this console session.",
	});
}

// ---------- modloader ----------
// The modloader stages the pinned ns2jb-modloader code (plus OunceCake's engine
// for ELicense access) and either reads compatible mods already on the Switch SD
// card (/ns2jb-modloader) or uploads the user's selected Atmosphere mod folder to
// /ns2jb-modloader with SHA-256 readback, then runs install.cjs as the session's
// final action on either a SmileBASIC (switch2) or CivSwitch FS (civ7) session.
const MODLOADER_PROFILES = ["kurumuz", "cellenseres"];
const MODLOADER_CODE = ["install.cjs", "LICENSE", "lib", "native"];
const MOD_ENTRIES = ["contents", "exefs_patches"];
const MAX_MOD_FILES = 20000;

function ownedFolderSync(value, label) {
	check(
		typeof value === "string" &&
			path.isAbsolute(value) &&
			value.length <= 4096 &&
			!/[\x00\r\n]/.test(value),
		"Choose an absolute " + label + ".",
	);
	const info = fs.lstatSync(value);
	check(
		info.isDirectory() && !info.isSymbolicLink() && info.uid === process.getuid(),
		"The " + label + " must be an owned directory without symlinks.",
	);
	return fs.realpathSync(value);
}

function regularTreeSync(source, count) {
	for (const entry of fs.readdirSync(source)) {
		const file = path.join(source, entry),
			info = fs.lstatSync(file);
		check(
			!info.isSymbolicLink() &&
				(info.isDirectory() || info.isFile()) &&
				info.uid === process.getuid(),
			"Folders may contain only owned regular files and directories: " + file,
		);
		if (info.isDirectory()) regularTreeSync(file, count);
		else check(++count.files <= MAX_MOD_FILES, "The selected folder has too many files.");
	}
}

function validateModsFolder(directory, collectMods) {
	const absolute = ownedFolderSync(directory, "mod folder");
	const count = { files: 0 };
	for (const name of MOD_ENTRIES) {
		const entry = path.join(absolute, name);
		if (!fs.existsSync(entry)) continue;
		const info = fs.lstatSync(entry);
		check(info.isDirectory() && !info.isSymbolicLink(), name + " must be a real directory.");
		regularTreeSync(entry, count);
	}
	collectMods(absolute);
	return absolute;
}

// Copies through file descriptors opened without following links and refuses
// a file whose identity, size or time changed while it was copied.
function copyRegularTree(source, destination) {
	const info = fs.lstatSync(source);
	check(!info.isSymbolicLink(), "A selected file changed to a symlink: " + source);
	if (info.isDirectory()) {
		fs.mkdirSync(destination, { mode: 0o700 });
		for (const entry of fs.readdirSync(source))
			copyRegularTree(path.join(source, entry), path.join(destination, entry));
		return;
	}
	check(info.isFile(), "Only regular files can be staged: " + source);
	const input = fs.openSync(source, fs.constants.O_RDONLY | fs.constants.O_NOFOLLOW);
	let output;
	try {
		const opened = fs.fstatSync(input);
		check(
			opened.isFile() && opened.dev === info.dev && opened.ino === info.ino,
			"A selected file changed during staging: " + source,
		);
		output = fs.openSync(
			destination,
			fs.constants.O_WRONLY | fs.constants.O_CREAT | fs.constants.O_EXCL,
			0o600,
		);
		const buffer = Buffer.allocUnsafe(1024 * 1024);
		let copied = 0;
		let size = fs.readSync(input, buffer, 0, buffer.length, null);
		while (size > 0) {
			let written = 0;
			while (written < size) written += fs.writeSync(output, buffer, written, size - written);
			copied += size;
			size = fs.readSync(input, buffer, 0, buffer.length, null);
		}
		const final = fs.fstatSync(input);
		check(
			copied === opened.size && final.size === opened.size && final.mtimeMs === opened.mtimeMs,
			"A selected file changed during staging: " + source,
		);
	} finally {
		if (output !== undefined) fs.closeSync(output);
		fs.closeSync(input);
	}
}

const boundedLine = (line) =>
	String(line)
		.trim()
		.replace(/[\x00-\x1f\x7f]/g, " ")
		.slice(0, 512);

async function modloader(toolRoot, workspace, proof, config, run) {
	const modloaderRoot = path.join(toolRoot, "modloader");
	const component = path.join(modloaderRoot, "integrations/ns2jb-modloader");
	const ouncecake = path.join(toolRoot, "ouncecake", "vendor/OunceCake");
	const target = proof.target || "switch2";
	let staging,
		child,
		uploaded,
		staged = { games: [], patches: 0 },
		captureStarted = false,
		uploadStarted = false;
	try {
		if (target === "civ7") {
			check(
				proof.result.target === "civ7",
				"The held CivSwitch FS session has an unexpected target.",
			);
		} else {
			check(
				target === "switch2" && MODLOADER_PROFILES.includes(config.profileID),
				"The modloader requires a held Kurumuz or Cellenseres RPC session, or a verified CivSwitch FS session.",
			);
		}
		const { collectMods } = require(path.join(component, "lib/mods.cjs"));
		const mods = config.modsFolder ? validateModsFolder(config.modsFolder, collectMods) : null;
		staging = fs.mkdtempSync(path.join(workspace, ".mac2jb-modloader-"));
		fs.chmodSync(staging, 0o700);
		for (const name of MODLOADER_CODE)
			copyRegularTree(path.join(component, name), path.join(staging, name));
		if (
			fs.existsSync(path.join(ouncecake, "engine")) &&
			fs.existsSync(path.join(ouncecake, "LICENSE"))
		) {
			const stagedOuncecake = path.join(staging, "vendor", "OunceCake");
			fs.mkdirSync(stagedOuncecake, { recursive: true, mode: 0o700 });
			copyRegularTree(path.join(ouncecake, "engine"), path.join(stagedOuncecake, "engine"));
			copyRegularTree(path.join(ouncecake, "LICENSE"), path.join(stagedOuncecake, "LICENSE"));
		}
		if (mods) {
			for (const name of MOD_ENTRIES) {
				if (fs.existsSync(path.join(mods, name)))
					copyRegularTree(path.join(mods, name), path.join(staging, name));
			}
			const collected = collectMods(staging);
			check(
				collected.games.length + collected.patches.length > 0,
				"No loadable mod files were staged.",
			);
			staged = { games: collected.games.map((g) => g.titleId), patches: collected.patches.length };
		}
		require(path.join(staging, "lib/native/artifacts.cjs")).artifacts();
		check(!run.stopping && liveCheck(proof), "The modloader stopped before its transfer started.");
		if (target === "civ7") {
			process.env.NS2_TARGET = "civ7";
			process.env.NS2_TARGET_ROOT = workspace;
			captureStarted = true;
			await require(path.join(staging, "lib/native-capture.cjs")).captureNative(
				workspace,
				path.join(staging, "preflight"),
			);
			fs.rmSync(path.join(staging, "preflight"), { recursive: true, force: true });
		}
		if (!mods) {
			emit("started", {
				message:
					"Reading compatible mods from /ns2jb-modloader on the Switch SD card. Installing; this is the final action of the session.",
			});
		} else {
			emit("started", {
				message:
					"Uploading " +
					staged.games.length +
					" game mod set(s) and " +
					staged.patches +
					" global patch(es) to /ns2jb-modloader on the Switch SD card.",
			});
			uploadStarted = true;
			const sdHelper = path.join(modloaderRoot, "scripts/gui-modloader-sd.cjs");
			if (fs.existsSync(sdHelper)) {
				uploaded = await require(sdHelper).uploadSelectedMods(workspace, staging);
				emit("progress", {
					message: "Verified " + uploaded.files + " SD mod file(s) on the console.",
				});
			}
		}
	} catch (error) {
		if (staging) fs.rmSync(staging, { recursive: true, force: true });
		if (captureStarted || uploadStarted) {
			error.cleanupBlocked = true;
			error.message += " Restart the console before trying again.";
		}
		throw error;
	}
	let ready = false,
		stoppedReason = "";
	try {
		const installArgs = [
			path.join(staging, "install.cjs"),
			"--run-dir",
			workspace,
			...(uploaded?.source && uploaded.source !== "/ns2jb-modloader"
				? ["--mod-source", uploaded.source]
				: []),
		];
		const childEnv = {
			...process.env,
			...(target === "civ7" ? { NS2_TARGET: "civ7", NS2_TARGET_ROOT: workspace } : {}),
		};
		child = spawn(process.execPath, installArgs, {
			cwd: staging,
			stdio: ["ignore", "pipe", "pipe"],
			env: childEnv,
		});
		const observe = (line) => {
			line = boundedLine(line);
			if (line.startsWith("READY")) ready = true;
			if (line.startsWith("INSTALL STOPPED: ")) stoppedReason = line.slice(17, 317);
			if (line) emit("progress", { message: line });
		};
		readOutputLines(child.stdout, observe);
		readOutputLines(child.stderr, observe);
		run.stopped.then(() => child.kill("SIGTERM"));
		const outcome = await new Promise((resolve) => {
			child.once("error", (error) => resolve({ error }));
			child.once("close", (code, signal) => resolve({ code, signal }));
		});
		check(
			!outcome.error && outcome.code === 0 && !outcome.signal && ready,
			(stoppedReason ? "Modloader stopped: " + stoppedReason.replace(/[.!?]+$/, "") + ". " : "") +
				"The game handoff was not verified.",
		);
		const lock = JSON.parse(
			fs.readFileSync(path.join(workspace, "ns2jb-modloader-install.json"), "utf8"),
		);
		const resident = JSON.parse(
			fs.readFileSync(
				path.join(staging, "state", path.basename(workspace), "resident-install.json"),
				"utf8",
			),
		);
		check(
			lock.status === "ready" &&
				resident.verified === true &&
				resident.readyForGameHandoff === true &&
				resident.handoff?.stockRoProtocolVerified === true,
			"The modloader did not verify the final game handoff.",
		);
	} catch (error) {
		error.cleanupBlocked = true;
		error.message += " Restart the console before trying again.";
		throw error;
	}
	emit("ready", {
		message:
			target === "civ7"
				? "Modloader installed. Disconnect this attachment, stop the CivSwitch FS host, then close Civilization VII without restarting the Switch before launching the game."
				: "Modloader installed. Finish this session, close SmileBASIC without restarting the console, then launch the game. The overlay stays until the console restarts.",
	});
}

// ---------- custom homebrew ----------
// A built GPU app folder (app.json) runs inside the pinned MintBrew pipeline
// from a fresh per-session library; the user's original folder is never
// changed. Other custom formats are refused in Go before this point.
function customHomebrewFolder(value) {
	const folder = ownedFolderSync(value, "built homebrew folder");
	for (const marker of [
		"mac2jb-rpc-app.json",
		"engine/oz-code/native.bin",
		"module.json",
		"manifest.json",
	]) {
		check(
			!fs.existsSync(path.join(folder, marker)),
			"Only a built GPU app is supported as custom homebrew.",
		);
	}
	const info = fs.lstatSync(path.join(folder, "app.json"));
	check(
		info.isFile() && info.size <= 1024 * 1024,
		"Choose a built GPU homebrew folder that contains app.json.",
	);
	const app = JSON.parse(fs.readFileSync(path.join(folder, "app.json"), "utf8"));
	check(
		/^[a-z0-9][a-z0-9-]{0,47}$/.test(app?.id || "") &&
			typeof app.name === "string" &&
			app.name !== "",
		"app.json must name a lowercase app id and a display name.",
	);
	regularTreeSync(folder, { files: 0 });
	return { folder, id: app.id };
}
async function main(args = process.argv.slice(2)) {
	if (args.length === 1 && args[0] === "--self-test") {
		process.stdout.write(JSON.stringify({ type: "self-test", tools: TOOLS }) + "\n");
		return;
	}
	const [stagedRoot, selected, workspace, consoleAddress, destination, configJSON] = args;
	check(
		args.length === 5 || args.length === 6,
		"usage: toolhost.cjs <staged-root> <tool> <workspace> <console-IPv4|-> <destination|-> [homebrew-config]",
	);
	check(TOOLS.includes(selected), "Unknown read-only tool.");
	tool = selected;
	let config = {};
	if (configJSON !== undefined) {
		check(
			HOMEBREW.includes(selected) || selected === "modloader",
			"Only a homebrew tool or the modloader accepts a launch configuration.",
		);
		config = JSON.parse(configJSON);
		check(
			config && typeof config === "object" && !Array.isArray(config),
			"Invalid homebrew launch configuration.",
		);
	}
	if (
		selected === "modloader" ||
		selected === "custom-homebrew" ||
		((selected === "native-hblauncher" || selected === "mintbrew") && config.appFolder)
	) {
		check(
			config.codeConsent === true,
			"Confirm running user-provided code on the console before this action.",
		);
	}
	const record = JSON.parse(await fsp.readFile(path.join(stagedRoot, "stage.json"), "utf8"));
	check(
		(record.source === "developer-staged" && record.redistributable === false) ||
			(record.source === "bundled" && record.redistributable === true),
		"Read-only tools require a verified stage of their pinned sources.",
	);
	const proof = await heldProof(path.resolve(workspace));
	await rpcStatus(proof.connection);
	const owned = proof.workspace;
	if (selected === "rpc-status") {
		emit("status", {
			ready: true,
			message: "The retained RPC lease answered a fresh status request.",
		});
		return;
	}
	const run = control();
	try {
		if (selected === "system-code-dump") {
			check(
				proof.target === "switch2",
				"System-code capture requires a verified SmileBASIC (ns2jb) session.",
			);
			check(
				destination !== "-" && path.isAbsolute(destination),
				"Choose a dedicated dump destination.",
			);
			await systemCodeDump(
				owned,
				proof,
				path.resolve(destination),
				path.join(owned, "switch2/firmware-dump.cjs"),
			);
		} else if (HOMEBREW.includes(selected)) {
			check(
				proof.target === "switch2",
				"SmileBASIC homebrew launchers require a verified ns2jb session.",
			);
			check(
				typeof config.supportDir === "string" && path.isAbsolute(config.supportDir),
				"The homebrew launch configuration is incomplete.",
			);
			const toolRoot = path.join(stagedRoot, "tools");
			if (selected === "mintbrew") {
				check(
					typeof config.homebrewRuntime === "string" && path.isAbsolute(config.homebrewRuntime),
					"The homebrew launch configuration is missing the verified runtime root.",
				);
				await superviseHomebrew(
					await mintbrewApp(toolRoot, owned, proof, config),
					owned,
					proof,
					run,
				);
			} else if (selected === "custom-homebrew") {
				check(
					typeof config.homebrewRuntime === "string" && path.isAbsolute(config.homebrewRuntime),
					"The homebrew launch configuration is missing the verified runtime root.",
				);
				const custom = customHomebrewFolder(config.customHomebrewFolder);
				await superviseHomebrew(
					await mintbrewApp(toolRoot, owned, proof, config, custom),
					owned,
					proof,
					run,
				);
			} else {
				await nativeHBLauncher(toolRoot, owned, proof, config, run);
			}
		} else if (selected === "modloader") {
			await modloader(path.join(stagedRoot, "tools"), owned, proof, config, run);
		} else {
			check(net.isIPv4(consoleAddress), "Enter the console IPv4 address.");
			const toolRoot = path.join(stagedRoot, "tools", "ouncecake");
			await ounceCake(
				toolRoot,
				owned,
				proof,
				consoleAddress,
				selected === "ouncecake-ftp" ? "ftp" : "webdav",
				run,
			);
			emit("stopped", { message: "The read-only tool released its own resources." });
		}
	} finally {
		run.close();
	}
}

if (require.main === module)
	main().catch((error) => {
		if (!error.homebrewReported) {
			emit("failed", {
				cleanupBlocked: Boolean(error?.cleanupBlocked),
				message: error.message + " This tool was not retried.",
			});
		}
		process.exitCode = 1;
	});
module.exports = {
	main,
	heldProof,
	TOOLS,
	HOMEBREW,
	HELD_TARGETS,
	ATTEMPT_MARKER,
	liveCheck,
	rpcProxy,
	homebrewEnvironment,
	releaseAttemptMarker,
	superviseHomebrew,
	mintbrewApp,
	nativeHBLauncher,
	validateModsFolder,
	modloader,
	customHomebrewFolder,
};
