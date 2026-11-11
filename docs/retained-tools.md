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
   the preview's SHA-256 digest to the request. The manager recomputes the
   digest at run time, and the host module checks it again against its private
   staged copies just before the pinned tool runs, so a source that changed
   after the preview is refused before any console change.
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

## Launcher save installers

A launcher save replaces a game's own startup code, so the installers are the
gated SaveKit write and nothing else: there is no second write path. Each
package is recorded in [`manifest/profiles.json`](../manifest/profiles.json)
with its upstream, revision, SHA-256, file count, console title and core file
names, and all ten are `blocked`: Artemis supplies no license at its pinned
revision, and the editor, Balatro and Gravity Circuit archives still need their
own grant and bundled-data review. A packaged app stages none of them and every
installer shows its recorded reason instead of an action; a developer stage
(`retained.StageTools`) is the only way to have the bytes present at all.

One install runs, in order:

1. **Restrictions.** `launcher.Validate` accepts a package only for the game,
   edition and version it supports: the demo packages only for their own demo
   title, the full-game Seedsow patch only for the full-game title, Balatro only
   after the user states the physical Switch 1 cartridge in Switch 2
   compatibility mode (the native Switch 2 edition is unsupported), and Gravity
   Circuit only for game version 1.0.0 or 1.2.1.
2. **Package verification and preview.** `launcher.Prepare` re-verifies the
   recorded source files and the archive digest, refuses an archive whose paths,
   entry types, sizes or file count differ from the record or whose own save
   metadata names another title, unpacks it into a private folder off the UI
   thread, and previews exactly those files. The full-game option instead
   patches the user's own verified full-game export with the pinned upstream
   patch module: the demo package is never written to the full game.
3. **Account choice.** The installer writes only to an account the console's own
   listing reported, and when more than one account exists none is assumed: the
   owning account is chosen explicitly.
4. **The write gate.** The confirmed preview digest, target validation, verified
   backup, single write and post-write verification are ticket 18's gate,
   unchanged. The manager prepares the package again at run time, so the digest
   binds the confirmation to the files that are actually written.
5. **Existing launcher.** `launcher.CheckExisting` compares the live save
   against the package's recorded core files. No core file present means a fresh
   install; all of them matching the pinned package means an upgrade; anything
   else (unrecognised or partial core files) is refused before any change, with
   the verified backup kept.

The install states its persistent side effects before it runs: the game's
startup code stays replaced until the backup is restored, and the installed
launcher opens its own LAN services (Artemis HTTP 8080, or Lua 9026 with FTP
5000 and 5001) whenever the game runs. That runtime is separate from the held
RPC session and keeps running after Finish.

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
- **The controller stage returns to `retained_rpc` once the app is running.**
  A running homebrew app, like a read-only endpoint, is a long-lived child the
  manager owns; the `running_tool` stage covers only the launch job. The
  single-active rule is enforced by the manager's owner check and the UI's
  owner text, not by the stage. Finish and Disconnect stop the owned app (and
  check its cleanup proof) before they release the session, so offering them
  while an app runs cannot leave it orphaned.
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
## Modloader and custom homebrew (experimental)

Both start only from a verified lease and check the user's folder in Go
(`session/folders.go`) before anything spawns; the tool host checks it again
before any transfer. An invalid folder is refused with `invalid_configuration`
and the session stays `retained_rpc`. Nothing game- or mod-related is bundled.

- **Modloader** is offered only on profiles whose manifest declares
  `supportsModloader` (Kurumuz and Cellenseres; not Astral) and the tool host
  refuses other profiles. The folder is an owned, symlink-free tree of at most
  20000 files: `contents/<16-digit Title ID>/` with `romfs/`, `exefs/` (files
  only), `romfs.bin`, `exefs.nsp`, notes, and `exefs_patches/<set>/<Build
  ID>.ips`. Runtime cheats or plugins are refused rather than partly
  installed. The pinned `ns2jb-modloader` code and the mods are copied
  through no-follow descriptors into a private folder of the run, then its
  installer runs as the **final action**. Failures up to that point are
  retry-safe. A verified handoff (`READY`, the install lock and the resident
  proof) sets `ModloaderInstalled`: the overlay stays until the console
  restarts, so every later tool launch is refused and the UI says to finish,
  close SmileBASIC without restarting, launch the game, and restart before
  any other tool. An unverified handoff blocks the modloader and homebrew and
  advises a restart.
- **Custom homebrew** sits under the Homebrew section, labelled experimental.
  This build runs only a built GPU app folder (`app.json` with a lowercase id
  and a name) inside the pinned MintBrew pipeline from a fresh per-session
  copy, with the same attempt-marker protocol. RPC packages, IZlauncher
  modules, native ABI 3 apps (use the native launcher's app folder) and
  custom launchers are refused with their reason.

`modloader` is recorded in the manifest with an embedded inventory of
`integrations/ns2jb-modloader` and a `blocked` decision (GPL v2 obligations
for its prebuilt native artifacts are unresolved); custom homebrew needs the
blocked `mintbrew` and `homebrew-runtime` stages. Tests:
`session/modloader_test.go` (capability gate, folder layouts, lease kept),
`retained/modloader_test.go` (recorded reasons, completion lock, retry-safe
and unverified failures), the modloader and custom homebrew fixtures in
`retained/toolhost.test.cjs`, and `modloader_view_test.go`. No modloader or
custom homebrew run against a console; hardware behavior is not verified.
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
  operation.
- `retained/savekit_test.go` drives the fixture host module through the manager:
  the lease requirement, the read-only list, inspect and verified-backup flow,
  and the single-owner refusal in both directions. `retained/savekit-host.test.cjs`
  covers the request schema (unknown fields rejected, mutations needing the
  confirmation flag), the argv builder and the inventory parsers.
- `retained/launcher_test.go` drives one install through the fixture host: the
  gate order (`accounts`, backup, write, read-back), the refusal of an
  unrecognised existing launcher before any change, and the recorded reason for
  every packaged installer. `launcher/launcher_test.go` covers the per-package
  restrictions and the rights gate, `launcher/prepare_test.go` the digest
  refusal, unsafe archives and the existing-launcher rule, and
  `launcher_view_test.go` the gated reason, stated side effects, per-install
  preview and confirmation and the account rule through MyGo's Tester. The
  opt-in `ANY2JB_REFERENCE_ROOT` check in `launcher/reference_test.go` prepares
  every pinned package from the mac2jb clone.
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
 `manifest/tools_test.go` and the opt-in
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
stage of blocked sources and a live console, which remains unverified; the same
holds for the launcher saves: no package was installed on a console, and the
full-game autosave patch in particular ran only against the pinned upstream
module's own inputs, never a real full-game save. The same
holds for the real ns2-savekit entrypoints and their title-name lookup, which
the fixture and protocol seam cover instead. The `rpc-copy` row (copying the
exact RPC command) and the OunceCake ZIP download are not implemented.
