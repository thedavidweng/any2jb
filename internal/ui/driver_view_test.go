package ui

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/internal/driver"
	"github.com/thedavidweng/any2jb/internal/session"
	"github.com/thedavidweng/any2jb/internal/session/sessiontest"
)

func TestDriverPanelBlockedAndDeclinePreservesSession(t *testing.T) {
	a, err := loadApp(filepath.Join(t.TempDir(), "preferences.json"))
	if err != nil {
		t.Fatal(err)
	}
	f := sessiontest.NewFakeEnvironment()
	f.Checks = []session.Check{{ID: "driver-loaded", State: session.RequiresAttention, GatesTransport: "usb-hid", NextAction: "Review driver approval."}}
	if err := a.initializeSession(f.Environment()); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Controller.Inspect(); err != nil {
		t.Fatal(err)
	}
	a.driverPanel.gate = blockedDriverGate(a.Manifest)
	tt := ui.NewTester(a.view, 900, 2200)
	uiStage(t, a, tt, session.Ready)
	before := a.Controller.Snapshot()
	a.flow.settings = true
	tt.Frame()
	if err := tt.Click("Review USB driver setup"); err != nil {
		t.Fatal(err)
	}
	const blockedReason = "USB driver distribution is blocked pending review of exact binary source/build materials and MIT/APSL notice obligations. No driver package or installer is bundled."
	for _, message := range []string{blockedReason, "USB driver and startup security", "This driver is unsigned. It requires reduced startup security and user-approved kernel extensions. Re-enabling full protection with this driver has not been verified."} {
		if !tt.HasText(message) {
			t.Fatalf("missing disclosure %q: %v", message, tt.Texts())
		}
	}
	if button, ok := tt.AccessByRole(ui.RoleButton, "Approve driver installation"); !ok || !button.Disabled {
		t.Fatal("blocked driver approval must be disabled")
	}
	if err := tt.Click("Decline driver installation"); err != nil {
		t.Fatal(err)
	}
	if a.driverPanel.open || !reflect.DeepEqual(before, a.Controller.Snapshot()) {
		t.Fatal("declining changed session state")
	}
	a.Preferences.Transport = "keyboard"
	if err := a.ApplyPreferences(); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Controller.Inspect(); err != nil {
		t.Fatal(err)
	}
	uiStage(t, a, tt, session.NeedsSetup)
	if err := tt.Click("Review USB driver setup"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Use Guided manual launch instead"); err != nil {
		t.Fatal(err)
	}
	if a.driverPanel.open || !a.Controller.Snapshot().Transport.Capabilities.Manual {
		t.Fatal("manual launch path is unavailable after declining the driver")
	}
}

func TestDriverPanelRequiresSecondConfirmationBeforeAdministrator(t *testing.T) {
	a, err := loadApp(filepath.Join(t.TempDir(), "preferences.json"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	for i := range a.Manifest.Components {
		component := &a.Manifest.Components[i]
		switch component.ID {
		case "usbgadget-binary", "usbgadget-info", "usbgadget-checksums", "driver-installer":
			component.SHA256 = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
			name := filepath.Join(root, filepath.FromSlash(component.Path))
			if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(name, []byte("hello"), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	called := make(chan string, 1)
	release := make(chan struct{})
	installed := make(chan func(), 1)
	a.driverPanel = driverPanel{open: true, gate: driver.New(driver.Options{Manifest: a.Manifest, DeveloperSourceRoot: root,
		Administrator: func(_ context.Context, installer string) error { called <- installer; <-release; return nil },
	})}
	a.driverPanel.update = func(apply func()) { installed <- apply }
	tt := ui.NewTester(a.view, 900, 2200)
	if err := tt.Click("Approve driver installation"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-called:
		t.Fatal("first approval skipped confirmation")
	default:
	}
	if err := tt.Click("Back"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Approve driver installation"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Install with administrator approval"); err != nil {
		t.Fatal(err)
	}
	select {
	case installer := <-called:
		if installer != filepath.Join(root, "scripts/install-driver.sh") {
			t.Fatalf("unexpected administrator action: %q", installer)
		}
	case <-time.After(time.Minute):
		t.Fatal("fake administrator was not reached")
	}
	// Receive the worker's result deterministically, then apply it on this
	// goroutine, as Window.Update does on the UI thread.
	close(release)
	select {
	case apply := <-installed:
		apply()
	case <-time.After(time.Minute):
		t.Fatal("fake installation did not complete")
	}
	tt.Frame()
	if !a.driverPanel.result.RestartRequired || !a.driverPanel.result.DeveloperSource {
		t.Fatal("missing restart/developer evidence")
	}
}
