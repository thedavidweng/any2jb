package transport

import (
	"errors"
	"os/exec"
)

func configureHelper(*exec.Cmd) error {
	return errors.New("Darwin USB helpers are unsupported on Windows")
}
func terminateHelper(cmd *exec.Cmd, force bool) error { return cmd.Process.Kill() }
