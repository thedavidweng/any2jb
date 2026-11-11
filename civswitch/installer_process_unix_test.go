//go:build darwin || linux

package civswitch

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/thedavidweng/any2jb/session"
)

func TestInstallerCancellationStopsDescendants(t *testing.T) {
	f := newPersistenceFixture(t, consoleBackup(map[string]string{"slot.sav": "original"}, false))
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	t.Setenv("CIVSWITCH_CHILD_PID", pidFile)
	const script = "#!/bin/sh\nsleep 30 &\necho $! > \"$CIVSWITCH_CHILD_PID\"\nwait\n"
	if err := os.WriteFile(f.adapter.options.DeveloperInstallerPython, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := f.adapter.InstallBootTrigger(ctx, "192.0.2.7", true, func(session.Progress) {})
		done <- err
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		data, err := os.ReadFile(pidFile)
		if err == nil {
			pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				p, _ := os.FindProcess(pid)
				p.Kill()
			})
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("installer never started its descendant")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled installer reported success")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("installer descendant retained the process pipes after cancellation")
	}
}
