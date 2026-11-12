# 0003: Staged flow with per-method readiness

Status: accepted; the stage count and route-choice location are superseded by
[0005](0005-prepare-the-switch-checklist.md); stage 1 and the
startup-security row are superseded by
[0004](0004-installer-style-first-stage.md).

## Context

The single screen exposed route choice, readiness, bootstrap setup, launch,
diagnostics and tools at once. Users could not tell what to do next. USB-only
facts also obscured the independent guided manual route.

## Decision

Present four left-to-right stages:

1. Choose and apply an input method, then show only its detection requirements.
   Guided manual input is independent of USB device-mode capability. Keyboard
   and composite input require their own local USB prerequisites. Controller
   discovery is available but not verified, not enumeration or console acceptance.
2. Select SmileBASIC/ns2jb or CivSwitch and complete that route's setup.
   Host/runtime checks belong here. A matching trustworthy saved-bootstrap
   record skips first-time setup, not readiness. CivSwitch remains visible on
   keyboard-only/manual methods with an explicit composite-HID reason and a
   return to stage 1. Existing verified retained sessions can be attached here.
3. Launch and show real progress, prompt acknowledgements and recovery.
   The manual primary action remains **Guided manual launch**.
4. Offer tools only after controller verification. CivSwitch loader proof is
   separate from the ns2jb verified-session and retained-lease contracts.

Diagnostics and Settings stay in the window corner outside the flow. Stage
position is a live status; focus moves to the stage heading on transitions.

`SessionController` remains the only session-state owner. Presentation state
selects views, never evidence or operation eligibility. Cancel, Stop, Finish,
restart recovery, single tool ownership, distribution and write gates remain
unchanged. Route selection is not authorization to launch: ns2jb still needs
`Snapshot.CanStart`, and CivSwitch still checks its own adapter and enumeration.

Read-only `/usr/bin/csrutil status` is the sole security-command exception.
Observed disabled SIP is verified; enabled SIP blocks unsigned USB HID and has
a Recovery/manual next action. Failed or unparseable status is explicitly
unknown and advisory, not guessed enabled or disabled. Startup-security policy
is a distinct informational row: the app has not checked it and the user must
verify it personally in Recovery. No security-changing operation is permitted.

## Consequences and evidence

Only controls needed for the current stage render. Existing adapters and tool
panels remain reused behind the same controller gates. English and Simplified
Chinese catalogs carry all new text. `flow_view_test.go` exercises stage
isolation, method gates, trusted-record reuse, focus and corner access through
MyGo Tester; readiness fixtures cover SIP states. Hardware behavior remains
not verified. The static security scan permits exactly one fixed status call in
the Darwin readiness adapter and rejects other security commands.
