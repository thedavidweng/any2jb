# any2jb

A native desktop and CLI manager for `ns2jb` and `CivSwitch` on Apple Silicon
macOS 14+ (`darwin/arm64`) and Linux (`linux/amd64` and `linux/arm64`, GTK3
without WebKitGTK). Console and physical USB hardware behavior remains
**not verified** until real hardware acceptance logs are recorded.

## What Is Included vs. Downloaded on Demand

### Bundled offline in each platform package

- **Runtime**: Official Node `24.22.1` standalone binary (`darwin-arm64`,
  `linux-x64`, or `linux-arm64`) plus `any2jb`'s local bridge and tool host.
- **Host profiles & bootstraps**: All five supported `ns2jb` profiles and
  SmileBASIC bootstraps (`kurumuz` default, `cellenseres`, `astral`, `pepsipu`,
  and `BermTheSequel`), SHA-256 verified before staging and launch.
- **Built-in USB device-mode input**:
  - **Apple Silicon macOS (`darwin/arm64`)**: Bundled `usbgadget.kext`,
    `install-driver.sh`, `libusbd.dylib`, and the four arm64 Mach-O HID helpers
    (`sender`, `commands`, `keyboard`, `civswitch-hid`).
  - **Linux (`linux/amd64`, `linux/arm64`)**: Built-in Go ConfigFS `usb_f_hid`
    keyboard and CivSwitch composite HID helper for Linux hardware with a USB
    Device Controller (`/sys/class/udc`), plus Guided Manual input on any PC.
- **CivSwitch**: Bootstrap payload, Control Center, single-save backup reader,
  and pure-Go persistent boot-trigger installer (no external Python or Terser
  needed).
- **Retained RPC session tools**: Live RPC status, system-code capture,
  OunceCake read-only WebDAV and local FTP exports, Super File Bros. `/sd`
  export, SaveKit save inspection/backups/writes, all ten pinned launcher save
  packages, and `ns2jb-modloader`.

### Downloaded on demand (~100 MB+)

- **Optional Homebrew environment**: `MintBrew`, `Native HBLauncher`,
  `IZlauncher` (`BlitBun`, `Shell`, `VacooTube`), and the portable
  `homebrew-runtime` are kept out of the base installer to keep package size
  small.
- Click **Download & prepare Homebrew** inside **Advanced Tools** (or run
  `any2jb tool install-homebrew`) to fetch the pinned upstream archive into app
  support and verify every file against `manifest/profiles.json` SHA-256
  digests before launch.

### Not included

- `browser-research` (no redistribution grant) and legacy `izmysh` (requires a
  separate native qualification adapter not implemented in `any2jb`).
- External microcontroller HID hardware (such as RP2040 / Pico) is not
  implemented. Use built-in USB device-mode or Guided Manual input.

---

## Apple Silicon macOS USB Gadget Driver Setup

Apple Silicon Macs include USB device-mode hardware that lets the Mac act as a
USB keyboard or composite controller for the Switch 2 over a USB-C data cable.
Doing so uses the unsigned `usbgadget.kext` kernel extension bundled inside
`any2jb.app`.

Because `usbgadget.kext` is unsigned, macOS blocks it under default security
settings. `any2jb` **never** runs `csrutil`, `bputil`, `nvram`, or `spctl` on
your behalf; you must lower Startup Security for your macOS disk and disable
System Integrity Protection (SIP) yourself in **macOS Recovery** before
installing the driver.

If you do not want to change macOS security settings or install a kernel
extension, select **Guided Manual** input in `any2jb` and enter the displayed
SmileBASIC bootstrap commands with a standard USB keyboard plugged into the
Switch 2.

### Step 1: Enter macOS Recovery on Apple Silicon

1. Shut down your Mac completely (**Apple menu -> Shut Down...**) and wait 10
   seconds.
2. Press and hold the **Power button (Touch ID)** until the screen displays
   **Loading startup options**.
3. Click **Options**, then click **Continue**.
4. Select your administrator account and enter your Mac password when prompted.

### Step 2: Allow kernel extensions in Startup Security Utility

1. From the top menu bar in macOS Recovery, choose **Utilities -> Startup
   Security Utility**.
2. Select your macOS system disk (click **Unlock** first if FileVault is
   enabled) and click **Security Policy...**.
3. Select **Reduced Security**, then check **Allow user management of kernel
   extensions from identified developers**.
4. Click **OK**, enter your administrator password, and wait for macOS Recovery
   to apply the policy.

### Step 3: Disable SIP and restart into macOS

1. From the top menu bar in macOS Recovery, choose **Utilities -> Terminal**.
2. Run:

   ```sh
   csrutil disable
   ```

3. Type `y` when prompted, enter your Mac administrator password, and wait for
   Terminal to print:

   ```text
   Successfully disabled System Integrity Protection. Please restart the machine for the changes to take effect.
   ```

4. Choose **Apple menu -> Restart** to boot back into normal macOS.

