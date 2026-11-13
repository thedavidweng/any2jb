# Hardware acceptance protocol

This is the manual gate that turns "implemented and fixture-tested" into
"hardware-verified". A human tester with the real rig runs the staged
sequence below and files one evidence log per combination in
[`hardware-evidence/`](hardware-evidence/). Tooling validates the log, maps
passing steps to parity ledger rows and advances their evidence column.
Agents never run these steps: no USB, driver or console operation is
performed by the tooling, and no row may claim `hardware-verified` without a
matching validated log (`go test . -run TestHardwareEvidenceMatchesLedger`
fails otherwise). Absent evidence reads as `not verified`.

Current status: **no evidence logs exist; everything hardware is not
verified.**

## The rig

- Apple Silicon Mac, macOS 14+, with the driver path in
  [driver security](driver-security.md) for USB transports (reduced startup
  security with user-managed kernel extensions, approved kext, no pending
  restart).
- P1a: Nintendo Switch 2 (reference tests target system 23.0.1), SmileBASIC
  full 4.4.9 or demo 4.4.6 on internal storage with save space, Switch on
  Ethernet, host and console on a mutually reachable LAN, US keyboard layout.
- P1b (CivSwitch): physical **Switch 1** Civilization VII cartridge 1.0.0,
  English (US) system language, same reachable network.
- The app runs developer-staged, never packaged, exactly as in
  [developer launch](developer-launch.md): `ANY2JB_DEVELOPER_CONFIG` pointing
  at the pinned mac2jb checkout and prepared resources. Nothing here
  authorizes redistribution.

## Combination record

Every log records the exact combination under test in its `combination`
object (see the templates). P1a requires: Mac model, macOS version, startup
security (USB transports), host profile, console system version, SmileBASIC
edition and version, the pinned bootstrap SHA-256 (checked against
`manifest/profiles.json`), transport, observed USB controller, cable/power
direction and LAN topology. CivSwitch replaces host profile and bootstrap
with console generation, cartridge version and system language.

Phase 2 Linux (`route: "p2"`) replaces `macModel`, `macosVersion` and
`startupSecurity` with `linuxDevice` (SBC, handheld or OTG device model and
architecture), `linuxKernel` (`uname -r`) and, for `usb-hid` or
`composite-hid`, `privilegeBoundary` (for example Polkit `pkexec` helper vs
narrow ConfigFS/hidg permissions) plus the `/sys/class/udc` `usbController`
identifier. Linux `p2` uses the same ordered step sequences as `p1a` (`usb-hid`
and `guided-manual`, with step 1 running `--internal-linux-usb-helper`
`--validate` / `--dry-run`) and `civswitch` (`composite-hid`). A log whose
combination contradicts the manifest (unknown profile, unsupported edition,
wrong bootstrap digest) fails validation.

## P1a staged sequence (Apple Silicon USB HID)

Run the steps in order and record each one independently in `steps`. Stop the
run at the first `fail` or `blocked` required step: a later step may not pass
in the same log. Fix the cause and file a new log for the next attempt.

1. **helper-dry-run** — on the target Mac, run
   `go run ./cmd/check -reference-root <pinned mac2jb>`; the native suite
   must PASS (libusbd build, `sender`/`commands`/`civswitch-hid --dry-run`,
   `keyboard --validate`). This is an offline gate: it advances no ledger row.
2. **usb-enumeration** — driver loaded and approved; launch the
   developer-staged app, select the observed `usb-drdN` and start keyboard
   input. Pass only on the helper's `MAC2JB_KEYBOARD_READY`. Advances
   `usb-role`, `usb-probe`.
3. **harmless-typing** — type into a harmless target first: a macOS text
   editor, then a non-destructive console prompt. Pass only when the accepted
   text matches exactly, including shifted letters. Advances `keyboard-text`.
4. **stop-key-release** — Stop/Cancel during capture and during a text
   transfer. Pass only when the neutral release is acknowledged, no key
   repeats on the target, and a cancelled run returns to ready. Advances
   `keyboard-stop`, `run-cancel`, `run-stop`.
5. **console-bootstrap-save** — empty slot 0, typed bootstrap, source
   verified on the console, edition-correct SAVE, overwrite prompt handled
   deliberately, then saved-identity confirmation and reuse on a later
   launch. Advances `bootstrap-open`, `bootstrap-type`, `keyboard-indent`,
   `bootstrap-save-full` or `bootstrap-save-demo` (by edition),
   `bootstrap-confirm`, `bootstrap-reuse`.
