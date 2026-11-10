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
  perform. The app does not try to judge a working custom configuration.
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

The four components `usbgadget-binary`, `usbgadget-info`, `usbgadget-checksums`
and `driver-installer` are recorded at mac2jb `ce1ce5f` as `blocked`, not
bundled, pending review of the exact binary's source/build materials and its
MIT/APSL notices. While blocked, `Gate.Availability()` returns the canonical
manifest reason, the panel shows it, and the approve action stays disabled.

A developer can opt in through the API only:
`driver.New(driver.Options{Manifest: m, DeveloperSourceRoot: <absolute pinned
mac2jb clone>})`. That path still verifies every digest, and the panel marks
it as developer-supplied and not cleared for redistribution. There is no
environment variable or UI import route.

## Integration

- `driver.New(driver.Options{Assets, DeveloperSourceRoot, Manifest,
  Administrator})`; `Administrator` defaults to the osascript prompt and is
  injected as a fake in tests.
- The production composition sets `app.driverPanel.gate`; otherwise the panel
  creates a blocked packaged gate from the canonical manifest.
- The panel opens from **Review USB driver setup**, shown when a driver
  readiness check is present.

## Evidence

[Gate tests](../driver/gate_test.go) cover declined approval, the canonical
blocker, digest mismatch, unexpected bundle files and restart after a failed
load. [Panel tests](../driver_view_test.go) cover the disclosure, the disabled
blocked action, decline, the manual route and the second confirmation, all with
a fake administrator. No real installer, administrator prompt, kext load or
security change was run. Driver loading, approval on real macOS and USB input
are **not verified**.
