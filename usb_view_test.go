package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/session"
	"github.com/thedavidweng/any2jb/session/sessiontest"
	"github.com/thedavidweng/any2jb/transport"
)

func TestUSBConsoleGateWaitsForExplicitAcknowledgement(t *testing.T) {
	a := testApp(t, session.New(sessiontest.NewFakeEnvironment().Environment(), session.Profile{ID: "fixture", Revision: "pinned"}))
	done := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() { done <- a.usbConfirmation.Wait(ctx, session.Run{ID: "prompt"}, transport.LoadPrompt) }()
	tt := ui.NewTester(a.view, 640, 600)
	deadline := time.After(3 * time.Second)
	for !tt.HasText("Confirm the console shows the LOAD YES or LOADV OK prompt.") {
		select {
		case <-deadline:
			t.Fatal(tt.Texts())
		default:
			time.Sleep(time.Millisecond)
			tt.Frame()
		}
	}
	select {
	case <-done:
		t.Fatal("prompt was acknowledged without a click")
	default:
	}
	if err := tt.Click("Console is ready, continue"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("acknowledgement did not release the gate")
	}
}

func TestNativeKeyboardCaptureReleasesOnFocusLossAndWindowClose(t *testing.T) {
	for _, stop := range []string{"focus", "window-close", "panel-hidden"} {
		t.Run(stop, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "reports")
			d := transport.NewHelperDarwin(transport.DarwinOptions{Controller: "usb-drd0", Timing: transport.DefaultTiming()}, func(string, ...string) *exec.Cmd {
				cmd := exec.Command(os.Args[0], "-test.run=^TestNativeKeyboardUIFixture$")
				// A -race child otherwise sleeps 1s in its exit hook, which close
				// correctly waits for before reporting cleanup.
				cmd.Env = append(os.Environ(), "ANY2JB_UI_KEYBOARD=1", "ANY2JB_UI_REPORTS="+path, "GORACE=atexit_sleep_ms=0")
				return cmd
			})
			f := sessiontest.NewFakeEnvironment()
			f.Checks = []session.Check{{ID: "driver-loaded", State: session.CheckVerified, GatesTransport: "usb-hid"}, {ID: "usb-drd0", State: session.Available, GatesTransport: "usb-hid"}}
			env := f.Environment()
			env.Input = d
			// The panel persists preferences, so the session runs on the
			// bundled profile the manifest can validate.
			m := testManifest(t)
			p, err := m.Profile(m.Default)
			if err != nil {
				t.Fatal(err)
			}
			c := session.New(env, p.SessionProfile())
			c.ConfigureTransport(session.Transport{Mode: "keyboard", Capabilities: session.Capabilities{Keyboard: true}}, "192.0.2.1")
			c.Inspect()
			a := testApp(t, c)
			a.keyboard.input = d
			a.keyboard.open = true
			a.keyboard.controller = "usb-drd0"
			tt := ui.NewTester(a.view, 640, 1600)
			uiStage(t, a, tt, session.NeedsSetup)
			if err := tt.Click("Apply USB settings"); err != nil {
				t.Fatal(err)
			}
			waitKeyboardUI(t, tt, func() bool { return a.keyboard.available })
			if err := tt.Click("Start forwarding"); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if c.Snapshot().Busy {
					a.stopKeyboard()
					uiStage(t, a, tt, session.Indeterminate)
				}
			})
			waitKeyboardUI(t, tt, func() bool { return c.Snapshot().KeyboardOpen && a.keyboard.focused })
			tt.CodeDown(ui.Shift, ui.CodeShiftLeft)
			tt.CodeDown(ui.Shift, ui.CodeKeyA)
			waitKeyboardUI(t, tt, func() bool { return c.Snapshot().Current >= 2 })
			if stop == "panel-hidden" {
				// Hiding the panel goes through its own handler, which
				// releases live forwarding before the capture disappears.
				a.closeKeyboardPanel()
				tt.Frame()
			} else if stop == "focus" {
				tt.SetFocused(false)
				tt.Frame()
			} else {
				a.stopKeyboard()
			}
			uiStage(t, a, tt, session.Indeterminate)
			data, err := os.ReadFile(path)
			if err != nil || len(data) < 24 {
				t.Fatalf("missing reports %v, %v", data, err)
			}
			want := []byte{2, 0, 0, 0, 0, 0, 0, 0, 2, 0, 4, 0, 0, 0, 0, 0}
			if !reflect.DeepEqual(data[:16], want) || !reflect.DeepEqual(data[len(data)-8:], make([]byte, 8)) {
				t.Fatalf("physical forwarding/release %v", data)
			}
		})
	}
}

