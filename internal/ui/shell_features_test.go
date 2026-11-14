package ui

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	appstate "github.com/thedavidweng/any2jb/internal/app"
	"github.com/thedavidweng/any2jb/internal/plugins"
	"github.com/thedavidweng/any2jb/internal/session/sessiontest"
	"github.com/thedavidweng/any2jb/locales"
)

var _ appstate.PluginManager = (*fakePluginManager)(nil)

type fakePluginManager struct {
	*fakeToolAdapter
	mu        sync.Mutex
	support   string
	toolRoot  string
	installed map[string]bool
	installs  []string
	removes   []string
}

func newFakePluginManager(t *testing.T) *fakePluginManager {
	t.Helper()
	return &fakePluginManager{
		fakeToolAdapter: &fakeToolAdapter{},
		support:         t.TempDir(),
		toolRoot:        t.TempDir(),
		installed:       map[string]bool{},
	}
}

func (f *fakePluginManager) PluginsStatus() []plugins.Status {
	f.mu.Lock()
	defer f.mu.Unlock()
	specs := plugins.All()
	out := make([]plugins.Status, 0, len(specs))
	for _, spec := range specs {
		out = append(out, plugins.Status{
			Spec:      spec,
			Installed: f.installed[spec.ID],
			Supported: true,
		})
	}
	return out
}

func (f *fakePluginManager) InstallPlugins(_ context.Context, selectors ...string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.installs = append(f.installs, selectors...)
	ids, err := plugins.ExpandTargets("darwin-arm64", selectors...)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		f.installed[id] = true
	}
	return ids, nil
}

func (f *fakePluginManager) UninstallPlugins(_ context.Context, selectors ...string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removes = append(f.removes, selectors...)
	ids, err := plugins.ExpandTargets("darwin-arm64", selectors...)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		delete(f.installed, id)
	}
	return ids, nil
}

func (f *fakePluginManager) ToolRoot() string            { return f.toolRoot }
func (f *fakePluginManager) SupportRoot() string         { return f.support }
func (f *fakePluginManager) NodePath() string            { return "" }
func (f *fakePluginManager) HomebrewRuntimeRoot() string { return f.toolRoot }

func TestAboutScreenRendersMarkdownCreditsAndGitHubLink(t *testing.T) {
	a := testApp(t, nil)
	tt := ui.NewTester(a.view, 900, 1200)
	if err := tt.Click(locales.AboutTitle()); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(tt.Texts(), "\n")
	for _, expected := range []string{
		"https://github.com/thedavidweng/any2jb",
		"cpypwn/mac2jb",
		"Cellenseres/ns2jb",
		"pepsipu/ns2jb",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("about screen missing %q in %q", expected, joined)
		}
	}
	if _, ok := tt.AccessByRole(ui.RoleLink, locales.AboutOpenGitHub()); !ok {
		t.Fatalf("missing official GitHub link control in %q", tt.Texts())
	}
}

