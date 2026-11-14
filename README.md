# any2jb

Cross-platform fork of [mac2jb](https://github.com/cpypwn/mac2jb) (`v0.8.2` parity) for macOS and Linux, with a native desktop UI, full CLI parity, and on-demand plugin installs.

## Features

- **macOS and Linux**: Native UI and single-binary CLI on Apple Silicon macOS (`darwin/arm64`) and Linux (`linux/amd64`, `linux/arm64`, GTK3 without WebKitGTK).
- **No bundled sub-tools**: Ships only the verified `ns2jb` host profiles, Node `24.22.1` runtime, and USB HID helpers. All 29 sub-tools (`SaveKit`, `OunceCake`, `MintBrew`, `HBLauncher`, `Modloader`, `CivSwitch`, `CivHomebrew` and `.civapp` packages, `Artemis` HTTP/FS, `NXFormat`, `NSP-Box`, `NUP`, `DNSwitch`, `Browser Probes`, and launcher saves) install on demand from pinned upstream sources with SHA-256 verification.
- **Flexible console input**: `usbgadget.kext` on Apple Silicon macOS, ConfigFS `usb_f_hid` on Linux devices with UDC hardware, or Guided Manual typing on any computer without lowering system security.
- **18 languages**: Follows system language by default and covers all 18 official Nintendo Switch 2 system languages.

## Usage

Run `any2jb` without arguments to open the desktop UI:

1. Complete the readiness check, or pick **Type the commands yourself** for Guided Manual mode.
2. Open **Plugins** in the top bar to install the sub-tools you need.
3. Choose a route (`SmileBASIC / ns2jb`, `CivSwitch`, or standalone `Utilities`) and follow the session prompts.

Pass arguments to run the CLI:

```sh
any2jb detect --json
any2jb plugins list
any2jb plugins install savekit ouncecake civhomebrew
any2jb about
```

See [docs/cli.md](docs/cli.md), [docs/cli-parity.md](docs/cli-parity.md), and [docs/external-projects.md](docs/external-projects.md).

### Optional: Apple Silicon macOS USB Driver

Automated USB typing on Apple Silicon macOS uses `usbgadget.kext`. Guided Manual mode works without changing macOS security settings. To use USB device mode:

1. Shut down the Mac, hold the power button until **Loading startup options**, and open **Options**.
2. In **Utilities -> Startup Security Utility**, select **Reduced Security** and enable **Allow user management of kernel extensions from identified developers**.
3. In **Utilities -> Terminal**, run `csrutil disable`, then restart into macOS.
4. Run `any2jb driver install` (or click **Install...** in the app), approve `usbgadget` in **System Settings -> Privacy & Security**, and restart once more.

To restore default security later, run `sudo rm -rf /Library/Extensions/usbgadget.kext /Library/Extensions/.mac2jb-usbgadget-restart-required && sudo kmutil clear-staging`, then re-enable `csrutil enable` and **Full Security** in macOS Recovery.

## Build and Test

Requires Go `1.27.3+` (and Xcode Command Line Tools on `darwin/arm64`). Development tools (`go`, `bun`, `node`, `gofumpt`, `golangci-lint`, `jactionlint`) are pinned in `mise.toml`.

```sh
mise install && bun install
mise run check                 # Go + JS lint/format/typecheck/tests + i18n + workflows
bun lint && bun check          # JS/JSON Biome lint, Bun typecheck, and Node runtime tests
bun run upstream:check         # Check upstream cpypwn/mac2jb and pinned plugin drift
go run ./cmd/prepare-runtime
go run ./cmd/check-package
go tool mygo dev
```

CI keeps three-platform race tests, package integrity checks, and packaged
self-tests for code, dependency, and build-workflow changes. Documentation and
auxiliary-workflow-only changes use the docs and workflow checks instead.
New commits cancel obsolete CI runs on the same branch; release runs are never
cancelled this way. CI caches Go modules, compilation output, and verified
runtime downloads. Release builds deliberately use no restored caches, run their
own quality gates, and build their own installers. CI can also be run manually
from GitHub Actions.

## Credits and License

Built on upstream work by `cpypwn` ([mac2jb](https://github.com/cpypwn/mac2jb)), `pepsipu`, `kurumuz`, `Cellenseres`, `BermTheSequel`, `Gezine`, `shinyquagsire23`, and all plugin authors listed in the in-app **About** view (`any2jb about`) and [docs/external-projects.md](docs/external-projects.md).
