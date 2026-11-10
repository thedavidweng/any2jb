# Retained RPC lease and tools

A verified session on a capable host keeps its RPC lease, so the read-only tools
run without rebuilding the exploit. The lease belongs to one verified run: tools
are offered only when the pinned profile declares `supportsHeldRPC`, and every
tool re-checks the session's private evidence and asks the live loopback
endpoint before it starts. A stale `result.json`, an open port or an earlier
success is never accepted as a lease.

## Ownership, attaching and loss

`retained.Manager` is the single owner of lease state. It implements the
controller's retained adapter and its tool runner:

- **Launch** is the ordinary flow: this app prepares the host, verifies the held
  contract and then owns the workspace through `engine.HostRunner.Workspace`.
- **Attach** is explicitly different. `Discover` lists `runs/run-*` sessions on
  this Mac whose evidence and live endpoint still verify; `AttachRetained`
  re-verifies the chosen one. It never prepares a host, types a command or takes
  the owner's lease, and the UI says so before and after connecting.
- **Detach** releases only this app's own tool children and attachment. The
  original host keeps running, and only the app that launched it can Finish it.
  `Finish` is refused while attached.
- A lost lease is reported, never retried. For an owned run the controller's
  required `HostRunner.Wait` watcher invalidates it; for an attachment
  `Manager.Watch` re-checks the evidence and live endpoint every two seconds.
  Either loss cancels tool work, clears the attachment and requires a console
  restart. No reconnection is attempted.
- Only one tool owns the session at a time. A second request is refused naming
  the current owner, and the disabled control states the same reason.

## Tools

| Tool | What runs | Boundary |
| --- | --- | --- |
| `rpc-status` | One fresh loopback status request | Point-in-time, not permission to reconnect |
| `system-code-dump` | The staged engine's own CodeFS capture module | Private empty destination in `verified-exports`, outside any session workspace |
| `ouncecake-webdav` | Pinned OunceCake engine modules | The console's own service at `http://<console>:2121/` |
| `ouncecake-ftp` | OunceCake plus mac2jb's FTP bridge | Binds `127.0.0.1` on this Mac only |
| `sd-browser` | Pinned Super File Bros. modules | The console's FTP service, `/sd` only, `ftp://<console>:2122-2199/sd/` |
| `savekit-read` | Pinned ns2-savekit read-only entrypoints through `retained/savekit-host.cjs` | Read-only: accounts, titles, archive inventory, inspect, verified backup export and timestamps |
| `savekit` | Pinned ns2-savekit write/delete entrypoints through the same host | The selected archive only: gated write, restore and delete, plus the read-only live-vs-export compare |

No save, mod, launcher or console filesystem write exists outside the gated
SaveKit path, and the SD export wrapper exposes only `list`, `stat`,
`openRead`, `read` and `close` under `/sd`. Go refuses any endpoint
outside the boundary above, so a tool cannot publish a LAN service of its own.
The capture's inventory, file sizes and SHA-256 digests are read back and
verified after it completes; a partial capture is kept and not retried.

The `savekit` mutation gate runs, in order, per operation:

1. **Preview and confirmation.** The view previews the affected files locally
   (write: a flat source folder; restore: a verified export; delete: the exact
   file name), states the target title, account and archive, and requires a
   confirmation that covers exactly one operation. `savekit.Validate` binds
   the preview's SHA-256 digest to the request, so the host module never sees
   a mutation without it; the manager recomputes the digest at run time, and a
   source that changed since the preview fails before anything runs.
2. **Target validation.** A fresh verified backup of the target archive is
   pulled first and its export manifest identity must match the requested
   title, account and archive; a mismatch aborts before any change and says
   why. The write and delete accept only the default account archive (space 1,
   type 1, rank 0, index 0); the restore accepts a rank 0, index 0 Account or
   Device archive whose export identity matches.
3. **Backup verification.** The backup is verified again immediately before
   the mutation. A backup failure aborts the operation, reports why, and
   blocks every further save mutation for the rest of the session.
4. **Post-write verification.** The archive is pulled again and compared
   against the intended result (write: exactly the previewed files, extra live
   files preserved; delete: exactly one file removed). The outcome reports
   verified or failed; it is never assumed. There is no automatic retry: a
   failure keeps the verified backup accessible with recovery instructions,
   and the session's save mutations stay blocked until a new session.
   The compare action is read-only; it only pulls the live save and reports
   match or difference, and a failed compare blocks nothing.

