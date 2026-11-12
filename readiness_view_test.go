package main

import (
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/locales"
	"github.com/thedavidweng/any2jb/session"
	"github.com/thedavidweng/any2jb/session/sessiontest"
)

func TestRoutineReadinessStatesUseNeutralInk(t *testing.T) {
	for _, dark := range []bool{false, true} {
		tt := ui.NewTester(func(c *ui.Context) {
			applyTheme(c)
			ui.Column(c).Fill().Children(func() {
				readinessView(c, session.Snapshot{Checks: []session.Check{
					{ID: "network", State: session.CheckVerified},
					{ID: "usb-controllers", State: session.Available},
				}}, func(session.Check) bool { return true })
			})
		}, 720, 300)
		tt.SetDark(dark)
		for _, label := range []string{locales.CheckVerified(), locales.CheckAvailable()} {
			if _, ok := tt.AccessByRole(ui.RoleStatus, label); !ok {
				t.Fatalf("missing readable status %q", label)
			}
		}
		for _, glyph := range []string{"✓", "○"} {
			if !tt.HasText(glyph) {
				t.Fatalf("missing status glyph %q", glyph)
			}
		}
		img := tt.Image()
		for y := range img.Bounds().Dy() {
			for x := range img.Bounds().Dx() {
				r, g, b, _ := img.At(x, y).RGBA()
				if max(r, g, b)-min(r, g, b) > 12*257 {
					t.Fatalf("routine status uses colored chrome at %d,%d (dark=%v)", x, y, dark)
				}
			}
		}
	}
}

// The first stage states each step plainly; the adapters' exact observations
// stay in Diagnostics, and the LAN override in Settings.
func TestRawReadinessEvidenceLivesInDiagnostics(t *testing.T) {
	a, err := loadApp(filepath.Join(t.TempDir(), "preferences.json"))
	if err != nil {
		t.Fatal(err)
	}
	a.driverPanel.gate = blockedDriverGate(a.manifest)
	f := sessiontest.NewFakeEnvironment()
	f.Checks = []session.Check{
		{ID: "network", State: session.CheckVerified, Message: "Assigned locally."},
		{ID: "sip", State: session.RequiresAttention, GatesTransport: "usb-hid", Message: "SIP is enabled. The unsigned USB gadget driver requires SIP disabled.", NextAction: "Disable SIP yourself in macOS Recovery before using the unsigned USB gadget driver, or choose guided manual input. This app never changes security settings."},
		{ID: "usb-drd0", State: session.Available, GatesTransport: "usb-hid", Message: "Not enumerated."},
		{ID: "driver-loaded", State: session.RequiresAttention, GatesTransport: "usb-hid", Message: "Not loaded.", NextAction: "Review driver approval."},
	}
	if err := a.openSession(f.Environment(), false); err != nil {
		t.Fatal(err)
	}
	a.diagnostic.ready = true
	tt := ui.NewTester(a.view, 720, 1800)
	uiStage(t, a, tt, session.Ready)
	raw := []string{"Assigned locally.", "SIP is enabled. The unsigned USB gadget driver requires SIP disabled.", locales.NextStep("Review driver approval."), "Not enumerated."}
	for _, text := range raw {
		if tt.HasText(text) {
			t.Fatalf("raw check text on the first stage: %q", text)
		}
	}
	if _, ok := tt.AccessByRole(ui.RoleTextField, locales.LanOverride()); ok {
		t.Fatal("the LAN override is still on the first stage")
	}
	clickManual(t, tt, locales.Diagnostics())
	for _, text := range append(raw, locales.CheckVerified(), locales.CheckAvailable(), locales.CheckAttention()) {
		if !tt.HasText(text) {
			t.Fatalf("Diagnostics lacks %q: %v", text, tt.Texts())
		}
	}
	clickManual(t, tt, locales.Diagnostics())
	clickManual(t, tt, locales.Settings())
	if _, ok := tt.AccessByRole(ui.RoleTextField, locales.LanOverride()); !ok {
		t.Fatal("the LAN override is missing from Settings")
	}
	reference := ui.NewTester(func(c *ui.Context) { referencePrerequisites(c, true) }, 720, 1800)
	for _, text := range []string{
		"Reference prerequisites, not compatibility guarantees",
		"Switch 2 firmware 23.0.1 is the reference target. Newer firmware is not assumed compatible.",
		"Use a USB-C data cable, not a charge-only cable. The Mac must receive power from the console for the required USB data direction.",
	} {
		if !reference.HasText(text) {
			t.Fatalf("missing %q", text)
		}
	}
}
