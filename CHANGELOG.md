# Changelog

## 0.1.1

- Update Cobra to `1.10.2` and synchronize the dependency inventory.
- Update seven GitHub Actions dependencies, including checkout `7.0.1` and
  cache `6.1.0`.
- Fix Linux test gates, CivSwitch process tests, and orphan-process cleanup tests.
- Record build-time digests for macOS USB helpers across Xcode SDK versions.

## 0.1.0

- Native MyGo desktop app and Cobra CLI (`cmd/any2jb`) for Apple Silicon macOS
  14+ (`darwin/arm64`) and Linux (`linux/amd64`, `linux/arm64` via GTK3 without
  WebKitGTK), with full English and Simplified Chinese structured catalog
  localization.
- Bundle pinned Node `24.22.1`, the integrity-checked `any2jb` engine bridge and
  tool host, five SHA-256-verified `ns2jb` host profiles and bootstraps, Darwin
  `usbgadget.kext` + USB HID helpers, Linux ConfigFS `usb_f_hid` keyboard and
  composite gadget support, SaveKit, launcher save packages, and `ns2jb-modloader`,
  with optional Homebrew (`MintBrew`, `Native HBLauncher`, `IZlauncher`)
  downloaded and SHA-256-verified on demand; `browser-research` and `izmysh`
  remain excluded/refused with recorded reasons.
- Organize all orchestration, adapter, UI, and CLI packages under `internal/*`
  behind a pure standard-library `internal/session` state machine, shared
  `internal/procgroup` child-process supervision, and `internal/readiness`
  first-stage setup projection.
- Retained RPC lease with single-flight tool ownership: live status, connecting
  to an existing verified session without taking host ownership, system-code
  capture, OunceCake WebDAV/FTP exports, `/sd` export, SaveKit backups/mutations,
  and verified launcher/homebrew workflows.
- Gate the CivSwitch boot trigger, JavaScript send, Payload Mode staging and
  Control Center behind a rechecked loader, explicit per-action confirmation and
  a disk-verified save backup, with read-only exports retained outside sessions.
- Application updates remain disabled until a production public key and
  reviewed release channel are configured.