func waitKeyboardUI(t *testing.T, tt *ui.Tester, ready func() bool) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for !ready() {
		select {
		case <-deadline:
			t.Fatal("keyboard UI did not reach expected state")
		case <-time.After(time.Millisecond):
			tt.Frame()
		}
	}
}

func TestNativeKeyboardUIFixture(t *testing.T) {
	if os.Getenv("ANY2JB_UI_KEYBOARD") != "1" {
		return
	}
	f, err := os.Create(os.Getenv("ANY2JB_UI_REPORTS"))
	if err != nil {
		os.Exit(2)
	}
	defer f.Close()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM)
	go func() {
		<-sig
		f.Write(make([]byte, 8))
		fmt.Println("MAC2JB_KEYBOARD_RELEASED")
		f.Close()
		os.Exit(0)
	}()
	out := bufio.NewWriter(os.Stdout)
	fmt.Fprintln(out, "MAC2JB_KEYBOARD_READY")
	out.Flush()
	for n := 1; ; n++ {
		var report [8]byte
		_, err := io.ReadFull(os.Stdin, report[:])
		if err == io.EOF {
			fmt.Fprintln(out, "MAC2JB_KEYBOARD_RELEASED")
			out.Flush()
			return
		}
		if err != nil {
			os.Exit(2)
		}
		f.Write(report[:])
		fmt.Fprintf(out, "MAC2JB_KEYBOARD_SENT %d\n", n)
		out.Flush()
	}
}

func TestUSBPanelDoesNotOfferForwardingWithoutVerifiedHelpers(t *testing.T) {
	a := &app{manifest: testManifest(t)}
	a.keyboard.open = true
	a.keyboard.defaults(nil)
	tt := ui.NewTester(a.keyboardView, 640, 720)
	if !tt.HasText("Live keyboard forwarding") || !tt.HasText("USB helpers are unavailable. Use guided manual input.") {
		t.Fatal(tt.Texts())
	}
	start, ok := tt.AccessByRole(ui.RoleButton, "Start forwarding")
	if !ok || !start.Disabled {
		t.Fatal("unavailable helper offered usable forwarding")
	}
}

func TestKeyboardAvailabilitySurvivesReInspectionAfterControllerChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reports")
	d := transport.NewHelperDarwin(transport.DarwinOptions{Controller: "usb-drd0", Timing: transport.DefaultTiming()}, func(string, ...string) *exec.Cmd {
		cmd := exec.Command(os.Args[0], "-test.run=^TestNativeKeyboardUIFixture$")
		cmd.Env = append(os.Environ(), "ANY2JB_UI_KEYBOARD=1", "ANY2JB_UI_REPORTS="+path, "GORACE=atexit_sleep_ms=0")
		return cmd
	})
	f := sessiontest.NewFakeEnvironment()
	f.Checks = []session.Check{{ID: "driver-loaded", State: session.CheckVerified, GatesTransport: "usb-hid"}, {ID: "usb-drd0", State: session.Available, GatesTransport: "usb-hid"}}
	env := f.Environment()
	env.Input = d
	m := testManifest(t)
	p, err := m.Profile(m.Default)
	if err != nil {
		t.Fatal(err)
	}
	c := session.New(env, p.SessionProfile())
	c.ConfigureTransport(session.Transport{Mode: "keyboard", Capabilities: session.Capabilities{Keyboard: true}}, "192.0.2.1")
	c.Inspect()
	a := testApp(t, c)
	a.keyboard.input = d
	a.keyboard.open = true
	a.keyboard.controller = "usb-drd0"
	tt := ui.NewTester(a.view, 640, 1600)
	uiStage(t, a, tt, session.NeedsSetup)
	// ConfigureTransport clears the observed checks and re-inspects. Hold that
	// inspection while the helper check finishes: availability must recover
	// once observation returns, not stay false forever.
	gate := make(chan struct{})
	f.InspectGate = gate
	if err := tt.Click("Apply USB settings"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 200 && !a.keyboard.checkOK; i++ {
		time.Sleep(time.Millisecond)
		tt.Frame()
	}
	if !a.keyboard.checkOK {
		t.Fatal("helper check did not complete behind the held inspection")
	}
	close(gate)
	waitKeyboardUI(t, tt, func() bool { return a.keyboard.available })
}
