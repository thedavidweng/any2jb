# Guided manual setup and launch

Guided manual input does not load a driver, emulate USB or type on the Mac.
The production adapter is `transport.NewManual`, with a loader that verifies
the selected staged profile before reading its `bootstrap.bas`. Packaged engines
remain distribution-blocked. A permitted developer-staged engine is required
to exercise the host locally; this does not authorize redistribution.

## Setup

1. In stage 1 choose **Type the commands yourself instead**, which applies
   guided manual input, and check its scoped
   readiness. USB driver/security/controller facts do not block this method.
2. In stage 2 select the SmileBASIC/ns2jb route and confirm its checklist,
   then in stage 3 select its host/edition.
   A trustworthy matching saved-bootstrap record skips first-time setup.
   Otherwise check the selected host and complete setup below.
3. Prepare bootstrap. Confirm an empty Slot 0 editor, enter the complete displayed
   `bootstrap.bas`, and confirm return to an empty Direct-mode prompt.
4. Submit SAVE. Enter the displayed edition-specific command and press Enter once.
5. Handle the console save/overwrite prompt deliberately. Only then choose
   **I saved the bootstrap**. SAVE submission is not proof of saving.

The controller records confirmation for the selected profile and edition.
Profile changes invalidate that assumption. Persistent
[saved-bootstrap identity](saved-bootstrap.md) belongs to the bootstrap
preferences integration, not to the manual adapter.

### SAVE syntax conflict

The ticket's proposed full-edition `SAVE "TXT:<saveName>"` differs from the
pinned upstream. We preserve the source-confirmed command under spec B8:

- Full: `SAVE "<saveName>"`.
- Demo: `PRGEDIT 0:C$="":FOR I=0 TO PRGSIZE(0)-1:C$=C$+PRGGET$() :NEXT:SAVEV "TXT:<saveName>",C$`.

Source: [commands.c lines 343–346 at ce1ce5f](https://github.com/cpypwn/mac2jb/blob/ce1ce5f4f40b412cf83ba0a4f10ac53a850ed308/commands.c#L343-L346).
The full and demo SAVE commands have explicit literal regression tests.

## Launch

After matching bootstrap confirmation and controller readiness, continue to
stage 4 for launch and progress. Tools appear only at stage 5 after a verified
session. Diagnostics and Settings stay reachable outside the stage flow.

The primary action reads **Guided manual launch**, not one-click.
The host must first listen and supply validated, run-bound pairing details.
The adapter checks run ID, IPv4, port and five-/nine-argument START syntax again
at the input boundary. The user acknowledges each step separately:

1. Full `LOAD "TXT:<saveName>"`, or demo
   `PRGEDIT 0:LOADV "TXT:<saveName>" OUT C$:PRGSET C$`.
2. Accept the console's YES/OK prompt. Wait at least one second for loading to finish.
3. `RUN`.
4. `REMOVE NSR,&H60000000,&H3FFFFFFF`. Expect **Out of memory**.
   Never repeat REMOVE in the same game session. Stop on a different result.
5. The exact session-specific `V=START(...)`, copied unchanged from the host.

Source: [commands.c lines 523–557](https://github.com/cpypwn/mac2jb/blob/ce1ce5f4f40b412cf83ba0a4f10ac53a850ed308/commands.c#L523-L557).
No fixed timer substitutes for the user's console acknowledgement.
There is no back/replay control. Stale run IDs and duplicate acknowledgements
are refused. STOP/cancellation clears the ephemeral prompt.

The START command is exposed only in the manual prompt and explicit clipboard
action. It is not stored in preferences, controller snapshots or public events.
Do not paste pairing commands into diagnostics or support messages.

## Verification and ending a session

Acknowledging START does not mark a session verified. The supervised host must
produce the complete structured result contract. A listening port, progress
counter or stdout `4/4` line is insufficient. Retained RPC additionally needs
the verified live lease contract; Finish still requires final cleaned-up evidence.

- **Cancel**, before input/pairing side effects, returns to ready after releasing
  input and stopping owned processes.
- **Stop**, after side effects, requires a console restart even when host cleanup succeeds.
- **Finish**, only after verified/retained RPC, waits for final verification and cleanup.

The UI reports resource cleanup only after input release, tool stop and host
cleanup all return successfully. This evidence does not prove console recovery.

**I restarted the console, prepare a new attempt** calls the shared
`ConfirmConsoleRestart(ctx, true)` command. Stale lease release must succeed
before recovery leaves indeterminate. A live or unknown owner remains blocked
and its release error is shown. Recovery clears old checks, verification and
progress, returns to idle, and never starts another attempt automatically.
The same confirmation is used for CivSwitch and clears its chat-focus checkbox.

## Window refresh and evidence

`SessionController.Changes()` wakes the single native window consumer. The manual
adapter has its own prompt wake channel. `refreshSession` coalesces observations
through `Window.Update`, reads fresh snapshots on the UI thread, and exits when
the window closes. No worker keeps a Context or frame-scoped element.

Tests:

- `internal/transport/manual_test.go`, `internal/transport/launch_test.go`: setup acknowledgements,
  exact full/demo commands for every profile, pairing refusal and stale acknowledgements.
- `internal/ui/manual_view_test.go`: actual native MyGo views with the supervised
  `internal/engine/testdata/fake-engine.cjs` structured-process fixture, including setup,
  complete verification, incomplete verification, exact clipboard text, Cancel,
  Stop and Finish. The fixture does not emulate console exploit behavior.
- `internal/session/changes_test.go`, `internal/app/session_refresh_test.go`: background completion
  wakes the window and the refresh watcher ends on close.
- `internal/session/recovery_test.go`, `internal/ui/restart_view_test.go`, `internal/session/civswitch_test.go`:
  explicit restart confirmation, stale lease refusal/error display and shared recovery.

Console, USB and actual exploit execution: **not verified, awaiting hardware**.
IZMYSH command formatting is tested, but its separate native qualification
adapter remains unavailable. No hardware claim follows from these fixtures.
