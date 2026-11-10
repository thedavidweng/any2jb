# Supervised keyboard input

`transport.NewDarwin(DarwinOptions)` implements the session input boundary.
It is keyboard-only, never a substitute for CivSwitch's composite transport.
All console/USB hardware behavior remains **not verified**.

## Authorization and local builds

The helpers and libusbd remain blocked from redistribution. Nothing is added
to resources or the application bundle, and canonical distribution decisions
are unchanged. Packaged startup must not supply developer source options.

Local developer callers explicitly call
`transport.BuildDeveloperHelpers(ctx, pinnedSourceRoot, appSupport, manifest, out)`.
This uses `internal/nativehelper.Build`, the same build function as `cmd/check`.
It checks the pinned mac2jb revision, libusbd tracked sources and all four C
source digests, then builds the GUI helpers in an owned disposable copy.
It does not initialize USB.

The returned `*DeveloperHelpers` retains the resulting binary/dylib digests in
memory. Every launch rechecks sealed regular files and these digests. An
arbitrary directory, zero-value object or provenance file does not authorize
execution. Close the object only after releasing every transport using it.
This protects against accidental changes, not an adversary controlling the
same user account.

Pass this object as `DarwinOptions.Helpers`, plus explicit developer
`engine.Options` as `Runtime`. Each launch requires successful runtime Stage,
VerifyProfile and matching profile revision, source/bootstrap digest, edition,
save name, START arity and port. `Bootstrap func(session.Run) (string, error)`
is the same loader shape as the manual adapter. It must load the selected
profile/edition; USB transfer independently checks the text digest.

`Darwin.Check(ctx, run)` performs authorization, integrity and settings checks
without executing any helper. It is not USB enumeration evidence. The packaged
`engine.VerifyUSB` gate stays blocked while the helpers are unbundled.

## Commands and confirmation

Bootstrap transfer uses the GUI sender's inherited-indent correction and real
`MAC2JB_GUI` line-start/line-complete events. The parser rejects invalid order,
changed totals, malformed events and an exit without full completion.
`Progress.Current/Total` and `Remaining/HasEstimate` reach the session snapshot.
Remaining time comes only from the helper's measured `remainingMs`, never a
simulated progress timer. Delivery does not prove SmileBASIC accepted the text:
inspect and verify the source on the console before saving it.

SAVE and launch commands use the supervised keyboard helper's report delivery
acknowledgements. SAVE sends exactly one Enter and never accepts an overwrite
prompt. Full SAVE and demo SAVEV remain distinct. Launch preserves
LOAD, confirming Enter, RUN, REMOVE, and the fresh host's START order.
Pairing details must match the current run and are never copied into progress
messages or diagnostics.

The pinned `commands.c:241-246` automatically sends LOAD's confirming Enter
after `gui_load_confirm_wait_ms`, without observing the console prompt. This
conflicts with the required confirmation boundary. Go therefore orchestrates
the sequence through raw keyboard reports instead of invoking that command
path. `DarwinOptions.Confirm(ctx, run, instruction)` must wait for an explicit
user acknowledgement before each command and, separately, before LOAD's
confirming Enter. After LOAD, RUN and REMOVE, the configured upstream waits
are still honored; waits never replace acknowledgement. There is no C patch.

## Settings

Supply `transport.DefaultTiming()` explicitly. Invalid values are rejected,
not silently replaced.

| Setting | Stock value | Bounds |
| --- | --- | --- |
| Release gap | 10 ms | 1..2000 ms |
| Key hold | 0 ms | 0..2000 ms |
| Enter settle | 0 ms | 0..2000 ms |
| USB bInterval | 10 | 1..16, descriptor exponent, not ms |
| Enumeration | 4 seconds | 1..120 seconds |
| LOAD wait | 1000 ms | 0..120000 ms |
| RUN / REMOVE waits | 0 ms | 0..120000 ms |

Select an actually observed `usb-drd0` through `usb-drd15`; `auto`, synthetic
candidates and silent controller switching are not accepted. The upstream
keyboard helper has a fixed bInterval of 10; the interval preference affects
the file sender only. Raw keyboard pacing uses the selected hold/gap/Enter
values in Go.

## Stop and live forwarding

Only one writer may be active on a Darwin instance. Helpers run in an owned
process group. Stop/error sends SIGTERM, allows two seconds for the helper's
neutral-report cleanup, then kills remaining group members and reaps the
process before returning. The session controller waits for this cleanup before
publishing `indeterminate_console_restart_required`. Partial input is never
replayed. Inspect and clear partial input and restart the console before a
fresh attempt.

`OpenKeyboard(ctx, run, report)` returns a live `*Keyboard`. `Send` accepts one
validated packed eight-byte HID report and waits for the exact next
`MAC2JB_KEYBOARD_SENT` count. Reports are not retried. `Paste` accepts at most
16 KiB of printable US ASCII, tabs and line breaks, preserves SmileBASIC's
inverted letter case, and adds no trailing Enter.

Call `FocusLost` when the forwarding control/window loses focus, and `Close`
when the mode/window closes. Both send a neutral report, close stdin and wait
for `MAC2JB_KEYBOARD_RELEASED` plus process exit. An unacknowledged Send cannot
block focus-loss cleanup. Invalid input, failed delivery or protocol errors
close the helper. EOF also makes the native keyboard release keys when its
parent exits. Disconnect may prevent delivery; an unconfirmed release returns
an error instructing the user to reconnect the cable if a key repeats.

## Checks and limits

`go test ./transport ./session` uses trusted fake helper processes, not USB.
Tests cover command order and prompt boundaries, measured progress, deduplicated
bootstrap starts, neutral release before Stop's indeterminate state, no blind
retry, live focus loss/close, unacknowledged Send interruption, case mapping,
paste-without-submit, and settings bounds.

```sh
go test -race ./transport ./session
ANY2JB_REFERENCE_ROOT=/absolute/pinned/mac2jb \
ANY2JB_RUNTIME_ASSETS=/absolute/runtime/assets \
go test ./transport -run TestPinnedDeveloperHelpers -v
```

The opt-in test compiles the real helpers and checks authorization without
calling Send/OpenKeyboard. `cmd/check` runs sender/commands/composite
`--dry-run` and keyboard `--validate`; no driver, privileges or real USB is used.
Zig payload rebuilding remains a separate prerequisite.

The upstream file sender does not observe stdin EOF. Its normal Stop/error
cleanup is supervised, but abrupt owner death while bootstrap typing remains
a release blocker until parent-death ownership is closed. The live keyboard
does observe EOF. Native panel focus/window integration and adjustable
settings controls must also be wired before claiming the user-visible
forwarding workflow is complete.
