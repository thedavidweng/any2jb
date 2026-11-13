// Package readiness observes local prerequisites without installing or loading drivers.
package readiness

import (
	"context"
	"net/netip"
	"slices"
	"strings"

	"github.com/thedavidweng/any2jb/internal/session"
	"github.com/thedavidweng/any2jb/locales"
)

type Address struct {
	Interface string
	IPv4      string
}

type Observation struct {
	Linux            *LinuxGadget
	SIPStatus        string
	Addresses        []Address
	DefaultInterface string
	NetworkError     error
	USBSupported     bool
	Driver           Driver
	Controllers      []string
	ControllerError  error
}

type LinuxGadget struct {
	BusyControllers   map[string]string
	ConfigFSMounted   bool
	ConfigFSReady     bool
	HIDModuleReady    bool
	CanLoadModules    bool
	DirectWriteAccess bool
	PolkitAvailable   bool
}

type Driver struct {
	Present, Loaded, RestartPending       bool
	PresentError, LoadError, RestartError error
}

// Inspector's observation boundary also supports deterministic platform fixtures.
type Inspector struct {
	Observe func(context.Context) Observation
}

func (p Inspector) Inspect(ctx context.Context, run session.Run) ([]session.Check, error) {
	o := p.Observe(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return append([]session.Check{networkCheck(o, run.LANIPv4)}, usbChecks(o)...), nil
}

func usable(ip string) bool {
	a, err := netip.ParseAddr(ip)
	return err == nil && a.Is4() && a.IsGlobalUnicast() && !a.IsLinkLocalUnicast() && ip != "255.255.255.255"
}

func isVirtualInterface(name string) bool {
	for _, prefix := range []string{
		"utun", "bridge", "vmnet", "vnic", "awdl", "llw",
		"docker", "br-", "virbr", "veth", "lxc", "lxd", "podman", "cni", "flannel", "calico",
		"tun", "tap", "wg", "tailscale",
	} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func setCheckText(c *session.Check, msg, action locales.Text) {
	c.Message, c.MessageText = msg.English(), msg
	c.NextAction, c.NextActionText = action.English(), action
}

func makeUSBCheck(id string, msg, action locales.Text, state session.CheckState) session.Check {
	c := session.Check{ID: id, State: state, GatesTransport: session.GateUSBHID}
	setCheckText(&c, msg, action)
	return c
}

func networkCheck(o Observation, override string) session.Check {
	connectAction := locales.Key("dynNetConnectLAN")
	if o.Linux != nil {
		connectAction = locales.Key("dynNetConnectLANLinux")
	}
	c := session.Check{ID: session.CheckNetwork, State: session.RequiresAttention}
	setCheckText(&c, locales.Key("dynNetNoUsableLAN"), connectAction)
	if o.NetworkError != nil {
		setCheckText(&c, locales.Key("dynNetDiscoveryFailed"), locales.Key("dynNetCheckAccess"))
		return c
	}
	var available, candidates, preferred []string
	for _, a := range o.Addresses {
		if !usable(a.IPv4) {
			continue
		}
		available = append(available, a.Interface+": "+a.IPv4)
		if override == a.IPv4 {
			c.LANIPv4 = override
		}
		ip, _ := netip.ParseAddr(a.IPv4)
		if ip.IsPrivate() && !isVirtualInterface(a.Interface) {
			candidates = append(candidates, a.IPv4)
			if a.Interface == o.DefaultInterface {
				preferred = append(preferred, a.IPv4)
			}
		}
	}
	slices.Sort(available)
	if override == "" {
		if len(preferred) == 1 {
			c.LANIPv4 = preferred[0]
		} else if len(candidates) == 1 {
			c.LANIPv4 = candidates[0]
		}
	}
	if len(available) == 0 {
		return c
	}
	assigned := locales.Format("dynNetAssigned", map[string]any{"addresses": strings.Join(available, "; ")})
	if c.LANIPv4 == "" {
		setCheckText(&c, locales.Join(assigned, locales.Key("dynNetNoSelection")), locales.Key("dynNetChooseAddress"))
		return c
	}
	c.State = session.CheckVerified
	parts := []locales.Text{assigned}
	if override == "" {
		parts = append(parts, locales.Format("dynNetRecommended", map[string]any{"ip": c.LANIPv4}))
	}
	parts = append(parts, locales.Format("dynNetSelected", map[string]any{"ip": c.LANIPv4}))
	setCheckText(&c, locales.Join(parts...), locales.Key("dynNetConnectConsole"))
	return c
}
