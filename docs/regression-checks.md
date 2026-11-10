# Offline regression checks

From the repository root, run:

```sh
go run ./cmd/check -reference-root /absolute/path/to/pinned/mac2jb
```

This one command runs `go test -v ./...`, the Node manifest/bridge tests,
all six canonical host profile checksum/source/bootstrap checks, pepsipu's
existing upstream tests and its reproducible payload check. The explicit
reference root must contain the revision and files pinned in
`manifest/profiles.json`. No clone or dependency installation is automatic.

The real-engine TCP test requires the pinned Node assets. First prepare them
with `go run ./cmd/prepare-runtime`, or pass an existing prepared resource
directory with `-runtime-assets /absolute/path/to/resources/darwin-arm64`.
The command passes both explicit roots to the Go tests. Ordinary `go test`
remains offline and skips opt-in suites with a printed reason in verbose mode.

The real-engine protocol suite developer-stages pepsipu, rechecks its digests,
and launches the staged Node. A loopback-only fake console sends the binary
greeting bound to the fresh `V=START(...)` token. It then disconnects halfway
through the first native response header. The actual upstream TCP/native client
must reject that partial reply. This does not emulate exploit execution or
claim console verification.

The **simulated engine** controller suite uses the real
`engine.NewProtocolRunner` and the shared `engine/testdata/fake-engine.cjs`
process fixture. It checks fresh pairing delivered to input, 4/4 verification,
live held RPC, Finish, input release, process removal and private
workspace/lease removal. Disconnect, incomplete restoration and cleanup errors
must instead produce `indeterminate_console_restart_required`, prohibit retry
and still release owned resources. The command's summary labels this as
simulated engine coverage. These fixture outcomes are not upstream exploit
or hardware evidence.

The **retained tools** suite reuses that same fixture's `held-verified`
scenario on loopback with a structured tool-process fixture. It checks the lease
requirement, the single owner, attach versus launch, detach leaving the host
running, a lost attachment, refused endpoints and a refused capture destination,
plus developer tool staging and the tool host's own held proof
(`node --test retained/toolhost.test.cjs`). See
[retained tools](retained-tools.md); these are not console or hardware evidence.

On Darwin arm64 the command force-rebuilds libusbd in a disposable copy,
compiles `sender`, `commands`, `keyboard` with the GUI define and
`civswitch-hid`, and runs their offline input checks. Keyboard uses
`--validate`, its equivalent of dry-run, including rejection of a truncated
report. The other three use `--dry-run`. Build or dry-run failures fail the
command, never silently skip. Native warnings are retained in the output.
No driver is installed/loaded, no USB device is initialized, and no `sudo`
or security-setting change is performed. The reference checkout is not changed.

## Payload prerequisites and skipped suites

`engine/build-payload.cjs --check` really compiles with **Zig 0.15.2** and
compares shipped bytes. Select an existing local executable using
`-zig /absolute/path/to/zig`. If it is unavailable, explicitly record why:

```sh
go run ./cmd/check -reference-root /absolute/path/to/pinned/mac2jb \
  -skip-payload-reason 'Zig 0.15.2 is not installed; payload reproducibility is not verified'
```

An empty/whitespace skip reason fails. Without that option, a missing or wrong
Zig fails the payload suite. `PASS`, `FAIL` and `SKIP` lines identify every
top-level suite; verbose Go tests retain their individual skip reasons.
Non-Darwin-arm64 native checks and physical hardware qualification always
report their platform/hardware reason. A successful run with skips is not
evidence that those skipped checks passed.

On 2026-11-10 the standalone checks passed on Darwin arm64, including all six
source inventories, pepsipu's 11 upstream tests, the real-engine partial-reply
test, simulated engine controller scenarios and all four compiled native helpers.
With the official minisign-verified Zig 0.15.2 compiler installed and selected
using `-zig`, the full harness also passed on 2026-11-10. The rebuilt payload
matched 4757 bytes with SHA-256
`d590cbfc5d9152c24d5a36f083f8470a2f3164be6b78130129d44132b939701a`.
The optional read-only local OS inspection was not enabled. Physical
console/USB behavior remains
**not verified**.
