package main

import (
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/locales"
	"github.com/thedavidweng/any2jb/manifest"
	"github.com/thedavidweng/any2jb/retained"
	"github.com/thedavidweng/any2jb/session"
	"github.com/thedavidweng/any2jb/session/sessiontest"
)

func leasedHomebrewApp(t *testing.T, f *sessiontest.FakeEnvironment, adapter *fakeToolAdapter, tools []string) (*app, *ui.Tester) {
	t.Helper()
	a := retainedApp(t, f, adapter, tools)
	tt := ui.NewTester(a.advancedToolsView, 720, 1600)
	if _, err := a.controller.Inspect(); err != nil {
		t.Fatal(err)
	}
	uiStage(t, a, tt, session.Ready)
	if _, err := a.controller.Start(); err != nil {
		t.Fatal(err)
	}
	uiStage(t, a, tt, session.RetainedRPC)
	return a, tt
}

func awaitToolRequest(t *testing.T, f *sessiontest.FakeEnvironment) session.ToolRequest {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if request := f.ToolRequest; request.Tool != "" {
			return request
		}
		if time.Now().After(deadline) {
			t.Fatalf("no tool request was recorded; trace: %v", f.Trace())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestHomebrewToolsStateTheirRecordedReasons(t *testing.T) {
	m, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	adapter := &fakeToolAdapter{reasons: map[string]string{}}
	for _, pair := range [][2]string{
		{"mintbrew", "mintbrew"}, {"native-hblauncher", "mintbrew-native"}, {"izlauncher", "izlauncher"},
	} {
		d, err := m.DistributionFor(pair[1])
		if err != nil {
			t.Fatal(err)
		}
		adapter.reasons[pair[0]] = d.Reason
	}
	f := sessiontest.NewFakeEnvironment()
	a := retainedApp(t, f, adapter, []string{"mintbrew", "native-hblauncher", "izlauncher"})
	tt := ui.NewTester(a.advancedToolsView, 720, 1600)
	for _, reason := range adapter.reasons {
		if !tt.HasText(reason) {
			t.Fatalf("missing recorded reason %q in %q", reason, tt.Texts())
		}
	}
	for _, label := range []string{"Launch MintBrew", "Launch native HBLauncher", "Launch IZlauncher (experimental)"} {
		if _, ok := tt.AccessByRole(ui.RoleButton, label); ok {
			t.Fatalf("gated tool %s remains an action", label)
		}
	}
}

func TestHomebrewSingleActiveSwitching(t *testing.T) {
	f := sessiontest.NewFakeEnvironment()
	adapter := &fakeToolAdapter{state: retained.State{Tool: "mintbrew"}}
	_, tt := leasedHomebrewApp(t, f, adapter, []string{"mintbrew", "native-hblauncher", "izlauncher"})
	if !tt.HasText("mintbrew owns the retained session. Stop it before starting another tool.") {
		t.Fatal(tt.Texts())
	}
	for _, label := range []string{"Launch native HBLauncher", "Launch IZlauncher (experimental)"} {
		control, ok := tt.AccessByRole(ui.RoleButton, label)
		if !ok || !control.Disabled {
			t.Fatalf("%s started while MintBrew owns the session: %+v", label, control)
		}
	}
}

func TestIZLauncherModuleSelectionReachesTheToolRequest(t *testing.T) {
	f := sessiontest.NewFakeEnvironment()
	adapter := &fakeToolAdapter{}
	a, tt := leasedHomebrewApp(t, f, adapter, []string{"izlauncher"})
	a.homebrewPanel.blitbun = true
	a.homebrewPanel.vacootube = true
	tt.Frame()
	if err := tt.Click("Launch IZlauncher (experimental)"); err != nil {
		t.Fatal(err)
	}
	request := awaitToolRequest(t, f)
	if request.Tool != "izlauncher" || !slices.Equal(request.Modules, []string{"blitbun", "vacootube"}) {
		t.Fatalf("unexpected tool request: %+v", request)
	}
}

func TestNativeLauncherFoldersReachTheToolRequest(t *testing.T) {
	f := sessiontest.NewFakeEnvironment()
	adapter := &fakeToolAdapter{}
	a, tt := leasedHomebrewApp(t, f, adapter, []string{"native-hblauncher"})
	a.homebrewPanel.sdFolder = "/sd/roms/nes"
	a.homebrewPanel.appFolder = t.TempDir()
	tt.Frame()
	if err := tt.Click(homebrewCodeConsent); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Launch native HBLauncher"); err != nil {
		t.Fatal(err)
	}
	request := awaitToolRequest(t, f)
	if request.Tool != "native-hblauncher" || request.SDRomFolder != "/sd/roms/nes" ||
		request.AppFolder != a.homebrewPanel.appFolder || !request.CodeConsent {
		t.Fatalf("unexpected tool request: %+v", request)
	}
	if a.homebrewPanel.nativeConsent {
		t.Fatal("native app consent was not consumed")
	}
}

func TestNativeLauncherRejectsAnInvalidSDFolder(t *testing.T) {
	f := sessiontest.NewFakeEnvironment()
	adapter := &fakeToolAdapter{}
	a, tt := leasedHomebrewApp(t, f, adapter, []string{"native-hblauncher"})
	a.homebrewPanel.sdFolder = "/etc/roms"
	tt.Frame()
	if err := tt.Click("Launch native HBLauncher"); err != nil {
		t.Fatal(err)
	}
	if !a.commandRejected {
		t.Fatal("an invalid SD folder was not rejected")
	}
	if f.ToolRequest.Tool != "" {
		t.Fatalf("an invalid launch reached the tool adapter: %+v", f.ToolRequest)
	}
}

func TestHomeShowsTheRunningToolOwner(t *testing.T) {
	withLocale(t, "zh-CN")
	f := sessiontest.NewFakeEnvironment()
	adapter := &fakeToolAdapter{state: retained.State{Tool: "mintbrew"}}
	a, _ := leasedHomebrewApp(t, f, adapter, []string{"mintbrew"})
	home := ui.NewTester(a.sessionView, 720, 1600)
	if a.controller.Snapshot().Stage != session.RetainedRPC {
		t.Fatal("fixture left the retained stage")
	}
	if _, ok := home.AccessByRole(ui.RoleStatus, locales.ToolRunning("mintbrew")); !ok {
		t.Fatalf("the home status does not show the running owner: %q", home.Texts())
	}
	adapter.state.Tool = ""
	home.Frame()
	if home.HasText(locales.ToolRunning("mintbrew")) {
		t.Fatal("a stopped tool is still shown as running")
	}
}
