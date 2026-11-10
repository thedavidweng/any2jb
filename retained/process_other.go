//go:build !darwin && !linux

package retained

import (
	"errors"
	"os/exec"
)

// Tools are supervised as an owned process group. Refuse to launch rather than
// pretending a single Kill tears down the whole tree.
func configureGroup(*exec.Cmd) error {
	return errors.New("read-only tool supervision is not implemented on this platform")
}

func signalGroup(*exec.Cmd, bool) error {
	return errors.New("read-only tool supervision is not implemented on this platform")
}
