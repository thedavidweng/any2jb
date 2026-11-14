# Read-only readiness

`readiness.New().Inspect(ctx, run)` supplies the network, driver and controller
checks, including SIP, for the production `PlatformServices` composition. It
installs nothing, does not initialize USB, and never invokes sudo, a driver load
request or a security-setting change.
The only permitted security command is the fixed read-only `/usr/bin/csrutil status`.
Every other use of csrutil, spctl, nvram, bputil, sudo and kextload remains
forbidden. Host/runtime readiness is a separate adapter concern.

## LAN selection

Only active, non-loopback interfaces supply IPv4 addresses. Automatic selection
uses RFC1918 addresses on non-virtual interfaces: prefer a single address on the
default-route interface, otherwise use the sole eligible address. Multiple
eligible addresses without that preference require an explicit override. VPN,
bridge and virtual interfaces are not automatic recommendations, but an explicit
override may select any usable, currently assigned IPv4.

`Check.LANIPv4` carries the observed local assignment. The controller copies it
into `Snapshot.LANIPv4`. `Snapshot.LANOverride` keeps the user's explicit choice
separate, so refresh can rediscover an automatic address without persisting it.
The controller re-inspects the selected address before preparing the host. This
proves assignment only, not console reachability, firewall access or listening.
The dashboard labels the recommendation as unverified for console connectivity.
Listening and console connection require independent launch-time evidence.

The LAN override and **Apply preferences** action are in Settings, outside
Developer Options. **Check again** refreshes all observations. A network-only change
does not discard matching saved-bootstrap confirmation.

## USB facts

`Check.GatesTransport = "usb-hid"` attaches driver/controller prerequisites only
to built-in USB input. Inspection reports them for every method, so stage 1
always shows the four plain steps of
[ADR 0004](adr/0004-installer-style-first-stage.md), and Continue applies
automatic USB input only once they allow it; the raw checks are listed in Diagnostics. Guided manual input never
gates on USB-only facts. Host/runtime checks are
shown separately in the route stage and remain authoritative at launch.
A discovered controller is sufficient for route selection, not for ns2jb Start;
that command still requires the controller's existing verification contract.
External HID boards such as a Pico are not supported; a computer without
USB device-mode hardware uses guided manual input.

On Apple Silicon macOS the adapter independently observes:

- SIP through the exact read-only `csrutil status` command. Disabled is verified;
  enabled requires attention and blocks USB because the driver is unsigned. Its
  next action is to disable SIP personally in Recovery or choose guided manual
  input. A failed command, custom or unparseable output is explicitly unknown,
  available but not verified, and does not itself block USB. Unknown is never
  promoted to a passing observation.
- Kext bundle presence at `/Library/Extensions/usbgadget.kext`. This is not an
  integrity or loading claim.
- Exact `gay.bowser.usbgadget` membership in `kmutil showloaded --list-only`.
- The pinned installer's restart marker
  `/Library/Extensions/.mac2jb-usbgadget-restart-required`, compared with kernel
  boot time. A marker at or after boot, or an unreadable restart state, forces
  the loaded readiness check to **requires attention**. Ticket 10's installer
  must preserve this marker contract.
- `ioreg` AppleARMIODevice nodes with a `gay_bowser_usbgadget` descendant, listed
  individually and numerically ordered. No synthetic candidate list is used.
  Empty, failed and ambiguous discovery each have distinct next actions.

A discovered controller remains **available but not verified**, even when the
driver is loaded. Enumeration and accepted console input require later transport
evidence. Windows and Intel macOS report built-in USB input unsupported;
ordinary USB-C never implies device-mode capability.

On Linux (`linux/amd64` and `linux/arm64`) the adapter independently observes:

- Default-route interface discovery from `/proc/net/route`, excluding common
  container, bridge and VPN interfaces (`docker*`, `br-*`, `virbr*`, `veth*`,
  `podman*`, `cni*`, `tun*`, `tap*`, `wg*`, `tailscale*`) from automatic LAN
  recommendations while keeping explicit IPv4 overrides available.
- Real USB Device Controller hardware in `/sys/class/udc` (`linux-udc`).
  Ordinary desktop and laptop PCs with host-only USB-C ports expose no UDC and
  report built-in USB typing unavailable while keeping guided manual input ready.
- Linux ConfigFS USB gadget availability at `/sys/kernel/config/usb_gadget`
  plus `/proc/mounts` and `/proc/modules` (`linux-configfs`).
- Narrow privilege availability (`linux-gadget-access`), either direct write
  access to `/sys/kernel/config/usb_gadget` or `/usr/bin/pkexec` running the
  narrowly scoped `--internal-linux-usb-helper` subcommand. The GUI and Node
  host never run as root.
- Non-clobber conflict inspection: any UDC already bound to another ConfigFS or
  legacy gadget is marked **requires attention** so `any2jb` never overwrites an
  existing gadget.

### Startup-security policy is not a SIP observation

The Apple Silicon Full / Reduced / Permissive startup-security policy cannot be
observed through `csrutil status`. Stage 1 does not show it as its own row:
**Allow the USB driver** covers SIP and startup security together, and is done
only when the driver is observed loaded. Disabled SIP alone is shown as in
progress. The Recovery steps are text the user follows; the app never invokes
bputil, changes security, or adds an unsatisfiable check to the launch contract.

## Evidence

- [Adapter fixtures](../internal/readiness/readiness_test.go) test default-route
  recommendation and explicit overrides.
- [Individual blocker tests](../internal/readiness/blockers_test.go) assert exact next
  actions, including unknown inspection states and a pending restart.
- [Native command-output fixtures](../internal/readiness/native_darwin_test.go) cover
  attached-controller parsing, exact driver identity and restart timing.
- [Controller tests](../internal/session/readiness_test.go) cover automatic selection,
  refresh, stale-address rejection before host launch, and transport gates.
- [Windowless readiness tests](../internal/ui/readiness_view_test.go) cover the raw checks
  and all three evidence labels in Diagnostics, the override in Settings, and
  the reference prerequisites; [first-stage tests](../internal/ui/flow_view_test.go) cover
  the four steps.

`ANY2JB_READINESS_INSPECTION=1 go test ./internal/readiness -run TestReadOnlySystemInspection -v`
opts into inspecting the current machine. It logs no local addresses or raw
command output. This is not hardware acceptance.

Console firmware/game compatibility, LAN console reachability, USB enumeration,
keyboard acceptance and cable role remain **not verified**. Required release
hardware testing is unchanged.
