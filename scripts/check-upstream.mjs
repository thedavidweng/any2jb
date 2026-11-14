#!/usr/bin/env node
import crypto from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const MAC2JB_UPSTREAM = "https://github.com/cpypwn/mac2jb";
const MYGO_UPSTREAM = "https://github.com/egoist/mygo";
const EXCLUDED_UPSTREAMS = new Set([
	"any2jb",
	"https://nodejs.org",
	MYGO_UPSTREAM,
	"https://github.com/Jmesmykil/switch2-captive-portal-research",
]);

export function verifyLocalPins(repoRoot) {
	const manifestPath = path.join(repoRoot, "internal/manifest/profiles.json");
	const manifest = JSON.parse(fs.readFileSync(manifestPath, "utf8"));
	const entries = [...(manifest.profiles ?? []), ...(manifest.components ?? [])];

	const mac2jbRevisions = new Set(
		entries.filter((item) => item.upstream === MAC2JB_UPSTREAM).map((item) => item.revision),
	);
	if (mac2jbRevisions.size !== 1) {
		throw new Error(
			`Expected exactly 1 cpypwn/mac2jb revision in profiles.json, found ${mac2jbRevisions.size}: ${[...mac2jbRevisions].join(", ")}`,
		);
	}
	const [mac2jbSha] = [...mac2jbRevisions];
	if (!/^[0-9a-f]{40}$/.test(mac2jbSha)) {
		throw new Error(`Invalid cpypwn/mac2jb commit SHA in profiles.json: ${mac2jbSha}`);
	}

	const parityLedger = fs.readFileSync(path.join(repoRoot, "docs/parity-ledger.md"), "utf8");
	const baselineMatch = parityLedger.match(
		/^Baseline:\s*\[cpypwn\/mac2jb at ([0-9a-f]{40})(?:\s*\(([^)]+)\))?\]/m,
	);
	if (!baselineMatch) {
		throw new Error("Missing Baseline header in docs/parity-ledger.md");
	}
	if (baselineMatch[1] !== mac2jbSha) {
		throw new Error(
			`docs/parity-ledger.md baseline (${baselineMatch[1]}) does not match profiles.json (${mac2jbSha})`,
		);
	}
	const mac2jbVersionLabel = baselineMatch[2] ?? "";

	const externalProjects = fs.readFileSync(
		path.join(repoRoot, "docs/external-projects.md"),
		"utf8",
	);
	const externalMatch = externalProjects.match(
		/\[cpypwn\/mac2jb\]\(https:\/\/github\.com\/cpypwn\/mac2jb\)\s*\|\s*\[`([0-9a-f]{40})`\]/,
	);
	if (!externalMatch || externalMatch[1] !== mac2jbSha) {
		throw new Error(
			`docs/external-projects.md cpypwn/mac2jb SHA (${externalMatch?.[1] ?? "missing"}) does not match profiles.json (${mac2jbSha})`,
		);
	}

	const mygoRevisions = new Set(
		entries.filter((item) => item.upstream === MYGO_UPSTREAM).map((item) => item.revision),
	);
	if (mygoRevisions.size === 1) {
		const [mygoSha] = [...mygoRevisions];
		const goMod = fs.readFileSync(path.join(repoRoot, "go.mod"), "utf8");
		const goModMatch = goMod.match(/github\.com\/egoist\/mygo\s+\S+-([0-9a-f]{12})\b/);
		if (goModMatch && !mygoSha.startsWith(goModMatch[1])) {
			throw new Error(
				`go.mod mygo revision (${goModMatch[1]}) does not match profiles.json (${mygoSha})`,
			);
		}
	}

	const upstreamGo = fs.readFileSync(path.join(repoRoot, "internal/plugins/upstream.go"), "utf8");
	const civMatch = upstreamGo.match(/civHomebrewCommit\s*=\s*"([0-9a-f]{40})"/);
	const civManifest = entries.find(
		(item) => item.upstream === "https://github.com/XITRIX/CivHomebrew",
	);
	if (!civMatch || !civManifest || civMatch[1] !== civManifest.revision) {
		throw new Error("CivHomebrew commit mismatch between upstream.go and profiles.json");
	}

	for (const item of entries) {
		if (item.upstream === "any2jb" && item.path && item.sha256) {
			const diskPath = path.join(repoRoot, "resources", item.path);
			const digest = crypto.createHash("sha256").update(fs.readFileSync(diskPath)).digest("hex");
			if (digest !== item.sha256) {
				throw new Error(
					`Local runtime component ${item.id} (${item.path}) SHA-256 ${digest} does not match profiles.json (${item.sha256})`,
				);
			}
		}
	}

	const ecosystemMap = new Map();
	for (const item of entries) {
		if (
			!item.upstream ||
			item.upstream === MAC2JB_UPSTREAM ||
			EXCLUDED_UPSTREAMS.has(item.upstream)
		) {
			continue;
		}
		if (!ecosystemMap.has(item.upstream)) {
			ecosystemMap.set(item.upstream, {
				upstream: item.upstream,
				revision: item.revision,
				components: [item.id],
			});
		} else {
			const existing = ecosystemMap.get(item.upstream);
			if (existing.revision !== item.revision) {
				throw new Error(
					`Conflicting revisions for ${item.upstream}: ${existing.revision} vs ${item.revision}`,
				);
			}
			existing.components.push(item.id);
		}
	}

	return {
		mac2jb: {
			repo: "cpypwn/mac2jb",
			upstream: MAC2JB_UPSTREAM,
			pinnedSha: mac2jbSha,
			pinnedLabel: mac2jbVersionLabel,
		},
		ecosystem: [...ecosystemMap.values()].sort((a, b) => a.upstream.localeCompare(b.upstream)),
	};
}

