//go:build darwin || linux

// Package procgroup supervises each owned child and its descendants as a group.
package procgroup

import (
	"errors"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

func Configure(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return nil
}

func Signal(cmd *exec.Cmd, force bool) error {
	signal := syscall.SIGTERM
	if force {
		signal = syscall.SIGKILL
	}
	err := syscall.Kill(-cmd.Process.Pid, signal)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	// macOS reports EPERM for zombie-only groups; never hide a live member.
	if runtime.GOOS == "darwin" && errors.Is(err, syscall.EPERM) {
		output, inspectErr := exec.Command("/bin/ps", "-axo", "pgid=,stat=").Output()
		if inspectErr == nil && onlyZombies(string(output), cmd.Process.Pid) {
			return nil
		}
	}
	return err
}

func onlyZombies(output string, pgid int) bool {
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return false
		}
		group, err := strconv.Atoi(fields[0])
		if err != nil {
			return false
		}
		if group == pgid && !strings.HasPrefix(fields[1], "Z") {
			return false
		}
	}
	return true
}
