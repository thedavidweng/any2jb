# Supervised keyboard input

`transport.NewDarwin(DarwinOptions)` implements the session input boundary.
It is keyboard-only, never a substitute for CivSwitch's composite transport.
All console/USB hardware behavior remains **not verified**.

## Authorization and local builds

The helpers and libusbd remain blocked from redistribution. Nothing is added
to resources or the application bundle. Packaged startup never supplies developer
source options, and canonical distribution decisions remain unchanged.

Local callers explicitly use
`transport.BuildDeveloperHelpers(ctx, pinnedSourceRoot, appSupport, manifest, out)`.
It shares `internal/nativehelper.Build` with `cmd/check`, verifies the pinned
mac2jb revision, tracked libusbd sources and four C source digests, then builds
in an owned disposable copy. Compilation never initializes USB.

The returned `*DeveloperHelpers` holds the resulting binary/dylib digests in
memory. Every launch checks sealed regular files and those digests. A directory,
zero-value object or provenance file alone cannot authorize execution. Close
the build only after releasing every transport using it. This protects against
accidental changes, not an adversary controlling the same account.

Supply the build as `DarwinOptions.Helpers`, explicit developer `engine.Options`
as `Runtime`, and the same `Bootstrap func(session.Run) (string, error)` loader
used by the manual adapter. Each launch requires successful Stage/VerifyProfile
and matching revision, source/bootstrap digest, edition, save name, START arity
and port. Bootstrap text is independently digest-checked before any input.

`Darwin.Check(ctx, run)` verifies provenance, bytes and settings without executing
a helper. `Configure(controller, timing)` refuses an active writer. Neither
method proves USB enumeration or console acceptance.

## B8 source compatibility decisions

The pinned `sender.c:350-401` corrects SmileBASIC's inherited editor indentation.
Go preserves that key sequence, including backspaces only for inherited spaces,
and the source's inverted letter-case mapping.

Bootstrap, SAVE, launch and live input all use the **same native keyboard
helper**. The file sender does not observe parent stdin EOF. Using it would
leave an abrupt-owner-death hole. `keyboard.c:19-45,124-135` instead releases
neutral on EOF, signals and invalid input. There is no new native supervisor,
C patch or exploit rewrite. The original send/commands helpers remain in the
shared offline build/dry-run checks, not the production typing path.

The pinned `commands.c:241-246` automatically sends LOAD's confirming Enter
after a fixed delay without observing the console prompt. Go therefore
orchestrates LOAD, its confirming Enter, RUN, REMOVE and the fresh host's START
through acknowledged keyboard reports. `DarwinOptions.Confirm` receives a typed
`ConsoleStep`; the native UI waits for an explicit console acknowledgement at
each boundary, separately for the LOAD YES/LOADV OK prompt. Waits never replace
acknowledgement. Full SAVE and demo SAVEV remain distinct. SAVE submits exactly
one Enter and never accepts an overwrite prompt for the user.

## Progress and readiness

Line progress advances only after the required key-down/key-up reports have
received exact, monotonically ordered `MAC2JB_KEYBOARD_SENT` acknowledgements.
Remaining time uses measured acknowledged stroke duration and the remaining
planned strokes. There is no simulated progress timer. `Current/Total` and
`Remaining/HasEstimate` reach the session snapshot and view.

`MAC2JB_KEYBOARD_READY` is the only source of positive enumeration evidence.
OS controller discovery remains available, not verified. Promotion applies only
to the explicitly selected, still-observed controller and its aggregate
enumeration check; it never promotes driver/security checks or other controllers.
Every operation still performs a fresh enumeration before accepting input.
Delivery and enumeration do not prove SmileBASIC accepted the text. Verify the
source on the console before saving it.

## Settings and native forwarding

The native panel selects only actually observed `usb-drd0` through `usb-drd15`.
It persists the non-sensitive controller and timing choices. Invalid values
fail visibly, without silently restoring defaults. Stock timing is explicit:

| Setting | Stock value | Bounds |
| --- | --- | --- |
| Release gap | 10 ms | 1..2000 ms |
| Key hold | 0 ms | 0..2000 ms |
| Enter settle | 0 ms | 0..2000 ms |
| Enumeration | 4 seconds | 1..120 seconds |
| LOAD wait | 1000 ms | 0..120000 ms |
| RUN / REMOVE waits | 0 ms | 0..120000 ms |

The native keyboard helper retains its fixed bInterval of 10, a USB descriptor
exponent, not milliseconds. Go applies the selected hold/gap/Enter pacing.

Presentation calls `SessionController.StartKeyboard`,
`ForwardKeyboardReport` and `CloseKeyboard`. The controller owns inspection,
the bounded queue, side-effect tracking and the optional `KeyboardTransport`
lease. A transport without this capability reports an explicit unavailable
reason. Only acknowledged reports count as delivered. Raw key codes come
directly from MyGo's USB-HID `KeyCode` values, including distinct left/right
modifier bits. Repeated presses are deduplicated; more than six held keys,
invalid reports or queue overflow stop the mode rather than dropping releases.
Escape stops capture; Tab leaves it using normal native focus navigation.

Focus loss, window blur/close, errors and mode closure release neutral and wait
for process teardown. Closing or stopping the mode ends the controller's writer
without cancelling the run. Close then sends the neutral report and requires its
`MAC2JB_KEYBOARD_SENT` acknowledgement, the only release evidence. An
acknowledgement left unread by a cancelled Send is consumed first and never
counts. Close then sends EOF and waits for exit. After READY, the open context
no longer signals the helper. Only when this two-second bound expires does the
helper get SIGTERM, then a group kill one second later. The controller cancels
the run only if its writer has not finished five seconds after the close request.
EOF releases input when the owning app dies. A failed release instructs the
user to reconnect the cable if a key repeats.

Cancel before publication/typing is retry-safe. Once any text/report is
published, Stop or mode closure reaches
`indeterminate_console_restart_required` only after cleanup. Inspect and clear
partial input, then restart the console before another attempt. No blind replay
is offered. Bootstrap adapters must report SideEffect before publication,
including before displaying manual instructions that a user could partly type.

## Evidence

- `transport/typing_test.go`: exact command order, observed LOAD prompt,
  inherited-indent reports, actual line progress, deduplicated starts and Stop.
- `transport/keyboard_test.go`: raw report acknowledgements, focus/close neutral
  release, stalled Send interruption, controller exclusivity, no signal before
  the acknowledged neutral report, and late acknowledgements never counted as release.
- `transport/owner_test.go`: abrupt fake-owner death produces helper EOF cleanup.
- `session/keyboard_test.go`, `session/usb_test.go`: authoritative lifecycle and
  selected-controller-only promotion.
- `usb_view_test.go`: native prompt acknowledgement, blocked helpers, physical
  Shift+A forwarding and focus/window-close cleanup through MyGo Tester.

`cmd/check` builds the real libusbd/helpers and uses only sender/commands/
composite `--dry-run` and keyboard `--validate`. The opt-in
`TestPinnedDeveloperHelpersAreIntegrityCheckedWithoutUSB` compiles and checks
authorization without calling Send/OpenKeyboard. Fake processes are not console
emulators. Driver loading, accepted console text, bootstrap saving and hardware
qualification remain **not verified**. Zig payload rebuilding is separate.
