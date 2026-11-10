# Session controller seam

`session.SessionController` is the only state owner. UI code calls commands and
reads copied `Snapshot` values and `Events(afterSequence)`; it cannot write state.
Commands return immediately with a snapshot or a typed `*session.Rejection`.
Long-running adapter calls run off the UI thread. `Snapshot.Busy` remains true
until the operation and any required cleanup finish.

The controller is constructed with a pinned `session.Profile` projection and one
`session.Environment`. This small profile type does not replace the canonical
manifest. The manifest integration supplies profile identity and capabilities.
`SelectProfile` and `ConfigureTransport` invalidate readiness; switching profiles
also invalidates saved-bootstrap assumptions.

`manifest.Profile.SessionProfile()` supplies the pinned projection, including
editions, bootstrap/source digests, save name, START arity and port. Canonical
profiles require a confirmed matching bootstrap before Start. The
`UseSavedBootstrap` command validates the full `BootstrapRecord`; same-digest
bootstraps from different hosts are not interchangeable. See
[host profile choices](host-profile-ui.md) for preference and bootstrap
integration obligations. `ConfigureTransport` accepts an empty IPv4 to request
network discovery; this does not establish readiness or a usable LAN address.

## Adapter contract

- `HostRunner.Prepare` owns a fresh run's host resources and returns only after
  listening and validating session-bound pairing details. The controller never
  calls launch input before both conditions hold. `PreparedHost.Opaque` carries
  private adapter data and is never exposed in snapshots or events.
- `InputTransport.Send` performs bootstrap, SAVE submission, or launch input.
  Manual input waits for user acknowledgment, not a fixed timer. The adapter owns
  exact command formatting, prompt gates, and neutral key reports on completion.
- `HostRunner.Run` returns structured verification evidence. All four final
  verification predicates are required. Retained RPC instead requires verified
  and restored FS, a ready endpoint, deferred cleanup, donor cleanup, and a live
  listening RPC lease. The adapter must also validate run identity and live
  process/result integrity, not infer these booleans from a log line.
- `HostRunner.Finish` supplies final cleaned-up verification, including completion
  of deferred cleanup for a held run. Only then can `Finish` reach `finished`.
- `PlatformServices` supplies observed readiness, redacted diagnostics and a
  `CredentialStore`. The production credential adapter will use Keychain; neither
  preferences nor snapshots hold account secrets.
- `ToolRunner` owns single-flight tool work. Only tools explicitly enabled in the
  verified profile may run. Tool-specific consent and backup gates belong to its
  adapter; a profile capability is not consent to write user data.

All adapters must honor context cancellation and finish their writes before
returning. Report `Progress.SideEffect` **before** publishing pairing details or
consuming console state. Reports are scoped to the adapter call and must stop
when it returns. Input entry is conservatively considered a side-effect boundary.
Raw adapter errors and progress messages are not copied to public events because
they may contain pairing secrets. Readiness display text must already be safe.

`Cancel` is valid only before console side effects. It cancels the adapter, waits
for it to unwind, then releases input, stops tools and cleans the owned host.
`Stop` does the same after side effects, but always requires a console restart.
Cleanup failure also requires restart. An indeterminate session cannot be
inspected or started again through this seam; later recovery UI must establish a
real console restart before creating a new controller. Relaunching the desktop
app alone is not evidence of console recovery.

## Bootstrap confirmation

`PrepareBootstrap` transfers the matching bootstrap. `SaveBootstrap(false)`
submits SAVE exactly once and stays in `bootstrap_setup`. After handling the
console's save/overwrite prompt, the user calls `SaveBootstrap(true)` to confirm
the saved program. Neither transfer completion nor SAVE submission proves saving.
Start still requires observed, verified readiness checks.

## Fake environment and UI

`session.NewFakeEnvironment` provides deterministic outcomes, blocking gates and
an observable action trace for tests. Configure it before constructing a
controller; do not mutate outcomes during a run. Every fake snapshot is marked
`Simulated`, and the injected UI displays “Simulated environment. No real
hardware checks.” The production entrypoint does not inject this environment.

The existing `app` view accepts a controller for windowless `ui.Tester` tests.
These cover Home, readiness blockers, active input, cancellation, stopping,
verification, finishing and restart-required recovery. A real window integration
must arrange refreshes on controller events using MyGo's main-thread
`Window.Update` boundary. Do not keep a `ui.Context` or element in a worker.

This seam has no hardware implementation or hardware verification. The real
host, workspace, bootstrap, driver and transport adapters are separate tickets.
