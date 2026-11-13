package launcher_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/thedavidweng/any2jb/launcher"
	"github.com/thedavidweng/any2jb/manifest"
	"github.com/thedavidweng/any2jb/savekit"
)

func TestLauncherChoicesAndVersionRefusalFollowManifest(t *testing.T) {
	m, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	c, err := m.ToolComponent("launcher-gravity-circuit")
	if err != nil {
		t.Fatal(err)
	}
	c.ID = "launcher-fixture"
	c.Launcher.Versions = []string{"2.0", "3.0"}
	m.Components = append(m.Components, c)
	if !slices.Contains(launcher.IDs(m), c.ID) {
		t.Fatal("manifest launcher omitted from choices")
	}
	err = launcher.Validate(launcher.Request{ID: c.ID}, c, savekit.Request{TitleID: c.Launcher.TitleID})
	if err == nil || !strings.Contains(err.Error(), "launcher-fixture requires version 2.0 or 3.0") {
		t.Fatalf("version refusal ignored component metadata: %v", err)
	}
}

func TestLauncherRestrictions(t *testing.T) {
	m, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range launcher.IDs(m) {
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
		if !d.Available() || !d.Bundled || d.Reason == "" {
			t.Fatalf("%s is not marked bundled and available with a reason", id)
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