### Step 4: Install and approve `usbgadget.kext`

1. Open `any2jb.app` (or run `any2jb driver install` from Terminal).
2. In Stage 1 (**Install the USB driver**) or **Settings -> Review USB driver
   setup**, click **Approve driver installation**, then click **Install with
   administrator approval** and enter your Mac password. `any2jb` verifies the
   SHA-256 digests of `usbgadget.kext` and `install-driver.sh` before invoking
   the macOS administrator prompt.
3. Open **System Settings -> Privacy & Security** and scroll down to
   **Security**. If macOS asks for approval to load system software from
   `usbgadget`, click **Allow**, enter your password, and let macOS finish
   rebuilding the kernel collection.
4. **Restart your Mac** when prompted. `any2jb` keeps the driver restart check
   blocked until a clean reboot completes after installation.
5. After your Mac restarts, connect a USB-C data cable directly between a
   Thunderbolt / USB-C port on the Mac and the Switch 2 (on MacBooks, keep external
   power on MagSafe or a separate port so the Switch 2 acts as the USB host),
   then click **Refresh readiness** in `any2jb`.

### Restoring default macOS security later

To remove the USB gadget driver and restore full macOS security when you no
longer need built-in USB device-mode input:

1. In macOS Terminal, remove the installed kext and restart marker, then rebuild
   the kernel collection:

   ```sh
   sudo rm -rf /Library/Extensions/usbgadget.kext /Library/Extensions/.mac2jb-usbgadget-restart-required
   sudo kmutil clear-staging
   ```

2. Shut down the Mac, hold the **Power button** to enter **macOS Recovery**,
   open **Utilities -> Terminal**, and run `csrutil enable`.
3. Open **Utilities -> Startup Security Utility**, select your macOS disk, click
   **Security Policy...**, restore **Full Security**, and restart your Mac.

---

## Command Line

The same `any2jb` binary runs as a CLI when invoked with arguments:

```sh
any2jb detect --json
any2jb driver install
any2jb tool list
any2jb tool install-homebrew
any2jb session --script steps.txt
```

See the [CLI guide](docs/cli.md) and [CLI parity table](docs/cli-parity.md).

---

## Build and Develop

Install Go **1.27.3** (and Xcode Command Line Tools on Apple Silicon macOS for
compiling the C USB helpers, code signing, and DMG packaging). No Bun or WebView
runtime is used. MyGo is pinned in `go.mod` to
`a4b6734f7715c9f73709092f7bae80d861572c03`.

Upstream third-party bundles and Node binaries are not committed to Git. During
the build, `cmd/prepare-runtime` fetches the pinned Node `24.22.1` release
archive and the pinned `cpypwn/mac2jb` reference archive into `.cache/`,
verifies every SHA-256 digest against `manifest/profiles.json`, compiles
`libusbd.dylib` and the four USB HID helpers on `darwin/arm64`, and stages
`resources/`. `cmd/check-package` then enforces the manifest inventory, binary
architectures, and third-party license notices before and after packaging.

```sh
# Stage verified runtime and bundled assets into resources/
go run ./cmd/prepare-runtime
go run ./cmd/check-package

# Run native UI in development mode
go tool mygo dev

# Run Go & Node tests, i18n/vet checks, and build a local package
go test ./...
node --test manifest/profiles.test.cjs retained/savekit-host.test.cjs retained/toolhost.test.cjs engine/*.test.cjs
go tool mygo i18n check && go tool mygo vet . && go vet ./...
go tool mygo build -platform darwin/arm64 -sign - -skip-notarize
go run ./cmd/check-package -platform darwin-arm64 -merged build/darwin-arm64/any2jb.app/Contents/Resources
build/darwin-arm64/any2jb.app/Contents/MacOS/any2jb --self-test
```

For explicit local upstream override staging during development, see
[developer launch](docs/developer-launch.md). For the hardware-free upstream
regression suite (including Zig `0.15.2` payload rebuild checks), run:

```sh
go run ./cmd/check -reference-root /absolute/path/to/pinned/mac2jb
```

See [regression checks](docs/regression-checks.md) and
[packaging and updates](docs/releases.md).

## UI Strings and Localization

Add every app-visible string to `locales/en.json` and its translation to
`locales/zh-CN.json`, then run `go tool mygo generate`. Commit
`locales/catalog_gen.go` so plain Go builds and tests run without code
generation. See [localization](docs/localization.md) and
[accessibility](docs/accessibility.md).

## Architecture and Terminology

- [Domain glossary](GLOSSARY.md)
- [External projects](docs/external-projects.md): pinned upstream revisions, digests, and licenses
- [Driver and startup security gate](docs/driver-security.md)
- [Retained RPC lease and tools](docs/retained-tools.md)
- [Direct Go orchestration over upstream engines](docs/adr/0001-direct-go-orchestration-over-upstream-engines.md)
- [Phase 1 release boundaries](docs/adr/0002-split-phase1-release-into-p1a-and-p1b.md)
