//go:build darwin || linux

package transport_test

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/thedavidweng/any2jb/session"
	"github.com/thedavidweng/any2jb/transport"
)

func TestAbruptOwnerDeathClosesKeyboardInputAndTriggersNeutralRelease(t *testing.T) {
	dir := t.TempDir()
	ready, release := filepath.Join(dir, "ready"), filepath.Join(dir, "released")
	owner := exec.Command(os.Args[0], "-test.run=^TestKeyboardOwnerProcess$")
	owner.Env = append(os.Environ(), "ANY2JB_OWNER_FIXTURE=1", "ANY2JB_OWNER_READY="+ready, "ANY2JB_EOF_RELEASED="+release, "ANY2JB_CAPTURE="+filepath.Join(dir, "reports"))
	input, err := owner.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { input.Close(); owner.Process.Kill() })
	awaitFile(t, ready, 5)
	if err := owner.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := owner.Wait(); err == nil {
		t.Fatal("fixture owner was not killed")
	}
	awaitFile(t, release, 8)
	data, err := os.ReadFile(release)
	if err != nil || !reflect.DeepEqual(data, make([]byte, 8)) {
		t.Fatalf("EOF cleanup %v, %v", data, err)
	}
}

func TestKeyboardOwnerProcess(t *testing.T) {
	if os.Getenv("ANY2JB_OWNER_FIXTURE") != "1" {
		return
	}
	d := transport.NewHelperDarwin(transport.DarwinOptions{Controller: "usb-drd0", Timing: transport.DefaultTiming()}, helperFactory(t, os.Getenv("ANY2JB_CAPTURE")))
	k, err := d.OpenKeyboard(context.Background(), session.Run{ID: "owner"}, func(session.Progress) {})
	if err != nil {
		t.Fatal(err)
	}
	if err := k.Send(context.Background(), [8]byte{0, 0, 4}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv("ANY2JB_OWNER_READY"), []byte("ready"), 0600); err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, os.Stdin)
}

func awaitFile(t *testing.T, path string, size int64) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		if info, err := os.Stat(path); err == nil && info.Size() == size {
			return
		}
		select {
		case <-deadline:
			t.Fatal("fixture did not reach expected ownership boundary")
		case <-time.After(time.Millisecond):
		}
	}
}
