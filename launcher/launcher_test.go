package launcher_test

import (
	"testing"

	"github.com/thedavidweng/any2jb/launcher"
	"github.com/thedavidweng/any2jb/manifest"
	"github.com/thedavidweng/any2jb/savekit"
)

func TestLauncherRestrictions(t *testing.T) {
	m, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range launcher.IDs {
		c, err := m.ToolComponent(id)
		if err != nil {
			t.Fatal(err)
		}
		target := savekit.Request{TitleID: c.Launcher.TitleID}
		r := launcher.Request{ID: id}
		if id == "launcher-balatro" || id == "launcher-balatro-showcase" {
			r.PhysicalCompatibility = true
		}
		if id == "launcher-gravity-circuit" {
			r.Version = "1.2.1"
		}
		if err := launcher.Validate(r, c, target); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		target.TitleID = "ffffffffffffffff"
		if err := launcher.Validate(r, c, target); err == nil {
			t.Fatalf("%s accepted wrong game/edition", id)
		}
		d, _ := m.DistributionFor(id)
		if d.Available() || d.Bundled || d.Reason == "" {
			t.Fatalf("%s bypassed rights gate", id)
		}
	}
	c, _ := m.ToolComponent("launcher-balatro")
	if err := launcher.Validate(launcher.Request{ID: c.ID}, c, savekit.Request{TitleID: c.Launcher.TitleID}); err == nil {
		t.Fatal("accepted digital/native Switch 2 Balatro")
	}
	c, _ = m.ToolComponent("launcher-gravity-circuit")
	if err := launcher.Validate(launcher.Request{ID: c.ID, Version: "1.2.0"}, c, savekit.Request{TitleID: c.Launcher.TitleID}); err == nil {
		t.Fatal("accepted unsupported Gravity Circuit version")
	}
}
