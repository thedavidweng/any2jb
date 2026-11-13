package readiness

import (
	"context"
	"net"
	"net/netip"
)

func New() Inspector { return Inspector{Observe: observe} }

func observe(ctx context.Context) Observation {
	o := Observation{}
	interfaces, err := net.Interfaces()
	o.NetworkError = err
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, err := iface.Addrs()
		if err != nil {
			o.NetworkError = err
			continue
		}
		for _, address := range addresses {
			prefix, err := netip.ParsePrefix(address.String())
			if err == nil && usable(prefix.Addr().String()) {
				o.Addresses = append(o.Addresses, Address{Interface: iface.Name, IPv4: prefix.Addr().String()})
			}
		}
	}
	observePlatform(ctx, &o)
	return o
}
