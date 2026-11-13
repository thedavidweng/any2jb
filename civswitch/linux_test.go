package civswitch_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/thedavidweng/any2jb/civswitch"
	"github.com/thedavidweng/any2jb/internal/usbgadget"
	"github.com/thedavidweng/any2jb/manifest"
	"github.com/thedavidweng/any2jb/readiness"
	"github.com/thedavidweng/any2jb/session"
)

func TestLinuxCivSwitchHelperSubprocessEntry(t *testing.T) {
	base := os.Getenv("ANY2JB_CIV_LINUX_FIXTURE")
	if base == "" {
		return
	}
	args := strings.Split(os.Getenv("ANY2JB_CIV_LINUX_ARGS"), "\n")
	roots := usbgadget.Roots{
		ConfigRoot:    filepath.Join(base, "config"),
		UDCRoot:       filepath.Join(base, "udc"),
		HidgClassRoot: filepath.Join(base, "class-hidg"),
		DevRoot:       filepath.Join(base, "dev"),
		PollInterval:  time.Millisecond,
		OpenEndpoint:  func(string) (usbgadget.Endpoint, error) { return nopEndpoint{}, nil },
	}
	fastSleep := func(ctx context.Context, _ int) error { return ctx.Err() }
	os.Exit(usbgadget.RunHelperWithRoots(roots, fastSleep, args, os.Stdin, os.Stdout))
}

type nopEndpoint struct{}

func (nopEndpoint) Write(p []byte) (int, error) { return len(p), nil }
func (nopEndpoint) Close() error                { return nil }

func TestLinuxCivSwitchAdapterEndToEndOverConfigFS(t *testing.T) {
	const udc = "1000480000.usb"
	base := t.TempDir()
	for _, dir := range []string{
		filepath.Join(base, "config/usb_gadget"),
		filepath.Join(base, "udc", udc),
		filepath.Join(base, "class-hidg/hidg0"),
		filepath.Join(base, "class-hidg/hidg1"),
		filepath.Join(base, "dev"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := os.WriteFile(filepath.Join(base, fmt.Sprintf("class-hidg/hidg%d/dev", i)), []byte(fmt.Sprintf("241:%d\n", i)), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(base, fmt.Sprintf("dev/hidg%d", i)), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	sourceRoot := t.TempDir()
	var lines []string
	for i := 0; i < 87; i++ {
		lines = append(lines, fmt.Sprintf("line-%02d-payload", i+1))
	}
	payloadData := strings.Join(lines, "\n") + "\n"
	cSource := "/* fixture civswitch-hid.c */"
	if err := os.WriteFile(filepath.Join(sourceRoot, "civswitch-payload"), []byte(payloadData), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceRoot, "civswitch-hid.c"), []byte(cSource), 0o600); err != nil {
		t.Fatal(err)
	}
	m := &manifest.Manifest{
		Components: []manifest.Component{
			{ID: "civswitch-payload", Path: "civswitch-payload", SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(payloadData))), Distribution: manifest.Distribution{Decision: "blocked", Reason: "unlicensed"}},
			{ID: "civswitch-hid.c", Path: "civswitch-hid.c", SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(cSource))), Distribution: manifest.Distribution{Decision: "blocked", Reason: "unlicensed"}},
		},
	}

	inspector := readiness.Inspector{Observe: func(context.Context) readiness.Observation {
		return readiness.Observation{
			Linux:        &readiness.LinuxGadget{ConfigFSMounted: true, ConfigFSReady: true, PolkitAvailable: true},
			Addresses:    []readiness.Address{{Interface: "eth0", IPv4: "192.168.1.20"}},
			USBSupported: true,
			Controllers:  []string{udc},
		}
	}}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	adapter := civswitch.New(civswitch.Options{
		Manifest:            m,
		DeveloperSourceRoot: sourceRoot,
		Support:             t.TempDir(),
		Inspector:           &inspector,
		LinuxHelperCommand: func(payload, controller string, dryRun bool) (*exec.Cmd, error) {
			args := []string{"civswitch", "--payload", payload, "--controller", controller}
			if dryRun {
				args = append(args, "--dry-run")
			}
			cmd := exec.Command(exe, "-test.run=^TestLinuxCivSwitchHelperSubprocessEntry$")
			cmd.Env = append(os.Environ(),
				"ANY2JB_CIV_LINUX_FIXTURE="+base,
				"ANY2JB_CIV_LINUX_ARGS="+strings.Join(args, "\n"),
			)
			return cmd, nil
		},
	})

	est, err := adapter.Prepare(context.Background())
	if err != nil || est <= 0 {
		t.Fatalf("Prepare = (%v, %v), want positive duration", est, err)
	}
	var lastCompleted int
	if err := adapter.Transfer(context.Background(), func(p session.Progress) { lastCompleted = p.Current }); err != nil {
		t.Fatalf("Transfer failed: %v", err)
	}
	if lastCompleted != 87 {
		t.Fatalf("lastCompleted = %d, want 87", lastCompleted)
	}
	if err := adapter.Release(context.Background()); err != nil {
		t.Fatalf("Release failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "config/usb_gadget", usbgadget.GadgetDirName)); !os.IsNotExist(err) {
		t.Fatalf("ConfigFS composite gadget was not torn down: %v", err)
	}
}
