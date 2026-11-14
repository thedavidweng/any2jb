# External projects

Every outside project any2jb builds with, ships, stages or downloads on demand,
with the exact version it is pinned to. Use this page to review what the app
depends on.

The sources of truth are [`internal/manifest/profiles.json`](../internal/manifest/profiles.json)
and [`go.mod`](../go.mod). `go test ./internal/manifest -run TestExternalProjects` fails
when a pinned upstream, revision or Go module in either is missing here.
License findings and release blockers are in the
[redistribution assessment](redistribution.md). This page is not legal clearance.

The **Use** column means:

- **Shipped**: inside the base packaged app (host profiles, USB sidecars, Node runtime).
- **On-demand plugin**: optional sub-tools downloaded from official upstream sources and SHA-256-verified on user request, never bundled inside the base installer.
- **Build**: needed to build or check any2jb, not packaged.
- **Excluded**: not distributed or executed.

## Reference implementation

| Project | Pinned | Role |
|---|---|---|
| [cpypwn/mac2jb](https://github.com/cpypwn/mac2jb) | [`717022b8a91a713be41a02b5e131d3229dd8b00a`](https://github.com/cpypwn/mac2jb/tree/717022b8a91a713be41a02b5e131d3229dd8b00a) (`v0.8.2`) | The macOS reference implementation any2jb aligns with. Bundled core files and on-demand adapter files from this pinned archive are SHA-256-verified (`mac2jb-notice` ships root `LICENSE`). |

## Shipped in the base app

| Project | Pinned | Role | License |
|---|---|---|---|
| [egoist/mygo](https://github.com/egoist/mygo) | [`a4b6734f7715c9f73709092f7bae80d861572c03`](https://github.com/egoist/mygo/tree/a4b6734f7715c9f73709092f7bae80d861572c03) | Native UI framework and build tool, and the placeholder icon (`placeholder-icon`, `mygo-notice`) | MIT |
| [Node.js](https://nodejs.org) | `v24.22.1` ([darwin-arm64](https://nodejs.org/dist/v24.22.1/node-v24.22.1-darwin-arm64.tar.gz), [linux-x64](https://nodejs.org/dist/v24.22.1/node-v24.22.1-linux-x64.tar.gz), [linux-arm64](https://nodejs.org/dist/v24.22.1/node-v24.22.1-linux-arm64.tar.gz)) | Runs the ns2jb host bridge and tool host (`node-darwin-arm64`, `node-linux-amd64`, `node-linux-arm64`, `node-notice`) | MIT plus bundled notices |

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
| Apple Command Line Tools (`clang`) | system | Builds the USB helpers and libusbd for macOS |

## ns2jb host profiles

Five `ns2jb` host profiles (`kurumuz`, `pepsipu`, `cellenseres`, `BermTheSequel`, and `astral`) are **Shipped** in packaged builds with their pinned bootstrap payloads.

| Profile | Upstream | Pinned | Notes |
|---|---|---|---|
| pepsipu | [gitlab.com/pepsipu/ns2jb](https://gitlab.com/pepsipu/ns2jb) | [`95f5525089273c5fce0b95047f3600f8442e96ae`](https://gitlab.com/pepsipu/ns2jb/-/tree/95f5525089273c5fce0b95047f3600f8442e96ae) | Shipped; the original ns2jb |
| kurumuz (default) | [gitlab.com/kurumuz/ns2jb](https://gitlab.com/kurumuz/ns2jb) | [`81d5bb114c856109dadaf1077d927a9f297fe58d`](https://gitlab.com/kurumuz/ns2jb/-/tree/81d5bb114c856109dadaf1077d927a9f297fe58d) | Shipped; includes mac2jb's recorded ABI3 range patch |
| Cellenseres | [github.com/Cellenseres/ns2jb](https://github.com/Cellenseres/ns2jb) | [`2f686045ff88e5a97f959d19752e00ef3b62011f`](https://github.com/Cellenseres/ns2jb/tree/2f686045ff88e5a97f959d19752e00ef3b62011f) | Shipped; includes the same ABI3 patch |
| BermTheSequel | [gitlab.com/BermTheSequel/ns2jb2](https://gitlab.com/BermTheSequel/ns2jb2) | [`9accf75508f4e7033ffc6912b2ebb7e0b6704e84`](https://gitlab.com/BermTheSequel/ns2jb2/-/tree/9accf75508f4e7033ffc6912b2ebb7e0b6704e84) | Shipped |
| Astral (custom) | [gitlab.com/kurumuz/ns2jb](https://gitlab.com/kurumuz/ns2jb) | `81d5bb114c856109dadaf1077d927a9f297fe58d` | Shipped; mac2jb's `0.2.0-experimental` derivative of kurumuz |

## USB input on macOS

| Project | Pinned | Role | Use |
|---|---|---|---|
| [shinyquagsire23/macos_usb_gadget_poc](https://github.com/shinyquagsire23/macos_usb_gadget_poc) | [`c8349b449a2171c1b5b7c59b882728b130fc6b88`](https://github.com/shinyquagsire23/macos_usb_gadget_poc/tree/c8349b449a2171c1b5b7c59b882728b130fc6b88) | Source of `usbgadget.kext` | Reference; prebuilt driver shipped on `darwin-arm64` (`usbgadget-binary`, `usbgadget-info`, `usbgadget-checksums`, `usbgadget-license`, `usbgadget-apsl`) |
| mac2jb `scripts/install-driver.sh` | mac2jb `717022b8a91a713be41a02b5e131d3229dd8b00a` | Installs and loads the driver after administrator prompt | Shipped on `darwin-arm64` (`driver-installer`) |
| [shinyquagsire23/libusbd](https://github.com/shinyquagsire23/libusbd) | [`4fe5521e424061b823c3f7aebaa754150f94a821`](https://github.com/shinyquagsire23/libusbd/tree/4fe5521e424061b823c3f7aebaa754150f94a821) | User-space USB device-mode library compiled at `prepare-runtime` time (`libusbd-source`) | Shipped on `darwin-arm64` (`libusbd-dylib`, `libusbd-license`, `libusbd-apsl`) |
| mac2jb `sender.c`, `commands.c`, `keyboard.c`, `civswitch-hid.c` | mac2jb `717022b8a91a713be41a02b5e131d3229dd8b00a` | Keyboard and composite HID helpers (`sender.c`, `commands.c`, `keyboard.c`, `civswitch-hid.c`) | Shipped on `darwin-arm64` (`sender`, `commands`, `keyboard`, `civswitch-hid`) |

## On-demand sub-tool plugins (never bundled in base installer)

All sub-tools are downloaded and SHA-256-verified on user request inside the app (`Plugins` / `any2jb plugins`) and are excluded from the base `resources/` package.

| Feature | Original project | Pinned | Manifest record |
|---|---|---|---|
| OunceCake read-only exports | [Cellenseres/OunceCake](https://github.com/Cellenseres/OunceCake) | [`6dfd7750752f8b6c18f2c40b34fdadc628cd9a6e`](https://github.com/Cellenseres/OunceCake/tree/6dfd7750752f8b6c18f2c40b34fdadc628cd9a6e) | On-demand plugin: `ouncecake` |
| SaveKit & Artemis HTTP | [dougchansan/ns2-savekit](https://github.com/dougchansan/ns2-savekit) | [`9d8ba7ff49e2544a8a4fe575a2f05cea90749218`](https://github.com/dougchansan/ns2-savekit/tree/9d8ba7ff49e2544a8a4fe575a2f05cea90749218) | On-demand plugin: `savekit` |
| MintBrew GPU homebrew | [NiftyNova/GPUBrewLauncher](https://github.com/NiftyNova/GPUBrewLauncher) lineage in [cpypwn/mac2jb](https://github.com/cpypwn/mac2jb) | `f35ff64ea4b9b42cf2a75095d24662fe250f03e4` / mac2jb `717022b8a91a713be41a02b5e131d3229dd8b00a` | On-demand plugin: `mintbrew` |
| Native HBLauncher | RavensClawX MIT fork & [ravensclawx/HBLauncher](https://github.com/ravensclawx/HBLauncher) `b40723be04baa51e5d1f121a1e2242c4167b6cbc` in [cpypwn/mac2jb](https://github.com/cpypwn/mac2jb) | mac2jb `717022b8a91a713be41a02b5e131d3229dd8b00a` | On-demand plugin: `mintbrew-native` |
| Portable homebrew runtime | [python-build-standalone](https://github.com/astral-sh/python-build-standalone) `20261021`, [Pillow](https://pypi.org/project/pillow/13.0.0/) 13.0.0, [DejaVu fonts](https://github.com/dejavu-fonts/dejavu-fonts) 2.37, [SPIRV-Tools](https://github.com/KhronosGroup/SPIRV-Tools) `ef96ed763b43b59b33b31b362f09a02b729fa1c9`, [glslang](https://github.com/KhronosGroup/glslang) 16.6.0 | mac2jb `717022b8a91a713be41a02b5e131d3229dd8b00a` | On-demand plugin: `homebrew-runtime` |
| Mod loader | [SebaaMG/ns2jb-modloader](https://github.com/SebaaMG/ns2jb-modloader) | [`cb9031dfa4989e3f21b2632aa2a48283c48ccaaf`](https://github.com/SebaaMG/ns2jb-modloader/tree/cb9031dfa4989e3f21b2632aa2a48283c48ccaaf) / mac2jb `717022b8a91a713be41a02b5e131d3229dd8b00a` | On-demand plugin: `modloader` |
| CivSwitch USB entry & persistent trigger | [Gezine/CivSwitch](https://github.com/Gezine/CivSwitch) | [`10b38e9f5abfd13a4b0bec8af24b026e44567da3`](https://github.com/Gezine/CivSwitch/tree/10b38e9f5abfd13a4b0bec8af24b026e44567da3) / mac2jb `717022b8a91a713be41a02b5e131d3229dd8b00a` | On-demand plugins: `civswitch-usb`, `civswitch-trigger` (`civswitch-license`, `civswitch-core`, `civswitch-save-template`, `civswitch-payload`, `civswitch-backup-payload`, `civswitch-installer`) |
| CivSwitch FS host | [mza1010/civswitch-kit](https://gitee.com/mza1010/civswitch-kit) | `76c136e8968a624c4814c8a0ce05af73f0848cf9` / mac2jb `717022b8a91a713be41a02b5e131d3229dd8b00a` | On-demand plugin: `civswitch-fs` |
| Artemis Seedsow FS host | [cpypwn/mac2jb](https://github.com/cpypwn/mac2jb) `integrations/artemis-fs` | `717022b8a91a713be41a02b5e131d3229dd8b00a` | On-demand plugin: `artemis-fs` |
| CivHomebrew launcher & companion | [XITRIX/CivHomebrew](https://github.com/XITRIX/CivHomebrew) | [`ebb3857ebd2c80e4978ab7934651f2bcbefa78b7`](https://github.com/XITRIX/CivHomebrew/tree/ebb3857ebd2c80e4978ab7934651f2bcbefa78b7) (`v0.4.3`) | On-demand plugin: `civhomebrew` |
| Moonlight for CivHomebrew | [XITRIX/Moonlight-CivSwitch](https://github.com/XITRIX/Moonlight-CivSwitch) | [`64cdae7f4a3f19202c7f8eaa0e5ac12ce3330d72`](https://github.com/XITRIX/Moonlight-CivSwitch/tree/64cdae7f4a3f19202c7f8eaa0e5ac12ce3330d72) (`v0.1.3`) | On-demand plugin: `moonlight-civswitch` |
| SW2NES for CivHomebrew | [Rodpad/SW2NES](https://github.com/Rodpad/SW2NES) | [`974f5b47a6364b995c12f27d318477680aca5038`](https://github.com/Rodpad/SW2NES/tree/974f5b47a6364b995c12f27d318477680aca5038) (`v1.0.1`) | On-demand plugin: `sw2nes` |
| Jellyfin for CivHomebrew | [THZoria/jellyfin](https://github.com/THZoria/jellyfin) | [`26da7a92f95512167cae8cae209da745a8984831`](https://github.com/THZoria/jellyfin/tree/26da7a92f95512167cae8cae209da745a8984831) (`0.0.1`) | On-demand plugin: `jellyfin-civswitch` |
| NXFormat (.nx) converter | [fab918/nxformat](https://github.com/fab918/nxformat) | [`V1.0.3`](https://github.com/fab918/nxformat/releases/tag/V1.0.3) | On-demand plugin: `nxformat` |
| NSP-Box packager | [smarkyie/NSP-Box](https://github.com/smarkyie/NSP-Box) | [`ea25556fdf88f3368d5cadf049a0558afcfaa5a4`](https://github.com/smarkyie/NSP-Box/tree/ea25556fdf88f3368d5cadf049a0558afcfaa5a4) | On-demand plugin: `nsp-box` |
| NUP archive tool | [a55uka/nup](https://github.com/a55uka/nup) | [`6fa98a8623afc5e40eeb30052b77691f1a68d542`](https://github.com/a55uka/nup/tree/6fa98a8623afc5e40eeb30052b77691f1a68d542) | On-demand plugin: `nup` |
| DNSwitch redirector | [cpypwn/mac2jb](https://github.com/cpypwn/mac2jb) `scripts/gui-dnswitch.cjs` | `717022b8a91a713be41a02b5e131d3229dd8b00a` | On-demand plugin: `dnswitch` |
| Local browser capability probes | [cpypwn/mac2jb](https://github.com/cpypwn/mac2jb) `integrations/browser-probes` | `717022b8a91a713be41a02b5e131d3229dd8b00a` | On-demand plugin: `browser-probes` |
| Artemis launcher saves | [alula/switch-artemis-launcher](https://github.com/alula/switch-artemis-launcher) & [soniciso1/switch-artemis-launcher](https://github.com/soniciso1/switch-artemis-launcher) | [v2 release](https://github.com/alula/switch-artemis-launcher/releases/tag/v2) / mac2jb `717022b8a91a713be41a02b5e131d3229dd8b00a` | On-demand plugins: `launcher-seedsow-demo`, `launcher-seedsow-full`, `launcher-nora2-demo`, `launcher-nora1-demo`, `launcher-hamidashi-demo` |
| Seedsow Lua Editor saves | [DialingSpoon527/seedsow-lua-editor](https://github.com/DialingSpoon527/seedsow-lua-editor) (`v0.1.3`) & [purple442/seedsow-lua-editor-pro](https://github.com/purple442/seedsow-lua-editor-pro) (`v0.4.0`) | mac2jb `717022b8a91a713be41a02b5e131d3229dd8b00a` | On-demand plugins: `launcher-lua-editor`, `launcher-lua-editor-pro` |
| Balatro & Gravity Circuit saves | [Luminger/balatro-switch-rce](https://github.com/Luminger/balatro-switch-rce) (`v1.1.0`) & [figurophobia/GravCirc-Switch-rce](https://github.com/figurophobia/GravCirc-Switch-rce) (`v1.1`) | mac2jb `717022b8a91a713be41a02b5e131d3229dd8b00a` | On-demand plugins: `launcher-balatro`, `launcher-balatro-showcase`, `launcher-gravity-circuit` |
| Captive portal research probes | [Jmesmykil/switch2-captive-portal-research](https://github.com/Jmesmykil/switch2-captive-portal-research) | [`07cd867af2f1dc8f7b6ce59d964c3827cc3997b3`](https://github.com/Jmesmykil/switch2-captive-portal-research/tree/07cd867af2f1dc8f7b6ce59d964c3827cc3997b3) | `browser-research`, **Excluded** |
