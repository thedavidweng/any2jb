# Retained RPC lease and read-only tools

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

## Read-only tools

| Tool | What runs | Boundary |
| --- | --- | --- |
| `rpc-status` | One fresh loopback status request | Point-in-time, not permission to reconnect |
| `system-code-dump` | The staged engine's own CodeFS capture module | Private empty destination in `verified-exports`, outside any session workspace |
| `ouncecake-webdav` | Pinned OunceCake engine modules | The console's own service at `http://<console>:2121/` |
| `ouncecake-ftp` | OunceCake plus mac2jb's FTP bridge | Binds `127.0.0.1` on this Mac only |
| `sd-browser` | Pinned Super File Bros. modules | The console's FTP service, `/sd` only, `ftp://<console>:2122-2199/sd/` |

There is no write path in this ticket: no save, mod, launcher or console
filesystem write exists in the tool host, and the SD export wrapper exposes only
`list`, `stat`, `openRead`, `read` and `close` under `/sd`. Go refuses any
endpoint outside the boundary above, so a tool cannot publish a LAN service of
its own. The capture's inventory, file sizes and SHA-256 digests are read back
and verified after it completes; a partial capture is kept and not retried.

The OunceCake export offers the console's installed titles and accepts exactly
one of them. A game outside the offered catalog, a second selection or an
unrequested path is refused.

## Homebrew launching (experimental)

From the same verified lease the Homebrew section offers MintBrew, the native
HBLauncher (with MintNES) and the experimental IZlauncher. They are not
read-only: each one allocates console graphics and audio, so the contract is
stricter than the read-only tools.

- **One owner at a time.** Launching a second homebrew while one runs is
  refused naming the owner; switching means stopping the active app and
  waiting for its proven cleanup. Switching is experimental and the UI says so.
- **Cleanup is proven, not assumed.** The tool host writes an exclusive
  attempt marker in the session workspace before any launch and verifies it
  again at stop: the owner children must confirm their own shutdown
  (`launcher-auto-loop.json`, `game-audio.json` or the IZlauncher frame
  proofs), the lease must still be live and the marker must be unchanged. Only
  then is the marker released and another launch allowed. An unproven cleanup
  keeps the marker, blocks every later homebrew launch with its stated reason
  and advises a console restart; the session controller surfaces it as
  `indeterminate_console_restart_required`. A failure before any console
  change is retry-safe: the unchanged marker is removed and the lease stays
  usable.
- **Every child speaks to the console through a loopback RPC proxy** that
  re-checks the live lease on each request and forwards only the documented
  bridge commands, never to the console over the LAN.

| Launcher | What runs | Input |
| --- | --- | --- |
| `mintbrew` | The pinned MintBrew stages and owner scripts, with the default Stremio configuration (no add-ons) | None |
| `native-hblauncher` | The pinned MIT fork's host with any2jb adapters: held proof, loopback requests, developer engine sources | Optional SD ROM folder under `/sd` (mapper-0 iNES only; default `/sd/roms/nes`) and an optional existing app folder on this Mac |
| `izlauncher` | The pinned IZ2S-Platform stages, frame and catalog proofs, RPC drain | Up to 15 distinct module ids from the staged catalog |

The native launcher's MintNES reads the any2jb-owned Mac ROM folder
(`<app support>/MintNES/roms`) and, when the SD browser component is staged,
the chosen SD folder read-only. No commercial ROMs or game assets are bundled;
the one open-source ROM shipped by MintNES (Diamond Chase, MIT, ROM-LICENSE)
is digest-pinned in the `mintbrew-native` component inventory.

All seven homebrew components (`mintbrew`, `mintbrew-native`, `izlauncher`,
its three modules and `homebrew-runtime`) are recorded with `blocked`
distribution decisions: MintBrew's rights review is unresolved, the IZ2S
platform is GPL-2.0-or-later with unresolved inherited rights, and the
portable runtime bundles unreviewed third-party interpreters. A packaged app
stages none of them; each gated launcher shows its recorded reason. The
IZlauncher modules share one staged destination, and a developer stage
additionally prepares the portable runtime (CPython, shader tools, font) and
the IZlauncher runtime, each re-verified against its manifest before use.
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
any tool may require it. `stage.json` marks the stage developer-staged and not
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
  operation. Its loopback homebrew fixtures run the full MintBrew and
  IZlauncher lifecycles against a fake bridge and staged tree: proven cleanup
  releasing the marker, a second owner refused, a retry-safe early failure,
  an unproven shutdown keeping the marker, module validation and the process
  qualification gate.
- `retained/homebrew_test.go` covers the manager-side contract: recorded
  reasons per launcher, the second-owner refusal, stop-then-switch and the
  unproven-cleanup block.
- `session/homebrew_test.go` covers launch input validation (module ids, the
  SD folder shape, the Mac app folder).
- `homebrew_view_test.go` exercises the gated reasons, single-active
  switching, module selection and folder inputs through MyGo's windowless
  Tester.
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
stage of blocked sources and a live console, which remains unverified. The
`rpc-copy` row (copying the exact RPC command) and the OunceCake ZIP download
are not implemented. The homebrew supervisors were verified only against
loopback fixtures: no real MintBrew, HBLauncher or IZlauncher launch ran
against a console, and the retry-safe and cleanup-blocked paths are proven by
fixtures, not hardware. Preparing the portable homebrew runtime and the
IZlauncher runtime needs network access and a C toolchain and stays a
developer-only step.
