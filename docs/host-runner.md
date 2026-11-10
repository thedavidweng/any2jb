# Host runner

`engine.NewHostRunner(engine.Options)` is the production adapter. Every Prepare
requires explicit developer source/profile options, successful `Stage`, matching
`ReadRecord` provenance and `VerifyProfile`. Packaged startup leaves the developer
options empty, so blocked engines cannot launch. No environment variable silently
authorizes an engine. A developer harness must explicitly supply those options.

The original any2jb Node bridge imports the selected pinned host's connection,
NativeClient, qualification and local bridge APIs. It never invokes upstream
`run.cjs` main or a mac2jb GUI wrapper. It copies only `engine/shared` and
`engine/switch2` into the private workspace and makes those copies writable.
The sealed runtime remains unchanged. The pipeline and its children inherit the
owned process group. Raw engine output stays in the private run log.

## Protocol and verification

Every JSON line contains `type`, `sequence` (starting at 1) and `runID`. Ordered
messages are `ready` (exact `command`), `connected`, four `stage` messages
(`stage: 1..4`), then `result`, or `held` followed by `result` on Finish.
`failed` is terminal from any unfinished state. Lines are bounded to 65536 bytes.
Unknown fields/types, wrong IDs/sequences, malformed/truncated lines, stage
reordering and invalid START arity/IP/port are errors, never guessed progress.

`Pairing(PreparedHost)` gives input adapters ephemeral, run-bound command and
workspace details. Do not place these in snapshots, diagnostics or support logs.
Pairing publication is conservatively a side-effect boundary.

Final verification requires a clean process exit, no result/cleanup errors,
completion, and strict `verified`, `fs.verified`, `fs.restored` and
`fs.cleanup.cleanedUp`. Stage 4 only means the final work began.

Held verification instead requires a live process, verified/restored filesystem,
ready retained endpoint, deferred cleanup, cleaned donor and listening held RPC.
The endpoint and connection must name the workspace session and `switch2` target.
Result/connection files must be private regular files owned by the current user,
at most 8 MiB. A fresh loopback RPC status request must report ready. Finish sends
one structured release request, then requires final cleanup, closed RPC and a
clean exit. The controller's required Wait watcher invalidates a crashed lease.

## Ownership and teardown

Prepare creates an exclusive `session.lock` and fresh `runs/run-*` workspace.
Another runner using the same app-support path is refused. Failed Prepare cleans
its own resources. Only the matching run can remove its lease. A stale lease is
not adopted or automatically erased on startup.

Darwin/Linux use a separate process group, SIGTERM, then SIGKILL after two seconds
for non-held runs or up to 300 seconds for held-capable runs. Remaining
grandchildren are killed even if their leader already exited. Cancellation
shortens the grace but still waits for process teardown before deleting files.
The bridge also observes stdin EOF when the owning app dies. It releases held
resources, removes only its own lease/workspace and kills its remaining group.
An owner-crash fixture kills the parent with SIGKILL and checks that no bridge,
lease or workspace remains.

The runner records its app PID separately from the run-ID lease for guarded
relaunch recovery. [Diagnostics](diagnostics.md) captures redacted evidence
before workspace deletion and preserves intentional backup/export directories.
After an interrupted app, explicit console-restart confirmation can release only
a lease whose matching owner PID is proven dead. No stale session is adopted.

## Fixture seam

`NewProtocolRunner(support, CommandFactory)` is the explicit trusted-process test
seam. It does not perform production engine authorization and is never used by
the app entrypoint. Ticket 07's harness can launch
`engine/testdata/fake-engine.cjs <scenario> <run-id> <workspace>` with a test Node.
Use LAN `192.0.2.1`, port `39380`, five START arguments. This fixture emits the
same structured protocol, not a console/exploit emulator.

Scenarios: `verified`, `partial`, `cleanup-error`, `dirty-exit`, `bind-failure`,
`crash`, `disconnect`, `protocol-error`, `truncated`, `out-of-order`, `waiting`
(before readiness), `ready-wait` (after publication), `grandchild` (ignores
SIGTERM), `held-verified`, `held-dead-rpc`, `held-wrong-session` and
`held-public-artifact`. Held fixtures serve real loopback status and accept
`{"type":"finish"}` on stdin before publishing final cleanup.

## Checks

```sh
go test ./...
CGO_ENABLED=0 go test ./...
go test -race ./engine ./session
node --test engine/*.test.cjs manifest/profiles.test.cjs
go tool mygo vet .
sh scripts/check-platforms.sh
ANY2JB_REFERENCE_ROOT=/path/to/pinned/mac2jb \
ANY2JB_RUNTIME_ASSETS="$PWD/resources/darwin-arm64" \
go test ./engine -run TestPinnedDeveloperHostListening -v
```

The opt-in test stages the real verified Node and pepsipu sources, listens only on
loopback, validates the nine-argument START command and tears down without a
console connection. The structured fixtures test verified/held outcomes,
protocol failures, bind failure, Cancel, Stop, lease ownership, grandchildren
and held-host crash recovery. They are not evidence of a console exploit.

## Limits

No console or USB hardware was verified. All engine redistribution blockers
remain unchanged. [Guided manual input](guided-manual.md) is integrated.
Automated input, tools and credentials remain unavailable until their adapters integrate.

IZMYSH has a separate native qualification contract, not the ns2jb filesystem
contract. Its adapter is not implemented. The UI and runtime readiness explain
the blocker and both Go and Node refuse its launch.

Windows is compile-checked but refuses process launch because process-tree
supervision/ACL validation is not implemented. Linux process supervision and
interfaces compile; the staged runtime is still darwin-arm64, not a Linux
release or USB capability claim.

Production `PlatformServices` combines runtime/profile/USB-helper integrity
checks with `readiness.New().Inspect`. The helper check uses `usb-hid` transport
gating, so missing helpers never block manual input. LAN recommendations and
overrides retain the readiness adapter's assignment-only evidence. Manual input
is available; an unavailable automated input adapter remains an explicit blocker.