export function classifyMac2jbPaths(files) {
	const groups = {
		packaging: [],
		scripts: [],
		gui: [],
		integrations: [],
		other: [],
	};
	for (const file of files) {
		const filename = typeof file === "string" ? file : file.filename;
		if (filename.startsWith("packaging/")) groups.packaging.push(filename);
		else if (filename.startsWith("scripts/")) groups.scripts.push(filename);
		else if (filename.startsWith("gui/")) groups.gui.push(filename);
		else if (filename.startsWith("integrations/")) groups.integrations.push(filename);
		else groups.other.push(filename);
	}
	return groups;
}

async function fetchJson(url, { fetchImpl = fetch, headers = {}, allow404 = false } = {}) {
	const res = await fetchImpl(url, { headers });
	if (allow404 && res.status === 404) {
		return null;
	}
	if (!res.ok) {
		throw new Error(`HTTP ${res.status} fetching ${url}`);
	}
	return res.json();
}

function githubHeaders(token) {
	const headers = {
		Accept: "application/vnd.github+json",
		"User-Agent": "any2jb-upstream-monitor",
		"X-GitHub-Api-Version": "2022-11-28",
	};
	if (token) {
		headers.Authorization = `Bearer ${token}`;
	}
	return headers;
}

export async function checkRemoteUpstream(
	baseline,
	{ fetchImpl = fetch, token = "", includeEcosystem = false } = {},
) {
	const ghHeaders = githubHeaders(token);
	const repoMeta = await fetchJson("https://api.github.com/repos/cpypwn/mac2jb", {
		fetchImpl,
		headers: ghHeaders,
	});
	const defaultBranch = repoMeta.default_branch || "main";
	const [headCommit, latestRelease, tags, comparison] = await Promise.all([
		fetchJson(
			`https://api.github.com/repos/cpypwn/mac2jb/commits/${encodeURIComponent(defaultBranch)}`,
			{ fetchImpl, headers: ghHeaders },
		),
		fetchJson("https://api.github.com/repos/cpypwn/mac2jb/releases/latest", {
			fetchImpl,
			headers: ghHeaders,
			allow404: true,
		}),
		fetchJson("https://api.github.com/repos/cpypwn/mac2jb/tags?per_page=5", {
			fetchImpl,
			headers: ghHeaders,
		}),
		fetchJson(
			`https://api.github.com/repos/cpypwn/mac2jb/compare/${baseline.mac2jb.pinnedSha}...${encodeURIComponent(defaultBranch)}`,
			{ fetchImpl, headers: ghHeaders, allow404: true },
		),
	]);

	const latestSha = headCommit.sha;
	const latestTag = latestRelease?.tag_name ?? tags?.[0]?.name ?? "";
	const status =
		comparison?.status ?? (latestSha === baseline.mac2jb.pinnedSha ? "identical" : "diverged");
	const aheadBy = comparison?.ahead_by ?? (latestSha === baseline.mac2jb.pinnedSha ? 0 : -1);
	const releaseChanged = Boolean(
		latestTag && baseline.mac2jb.pinnedLabel && latestTag !== baseline.mac2jb.pinnedLabel,
	);
	const hasUpdate = latestSha !== baseline.mac2jb.pinnedSha || releaseChanged;

	const commits = (comparison?.commits ?? []).slice(-20).map((c) => ({
		sha: c.sha,
		shortSha: c.sha.slice(0, 8),
		date: c.commit?.committer?.date ?? "",
		title: (c.commit?.message ?? "").split("\n")[0],
		url: c.html_url,
	}));
	const pathGroups = classifyMac2jbPaths(comparison?.files ?? []);

	const ecosystemResults = [];
	if (includeEcosystem) {
		for (const entry of baseline.ecosystem) {
			try {
				const ghMatch = entry.upstream.match(/^https:\/\/github\.com\/([^/]+\/[^/]+)$/);
				const glMatch = entry.upstream.match(/^https:\/\/gitlab\.com\/(.+)$/);
				if (ghMatch) {
					const slug = ghMatch[1];
					const isCommitPin = /^[0-9a-f]{40}$/.test(entry.revision);
					if (isCommitPin) {
						const info = await fetchJson(`https://api.github.com/repos/${slug}`, {
							fetchImpl,
							headers: ghHeaders,
						});
						const head = await fetchJson(
							`https://api.github.com/repos/${slug}/commits/${encodeURIComponent(info.default_branch || "main")}`,
							{ fetchImpl, headers: ghHeaders },
						);
						ecosystemResults.push({
							...entry,
							latestRevision: head.sha,
							updated: head.sha !== entry.revision,
							status: head.sha === entry.revision ? "up-to-date" : "ahead",
						});
					} else {
						const rel = await fetchJson(`https://api.github.com/repos/${slug}/releases/latest`, {
							fetchImpl,
							headers: ghHeaders,
							allow404: true,
						});
						const latest = rel?.tag_name ?? entry.revision;
						ecosystemResults.push({
							...entry,
							latestRevision: latest,
							updated: latest !== entry.revision,
							status: latest === entry.revision ? "up-to-date" : "ahead",
						});
					}
				} else if (glMatch) {
					const projectId = encodeURIComponent(glMatch[1]);
					const commitsList = await fetchJson(
						`https://gitlab.com/api/v4/projects/${projectId}/repository/commits?per_page=1`,
						{ fetchImpl, headers: { "User-Agent": "any2jb-upstream-monitor" } },
					);
					const latest = commitsList?.[0]?.id ?? entry.revision;
					ecosystemResults.push({
						...entry,
						latestRevision: latest,
						updated: latest !== entry.revision,
						status: latest === entry.revision ? "up-to-date" : "ahead",
					});
				}
			} catch (err) {
				ecosystemResults.push({
					...entry,
					latestRevision: "unknown",
					updated: false,
					status: `error: ${err instanceof Error ? err.message : String(err)}`,
				});
			}
		}
	}

	return {
		checkedAt: new Date().toISOString(),
		mac2jb: {
			repo: "cpypwn/mac2jb",
			defaultBranch,
			pinnedSha: baseline.mac2jb.pinnedSha,
			pinnedShortSha: baseline.mac2jb.pinnedSha.slice(0, 8),
			pinnedLabel: baseline.mac2jb.pinnedLabel,
			latestSha,
			latestShortSha: latestSha.slice(0, 8),
			latestTag,
			latestReleaseUrl: latestRelease?.html_url ?? "",
			compareUrl: `https://github.com/cpypwn/mac2jb/compare/${baseline.mac2jb.pinnedSha}...${latestSha}`,
			status,
			aheadBy,
			releaseChanged,
			hasUpdate,
			commits,
			pathGroups,
		},
		ecosystem: ecosystemResults,
	};
}

