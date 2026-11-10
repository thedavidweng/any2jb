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

`ConfirmCivSwitchRestart(true)` requires an idle writer and indeterminate
console state. It records the user's explicit restart confirmation and returns
to idle without sending input. The UI clears the chat-focus confirmation.

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

Ticket 22 must recheck the loader before dependent operations and keep JS,
Control Center, and persistent-write consent/backup gates separate.
No persistence or arbitrary JavaScript send action is implemented here.

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
- the canonical `Manifest`, which verifies both source and payload bytes.

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
- Pinned upstream `node --test scripts/test-civswitch-hid.cjs` passed both
  descriptor comparison and macro `--dry-run`. It opens no USB device.
  Its linker warned that the reference `libusbd.dylib` was built for macOS 27.2
  while targeting macOS 14.0. macOS 14 deployment compatibility is **not
  verified** and needs a rebuilt/qualified dependency before release.

No real USB, console, kext installation, sudo, or network sends to consoles
were used. Composite enumeration, accepted chat input, loader behavior on a
real console, and safe physical cancellation are **not hardware verified**.
