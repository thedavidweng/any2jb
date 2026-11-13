package parity

import "testing"

func TestWhitelistIntegrity(t *testing.T) {
	if len(Whitelist) == 0 {
		t.Fatal("expected non-empty intentional difference whitelist")
	}
	seen := make(map[string]bool, len(Whitelist))
	for _, d := range Whitelist {
		if d.ID == "" || d.Layer == "" || d.Summary == "" || d.Rationale == "" {
			t.Fatalf("incomplete whitelist entry: %+v", d)
		}
		if seen[d.ID] {
			t.Fatalf("duplicate whitelist ID %q", d.ID)
		}
		seen[d.ID] = true
	}
}
