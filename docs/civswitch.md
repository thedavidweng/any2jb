# CivSwitch path

CivSwitch is separate from SmileBASIC. It requires the **physical Switch 1
Civilization VII cartridge, version 1.0.0**, English (US) system language,
host/console on the same reachable network, and the correct lobby/chat focus.
These console facts require explicit user confirmation. Local driver facts and
LAN assignment are observed independently. Neither proves console reachability.

## Controller boundary

`SessionController.StartCivSwitch` requires those confirmations and **both**
keyboard and gamepad capabilities. A keyboard-only transport is refused.
`CivSwitchFetching` validates developer assets and obtains the helper's
`--dry-run` estimate before `Inputting`. Progress uses actual
`line-complete`/`complete` events, not elapsed time. The pinned macro has 87 lines;
the upstream dry-run reports 21,675 characters and 1,299,100 ms (about 21.7
minutes). Enumeration and loader wait are additional time.

The input helper is the pinned `civswitch-hid.c` composite implementation, not
`sender.c`, `keyboard.c`, desktop key injection, or the ns2jb transport.
Cancellation sends SIGTERM and waits for helper exit. Its `cancelled` event is
accepted as neutral-report confirmation because the pinned helper emits it
after releasing its keyboard and gamepad reports. A silent/killed helper does
not establish release. Partial input requires console restart and cannot be
automatically replayed.

The adapter selects the one discovered `usb-drdN` explicitly. It does not use
the upstream `auto` parent, which can forcibly kill a worker and still emit
`cancelled`. Enumeration still happens in the directly owned helper.

`ConfirmConsoleRestart(ctx, true)` is shared with SmileBASIC. It requires
indeterminate console state and a stopped writer, releases any interrupted lease
through the diagnostics adapter, and returns to idle without sending input.
Release failure leaves the session indeterminate and shows the error.
The UI clears the chat-focus confirmation. Both routes use the same
`SessionController.Changes()` window refresh path.

`ConnectCivSwitch` is a separate action. It does not prepare assets, enumerate
HID, or type a payload. `RecheckCivSwitch` only probes; it never retypes.
The loader probe uses TCP **9026**, sends a fresh log nonce, and requires both
that exact nonce and `Executed successfully` in the response. Open TCP is not
verification. Responses are bounded to 64 KiB. The initial loader wait is up to
90 seconds; connect/recheck uses the upstream short, single-connect workflow
budget. All attempts respect cancellation.

`Snapshot.CivSwitch` discriminates this route; `LoaderVerified` is its proof.
The selected SmileBASIC profile stays available for returning to that route,
but its `Outcome` remains empty. CivSwitch never enters `RetainedRPC`, never
starts an ns2jb host, and cannot run ns2jb homebrew or SaveKit.
Finish releases local resources, not the console-side loader.

## Dependent actions: persistence, code and Control Center

Each action below verifies the developer-staged sources it needs, re-probes the
loader with a fresh nonce, and then does one thing. A loader verified earlier in
the session never authorizes them, and the controller offers them only on the
verified CivSwitch route with nothing else running.

`InstallBootTrigger` **replaces the game's single-player save files**. It needs
the user's explicit confirmation of that replacement, then reads
`save:/saves/Single` through the pinned backup payload, checks every file's
declared length and FNV-1a checksum, re-reads the stored bytes from disk to
compare SHA-256, and writes `manifest.json`. Only that proven backup allows the
pinned upstream installer to run, as a fixed argument list
(`python3 install.py <address>`) through no shell, and its
`INSTALL DONE - reboot, Load Game` line is required before the trigger counts as
installed. A console whose saves change mid-backup fails the install; nothing is
retried automatically. Reading saves is read-only, so a failed backup is a safe
failure; the installer is the first console write and marks the session
side-effecting, which means an interrupted install needs a console restart.

`SendJavaScript` and `StageJavaScript` are **arbitrary code execution** in the
loader context, which no prerequisite confirmation covers. Each needs its own
explicit consent and an absolute, regular, UTF-8 `.js` file under 480 KB.
Staging additionally requires Payload Mode open on the console: the host builds
begin/append/complete envelopes whose every value is JSON-encoded, sends them in
acknowledged chunks of 70,000 characters, and refuses a `PAYLOAD_STAGE|ERROR`
frame or a missing execution confirmation. The console still decides with A or
B, and Payload Mode does not restrict that code.

