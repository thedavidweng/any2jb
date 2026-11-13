package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	appstate "github.com/thedavidweng/any2jb/internal/app"
	"github.com/thedavidweng/any2jb/internal/session"
	"github.com/thedavidweng/any2jb/internal/session/sessiontest"
	"github.com/thedavidweng/any2jb/locales"
)

func withLocale(t *testing.T, locale string) {
	t.Helper()
	i18n.SetLocaleFunc(func() string { return locale })
	t.Cleanup(func() { i18n.SetLocaleFunc(nil) })
}

func TestChineseCatalogRegistered(t *testing.T) {
	if got := locales.In("zh-CN").StartJailbreak(); got != "开始越狱" {
		t.Fatalf("zh-CN catalog not registered: %q", got)
	}
	if got := locales.In("zh").StartJailbreak(); got != "开始越狱" {
		t.Fatalf("a system language of zh should match zh-CN: %q", got)
	}
}

func TestStructuredComposedReadinessMessage(t *testing.T) {
	msg := locales.Join(
		locales.Format("dynNetAssigned", map[string]any{"addresses": "en0: 192.168.1.10"}),
		locales.Key("dynNetNoSelection"),
	)
	if got := msg.English(); got != "Assigned LAN IPv4 addresses: en0: 192.168.1.10. No unambiguous address is selected." {
		t.Fatalf("unexpected source message: %q", got)
	}
	withLocale(t, "zh-CN")
	got := msg.Local()
	if !strings.Contains(got, "en0: 192.168.1.10") {
		t.Fatalf("live data was lost: %q", got)
	}
	if strings.Contains(got, "unambiguous") || strings.Contains(got, "Assigned") {
		t.Fatalf("composed message was not localized: %q", got)
	}
	if strings.Contains(got, "。 ") {
		t.Fatalf("unexpected ASCII space after fullwidth period: %q", got)
	}
}

func TestPresentErrorHandlesStructuredAndPlainErrors(t *testing.T) {
	withLocale(t, "zh-CN")
	if got := appstate.PresentError(errors.New("192.168.1.10")); got != "192.168.1.10" {
		t.Fatalf("unknown text changed: %q", got)
	}
	wrapped := locales.WrapError("dynDrvInstallIncomplete", locales.Key("dynDrvAdminFailed"))
	if got := appstate.PresentError(wrapped); strings.Contains(got, "driver installation") || strings.Contains(got, "administrator") {
		t.Fatalf("wrapped catalog error was not localized: %q", got)
	}
	withLocale(t, "en")
	if got := appstate.PresentError(wrapped); got != "driver installation did not complete; refresh readiness before retrying: administrator authorization or driver installation failed" {
		t.Fatalf("unexpected English wrapped error: %q", got)
	}
}

func TestReadinessMessagesDisplayChinese(t *testing.T) {
	withLocale(t, "zh-CN")
	a := newTestApp(t)
	f := sessiontest.NewFakeEnvironment()
	msg := locales.Key("dynRtHelpersUnavailable")
	action := locales.Key("dynRtHelpersRequired")
	f.Checks = []session.Check{{
		ID:             session.CheckUSBRuntime,
		State:          session.RequiresAttention,
		Message:        msg.English(),
		MessageText:    msg,
		NextAction:     action.English(),
		NextActionText: action,
	}}
	a.Controller = session.New(f.Environment(), a.Controller.Snapshot().Profile)
	a.Controller.ConfigureTransport(appstate.PreferredTransport("keyboard"), "")
	a.Preferences.Transport = "keyboard"
	a.diagnostic.ready = true
	tt := ui.NewTester(a.view, 800, 1000)
	if err := tt.Click("重新检查"); err != nil {
		t.Fatal(err)
	}
	uiStage(t, a, tt, session.NeedsSetup)
	clickManual(t, tt, locales.Diagnostics())
	for _, text := range []string{
		"已验证的 USB 辅助程序不可用。",
		"USB 输入需要获准且通过完整性验证的辅助程序。引导式手动输入不需要这些辅助程序。",
	} {
		if !tt.HasText(text) {
			t.Errorf("missing %q in %q", text, tt.Texts())
		}
	}
}
