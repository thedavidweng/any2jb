package manifest

import (
	"slices"
	"testing"
)

func TestSessionProfileMatrix(t *testing.T) {
	m, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []struct {
		id                                string
		arity, port                       int
		save                              string
		held, demo, savekit, native, mods bool
	}{
		{"pepsipu", 9, 39380, "NS2", false, false, false, false, false},
		{"kurumuz", 5, 39380, "NS2", true, true, true, true, true},
		{"cellenseres", 5, 39380, "NS2", true, true, true, true, true},
		{"BermTheSequel", 5, 39380, "NS2", true, true, false, false, false},
		{"izmysh", 9, 39395, "NS2I", false, false, false, false, false},
		{"astral", 5, 39380, "NS2", true, true, true, true, false},
	} {
		t.Run(want.id, func(t *testing.T) {
			p, err := m.Profile(want.id)
			if err != nil {
				t.Fatal(err)
			}
			s := p.SessionProfile()
			if s.StartArgumentCount != want.arity || s.StartPort != want.port || s.SaveName != want.save ||
				s.RetainsRPC != want.held || slices.Contains(s.Editions, "demo") != want.demo ||
				slices.Contains(s.Tools, "savekit") != want.savekit || slices.Contains(s.Tools, "native-hblauncher") != want.native ||
				slices.Contains(s.Tools, "modloader") != want.mods || s.BootstrapSHA256 != p.BootstrapSHA256 || s.SourceSHA256 != p.SourceSHA256 {
				t.Fatalf("incorrect projection: %+v", s)
			}
		})
	}
}
