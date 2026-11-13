# Diagnostics and local data

Diagnostics is a local, read-only view of the selected run. It polls off the UI
thread every 500 ms and publishes through `Window.Update`. A radio group, Log
source, selects All, Host (the default), USB and typing, Pipeline, or Acquisition. Follow keeps the log scrolled
to its newest output; turning it off allows selection and copying.

## Sources and privacy

Only these files are collected:

| Stream | Private workspace source |
| --- | --- |
| Host | `host.log`, validated bridge lifecycle events without START parameters |
| USB and typing | `usb.log`, supplied by the input adapter |
| Pipeline | `run.log`, upstream pipeline stdout/stderr |
| Acquisition | `switch2/acquisition.log`, upstream acquisition output |

No output means no evidence from that source, not a successful operation.
Each source is bounded to its newest 256 KiB. Truncated leading lines are omitted.
Capture reads regular files under an `os.Root`; a log symlink is refused.

The view removes pairing/START commands, tokens, keys, account/user identifiers,
credentials and save-data fields. JSON admits only diagnostic fields; unknown
fields and malformed JSON fragments are omitted. Long hex/numeric identifiers
and home-directory paths, IPv4 and MAC addresses, and serial-shaped identifiers
are masked. This is not a promise that arbitrary
upstream prose is safe to publish. Private workspaces and their raw logs must
not be shared.

Export creates a private ZIP in the displayed `support-bundles` directory.
`diagnostics.json` has an explicit allowlist: schema version, known controller
stage/recovery enums, numeric progress, boolean verification, platform/CPU,
canonical manifest host/revision and per-source line counts/fixed observation
classifications. It never serializes a snapshot or copies free-form text.
Unknown fields cannot enter a bundle. Raw logs, addresses, paths, process/run IDs,
account data, pairing artifacts, binary files and save exports are excluded.
The ZIP is local; nothing is uploaded.

## Lifetime and recovery

The runner captures redacted logs into `diagnostics/<run-id>` before teardown.
Automatic cleanup preserves `backups`, `savekit-exports`, `verified-exports` and
`dumps` by moving them to `past-runs/<workspace-name>` before deleting temporary
engine workspaces. Tool adapters must put intentionally retained artifacts in
those directories or outside the workspace. Retention errors stop deletion.
The read-only system-code capture writes into `verified-exports/system-code-<run
id>`, outside every session workspace, and must find that destination empty.

The bridge's stdin-EOF cleanup also preserves those directories and private log
sources, and writes `session-interrupted` before deleting pairing artifacts.
The next app can view the retained logs without adopting any pairing or outcome.

App startup treats any leftover `runs` entry, `session.lock` or
`session-interrupted` marker as interrupted.
`SessionController.MarkInterrupted` clears all verification and enters
`indeterminate_console_restart_required`. No connection/result artifact is
read to restore a session or RPC lease.

After the user confirms a console restart, the confirmation command must call
`SessionController.ReleaseInterrupted(ctx)` before leaving that state.
`PlatformServices.ReleaseInterrupted` is required, including in fakes.
Production compares `session.lock` with private `session-owner.json`, and uses
signal 0 to prove that the owning app PID is gone. A live PID, PID reuse, unknown
ownership or an unsupported platform blocks release. It never kills a process.
An OS advisory recovery lock serializes recovery and releases on process exit.
Orphaned runs move to `past-runs`, then the stale lease is released. With no
lease, an exclusive temporary lease prevents a concurrent launch during the move.
Ownerless leases from pre-release builds fail closed.

## User cleanup

Preview lists exact relative paths. Confirmation rechecks that list and obtains
the runner's exclusive lease before deleting anything. Any active or unknown
lease blocks cleanup. Symlinks and nonregular files are not eligible.

Only `host.log`, `run.log`, `usb.log`, `diagnostics-id`, `connect.txt`,
`connection.json`, `result.json`, `switch2/pipeline.json` and
`switch2/acquisition.log` under inactive `runs/run-*` or `past-runs/run-*` are
eligible. Unknown files, backups, verified exports, dumps, installed runtime
assets, retained redacted diagnostics and support bundles are kept.

## Evidence and limits

`internal/diagnostics/*_test.go` checks every recognized sensitive field, unknown JSON
fields, malformed fragments, source separation, bounded following, symlink
refusal, backup-preserving cleanup, dead/mismatched/live owner recovery and
stale artifact detection. `internal/engine/diagnostics_test.go` uses the real supervised
Node fixture to check retained evidence and backups. `internal/session/recovery_test.go`
checks that stale state cannot launch. `internal/ui/diagnostics_view_test.go` exercises source
selection, following and cleanup confirmation through MyGo Tester.

These are synthetic process/UI fixtures, not console or USB hardware evidence.
USB adapters must write their private `usb.log`; no USB events are fabricated.
Native folder opening and physical console restart were not hardware verified.