export function renderMarkdownReport(report) {
	const m = report.mac2jb;
	const lines = [
		"## Upstream `cpypwn/mac2jb` Status",
		"",
		`- **Status:** ${m.hasUpdate ? "Update available" : "Aligned with upstream"}`,
		`- **Pinned baseline:** [\`${m.pinnedShortSha}\`](https://github.com/cpypwn/mac2jb/tree/${m.pinnedSha})${m.pinnedLabel ? ` (\`${m.pinnedLabel}\`)` : ""}`,
		`- **Upstream \`${m.defaultBranch}\`:** [\`${m.latestShortSha}\`](https://github.com/cpypwn/mac2jb/commit/${m.latestSha})${m.latestTag ? ` (latest tag: \`${m.latestTag}\`)` : ""}`,
	];
	if (m.hasUpdate) {
		lines.push(
			`- **Compare:** [${m.pinnedShortSha}...${m.latestShortSha}](${m.compareUrl}) (${m.status}, ahead by ${m.aheadBy >= 0 ? m.aheadBy : "diverged"})`,
		);
		lines.push("");
		lines.push("### Touched Subsystems");
		lines.push("");
		for (const [group, paths] of Object.entries(m.pathGroups)) {
			if (paths.length > 0) {
				lines.push(
					`- **\`${group}\`** (${paths.length}): ${paths
						.slice(0, 8)
						.map((p) => `\`${p}\``)
						.join(", ")}${paths.length > 8 ? ", ..." : ""}`,
				);
			}
		}
		if (m.commits.length > 0) {
			lines.push("");
			lines.push("### Recent Upstream Commits");
			lines.push("");
			for (const c of m.commits) {
				lines.push(`- [\`${c.shortSha}\`](${c.url}) ${c.title}`);
			}
		}
	}

	if (report.ecosystem.length > 0) {
		lines.push("");
		lines.push("### Pinned Sub-Tool & Host Upstreams");
		lines.push("");
		lines.push("| Upstream | Pinned | Latest | Status |");
		lines.push("| --- | --- | --- | --- |");
		for (const item of report.ecosystem) {
			const pinned = /^[0-9a-f]{40}$/.test(item.revision)
				? `\`${item.revision.slice(0, 8)}\``
				: `\`${item.revision}\``;
			const latest = /^[0-9a-f]{40}$/.test(item.latestRevision)
				? `\`${item.latestRevision.slice(0, 8)}\``
				: `\`${item.latestRevision}\``;
			lines.push(`| ${item.upstream} | ${pinned} | ${latest} | ${item.status} |`);
		}
	}

	lines.push("");
	lines.push(`<!-- upstream-mac2jb-sha:${m.latestSha} -->`);
	return `${lines.join("\n")}\n`;
}

