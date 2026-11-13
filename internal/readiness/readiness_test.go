package readiness_test

import (
	"context"
	"testing"

	"github.com/thedavidweng/any2jb/internal/readiness"
	"github.com/thedavidweng/any2jb/internal/session"
)

func TestLANRecommendationAndOverride(t *testing.T) {
	o := readiness.Observation{Addresses: []readiness.Address{
		{Interface: "en0", IPv4: "192.168.1.10"},
		{Interface: "en1", IPv4: "10.0.0.4"},
	}, DefaultInterface: "en1"}
	p := readiness.Inspector{Observe: func(context.Context) readiness.Observation { return o }}
	for _, tc := range []struct {
		override, want string
		state          session.CheckState
	}{
		{"", "10.0.0.4", session.CheckVerified},
		{"192.168.1.10", "192.168.1.10", session.CheckVerified},
		{"192.168.1.99", "", session.RequiresAttention},
		{"127.0.0.1", "", session.RequiresAttention},
	} {
		checks, err := p.Inspect(context.Background(), session.Run{LANIPv4: tc.override})
		if err != nil {
			t.Fatal(err)
		}
		c := checks[0]
		if c.ID != "network" || c.LANIPv4 != tc.want || c.State != tc.state {
			t.Fatalf("override %q: %+v", tc.override, c)
		}
		if tc.state == session.RequiresAttention && c.NextAction != "Choose an assigned LAN IPv4 address below, then refresh readiness." {
			t.Fatal(c)
		}
	}
}
