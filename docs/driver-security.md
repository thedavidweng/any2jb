# Driver and startup security gate

The `usbgadget.kext` driver from mac2jb is unsigned. Using it requires reduced
startup security and user-approved kernel extensions. The pinned upstream setup
also asks the user to disable SIP. Re-enabling full protection with this driver
has not been verified. Signing or notarizing any2jb does not approve the kext.

## What the app does and does not do

- It inspects state through the [read-only readiness adapter](readiness.md).
  It never runs `csrutil`, `spctl`, `nvram`, `bputil`, `sudo` or a
  security-policy change. A static test,
  [`driver/static_test.go`](../driver/static_test.go), fails if non-test app
  code names one of these commands or uses `kmutil` beyond `showloaded`.
- Recovery and Startup Security Utility steps are shown as text for the user to
  perform in the first stage's **Allow the USB driver** step, with links to
  Apple's startup-security, macOS Recovery and SIP documentation. That step is done only once the driver is observed loaded; the app does
  not check or judge the startup-security policy itself.
- Installation runs the pinned upstream `scripts/install-driver.sh` only after
  two explicit clicks (**Approve driver installation**, then **Install with
  administrator approval**) and the macOS administrator prompt. The prompt is
  `/usr/bin/osascript -e 'do shell script "/bin/bash '<installer>'" with
  administrator privileges'`; the installer path is shell-quoted. The upstream
  installer stages the kext, writes the restart marker and asks `kmutil load`.
  It records `csrutil status` diagnostically and does not change SIP.
- Before the prompt, `driver.Gate.Install` verifies the SHA-256 of the kext
  binary, its `Info.plist`, `drivers/SHA256SUMS` and the installer against
  [`manifest/profiles.json`](../manifest/profiles.json), and rejects any extra
  or linked bundle file. A mismatch never reaches the administrator boundary.
- Every attempted install, including one whose load request fails afterwards,
  reports `RestartRequired`. The panel then re-runs readiness. The installer's
  marker `/Library/Extensions/.mac2jb-usbgadget-restart-required` keeps the
  `driver-restart` check at **requires attention** until the Mac has booted
  after it (see [readiness](readiness.md#usb-facts)).
- Declining closes the panel without touching session state. **Use Guided
  manual launch instead** switches the input preference to manual input, which
  the driver checks never block.

## Distribution status

The six components `usbgadget-binary`, `usbgadget-info`, `usbgadget-checksums`,
`usbgadget-license`, `usbgadget-apsl` and `driver-installer` are recorded at
mac2jb `ce1ce5f` as `may-ship` (`bundled: true`, `resourcePlatform:
"darwin-arm64"`). `cmd/prepare-runtime` stages them into
`resources/darwin-arm64/` with retained MIT and APSL-2.0 notices, and
`cmd/check-package` verifies their SHA-256 digests and license notices in
`darwin-arm64` packages while ensuring no macOS kext files enter Linux packages.
If a build is missing its driver files, `Gate.Availability()` refuses
installation and the panel disables the approve action.

A developer can also supply an explicit local source tree through the API:
`driver.New(driver.Options{Manifest: m, DeveloperSourceRoot: <absolute pinned
mac2jb clone>})`. That path still verifies every digest, and the panel marks
it as developer-supplied.

## Integration

- `driver.New(driver.Options{Assets, DeveloperSourceRoot, Manifest,
  Administrator})`; `Administrator` defaults to the osascript prompt and is
  injected as a fake in tests.
- `main` sets `app.driverPanel.gate` from `productionDriverGate`, using the
  merged MyGo resource root and no developer source. Tests verify it reports
  the canonical blocker.
- The panel opens from **Install…** in the first stage's **Install the USB
  driver** step, shown when the driver is absent and the gate is available, and
  from **Review USB driver setup** in Settings. While the gate is blocked the
  step says this version can't install the driver yet and Continue stays
  disabled; guided manual input is only the secondary **Type the commands
  yourself instead** link.

## Evidence

[Gate tests](../driver/gate_test.go) cover declined approval, the canonical
blocker, digest mismatch, unexpected bundle files and restart after a failed
load. [Panel tests](../driver_view_test.go) cover the disclosure, the disabled
blocked action, decline, the manual route and the second confirmation, all with
a fake administrator. No real installer, administrator prompt, kext load or
security change was run. Driver loading, approval on real macOS and USB input
are **not verified**.
