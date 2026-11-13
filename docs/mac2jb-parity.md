# Differential output parity against mac2jb and ns2jb

`any2jb` replaces `mac2jb`'s Electron/SwiftUI wrappers and C command builders with
Go session orchestration (`engine`, `transport`, `session`). File checksums and
state-machine unit tests do not by themselves prove that `any2jb` emits the same
bytes to a Switch 2 or shows the same BASIC commands to a user.

The differential parity suite verifies `any2jb`'s externally observable outputs
against the pinned `mac2jb` reference (`ce1ce5f4f40b412cf83ba0a4f10ac53a850ed308`)
and the pinned upstream `ns2jb` hosts without physical Switch or USB hardware.

## Covered output seams

1. **USB HID report & virtual timing stream (`internal/transport/mac2jb_parity_test.go`, `internal/parity/hid.go`)**
   - Compiles `mac2jb`'s `sender.c`, `commands.c` and `keyboard.c` (`-DMAC2JB_GUI=1`)
     with `internal/parity/mock_libusbd.h` force-included so `libusbd_*` and
     `clock_gettime`/`nanosleep`/`sleep` record `<virtual_ns> <8-byte-hid-report>`
     instead of opening USB hardware or sleeping wall time.
   - Compares `mac2jb-send` and `mac2jb-commands` directly against `any2jb`'s
     `transport.NewHelperDarwin` + `transport.EncodeText` across full printable
     ASCII + indentation/CRLF edge cases, all 256 byte values (`0x00..0xff`),
     every unique pinned `bootstrap.bas`, and all 6 profiles × supported editions
     (`full`/`demo`) × actions (`Launch`/`Save`) under both default and fast
     distinct-wait timings.
   - Requires exact 8-byte HID state-transition reports and exact nanosecond
     virtual timestamps (`Transcript.State()`).

2. **Guided manual prompt stream (`TestGuidedTextMatchesMac2jb`)**
   - Compares `transport.NewManual` prompts against `mac2jb-commands --dry-run`
     and the pinned `bootstrap.bas` payloads across all profiles, editions and
     actions.

3. **Three-way engine TCP wire & workspace comparison (`internal/engine/mac2jb_parity_test.go`, `internal/parity/console.go`)**
   - Runs three hosts sequentially against the same deterministic loopback
     protocol-v1 fake console (`parity.ServeConsole`):
     1. Upstream `ns2jb` (`run.cjs` / `run.cjs --rpc 127.0.0.1`)
     2. `mac2jb` (`scripts/gui-host.cjs host 127.0.0.1 [--post-run-options]`)
     3. `any2jb` (`engine.NewHostRunner` -> `Prepare` -> `Run` -> `VerifyHeld` -> `Finish` -> `Cleanup`)
   - Seeds `crypto.randomBytes` deterministically per process via
     `NODE_OPTIONS=--require` (`internal/parity/engine_preload.cjs`) and drives
     all local HTTP RPC bridge commands (`status`, `query`, `verify-function`,
     `write`, `read`, `call`, `call-address`, `pair`, plus held-RPC release
     cleanup) at the `switch2/pipeline.cjs` boundary without modifying any
     verified file on disk.
   - Verifies that `START` (`V=START(...)`, both 5-argument and 9-argument
     `pepsipu` token forms), every `0x8080`-byte TCP packet sent to the console
     from greeting through `native.stop()` (`1,118,464` bytes for `pepsipu`,
     `1,184,256` bytes for held-RPC profiles), and the SHA-256 inventory of
     copied `<workspace>/{shared,switch2}` files + relative `.cjs` module
     resolution are **byte-for-byte identical** across all three runners.

## Hybrid oracle: live reference + committed digests

Redistribution-blocked upstream files (`bootstrap.bas`, `engine/**`) are never
committed to this repository. Instead, normalized transcript SHA-256 digests are
stored in:

- `internal/transport/testdata/mac2jb-parity.json` (72 HID and guided-prompt digests)
- `internal/engine/testdata/mac2jb-parity.json` (5 engine profile wire/workspace/transcript records)

This supports two execution modes:

- **Live differential mode** (macOS/Linux developer machines with the pinned
  reference checkout):
  ```sh
  ANY2JB_REFERENCE_ROOT=/absolute/path/to/pinned/mac2jb \
  ANY2JB_RUNTIME_ASSETS="$PWD/resources/darwin-arm64" \
  go test -v ./internal/transport ./internal/engine
  ```
  Also runs automatically inside `go run ./cmd/check -reference-root ...`.
- **Offline / cross-platform digest mode** (CI, Windows, or checkouts without
  `mac2jb`):
  ```sh
  go test ./...
  ```
  Verifies `any2jb`'s HID encoder, guided prompts, fake-console framing and
  committed profile digest records without needing C POSIX headers or upstream
  checkouts. Future Windows and Linux transport adapters can validate their
  output against the exact same `Transcript.State()` digests in
  `internal/transport/testdata/mac2jb-parity.json`.

To regenerate committed digests after an intentional manifest or transport
change, run against the verified reference checkout:

```sh
ANY2JB_REFERENCE_ROOT=/absolute/path/to/pinned/mac2jb \
ANY2JB_RUNTIME_ASSETS="$PWD/resources/darwin-arm64" \
go test ./internal/transport ./internal/engine -update-parity
```

## Intentional difference whitelist

Only the differences cataloged in `internal/parity/whitelist.go` are normalized
or exempted; any other divergence fails the suite:

| ID | Layer | Difference & rationale |
| --- | --- | --- |
| `hid-redundant-neutral-reports` | `transport/hid` | `mac2jb-commands` repeats all-zero release reports between commands, and `any2jb` writes one closing neutral report. Boot-keyboard reports represent full state, so consecutive identical `00×8` reports are no-ops; `Transcript.State()` still requires exact bytes and timestamps on every state transition. |
| `load-confirm-human-gate` | `transport/hid` | `any2jb` waits for explicit user confirmation (`Confirm`) before `LOAD` and its modal dialog instead of `mac2jb`'s blind `--load-confirm-wait-ms` timer. |
| `guided-load-confirm-step` | `transport/manual` | `any2jb` models mode setup (`slot`/`direct`) and `load-confirm` as interactive acknowledgments rather than prose notes; all BASIC command strings and bootstrap bytes are identical. |
| `workspace-isolation-and-lib-copy` | `engine/workspace` | `any2jb` keeps staged sources sealed (`0400`/`0500`) and copies `engine/{lib,shared,switch2}` into an owned `<support>/runs/run-*` workspace instead of writing beside `run.cjs`. |
| `held-stage4-deduplication-and-structured-finish` | `engine/control` | `mac2jb`'s `gui-session.cjs` emits stage 4 twice in held mode (`4/4 ...` + `4/4 verified...`) and stops `run.cjs` via `SIGTERM`; `any2jb` emits stages `[1,2,3,4]` once and finishes via stdin JSON `{"type":"finish"}`, producing the exact same console wire bytes. |
| `izmysh-profile-refusal` | `engine/profile` | `any2jb` refuses `izmysh` engine launch (separate GPL-2.0 server protocol) while keeping its HID/guided command formatting verified. |

## Limits

These checks prove byte-level output parity with `mac2jb` and `ns2jb` up to the
USB HID report writer and the SmileBASIC native TCP socket. They do not emulate
Horizon OS kernel/FS exploit stages inside `switch2/pipeline.cjs` (whose copied
files are verified byte-identical via `VerifyProfile` and `copiedEngineSha256`),
nor do they replace physical USB controller enumeration or Switch 2 hardware
acceptance (`docs/hardware-acceptance.md`).
