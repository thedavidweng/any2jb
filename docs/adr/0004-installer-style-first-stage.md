# 0004: Installer-style first stage without an up-front method choice

Status: accepted. Supersedes the parts of
[0003](0003-staged-flow-and-scoped-readiness.md) named below.

## Context

The first stage asked beginners to pick an input method, showed raw readiness
rows with adapter wording, and added a separate "startup-security policy: not
checked by this app" row. Users could not tell what their Mac needed or what
to do next.

## Decision

The window follows the macOS Installer layout: the four stages are listed in a
left sidebar (✓ passed, ● current and bold, ○ ahead), the stage body is on the
right, and Back and the primary Continue sit in the pinned bottom bar. The stage
heading keeps its "Stage N of 4" status label and still takes focus on every
stage change.

Stage 1, **Check this Mac**, replaces "choose and apply an input method"
(0003, step 1):

- The read-only inspection runs once when the session opens, unless an
  interrupted run first needs a console restart. **Check again** repeats it.
- Exactly four plain rows, each with a state label, at most one sentence and
  at most one action: **Network**, **Allow the USB driver**, **Install the USB
  driver** and **Connect the Switch**. Raw check messages and identifiers move
  to Diagnostics; the LAN override and the composite / external Pico choice
  live in Settings.
- **Allow the USB driver** merges SIP and startup security. It is done only
  when `driver-loaded` is verified, because a running driver is the evidence
  that both settings allow it. Observed disabled SIP alone is "In progress".
  The row offers the Recovery walkthrough as text. The separate
  startup-security row is removed from the window (the command-line `status`
  note is unchanged): it never carried an observation, and the driver-loaded
  proof covers what it asked the user to check. While it needs action, the row
  shows six one-line steps in Apple's own UI wording and links to Apple's
  guides: startup security and macOS Recovery on Apple silicon (localized,
  through each catalog's URL) and Apple Developer's SIP page (English only),
  opened with `ui.Link`. In progress or unknown, the same steps sit behind
  **Show steps**. One sentence notes the FileVault unlock and that the settings
  lower macOS protection.
- Automatic USB input is the product's purpose; users who want manual input
  can use ns2jb directly. There is no method choice and no silent fallback.
  Continue applies the plain USB keyboard (or composite, if chosen in Settings)
  only when the driver is loaded and no applicable USB check needs attention.
  Otherwise it stays disabled with a plain reason, including when this Mac or
  this build cannot use built-in USB input (`usb-platform` or `usb-runtime`
  needs attention, or the driver gate is blocked and no driver is installed).
  Rows of a step this build cannot reach say "Waiting" or "Not available" and
  never ask the user to lower security for nothing.
- Guided manual input stays reachable as a quiet link, **Type the commands
  yourself instead**, below the steps. It is never a button and never sits in
  the bottom bar beside Continue.
- A discovered controller stays "Found, not tested", never done. Only observed
  keyboard enumeration marks the connection done.
- The `input-adapter` check moves to stage 2, which inspects again with the
  method Continue applied.

Everything else in 0003 stands: `SessionController` alone owns evidence and
eligibility, `Snapshot.CanStart` and the launch gates are unchanged, the
recommendation never relaxes a check, and no security-changing command is run
or offered. The only security command remains the fixed read-only
`/usr/bin/csrutil status`; the walkthrough names `csrutil disable` as text the
user types in Recovery Terminal.

## Consequences and evidence

`flow_view_test.go` covers the four rows from fake checks (SIP enabled, SIP
disabled, ready, restart pending, blocked driver gate, unsupported platform)
in English and Simplified Chinese, the method Continue applies, the reason it
is disabled, the manual link, the Apple guide URLs for each language, the
single automatic inspection and the sidebar and heading focus. `ANY2JB_FLOW_RENDER_DIR` renders those states at 720 and 560 DIP.
Hardware behavior, driver loading on real macOS and USB enumeration remain
**not verified**.