function parseArgs(argv) {
	const opts = {
		offline: false,
		ecosystem: false,
		json: false,
		markdown: false,
		failOnUpdate: false,
		output: "",
		markdownOutput: "",
	};
	for (let i = 0; i < argv.length; i++) {
		const arg = argv[i];
		if (arg === "--offline") opts.offline = true;
		else if (arg === "--ecosystem") opts.ecosystem = true;
		else if (arg === "--json") opts.json = true;
		else if (arg === "--markdown") opts.markdown = true;
		else if (arg === "--fail-on-update") opts.failOnUpdate = true;
		else if (arg === "--output") opts.output = argv[++i] ?? "";
		else if (arg === "--markdown-output") opts.markdownOutput = argv[++i] ?? "";
		else throw new Error(`Unknown argument: ${arg}`);
	}
	return opts;
}

async function main() {
	const opts = parseArgs(process.argv.slice(2));
	const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
	const baseline = verifyLocalPins(repoRoot);

	if (opts.offline) {
		if (opts.json) {
			process.stdout.write(`${JSON.stringify(baseline, null, 2)}\n`);
		} else {
			console.log(
				`Verified local upstream pins: cpypwn/mac2jb @ ${baseline.mac2jb.pinnedSha.slice(0, 8)} (${baseline.mac2jb.pinnedLabel || "unlabeled"}), ${baseline.ecosystem.length} ecosystem upstreams consistent.`,
			);
		}
		return;
	}

	const token = process.env.GITHUB_TOKEN || process.env.GH_TOKEN || "";
	const report = await checkRemoteUpstream(baseline, {
		token,
		includeEcosystem: opts.ecosystem,
	});
	const markdown = renderMarkdownReport(report);

	if (opts.output) {
		fs.writeFileSync(opts.output, `${JSON.stringify(report, null, 2)}\n`);
	}
	if (opts.markdownOutput) {
		fs.writeFileSync(opts.markdownOutput, markdown);
	}

	if (opts.json) {
		process.stdout.write(`${JSON.stringify(report, null, 2)}\n`);
	} else if (opts.markdown) {
		process.stdout.write(markdown);
	} else {
		process.stdout.write(markdown);
	}

	if (opts.failOnUpdate && report.mac2jb.hasUpdate) {
		process.exitCode = 2;
	}
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
	main().catch((err) => {
		console.error(err instanceof Error ? err.message : String(err));
		process.exit(1);
	});
}
