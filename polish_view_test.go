package main

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/locales"
	"github.com/thedavidweng/any2jb/retained"
	"github.com/thedavidweng/any2jb/session"
	"github.com/thedavidweng/any2jb/session/sessiontest"
)

// Fixtures use only the controller's public commands, never fabricated evidence.
func TestPolishStateMatrix(t *testing.T) {
	for _, language := range []string{"en", "zh-CN"} {
		t.Run(language, func(t *testing.T) {
			withLocale(t, language)
			for _, state := range []string{"detection", "blocked", "prepare", "selection", "launch", "progress", "restart", "verified", "tool"} {
				t.Run(state, func(t *testing.T) {
					f := sessiontest.NewFakeEnvironment()
					if state == "blocked" {
						// Observed checks only: guidance and preferences are not checks.
						f.Checks = []session.Check{
							{ID: "network", State: session.CheckVerified, Message: locales.DynNetAssignedPrefix() + "en0: 192.0.2.24." + locales.DynNetSelectedPrefix() + "192.0.2.24" + locales.DynNetSelectedSuffix()},
							{ID: "sip", GatesTransport: "usb-hid", State: session.RequiresAttention, Message: locales.DynSipEnabled(), NextAction: locales.DynSipEnabledAction()},
							{ID: "usb-controllers", GatesTransport: "usb-hid", State: session.Available, Message: locales.DynUsbOneController(), NextAction: locales.DynUsbVerifyEnumeration()},
							{ID: "driver-loaded", GatesTransport: "usb-hid", State: session.RequiresAttention, Message: locales.DynUsbKextNotLoaded(), NextAction: locales.DynUsbCompleteApproval()},
							{ID: "engine", State: session.RequiresAttention, Message: "/Users/a-very-long-account-name/Library/Application Support/io.github.thedavidweng.any2jb/sessions/7FBEHYZTUT5Z2PPVHLZXNI7XSD/runs/host-stdout-with-an-unbreakable-segment-0123456789abcdef.log"},
						}
					}
					gate := make(chan struct{})
					if state == "progress" {
						f.PrepareGate = gate
						f.PrepareProgress = session.Progress{Current: 38, Total: 100}
						defer close(gate)
					}
					if state == "tool" {
						f.ToolGate = gate
						defer close(gate)
					}
					controller := session.New(f.Environment(), session.Profile{ID: "test", Revision: "pinned", RetainsRPC: true, Tools: []string{"rpc-status"}})
					if state == "blocked" {
						// USB-only checks apply to a USB method, not to guided manual input.
						if _, err := controller.ConfigureTransport(preferredTransport("keyboard"), ""); err != nil {
							t.Fatal(err)
						}
					}
					a := testApp(t, controller)
					tt := ui.NewTester(a.view, 960, 1400)
					if state != "detection" && state != "restart" {
						a.command(a.controller.Inspect())
						want := session.Ready
						if state == "blocked" {
							want = session.NeedsSetup
						}
						uiStage(t, a, tt, want)
					}
					switch state {
					case "prepare":
						a.flow.stage = flowPrepare
					case "selection":
						a.flow.stage = flowSelection
					case "launch":
						a.flow.stage = flowLaunch
					case "progress", "verified", "tool":
						a.command(a.controller.Start())
						want := session.RetainedRPC
						if state == "progress" {
							want = session.PreparingHost
						}
						if state == "progress" {
							polishBusyStage(t, a, want)
						} else {
							uiStage(t, a, tt, want)
						}
						if state == "tool" {
							a.tools = &fakeToolAdapter{state: retained.State{Tool: "rpc-status"}}
							a.command(a.controller.RunTool("rpc-status"))
							polishBusyStage(t, a, session.RunningTool)
						}
					case "restart":
						a.controller.MarkInterrupted()
					}
					tt.Frame()
					// The first stage is on screen as the window opens; later
					// stages fade in as they replace it.
					if a.flow.stage != flowDetection && !tt.FramePending() {
						t.Fatal("the stage did not fade in")
					}
					tt.Advance(time.Second)
					if tt.FramePending() {
						t.Fatal("the stage entrance keeps asking for frames")
					}
					for _, size := range []struct {
						w, h int
						dark bool
					}{{560, 360, false}, {960, 900, false}, {960, 900, true}} {
						tt.SetSize(size.w, size.h)
						tt.SetDark(size.dark)
						for _, label := range []string{locales.Diagnostics(), locales.Settings()} {
							control, ok := tt.AccessByRole(ui.RoleButton, label)
							if !ok || control.Bounds.X < 0 || control.Bounds.X+control.Bounds.W > float32(size.w) || control.Bounds.Y+control.Bounds.H > float32(size.h) {
								t.Fatalf("corner control outside window: %+v", control)
							}
						}
						if dir := os.Getenv("ANY2JB_POLISH_RENDER_DIR"); dir != "" {
							if err := os.MkdirAll(dir, 0700); err != nil {
								t.Fatal(err)
							}
							name := fmt.Sprintf("%s-%s-%dx%d", language, state, size.w, size.h)
							if size.dark {
								name += "-dark"
							}
							path := filepath.Join(dir, name+".png")
							file, err := os.Create(path)
							if err != nil {
								t.Fatal(err)
							}
							if err := png.Encode(file, tt.Image()); err != nil {
								t.Fatal(err)
							}
							if err := file.Close(); err != nil {
								t.Fatal(err)
							}
						}
					}
					if state == "progress" {
						a.controller.Cancel()
					}
					if state == "verified" {
						a.controller.Finish()
					}
					if state == "tool" {
						a.controller.StopTool()
					}
				})
			}
		})
	}
}

