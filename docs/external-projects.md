# External projects

Every outside project any2jb builds with, ships, stages or reimplements, with
the exact version it is pinned to. Use this page to review what the app
depends on.

The sources of truth are [`internal/manifest/profiles.json`](../internal/manifest/profiles.json)
and [`go.mod`](../go.mod). `go test ./internal/manifest -run TestExternalProjects` fails
when a pinned upstream, revision or Go module in either is missing here.
License findings and release blockers are in the
[redistribution assessment](redistribution.md). This page is not legal clearance.

The **Use** column means:

- **Shipped**: inside the packaged app.
- **On-demand**: optional large tools downloaded and SHA-256-verified on user request, not bundled in the base app.
- **Build**: needed to build or check any2jb, not packaged.
- **Developer-staged**: read from a local pinned mac2jb checkout only through
  [developer launch](developer-launch.md), never packaged.
- **Blocked** / **Excluded**: the manifest's distribution decision. Blocked
  items are not implemented in packaged mode. Excluded items are not used at all.

When the manifest's upstream is mac2jb, any2jb takes the file from mac2jb's
copy, and the original project is named beside it. Those original revisions
come from mac2jb's own records (`upstream.lock`, `packaging/*provenance*`,
`integrations/*/provenance.json`) at the pinned mac2jb revision.

## Reference implementation

