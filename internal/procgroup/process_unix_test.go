//go:build darwin || linux

package procgroup_test

import (
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/thedavidweng/any2jb/internal/procgroup"
)

func TestSignalReapsZombieOnlyGroup(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "exit 0")
	if err := procgroup.Configure(cmd); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Wait()
	deadline := time.Now().Add(3 * time.Second)
	for {
		output, err := exec.Command("/bin/ps", "-o", "stat=", "-p", strconv.Itoa(cmd.Process.Pid)).Output()
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(strings.TrimSpace(string(output)), "Z") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fixture did not become a zombie")
		}
		time.Sleep(time.Millisecond)
	}
	if err := procgroup.Signal(cmd, true); err != nil {
		t.Fatalf("zombie-only group should need no further signal: %v", err)
	}
}