6. **host-connection** — LOAD (console prompt acknowledged), RUN, one-time
   REMOVE (`Out of memory` expected; never repeat it), then the fresh
   session's START. Pass only when the host reports the console connected.
   Advances `host-only`, `start-jailbreak`, `command-load`, `command-run`,
   `command-remove`, `command-start`.
7. **verification-4of4** — pass only on the complete structured result
   (exploit, filesystem verified/restored, cleanup), never a stdout `4/4`
   line or an open port. Advances `run-progress`.
8. **retained-rpc-readonly** — on a held-RPC profile (kurumuz, cellenseres,
   BermTheSequel, astral): hold RPC, run `rpc-status` and
   `system-code-dump`, confirm the single-owner refusal, then Finish with
   cleanup evidence. Advances `host-hold`, `rpc-check`, `rpc-ownership`,
   `system-dump`, `run-finish`. pepsipu and izmysh declare no held RPC:
   record `blocked` with that reason.

## P1a guided manual variant

With `transport: "guided-manual"` the USB steps do not apply; record only
steps 5–8 above, typing every command by hand per
[guided manual](guided-manual.md). Step 5 then advances
`bootstrap-open`, `bootstrap-confirm`, `bootstrap-reuse` and the
edition SAVE row; step 6 advances `host-only`, `manual-launch` and the
`command-*` rows; steps 7–8 are unchanged.

## P1b CivSwitch sequence

CivSwitch is a distinct route (`route: "civswitch"`, transport
`composite-hid`) and stays **not verified** until a human files a passing
log. Required steps, in order:

1. **civ-prerequisites** — cartridge 1.0.0, English (US), same LAN, lobby
   chat focus. Advances `civ-prerequisites`.
2. **civ-composite-enumeration** — both keyboard and gamepad interfaces
   enumerate. Gate step; advances no row.
3. **civ-chat-transfer** — all 87 macro lines accepted as chat input.
   Advances `civ-transfer`.
4. **civ-stop-release** — Stop releases both HID interfaces; partial input
   means console restart, no replay. Advances `civ-stop`.
5. **civ-loader-verify** — fresh nonce plus `Executed successfully` on TCP
   9026; open TCP alone is not verification. Also attach to the already-open
   loader once. Advances `civ-probe`, `civ-attach`.
6. **civ-save-backup** — `save:/saves/Single` backup with verified checksums,
   retained under app support. Advances `civ-original`.

Optional steps, recorded only when performed:

- **civ-trigger-install** — verified backup, explicit replacement consent,
  `INSTALL DONE - reboot, Load Game`. Advances `civ-trigger`.
- **civ-control-center** — `CONTROL_CENTER_READY`, verified
  `XSTRIG.Civ7Save` export, clean stop. Advances `civ-control-start`,
  `civ-control-export`, `civ-control-stop`.
- **civ-javascript** — explicit consent, exact `.js` file sent, execution
  confirmation. Advances `civ-js`.

## Filing a log

1. Copy `hardware-evidence/TEMPLATE.json` (or `TEMPLATE-civswitch.json`) to
   `hardware-evidence/YYYY-MM-DD-<route>-<profile>.json` and fill it in.
2. Redact: never paste the START pairing command (validation rejects
   `START(`), pairing keys, account identifiers, serial numbers or private
   paths. Put longer redacted output in files next to the JSON and reference
   them by relative filename in `log`.
3. `go run ./cmd/evidence validate docs/hardware-evidence/<file>.json`
4. `go run ./cmd/evidence apply docs/hardware-evidence/<file>.json` to
   advance the verified ledger rows to `hardware-verified` with a link to
   the log. Passing steps advance rows; `fail`/`blocked` steps advance
   nothing. A `blocked` row (distribution blocker) is refused, never
   advanced. Commit the log and the ledger update together.
5. `go run ./cmd/evidence report` prints the SPEC release-checklist hardware
   bullets (platform, host, console system/game versions, bootstrap digest,
   transport, per-step outcomes, redacted log reference, date) for milestone
   reporting.

The step-to-row mapping and all validation rules live in
`internal/hwevidence`; this document mirrors them.
