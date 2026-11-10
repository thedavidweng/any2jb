# Read-only readiness

`readiness.New().Inspect(ctx, run)` supplies the network, driver and controller
checks for the production `PlatformServices` composition. It installs nothing,
does not initialize USB, and never invokes sudo, a driver load request or security
settings. Host/runtime readiness is a separate adapter concern.

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

The visible LAN override and **Apply preferences** action are outside Developer
Options. **Check readiness** refreshes all observations. A network-only change
does not discard matching saved-bootstrap confirmation.

## USB facts

`Check.GatesTransport = "usb-hid"` attaches driver/controller prerequisites only
to built-in USB input. The dashboard still displays them for guided manual
input, but they cannot block that transport. An external Pico is not proof of
built-in gadget capability and needs its own transport adapter.

On Apple Silicon macOS the adapter independently observes:

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
evidence. Linux, Windows and Intel macOS report built-in USB input unsupported;
ordinary USB-C never implies device-mode capability.

## Evidence

- [Adapter fixtures](../readiness/readiness_test.go) test default-route
  recommendation and explicit overrides.
- [Individual blocker tests](../readiness/blockers_test.go) assert exact next
  actions, including unknown inspection states and a pending restart.
- [Native command-output fixtures](../readiness/native_darwin_test.go) cover
  attached-controller parsing, exact driver identity and restart timing.
- [Controller tests](../session/readiness_test.go) cover automatic selection,
  refresh, stale-address rejection before host launch, and transport gates.
- [Windowless dashboard test](../readiness_view_test.go) covers visible override,
  all three evidence labels, and the reference prerequisites.

`ANY2JB_READINESS_INSPECTION=1 go test ./readiness -run TestReadOnlySystemInspection -v`
opts into inspecting the current machine. It logs no local addresses or raw
command output. This is not hardware acceptance.

Console firmware/game compatibility, LAN console reachability, USB enumeration,
keyboard acceptance and cable role remain **not verified**. Required release
hardware testing is unchanged.
