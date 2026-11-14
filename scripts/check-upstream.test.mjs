import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import {
	checkRemoteUpstream,
	classifyMac2jbPaths,
	renderMarkdownReport,
	verifyLocalPins,
} from "./check-upstream.mjs";

const REPO_ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

test("verifyLocalPins succeeds against repository manifest and docs", () => {
	const baseline = verifyLocalPins(REPO_ROOT);
	assert.match(baseline.mac2jb.pinnedSha, /^[0-9a-f]{40}$/);
	assert.equal(baseline.mac2jb.pinnedLabel, "v0.8.2");
	assert.ok(baseline.ecosystem.length >= 10);
});

test("verifyLocalPins rejects drifted parity ledger baseline", () => {
	const tmp = fs.mkdtempSync(path.join(os.tmpdir(), "any2jb-upstream-test-"));
	try {
		for (const rel of [
			"internal/manifest/profiles.json",
			"internal/plugins/upstream.go",
			"resources/runtime/bridge.cjs",
			"resources/runtime/toolhost.cjs",
			"docs/parity-ledger.md",
			"docs/external-projects.md",
			"go.mod",
		]) {
			const dest = path.join(tmp, rel);
			fs.mkdirSync(path.dirname(dest), { recursive: true });
			fs.copyFileSync(path.join(REPO_ROOT, rel), dest);
		}
		const ledgerPath = path.join(tmp, "docs/parity-ledger.md");
		const drifted = fs
			.readFileSync(ledgerPath, "utf8")
			.replace(/[0-9a-f]{40}/, "0000000000000000000000000000000000000000");
		fs.writeFileSync(ledgerPath, drifted);
		assert.throws(() => verifyLocalPins(tmp), /does not match profiles\.json/);
	} finally {
		fs.rmSync(tmp, { recursive: true, force: true });
	}
});

test("checkRemoteUpstream detects new mac2jb commits and classifies paths", async () => {
	const pinnedSha = "717022b8a91a713be41a02b5e131d3229dd8b00a";
	const latestSha = "9999999999999999999999999999999999999999";
	const baseline = {
		mac2jb: {
			repo: "cpypwn/mac2jb",
			upstream: "https://github.com/cpypwn/mac2jb",
			pinnedSha,
			pinnedLabel: "v0.8.2",
		},
		ecosystem: [
			{
				upstream: "https://github.com/Gezine/CivSwitch",
				revision: "10b38e9f5abfd13a4b0bec8af24b026e44567da3",
				components: ["civswitch"],
			},
		],
	};

	const responses = new Map([
		["https://api.github.com/repos/cpypwn/mac2jb", { default_branch: "main" }],
		["https://api.github.com/repos/cpypwn/mac2jb/commits/main", { sha: latestSha }],
		[
			"https://api.github.com/repos/cpypwn/mac2jb/releases/latest",
			{ tag_name: "v0.8.3", html_url: "https://github.com/cpypwn/mac2jb/releases/tag/v0.8.3" },
		],
		["https://api.github.com/repos/cpypwn/mac2jb/tags?per_page=5", [{ name: "v0.8.3" }]],
		[
			`https://api.github.com/repos/cpypwn/mac2jb/compare/${pinnedSha}...main`,
			{
				status: "ahead",
				ahead_by: 2,
				commits: [
					{
						sha: latestSha,
						html_url: `https://github.com/cpypwn/mac2jb/commit/${latestSha}`,
						commit: {
							message: "feat: bump OunceCake integration\ndetails",
							committer: { date: "2026-11-13T00:00:00Z" },
						},
					},
				],
				files: [
					{ filename: "packaging/host-profiles.json" },
					{ filename: "gui/ContentView.swift" },
					{ filename: "scripts/tool.cjs" },
				],
			},
		],
		["https://api.github.com/repos/Gezine/CivSwitch", { default_branch: "main" }],
		[
			"https://api.github.com/repos/Gezine/CivSwitch/commits/main",
			{ sha: "10b38e9f5abfd13a4b0bec8af24b026e44567da3" },
		],
	]);

	const fakeFetch = async (url) => {
		const payload = responses.get(url);
		if (!payload) {
			return { ok: false, status: 404, json: async () => ({}) };
		}
		return { ok: true, status: 200, json: async () => payload };
	};

	const report = await checkRemoteUpstream(baseline, {
		fetchImpl: fakeFetch,
		includeEcosystem: true,
	});
	assert.equal(report.mac2jb.hasUpdate, true);
	assert.equal(report.mac2jb.aheadBy, 2);
	assert.equal(report.mac2jb.latestTag, "v0.8.3");
	assert.deepEqual(report.mac2jb.pathGroups.packaging, ["packaging/host-profiles.json"]);
	assert.deepEqual(report.mac2jb.pathGroups.gui, ["gui/ContentView.swift"]);
	assert.deepEqual(report.mac2jb.pathGroups.scripts, ["scripts/tool.cjs"]);
	assert.equal(report.ecosystem[0].status, "up-to-date");

	const md = renderMarkdownReport(report);
	assert.match(md, /Update available/);
	assert.match(md, /v0\.8\.3/);
	assert.match(md, /upstream-mac2jb-sha:9999999999999999999999999999999999999999/);

	assert.deepEqual(classifyMac2jbPaths(["README.md"]).other, ["README.md"]);
});