`ControlCenter` is read-only. The host sends the pinned Control Center payload
and nothing else for the rest of the session, so the input monitor and the
inventories carry no write path. It requires `CONTROL_CENTER_READY`, and accepts
a save export only from `save:/saves/Single/XSTRIG.Civ7Save` with ordered
chunks, the declared size and chunk count, the `CIV7` magic and a SHA-256
recorded beside it. Stopping the session closes the host connection only: the
loader and Control Center keep running on the console.

Verified backups and exports are written under `<app support>/civswitch/`
(`backups/` and `save-exports/`), outside every session workspace, so neither
automatic teardown nor user cleanup can delete them.

Loader and console output is data. It is parsed into fixed frames, never
executed, never interpolated into a command and never echoed back; console save
names are stored as literal bytes inside the backup directory through an
`os.Root` that refuses an escaping name. Reported errors carry fixed text, not
loader output.
## Distribution and developer staging

The packaged app shows an unavailable reason. It does not hide this route or
silently approximate it with keyboard HID. No CivSwitch payload, compiled
composite helper, USB driver, or libusbd library is added to the package.
`manifest.DistributionFor("civswitch-payload")` records the payload's pending
derived-artifact/dependency notice review.

An explicit developer composition can supply `civswitch.Options`:

- `DeveloperSourceRoot`: an absolute, non-symlink directory containing the
  pinned mac2jb `civswitch-hid.c` and `integrations/civswitch/payload.txt`;
- `DeveloperHelperSHA256`: the explicit SHA-256 of the locally built
  `mac2jb-civswitch-hid` in that directory;
- the canonical `Manifest`, which verifies both source and payload bytes, plus
  the recorded `civswitch-backup-payload`, `civswitch-installer` and
  `civswitch-control-center` bytes each dependent action needs;
- `Support`: the app support directory that holds retained backups and exports;
- `DeveloperInstallerPython`: an absolute Python 3 path for the trigger
  installer. Without it the install is refused; no interpreter is bundled and
  PATH is never searched.

There is no environment-variable opt-in or packaged startup bypass.
The developer is responsible for building the pinned helper with its pinned
libusbd dependency; a local binary digest is not reproducible-build or
redistribution clearance. Real USB operations additionally require local
driver presence, loading, no pending restart, a supported platform, and attached
controller discovery. The helper itself must enumerate both interfaces before
typing. Preparation never installs a driver, uses sudo, or changes security.

## Evidence and limits

- [Controller tests](../session/civswitch_test.go): separate prerequisites,
  composite refusal, partial-transfer stop/release, loader failure/recheck,
  separate attach, and no retained RPC.
- [Adapter tests](../civswitch/adapter_test.go): local shell subprocess fixtures,
  measured estimate parsing, completed-line progress, SIGTERM cancellation,
  unconfirmed release, integrity refusal, and packaged distribution refusal.
  These are fake helpers, not USB hardware.
- [Probe tests](../civswitch/probe_test.go): loopback TCP fixtures prove that
  missing nonce or execution confirmation is refused. No console is contacted.
- [UI test](../civswitch_view_test.go): unavailable reason, disabled actions,
  separate prerequisites, and no SmileBASIC controls.
- [Persistence tests](../civswitch/persistence_test.go): consent refused before
  any connection, a corrupt or incomplete backup blocking the installer, the
  installer's exact argument list, and hostile save names stored literally.
- [Code tests](../civswitch/code_test.go): consent refusal, file validation,
  the exact chosen bytes sent, refused payload errors, and JSON-encoded staging
  chunks that reassemble to the chosen file.
- [Control Center tests](../civswitch/control_test.go): a read-only session that
  sends no further code, a verified export retained through user cleanup,
  refused unverified exports, and a deliberate stop.
- [Gate tests](../session/civswitch_actions_test.go) and
  [panel tests](../civswitch_view_test.go): a verified loader and a staged
  writer are required, confirmations are per action, a failed install keeps its
  backup and requires a console restart, and the read-only session stops
  without being reported as a failure.
- Pinned upstream `node --test scripts/test-civswitch-hid.cjs` passed both
  descriptor comparison and macro `--dry-run`. It opens no USB device.
  Its linker warned that the reference `libusbd.dylib` was built for macOS 27.2
  while targeting macOS 14.0. macOS 14 deployment compatibility is **not
  verified** and needs a rebuilt/qualified dependency before release.

No real USB, console, kext installation, sudo, or network sends to consoles
were used. Composite enumeration, accepted chat input, loader behavior on a
real console, and safe physical cancellation are **not hardware verified**.
The trigger install, Payload Mode staging and the console save export were
exercised only against loopback fake loaders and local subprocess fixtures, so
they are **not hardware verified** either.
