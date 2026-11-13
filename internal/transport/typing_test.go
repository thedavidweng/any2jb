package transport_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/thedavidweng/any2jb/internal/engine"
	"github.com/thedavidweng/any2jb/internal/session"
	"github.com/thedavidweng/any2jb/internal/session/sessiontest"
	"github.com/thedavidweng/any2jb/internal/transport"
)

func TestLaunchWaitsForObservedLoadPromptAndPreservesCommandOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reports")
	var observed []string
	options := transport.DarwinOptions{Controller: "usb-drd0", Timing: transport.DefaultTiming()}
	options.Timing.GapMS = 1
	options.Timing.LoadMS = 0
	options.Confirm = func(_ context.Context, _ session.Run, _ transport.ConsoleStep) error {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		observed = append(observed, decodeConsoleReports(t, data))
		return nil
	}
	d := transport.NewHelperDarwin(options, helperFactory(t, path))
	run := session.Run{ID: "launch", Edition: "full", LANIPv4: "192.0.2.1", Profile: session.Profile{SaveName: "NS2", StartArgumentCount: 5, StartPort: 39380}}
	host := session.PreparedHost{Listening: true, PairingValid: true, Opaque: engine.PairingDetails{RunID: "launch", Command: "V=START(39380,192,0,2,1)"}}
	var completed []int
	err := d.Send(context.Background(), run, session.Launch, host, func(p session.Progress) {
		if p.Total > 0 {
			completed = append(completed, p.Current)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"", "LOAD \"TXT:NS2\"\n", "LOAD \"TXT:NS2\"\n\n", "LOAD \"TXT:NS2\"\n\nRUN\n", "LOAD \"TXT:NS2\"\n\nRUN\nREMOVE NSR,&H60000000,&H3FFFFFFF\n"}
	if !reflect.DeepEqual(observed, want) {
		t.Fatalf("confirmation boundaries %q, want %q", observed, want)
	}
	data, _ := os.ReadFile(path)
	if got := decodeConsoleReports(t, data); got != want[4]+"V=START(39380,192,0,2,1)\n" {
		t.Fatalf("typed %q", got)
	}
	if !reflect.DeepEqual(completed, []int{0, 1, 1, 2, 2, 3, 3, 4}) {
		t.Fatalf("progress %v", completed)
	}
}

func TestBootstrapStopWaitsForNeutralReleaseAndForbidsBlindRetry(t *testing.T) {
	const text = "PRINT 1\nPRINT 2\n"
	sum := sha256.Sum256([]byte(text))
	capture := filepath.Join(t.TempDir(), "released")
	options := transport.DarwinOptions{Controller: "usb-drd0", Timing: transport.DefaultTiming(),
		Bootstrap: func(session.Run) (string, error) { return text, nil }, Confirm: func(context.Context, session.Run, transport.ConsoleStep) error { return nil }}
	baseFactory := helperFactory(t, capture)
	factory := func(name string, args ...string) *exec.Cmd {
		if name != "keyboard" {
			t.Fatalf("unexpected helper %s", name)
		}
		cmd := baseFactory(name, args...)
		cmd.Env = append(cmd.Env, "ANY2JB_ACK_LIMIT=16")
		return cmd
	}
	d := transport.NewHelperDarwin(options, factory)
	f := sessiontest.NewFakeEnvironment()
	f.Checks = append(f.Checks, session.Check{ID: "usb-drd0", Controller: "usb-drd0", State: session.Available, GatesTransport: "usb-hid"})
	env := f.Environment()
	env.Input = d
	c := session.New(env, session.Profile{ID: "fixture", Revision: "pinned", BootstrapSHA256: hex.EncodeToString(sum[:]), RequiredTransport: session.Capabilities{Keyboard: true}})
	t.Cleanup(func() {
		if c.Snapshot().Busy {
			c.Stop()
			awaitTransportStage(t, c, session.Indeterminate)
		}
	})
	c.ConfigureTransport(session.Transport{Mode: "usb-hid", Capabilities: session.Capabilities{Keyboard: true}, Controller: "usb-drd0"}, "192.0.2.1")
	c.Inspect()
	awaitTransportStage(t, c, session.NeedsSetup)
	if _, err := c.PrepareBootstrap(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for c.Snapshot().Current != 1 {
		if time.Now().After(deadline) {
			t.Fatal("no helper line progress")
		}
		time.Sleep(time.Millisecond)
	}
	if _, err := c.PrepareBootstrap(); err == nil {
		t.Fatal("duplicate transfer started")
	}
	if s := c.Snapshot(); !s.HasEstimate || s.Remaining < 0 {
		t.Fatalf("helper-measured estimate was lost: %+v", s)
	}
	if _, err := c.Stop(); err != nil {
		t.Fatal(err)
	}
	awaitTransportStage(t, c, session.Indeterminate)
	data, err := os.ReadFile(capture)
	if err != nil || len(data) < 8 || !reflect.DeepEqual(data[len(data)-8:], make([]byte, 8)) {
		t.Fatalf("Stop reported before neutral release: %v, %v", data, err)
	}
	if _, err := c.PrepareBootstrap(); err == nil {
		t.Fatal("partial transfer offered blind retry")
	}
}

func TestBootstrapUsesAcknowledgedKeyboardReportsAndInheritedIndent(t *testing.T) {
	const text = "  Aa\n B\nC\n"
	sum := sha256.Sum256([]byte(text))
	path := filepath.Join(t.TempDir(), "reports")
	options := transport.DarwinOptions{Controller: "usb-drd0", Timing: transport.DefaultTiming(), Bootstrap: func(session.Run) (string, error) { return text, nil }, Confirm: func(context.Context, session.Run, transport.ConsoleStep) error { return nil }}
	options.Timing.GapMS = 1
	d := transport.NewHelperDarwin(options, helperFactory(t, path))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var lines []int
	run := session.Run{ID: "bootstrap-keyboard", Edition: "full", Profile: session.Profile{BootstrapSHA256: hex.EncodeToString(sum[:])}}
	if err := d.Send(ctx, run, session.Bootstrap, session.PreparedHost{}, func(p session.Progress) {
		if p.Total > 0 {
			lines = append(lines, p.Current)
			if p.Current > 0 && (!p.HasEstimate || p.Remaining < 0) {
				t.Error("no measured acknowledgement-based estimate")
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var keys, mods []byte
	for i := 0; i < len(data); i += 8 {
		if data[i+2] != 0 {
			keys = append(keys, data[i+2])
			mods = append(mods, data[i])
		}
	}
	if want := []byte{44, 44, 4, 4, 40, 42, 5, 40, 42, 6, 40}; !reflect.DeepEqual(keys, want) {
		t.Fatalf("strokes %v, want %v", keys, want)
	}
	// SmileBASIC inverts letter case: lowercase letters carry the shift modifier.
	if want := []byte{0, 0, 0, 2, 0, 0, 0, 0, 0, 0, 0}; !reflect.DeepEqual(mods, want) {
		t.Fatalf("modifiers %v, want %v", mods, want)
	}
	if !reflect.DeepEqual(lines, []int{0, 1, 2, 3}) {
		t.Fatalf("acknowledged line progress %v", lines)
	}
}

func awaitTransportStage(t *testing.T, c *session.SessionController, stage session.Stage) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		s := c.Snapshot()
		if s.Stage == stage && !s.Busy {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("wanted %s, got %+v", stage, s)
		}
		time.Sleep(time.Millisecond)
	}
}

func decodeConsoleReports(t *testing.T, data []byte) string {
	t.Helper()
	if len(data)%8 != 0 {
		t.Fatal("partial report")
	}
	var text strings.Builder
	for i := 0; i < len(data); i += 8 {
		key, shift := data[i+2], data[i]&2 != 0
		switch {
		case key == 0:
		case key >= 4 && key <= 29:
			c := byte('A' + key - 4)
			if shift {
				c += 'a' - 'A'
			}
			text.WriteByte(c)
		case key >= 30 && key <= 39:
			if shift {
				text.WriteByte("!@#$%^&*()"[key-30])
			} else {
				text.WriteByte("1234567890"[key-30])
			}
		case key == 40:
			text.WriteByte('\n')
		case key == 44:
			text.WriteByte(' ')
		default:
			var c byte
			switch key {
			case 45:
				c = '-'
			case 46:
				c = '='
				if shift {
					c = '+'
				}
			case 51:
				if shift {
					c = ':'
				} else {
					c = ';'
				}
			case 52:
				if shift {
					c = '"'
				} else {
					c = '\''
				}
			case 54:
				if shift {
					c = '<'
				} else {
					c = ','
				}
			}
			if c == 0 {
				t.Fatalf("unexpected report %v", data[i:i+8])
			}
			text.WriteByte(c)
		}
	}
	return text.String()
}