The OunceCake export offers the console's installed titles and accepts exactly
one of them. A game outside the offered catalog, a second selection or an
unrequested path is refused.

## Distribution and local developer staging

`resources/darwin-arm64/runtime/toolhost.cjs` is original any2jb code, recorded
as `tool-host` and bundled. The tool implementations are not: `ouncecake` and
`sd-browser` are recorded in [`manifest/profiles.json`](../manifest/profiles.json)
with their upstream, revision, inventory digest and `blocked` decision, and the
system-code capture lives inside the blocked host profiles. A packaged app
stages none of them, and every Advanced Tools entry shows
`DistributionFor(<component>)`'s reason instead of an action. The excluded
browser-research probe set shows its reason too and has no control at all.

`retained.StageTools` is the explicit developer path, mirroring
[runtime staging](runtime-staging.md): it verifies the inventory and every
recorded file in a pinned mac2jb clone, copies exactly those files into
`<app support>/tools/<version>/tools/<component>`, and verifies the copy before
any tool may require it. `savekit` is recorded in the `selected-files` format:
the component path is a digest-verified notice and `extra` names all sixteen
staged files: the read-only entrypoints, the write and delete entrypoints
ticket 18's gate requires, and their shared libraries. `stage.json` marks the stage developer-staged and not
redistributable, and the tool host refuses to run without it. A changed or
unrecorded file fails the stage and leaves that tool unavailable with its
recorded reason; it is never silently repaired.

## Tests

- `retained/manager_test.go` drives the structured engine fixture's
  `held-verified` scenario on loopback: a tool without a lease, a tool after the
  lease closed, the single-owner refusal, attach versus launch, detach leaving
  the host running, a lost attachment, an endpoint outside the stated boundary,
  a capture refused inside the workspace, and the offered-game rule.
- `retained/stage_test.go` checks that a verified copy is staged, that a changed
  pinned source is refused and its stale stage removed, and that packaged
  startup stages nothing.
- `retained/toolhost.test.cjs` checks the tool host's own held proof (finished,
  public and foreign sessions are refused) and that the SD wrapper has no write
  operation.
- `retained/savekit_test.go` drives the fixture host module through the manager:
  the lease requirement, the read-only list, inspect and verified-backup flow,
  and the single-owner refusal in both directions. `retained/savekit-host.test.cjs`
  covers the request schema (unknown fields rejected, mutations needing the
  confirmation flag), the argv builder and the inventory parsers.
- `retained/savekit_write_test.go` drives the write gate on the same fixture:
  the backup/write/read-back order, backup-failure abort with its session
  block, target-validation abort that stays correctable, the replacement and
  restore guards, a post-write verification failure with no retry, the
  read-only compare, and the single-owner refusal between read and write.
  `savekit/client_mutation_test.go` covers the mutation result schema and
  `savekit/mutation_gate_test.go` the previews, digests, limits and compares.
- `savekit/` validates requests, runs the host module's one-request protocol and
  verifies every export: `savekit/export_test.go` refuses a tampered or partial
  export, an unrecorded file and an escaping path. A verified backup is kept
  under `savekit-exports` and never retried.
- `manifest/tools_test.go` and the opt-in
  `ANY2JB_REFERENCE_ROOT` inventory test check the recorded tool sources,
  including against the pinned mac2jb clone.
- `advanced_tools_view_test.go` exercises the gated reasons, the one-owner
  disabled states, the lost-lease message and the attach/detach distinction
  through MyGo's windowless Tester.

## Limits

No console, USB or hardware behavior was verified. The fixtures are structured
processes: they prove the lease rules, refusals, boundaries and teardown of this
app, not that an upstream export or capture works against a real Switch 2.
Running the real OunceCake, SD browser or capture modules needs a developer
stage of blocked sources and a live console, which remains unverified; the same
holds for the real ns2-savekit entrypoints and their title-name lookup, which
the fixture and protocol seam cover instead. The `rpc-copy` row (copying the
exact RPC command) and the OunceCake ZIP download are not implemented.
