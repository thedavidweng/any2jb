//go:build darwin || linux

package retained

import (
	"errors"
	"os/exec"
	"syscall"
)

func configureGroup(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return nil
}

func signalGroup(cmd *exec.Cmd, force bool) error {
	signal := syscall.SIGTERM
	if force {
		signal = syscall.SIGKILL
	}
	err := syscall.Kill(-cmd.Process.Pid, signal)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}
