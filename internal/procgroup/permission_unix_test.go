//go:build darwin || linux

package procgroup

import "testing"

func TestPermissionErrorRequiresProofNoLiveMemberRemains(t *testing.T) {
	for _, tc := range []struct {
		name, output string
		gone         bool
	}{
		{"zombies", "42 Z\n42 Z+\n8 S", true},
		{"already reaped", "8 S", true},
		{"live leader", "42 S\n42 Z", false},
		{"live descendant", "42 Z\n42 R+", false},
		{"unreadable", "", false},
		{"malformed", "42", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := onlyZombies(tc.output, 42); got != tc.gone {
				t.Fatalf("gone = %t, want %t", got, tc.gone)
			}
		})
	}
}