func TestLanguageSwitcherDefaultsToSystemAndAppliesSwitch2Languages(t *testing.T) {
	t.Cleanup(func() { i18n.SetLocaleFunc(nil) })
	a := testApp(t, nil)
	if a.Preferences.Language != "" {
		t.Fatalf("expected default language preference to follow system (empty), got %q", a.Preferences.Language)
	}
	var applied []string
	a.applyLocale = func(tag string) {
		applied = append(applied, tag)
		if tag == "" {
			i18n.SetLocaleFunc(func() string { return "en" })
		} else {
			i18n.SetLocaleFunc(func() string { return tag })
		}
	}
	tt := ui.NewTester(a.view, 960, 800)
	if !tt.HasText(locales.LanguageLabel() + ": " + locales.LanguageSystem()) {
		t.Fatalf("missing default Follow System label in %q", tt.Texts())
	}
	if err := tt.Click(locales.LanguageLabel() + ": " + locales.LanguageSystem()); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("简体中文"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.Preferences.Language != "zh-CN" {
		t.Fatalf("expected saved preference zh-CN, got %q", a.Preferences.Language)
	}
	if len(applied) != 1 || applied[0] != "zh-CN" {
		t.Fatalf("unexpected locale callbacks: %v", applied)
	}
	reloaded, err := loadApp(a.PreferencesPath)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Preferences.Language != "zh-CN" {
		t.Fatalf("persisted language preference = %q, want zh-CN", reloaded.Preferences.Language)
	}
}

func TestPluginsPanelInstallsAndUninstallsOffUIThread(t *testing.T) {
	a := testApp(t, nil)
	pm := newFakePluginManager(t)
	a.Tools = pm
	updates := make(chan func(), 2)
	a.pluginsPanel.update = func(fn func()) { updates <- fn }

	tt := ui.NewTester(a.view, 960, 1000)
	if err := tt.Click(locales.PluginsTitle()); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click(locales.PluginsInstall()); err != nil {
		t.Fatal(err)
	}
	select {
	case apply := <-updates:
		apply()
		tt.Frame()
	case <-time.After(2 * time.Second):
		t.Fatal("plugin install did not complete")
	}
	if len(pm.installs) != 1 {
		t.Fatalf("expected 1 install call, got %v", pm.installs)
	}
	if err := tt.Click(locales.PluginsUninstall()); err != nil {
		t.Fatal(err)
	}
	select {
	case apply := <-updates:
		apply()
		tt.Frame()
	case <-time.After(2 * time.Second):
		t.Fatal("plugin uninstall did not complete")
	}
	if len(pm.removes) != 1 {
		t.Fatalf("expected 1 uninstall call, got %v", pm.removes)
	}
}

func TestUtilitiesPanelGatesArtemisWriteAndEvalOnConsent(t *testing.T) {
	a := testApp(t, nil)
	a.Tools = newFakePluginManager(t)
	a.utilitiesPanel.update = func(fn func()) { fn() }
	tt := ui.NewTester(a.view, 960, 1400)
	if err := tt.Click(locales.UtilTitle()); err != nil {
		t.Fatal(err)
	}
	a.utilitiesPanel.artemisHost = "192.0.2.9"
	a.utilitiesPanel.artemisRemotePath = "/save/test.txt"
	a.utilitiesPanel.artemisLocalPath = "/tmp/test.txt"
	a.utilitiesPanel.artemisLua = "return 1"
	tt.Frame()
	for _, label := range []string{locales.ArtemisEval(), locales.ArtemisPut(), locales.ArtemisRemove()} {
		ctrl, ok := tt.AccessByRole(ui.RoleButton, label)
		if !ok || !ctrl.Disabled {
			t.Fatalf("expected %q to stay disabled without consent, got %+v", label, ctrl)
		}
	}
	if err := tt.Click(locales.ArtemisWriteConsent()); err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{locales.ArtemisEval(), locales.ArtemisPut(), locales.ArtemisRemove()} {
		ctrl, ok := tt.AccessByRole(ui.RoleButton, label)
		if !ok || ctrl.Disabled {
			t.Fatalf("expected %q to enable after consent, got %+v", label, ctrl)
		}
	}
}

func TestModloaderSDModeSendsEmptyModsFolder(t *testing.T) {
	f := sessiontest.NewFakeEnvironment()
	a, tt := leasedHomebrewApp(t, f, &fakeToolAdapter{}, []string{"modloader"})
	a.homebrewPanel.modSource = "sd"
	tt.Frame()
	if err := tt.Click(modConsent); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click(installMods); err != nil {
		t.Fatal(err)
	}
	request := awaitToolRequest(t, f)
	if request.Tool != "modloader" || request.ModsFolder != "" || !request.CodeConsent {
		t.Fatalf("unexpected SD modloader request: %+v", request)
	}
}
