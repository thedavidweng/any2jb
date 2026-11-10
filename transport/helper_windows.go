package transport

import (
	"errors"
	"os"
	"os/exec"
)

func configureHelper(*exec.Cmd) error {
	return errors.New("Darwin USB helpers are unsupported on Windows")
}
func terminateHelper(cmd *exec.Cmd, force bool) error { return cmd.Process.Kill() }
func lockHelperLease(*os.File) error {
	return errors.New("Darwin USB leases are unsupported on Windows")
}
