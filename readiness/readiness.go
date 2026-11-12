// Package readiness observes local prerequisites without installing or loading drivers.
package readiness

import (
	"context"
	"net/netip"
	"slices"
	"strings"

	"github.com/thedavidweng/any2jb/session"
)

type Address struct {
	Interface string
	IPv4      string
}

type Observation struct {
	SIPStatus        string
	Addresses        []Address
	DefaultInterface string
	NetworkError     error
	USBSupported     bool
	Driver           Driver
	Controllers      []string
	ControllerError  error
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

func networkCheck(o Observation, override string) session.Check {
	c := session.Check{ID: "network", State: session.RequiresAttention,
		Message:    "No usable LAN IPv4 address was found.",
		NextAction: "Connect this Mac to the console LAN, then refresh readiness."}
	if o.NetworkError != nil {
		c.Message = "LAN interface discovery could not be completed."
		c.NextAction = "Check network interface access, then refresh readiness."
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
		if ip.IsPrivate() && !strings.HasPrefix(a.Interface, "utun") && !strings.HasPrefix(a.Interface, "bridge") &&
			!strings.HasPrefix(a.Interface, "vmnet") && !strings.HasPrefix(a.Interface, "vnic") &&
			!strings.HasPrefix(a.Interface, "awdl") && !strings.HasPrefix(a.Interface, "llw") {
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
	c.Message = "Assigned LAN IPv4 addresses: " + strings.Join(available, "; ") + "."
	if c.LANIPv4 == "" {
		c.Message += " No unambiguous address is selected."
		c.NextAction = "Choose an assigned LAN IPv4 address below, then refresh readiness."
		return c
	}
	c.State = session.CheckVerified
	if override == "" {
		c.Message += " Recommended address: " + c.LANIPv4 + " (available but not verified for console connectivity)."
	}
	c.Message += " Selected: " + c.LANIPv4 + ". Local assignment verified; console connectivity is not verified."
	c.NextAction = "Connect the console by Ethernet on the reachable LAN; host listening and console connection are checked during launch."
	return c
}
