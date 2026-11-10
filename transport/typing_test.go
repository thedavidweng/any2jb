package transport_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/thedavidweng/any2jb/engine"
	"github.com/thedavidweng/any2jb/session"
	"github.com/thedavidweng/any2jb/transport"
)

func TestLaunchWaitsForObservedLoadPromptAndPreservesCommandOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reports")
	var observed []string
	options := transport.DarwinOptions{Controller: "usb-drd0", Timing: transport.DefaultTiming()}
	options.Timing.GapMS = 1
	options.Timing.LoadMS = 0
	options.Confirm = func(_ context.Context, _ session.Run, _ string) error {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		observed = append(observed, decodeConsoleReports(t, data))
		return nil
	}
	d := transport.NewHelperDarwin(options, helperFactory(t, path))
	run := session.Run{ID: "launch", Edition: "full", Profile: session.Profile{SaveName: "NS2"}}
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
		Bootstrap: func(session.Run) (string, error) { return text, nil }}
	factory := func(name string, args ...string) *exec.Cmd {
		if name != "sender" {
			t.Fatalf("unexpected helper %s", name)
		}
		data, err := os.ReadFile(args[0])
		if err != nil || string(data) != text {
			t.Fatalf("wrong bootstrap %q, %v", data, err)
		}
		cmd := exec.Command(os.Args[0], "-test.run=^TestSenderHelperProcess$")
		cmd.Env = append(os.Environ(), "ANY2JB_SENDER_FIXTURE=1", "ANY2JB_CAPTURE="+capture)
		return cmd
	}
	d := transport.NewHelperDarwin(options, factory)
	f := session.NewFakeEnvironment()
	env := f.Environment()
	env.Input = d
	c := session.New(env, session.Profile{ID: "fixture", Revision: "pinned", BootstrapSHA256: hex.EncodeToString(sum[:]), RequiredTransport: session.Capabilities{Keyboard: true}})
	t.Cleanup(func() {
		if c.Snapshot().Busy {
			c.Stop()
			awaitTransportStage(t, c, session.Indeterminate)
		}
	})
	c.ConfigureTransport(session.Transport{Mode: "usb-hid", Capabilities: session.Capabilities{Keyboard: true}}, "192.0.2.1")
	c.Inspect()
	awaitTransportStage(t, c, session.Ready)
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
	if s := c.Snapshot(); !s.HasEstimate || s.Remaining != 1250*time.Millisecond {
		t.Fatalf("helper-measured estimate was lost: %+v", s)
	}
	if _, err := c.Stop(); err != nil {
		t.Fatal(err)
	}
	awaitTransportStage(t, c, session.Indeterminate)
	data, err := os.ReadFile(capture)
	if err != nil || !reflect.DeepEqual(data, make([]byte, 8)) {
		t.Fatalf("Stop reported before neutral release: %v, %v", data, err)
	}
	if _, err := c.PrepareBootstrap(); err == nil {
		t.Fatal("partial transfer offered blind retry")
	}
}

func TestSenderHelperProcess(t *testing.T) {
	if os.Getenv("ANY2JB_SENDER_FIXTURE") != "1" {
		return
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	fmt.Println(`MAC2JB_GUI {"type":"line-start","line":1,"completed":0,"total":2}`)
	fmt.Println(`MAC2JB_GUI {"type":"line-complete","line":1,"completed":1,"total":2,"remainingMs":1250}`)
	<-signals
	if err := os.WriteFile(os.Getenv("ANY2JB_CAPTURE"), make([]byte, 8), 0600); err != nil {
		os.Exit(2)
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