| Project | Pinned | Role |
|---|---|---|
| [cpypwn/mac2jb](https://github.com/cpypwn/mac2jb) | [`ce1ce5f4f40b412cf83ba0a4f10ac53a850ed308`](https://github.com/cpypwn/mac2jb/tree/ce1ce5f4f40b412cf83ba0a4f10ac53a850ed308) | The Mac tool any2jb reaches parity with. Bundled and on-demand files from this pinned archive are digest-checked (`mac2jb-notice` ships root `LICENSE`). |

## Shipped in the app

| Project | Pinned | Role | License |
|---|---|---|---|
| [egoist/mygo](https://github.com/egoist/mygo) | [`a4b6734f7715c9f73709092f7bae80d861572c03`](https://github.com/egoist/mygo/tree/a4b6734f7715c9f73709092f7bae80d861572c03) | Native UI framework and build tool, and the placeholder icon (records `placeholder-icon`, `mygo-notice`) | MIT |
| [Node.js](https://nodejs.org) | `v24.22.1` ([darwin-arm64](https://nodejs.org/dist/v24.22.1/node-v24.22.1-darwin-arm64.tar.gz), [linux-x64](https://nodejs.org/dist/v24.22.1/node-v24.22.1-linux-x64.tar.gz), [linux-arm64](https://nodejs.org/dist/v24.22.1/node-v24.22.1-linux-arm64.tar.gz)) | Runs the ns2jb host bridge and the tool host. Each archive is checked against its release digest (records `node-darwin-arm64`, `node-linux-amd64`, `node-linux-arm64`, `node-notice`). | MIT plus bundled notices |

Go modules compiled into the binary, from `go.mod`:

| Module | Version | Role |
|---|---|---|
| [github.com/egoist/mygo](https://github.com/egoist/mygo) | `v0.8.4-0.20261110062621-a4b6734f7715` | UI framework (above) |
| [github.com/spf13/cobra](https://github.com/spf13/cobra) | `v1.10.1` | Command line |
| [github.com/spf13/pflag](https://github.com/spf13/pflag) | `v1.0.9` | Cobra flags |
| [github.com/inconshreveable/mousetrap](https://github.com/inconshreveable/mousetrap) | `v1.1.0` | Cobra on Windows |
| [github.com/ebitengine/purego](https://github.com/ebitengine/purego) | `v0.11.1` | MyGo native calls without cgo |
| [github.com/go-text/typesetting](https://github.com/go-text/typesetting) | `v0.3.5` | MyGo text shaping |
| [golang.org/x/image](https://pkg.go.dev/golang.org/x/image) | `v0.46.0` | MyGo images and fonts |
| [golang.org/x/net](https://pkg.go.dev/golang.org/x/net) | `v0.59.0` | MyGo dependency |
| [golang.org/x/text](https://pkg.go.dev/golang.org/x/text) | `v0.43.0` | MyGo dependency |

## Build and check toolchain

| Project | Pinned | Role |
|---|---|---|
| [Go](https://go.dev/dl/) | `1.27.3` (`go.mod`) | Builds the app |
| [Zig](https://ziglang.org/download/) | `0.15.2` | Rebuilds pepsipu's payload in the [regression check](regression-checks.md) |
| Apple Command Line Tools (`clang`) | system | Builds the USB helpers and libusbd for developer launch |

## ns2jb host profiles

Five `ns2jb` host profiles (`kurumuz`, `pepsipu`, `cellenseres`, `BermTheSequel`, and `astral`) are **Shipped** in packaged builds with their pinned bootstrap payloads. `izmysh` remains **Blocked** (its separate legacy adapter is not implemented in `any2jb`). See [host profiles](host-profile-ui.md).

| Profile | Upstream | Pinned | Notes |
|---|---|---|---|
| pepsipu | [gitlab.com/pepsipu/ns2jb](https://gitlab.com/pepsipu/ns2jb) | [`95f5525089273c5fce0b95047f3600f8442e96ae`](https://gitlab.com/pepsipu/ns2jb/-/tree/95f5525089273c5fce0b95047f3600f8442e96ae) | Shipped; the original ns2jb |
| kurumuz (default) | [gitlab.com/kurumuz/ns2jb](https://gitlab.com/kurumuz/ns2jb) | [`81d5bb114c856109dadaf1077d927a9f297fe58d`](https://gitlab.com/kurumuz/ns2jb/-/tree/81d5bb114c856109dadaf1077d927a9f297fe58d) | Shipped; includes mac2jb's recorded ABI3 range patch |
| Cellenseres | [github.com/Cellenseres/ns2jb](https://github.com/Cellenseres/ns2jb) | [`2f686045ff88e5a97f959d19752e00ef3b62011f`](https://github.com/Cellenseres/ns2jb/tree/2f686045ff88e5a97f959d19752e00ef3b62011f) | Shipped; includes the same ABI3 patch |
| BermTheSequel | [gitlab.com/BermTheSequel/ns2jb2](https://gitlab.com/BermTheSequel/ns2jb2) | [`9accf75508f4e7033ffc6912b2ebb7e0b6704e84`](https://gitlab.com/BermTheSequel/ns2jb2/-/tree/9accf75508f4e7033ffc6912b2ebb7e0b6704e84) | Shipped |
| IZMYSH | [github.com/byizmysh/IZ2S-Platform](https://github.com/byizmysh/IZ2S-Platform) | [`d7a0df16e7e0c68acf78d2f68da8496ef3d9fd6b`](https://github.com/byizmysh/IZ2S-Platform/tree/d7a0df16e7e0c68acf78d2f68da8496ef3d9fd6b) (0.16.0-rc.5) | Blocked; legacy IZMYSH engine not implemented in any2jb |
| Astral (custom) | [gitlab.com/kurumuz/ns2jb](https://gitlab.com/kurumuz/ns2jb) | `81d5bb114c856109dadaf1077d927a9f297fe58d` | Shipped; mac2jb's `0.2.0-experimental` derivative of kurumuz |

## USB input on macOS

This chain is how a Mac types on the Switch. On `darwin-arm64`, the kext, installer script, `libusbd.dylib`, and the four compiled Mach-O USB helpers are **Shipped** in the macOS app bundle (Linux packages use the built-in Go ConfigFS `usb_f_hid` helper instead). See [driver security](driver-security.md) and [keyboard input](usb-keyboard.md).

| Project | Pinned | Role | Use |
|---|---|---|---|
| [shinyquagsire23/macos_usb_gadget_poc](https://github.com/shinyquagsire23/macos_usb_gadget_poc) | [`c8349b449a2171c1b5b7c59b882728b130fc6b88`](https://github.com/shinyquagsire23/macos_usb_gadget_poc/tree/c8349b449a2171c1b5b7c59b882728b130fc6b88) (mac2jb `upstream.lock`) | Source of `usbgadget.kext`, the unsigned kernel extension that switches the Mac's USB-C controller to device mode | Reference |
| `usbgadget.kext` binary, `Info.plist`, `SHA256SUMS` and licenses in mac2jb `drivers/` | mac2jb `ce1ce5f` | Prebuilt driver and notices copied by the macOS driver installer | Shipped on `darwin-arm64` (`usbgadget-binary`, `usbgadget-info`, `usbgadget-checksums`, `usbgadget-license`, `usbgadget-apsl`) |
| mac2jb `scripts/install-driver.sh` | mac2jb `ce1ce5f` | Installs and loads the driver after the administrator prompt | Shipped on `darwin-arm64` (`driver-installer`) |
| [shinyquagsire23/libusbd](https://github.com/shinyquagsire23/libusbd) | [`4fe5521e424061b823c3f7aebaa754150f94a821`](https://github.com/shinyquagsire23/libusbd/tree/4fe5521e424061b823c3f7aebaa754150f94a821) (mac2jb `upstream.lock`) | User-space USB device-mode library compiled deterministically at `prepare-runtime` time (`libusbd-source`) | Shipped on `darwin-arm64` (`libusbd-dylib`, `libusbd-license`, `libusbd-apsl`) |
| mac2jb `sender.c`, `commands.c`, `keyboard.c`, `civswitch-hid.c` | mac2jb `ce1ce5f` | Keyboard and composite HID helpers compiled deterministically against `libusbd` (`sender.c`, `commands.c`, `keyboard.c`, `civswitch-hid.c`) | Shipped on `darwin-arm64` (`sender`, `commands`, `keyboard`, `civswitch-hid`) |

## Console-side tools and integrations

Session exports, SaveKit, Mod Loader, CivSwitch, and launcher save archives are **Shipped** in packaged builds. Large GPU/homebrew packages are downloaded and SHA-256-verified **On-demand**. Browser research probes remain **Excluded**.

| Feature | Original project | Pinned | Manifest record |
|---|---|---|---|
| CivSwitch payload and installer | [Gezine/CivSwitch](https://github.com/Gezine/CivSwitch) | [`10b38e9f5abfd13a4b0bec8af24b026e44567da3`](https://github.com/Gezine/CivSwitch/tree/10b38e9f5abfd13a4b0bec8af24b026e44567da3) | Shipped: `civswitch-payload`, `civswitch-installer`, `civswitch-core`, `civswitch-save-template`, `civswitch-license`, `civswitch-backup-payload` |
| CivSwitch Control Center | [MatthewKuKanich/CivSwitch-ControlCenter](https://github.com/MatthewKuKanich/CivSwitch-ControlCenter) | [`259cbe5e66f2e649a79a16f06db4cdb012017daa`](https://github.com/MatthewKuKanich/CivSwitch-ControlCenter/tree/259cbe5e66f2e649a79a16f06db4cdb012017daa) (1.0.0) | Shipped: `civswitch-control-center`, `civswitch-cc-license` |
| OunceCake exports | [Cellenseres/OunceCake](https://github.com/Cellenseres/OunceCake) | [`6dfd7750752f8b6c18f2c40b34fdadc628cd9a6e`](https://github.com/Cellenseres/OunceCake/tree/6dfd7750752f8b6c18f2c40b34fdadc628cd9a6e) | Shipped: `ouncecake` |
| `/sd` browser | [byizmysh/super-file-bros](https://github.com/byizmysh/super-file-bros) | [`d7b216bacdd53d25f373c4c07cb6c88fa9b319c4`](https://github.com/byizmysh/super-file-bros/tree/d7b216bacdd53d25f373c4c07cb6c88fa9b319c4) (v0.1.0) | Shipped: `sd-browser` |
| SaveKit | [dougchansan/ns2-savekit](https://github.com/dougchansan/ns2-savekit) | [`700814ff37fa8f11f12e77b65dc8a33d421e8dd1`](https://github.com/dougchansan/ns2-savekit/tree/700814ff37fa8f11f12e77b65dc8a33d421e8dd1) | Shipped: `savekit` |
| Seedsow Lua editor save | [DialingSpoon527/seedsow-lua-editor](https://github.com/DialingSpoon527/seedsow-lua-editor) | v0.1.3 (mac2jb copy) | Shipped: `launcher-lua-editor` |
| MintBrew homebrew | [NiftyNova/GPUBrewLauncher](https://github.com/NiftyNova/GPUBrewLauncher) lineage | [`f35ff64ea4b9b42cf2a75095d24662fe250f03e4`](https://github.com/NiftyNova/GPUBrewLauncher/tree/f35ff64ea4b9b42cf2a75095d24662fe250f03e4) (mac2jb copy) | On-demand: `mintbrew` |
| Native homebrew launch | RavensClawX's MIT fork, documented in mac2jb `integrations/mintbrew-native/`. mac2jb also pins [ravensclawx/HBLauncher](https://github.com/ravensclawx/HBLauncher) at `b40723be04baa51e5d1f121a1e2242c4167b6cbc`. | mac2jb copy | On-demand: `mintbrew-native` |
| IZlauncher | [byizmysh/IZ2S-Platform](https://github.com/byizmysh/IZ2S-Platform) | [`73f1b6d98b3fd7c3a44c2b4f6e3b94704ce9435f`](https://github.com/byizmysh/IZ2S-Platform/tree/73f1b6d98b3fd7c3a44c2b4f6e3b94704ce9435f) (0.21.0) | On-demand: `izlauncher` |
| BlitBun | [byizmysh/BlitBun](https://github.com/byizmysh/BlitBun), with [ozkl/doomgeneric](https://github.com/ozkl/doomgeneric) `e257a09527062bfb986b6e5e0780839b6cbd5b71` and [Freedoom](https://github.com/freedoom/freedoom/releases/tag/v0.13.0) v0.13.0 | mac2jb copy | On-demand: `izlauncher-blitbun` |
| VacooTube | [byizmysh/VacooTube](https://github.com/byizmysh/VacooTube) | [`f28e762a085c4a4adcd655f12b40f4a68d646a4f`](https://github.com/byizmysh/VacooTube/tree/f28e762a085c4a4adcd655f12b40f4a68d646a4f) (v1.4.1, Electron 44.5.1) | On-demand: `izlauncher-vacootube` |
| IZlauncher shell | A prebuilt module in mac2jb, shaders compiled with [KhronosGroup/glslang](https://github.com/KhronosGroup/glslang) 16.6.0 | mac2jb copy | On-demand: `izlauncher-shell` |
| Homebrew build runtime | [python-build-standalone](https://github.com/astral-sh/python-build-standalone) `20261021` (CPython 3.13.17), [Pillow](https://pypi.org/project/pillow/13.0.0/) 13.0.0, [DejaVu fonts](https://github.com/dejavu-fonts/dejavu-fonts) 2.37, [SPIRV-Tools](https://github.com/KhronosGroup/SPIRV-Tools) `ef96ed763b43b59b33b31b362f09a02b729fa1c9`, [SPIRV-Headers](https://github.com/KhronosGroup/SPIRV-Headers) `496543121ce6419f23d6fa5d7194ba66c36212d2`, [glslang](https://github.com/KhronosGroup/glslang) 16.6.0 | mac2jb `packaging/homebrew-runtime.lock.json` | On-demand: `homebrew-runtime` |
| Mod loader | [SebaaMG/ns2jb-modloader](https://github.com/SebaaMG/ns2jb-modloader) | [`b3e1736848ea2ed0157148c3049ab139b55c7ac0`](https://github.com/SebaaMG/ns2jb-modloader/tree/b3e1736848ea2ed0157148c3049ab139b55c7ac0), with mac2jb changes | Shipped: `modloader` |
| Artemis launcher saves (Seedsow, Nora 2, Hamidashi) | [alula/switch-artemis-launcher](https://github.com/alula/switch-artemis-launcher) | [v2 release](https://github.com/alula/switch-artemis-launcher/releases/tag/v2) | Shipped: `launcher-seedsow-demo`, `launcher-seedsow-full`, `launcher-nora2-demo`, `launcher-hamidashi-demo` |
| Artemis launcher save (Nora 1) | [alula/switch-artemis-launcher](https://github.com/alula/switch-artemis-launcher) | Later upstream source (mac2jb copy) | Shipped: `launcher-nora1-demo` |
| Seedsow Lua Editor Pro | [purple442/seedsow-lua-editor-pro](https://github.com/purple442/seedsow-lua-editor-pro) | [v0.4.0](https://github.com/purple442/seedsow-lua-editor-pro/releases/tag/v0.4.0) | Shipped: `launcher-lua-editor-pro` |
| Balatro saves | [Luminger/balatro-switch-rce](https://github.com/Luminger/balatro-switch-rce) | [v1.1.0](https://github.com/Luminger/balatro-switch-rce/releases/tag/v1.1.0) | Shipped: `launcher-balatro`, `launcher-balatro-showcase` |
| Gravity Circuit save | [figurophobia/GravCirc-Switch-rce](https://github.com/figurophobia/GravCirc-Switch-rce) | v1.1 (mac2jb copy) | Shipped: `launcher-gravity-circuit` |
| Browser research probes | [Jmesmykil/switch2-captive-portal-research](https://github.com/Jmesmykil/switch2-captive-portal-research) | [`07cd867af2f1dc8f7b6ce59d964c3827cc3997b3`](https://github.com/Jmesmykil/switch2-captive-portal-research/tree/07cd867af2f1dc8f7b6ce59d964c3827cc3997b3) | `browser-research`, **Excluded** |

## Not used

mac2jb vendors more projects than any2jb records, for example NSP-Box, ftpd-sb,
other OunceCake forks and research tools. They are not in the manifest, so
any2jb neither stages nor runs them.
