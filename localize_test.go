package main

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/locales"
	"github.com/thedavidweng/any2jb/session"
	"github.com/thedavidweng/any2jb/session/sessiontest"
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

func TestDynamicSegmentsMatchSourceCatalog(t *testing.T) {
	withLocale(t, "en")
	for _, segment := range dynamicSegments {
		if got := segment.text(); got != segment.source {
			t.Errorf("catalog source %q does not match table source %q", got, segment.source)
		}
	}
}

func TestLocalizeCoversEveryDynamicSegment(t *testing.T) {
	withLocale(t, "zh-CN")
	for _, segment := range dynamicSegments {
		if got := localize(segment.source); got == segment.source {
			t.Errorf("untranslated dynamic segment: %q", segment.source)
		}
	}
}

func TestLocalizeComposedReadinessMessage(t *testing.T) {
	withLocale(t, "zh-CN")
	en := "Assigned LAN IPv4 addresses: en0: 192.168.1.10. No unambiguous address is selected."
	got := localize(en)
	if !strings.Contains(got, "en0: 192.168.1.10") {
		t.Fatalf("live data was lost: %q", got)
	}
	if strings.Contains(got, "unambiguous") || strings.Contains(got, "Assigned") {
		t.Fatalf("composed message was not localized: %q", got)
	}
}

func TestLocalizeKeepsUnknownText(t *testing.T) {
	withLocale(t, "zh-CN")
	if got := localize("192.168.1.10"); got != "192.168.1.10" {
		t.Fatalf("unknown text changed: %q", got)
	}
}

func TestReadinessMessagesDisplayChinese(t *testing.T) {
	withLocale(t, "zh-CN")
	a := newTestApp(t)
	f := sessiontest.NewFakeEnvironment()
	f.Checks = []session.Check{{ID: "usb-runtime", State: session.RequiresAttention,
		Message:    "Verified USB helpers are not available.",
		NextAction: "USB input requires permitted, integrity-verified helper binaries. Guided manual input does not require these helpers."}}
	a.controller = session.New(f.Environment(), a.controller.Snapshot().Profile)
	tt := ui.NewTester(a.view, 800, 1000)
	if err := tt.Click("检查就绪状态"); err != nil {
		t.Fatal(err)
	}
	uiStage(t, a, tt, session.NeedsSetup)
	for _, text := range []string{
		"已验证的 USB 辅助程序不可用。",
		"USB 输入需要获准且通过完整性验证的辅助程序。引导式手动输入不需要这些辅助程序。",
	} {
		if !tt.HasText(text) {
			t.Errorf("missing %q in %q", text, tt.Texts())
		}
	}
}

func TestEnglishDynamicSegmentsAreSource(t *testing.T) {
	withLocale(t, "en")
	en := "The USB gadget kext is not loaded."
	if got := localize(en); got != en {
		t.Fatalf("source locale changed text: %q", got)
	}
}