func polishBusyStage(t *testing.T, a *app, stage session.Stage) {
	t.Helper()
	deadline := time.NewTimer(time.Minute)
	defer deadline.Stop()
	for a.controller.Snapshot().Stage != stage {
		select {
		case <-deadline.C:
			t.Fatalf("did not reach %s", stage)
		default:
			runtime.Gosched()
		}
	}
}

func TestPolishRespectsDesktopAccessibilityPreferences(t *testing.T) {
	for _, language := range []string{"en", "zh-CN"} {
		t.Run(language, func(t *testing.T) {
			withLocale(t, language)
			for _, dark := range []bool{false, true} {
				a := newTestApp(t)
				a.preferences.Transport = "keyboard"
				if _, err := a.controller.ConfigureTransport(preferredTransport("keyboard"), ""); err != nil {
					t.Fatal(err)
				}
				tt := ui.NewTester(a.view, 560, 360)
				tt.SetDark(dark)
				normal, ok := tt.AccessByRole(ui.RoleButton, locales.Settings())
				if !ok {
					t.Fatal("Settings is missing")
				}
				tt.SetPreferences(ui.Preferences{HighContrast: true, TextScale: 1.5, ReduceMotion: true})
				tt.Advance(time.Second)
				for _, label := range []string{locales.Diagnostics(), locales.Settings(), locales.FlowNextSelection()} {
					control, ok := tt.AccessByRole(ui.RoleButton, label)
					if !ok || control.Bounds.X < 0 || control.Bounds.Y < 0 ||
						control.Bounds.X+control.Bounds.W > 560 || control.Bounds.Y+control.Bounds.H > 360 {
						t.Fatalf("pinned control outside minimum window: %q %+v", label, control)
					}
				}
				scaled, _ := tt.AccessByRole(ui.RoleButton, locales.Settings())
				if scaled.Bounds.H <= normal.Bounds.H {
					t.Fatal("text scaling did not enlarge the native control")
				}
				if !tt.HasText("○") || !tt.HasText(locales.MacNotChecked()) || !tt.HasText(locales.MacAllow()) {
					t.Fatal("a first-stage step lost its glyph, state or name")
				}
				if tt.FramePending() {
					t.Fatal("reduced-motion stage did not settle")
				}
			}
		})
	}
}
