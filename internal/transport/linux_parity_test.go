package transport_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/thedavidweng/any2jb/internal/engine"
	"github.com/thedavidweng/any2jb/internal/manifest"
	"github.com/thedavidweng/any2jb/internal/parity"
	"github.com/thedavidweng/any2jb/internal/session"
	"github.com/thedavidweng/any2jb/internal/transport"
	"github.com/thedavidweng/any2jb/internal/usbgadget"
)

func TestLinuxConfigFSHelperSubprocessEntry(t *testing.T) {
	base := os.Getenv("ANY2JB_LINUX_HELPER_FIXTURE")
	if base == "" {
		return
	}
	controller := os.Getenv("ANY2JB_LINUX_HELPER_UDC")
	capture := os.Getenv("ANY2JB_LINUX_HELPER_CAPTURE")
	capFile, err := os.OpenFile(capture, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		os.Exit(2)
	}
	roots := usbgadget.Roots{
		ConfigRoot:    filepath.Join(base, "config"),
		UDCRoot:       filepath.Join(base, "udc"),
		HidgClassRoot: filepath.Join(base, "class-hidg"),
		DevRoot:       filepath.Join(base, "dev"),
		PollInterval:  time.Millisecond,
		OpenEndpoint: func(string) (usbgadget.Endpoint, error) {
			return &captureEndpoint{file: capFile}, nil
		},
	}
	code := usbgadget.RunHelperWithRoots(roots, nil, []string{"keyboard", "--controller", controller, "--enumeration-timeout-seconds", "4"}, os.Stdin, os.Stdout)
	_ = capFile.Close()
	os.Exit(code)
}

type captureEndpoint struct {
	file *os.File
}

func (c *captureEndpoint) Write(p []byte) (int, error) {
	return c.file.Write(p)
}

func (c *captureEndpoint) Close() error { return nil }

func setupFakeConfigFS(t *testing.T, controller string) string {
	t.Helper()
	base := t.TempDir()
	for _, dir := range []string{
		filepath.Join(base, "config/usb_gadget"),
		filepath.Join(base, "udc", controller),
		filepath.Join(base, "class-hidg/hidg0"),
		filepath.Join(base, "dev"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(base, "class-hidg/hidg0/dev"), []byte("241:0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "dev/hidg0"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return base
}

func linuxConfigFSTranscript(t *testing.T, c hidCase) parity.Transcript {
	t.Helper()
	const controller = "fe980000.usb"
	base := setupFakeConfigFS(t, controller)
	capture := filepath.Join(t.TempDir(), "hidg0.capture")
	if err := os.WriteFile(capture, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	factory := func(string, ...string) *exec.Cmd {
		cmd := exec.Command(exe, "-test.run=^TestLinuxConfigFSHelperSubprocessEntry$")
		cmd.Env = append(os.Environ(),
			"ANY2JB_LINUX_HELPER_FIXTURE="+base,
			"ANY2JB_LINUX_HELPER_UDC="+controller,
			"ANY2JB_LINUX_HELPER_CAPTURE="+capture,
		)
		return cmd
	}

	type wait struct {
		after int
		ns    uint64
	}
	var waits []wait
	defer transport.SetSleep(func(ctx context.Context, ms int) error {
		info, err := os.Stat(capture)
		if err != nil || info.Size()%8 != 0 {
			return fmt.Errorf("capture size: %v", err)
		}
		waits = append(waits, wait{int(info.Size() / 8), uint64(ms) * uint64(time.Millisecond)})
		return ctx.Err()
	})()

	run := c.run
	sum := sha256.Sum256([]byte(c.text))
	run.Profile.BootstrapSHA256 = hex.EncodeToString(sum[:])
	options := transport.LinuxOptions{
		Controller: controller,
		Timing:     c.timing,
		Bootstrap:  func(session.Run) (string, error) { return c.text, nil },
		Confirm:    func(context.Context, session.Run, transport.ConsoleStep) error { return nil },
	}
	host := session.PreparedHost{Listening: true, PairingValid: true, Opaque: engine.PairingDetails{RunID: run.ID, Command: startCommand(run)}}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := transport.NewHelperLinux(options, factory).Send(ctx, run, c.action, host, func(session.Progress) {}); err != nil {
		t.Fatal(err)
	}
	// Verify ConfigFS gadget was cleanly unbound and removed after Send!
	if _, err := os.Stat(filepath.Join(base, "config/usb_gadget", usbgadget.GadgetDirName)); !os.IsNotExist(err) {
		t.Fatalf("ConfigFS gadget was not torn down after Send: %v", err)
	}

	data, err := os.ReadFile(capture)
	if err != nil || len(data)%8 != 0 {
		t.Fatalf("invalid capture (%d bytes): %v", len(data), err)
	}
	// Note: ConfigFS Open writes 1 initial neutral [8]byte{} during enumeration
	// before MAC2JB_KEYBOARD_READY, so waits recorded against capture file size
	// already align with exact report indices in data!
	var reports parity.Transcript
	var at uint64
	w := 0
	for i := 0; i < len(data)/8; i++ {
		for ; w < len(waits) && waits[w].after == i; w++ {
			at += waits[w].ns
		}
		var r parity.Report
		r.AtNS = at
		copy(r.Bytes[:], data[i*8:(i+1)*8])
		reports = append(reports, r)
	}
	return reports
}

func TestLinuxConfigFSHIDParityMatchesCommittedDigests(t *testing.T) {
	m, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(parityDigests)
	if err != nil {
		t.Fatal(err)
	}
	var committed struct {
		Digests map[string]string `json:"digests"`
	}
	if err := json.Unmarshal(raw, &committed); err != nil {
		t.Fatal(err)
	}
	for _, c := range hidCases(t, m, os.Getenv("ANY2JB_REFERENCE_ROOT")) {
		if c.reference {
			continue
		}
		c := c
		t.Run(c.name, func(t *testing.T) {
			got := linuxConfigFSTranscript(t, c).State().Digest()
			want := committed.Digests["hid/"+c.name]
			if want == "" {
				t.Fatalf("missing committed digest for hid/%s", c.name)
			}
			if got != want {
				t.Fatalf("Linux ConfigFS HID digest %s != committed mac2jb digest %s", got, want)
			}
		})
	}
}

func TestLinuxControllerValidationRejectsAmbiguousAndDarwinNames(t *testing.T) {
	for _, bad := range []string{"", "auto", "usb-drd0", "usb-drd16", "../udc", "fe980000/usb", "network", "usb-controllers"} {
		l := transport.NewHelperLinux(transport.LinuxOptions{Controller: bad, Timing: transport.DefaultTiming()}, func(string, ...string) *exec.Cmd {
			t.Fatalf("launched helper with invalid Linux controller %q", bad)
			return nil
		})
		if _, err := l.OpenKeyboard(context.Background(), session.Run{ID: "bad"}, func(session.Progress) {}); err == nil {
			t.Fatalf("accepted invalid Linux controller %q", bad)
		}
	}
	for _, good := range []string{"fe980000.usb", "1000480000.usb", "dwc3.0.auto", "dummy_udc.0", "ci_hdrc.0"} {
		if err := transport.ValidateLinuxController(good); err != nil {
			t.Fatalf("rejected valid Linux UDC %q: %v", good, err)
		}
		if strings.HasPrefix(good, "usb-drd") {
			t.Fatal("unexpected prefix")
		}
	}
}
