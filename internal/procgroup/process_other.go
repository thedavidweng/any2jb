//go:build !darwin && !linux

package procgroup

import (
	"errors"
	"os/exec"
)

func Configure(*exec.Cmd) error {
	return errors.New("process-group supervision is not implemented on this platform")
}

func Signal(*exec.Cmd, bool) error {
	return errors.New("process-group supervision is not implemented on this platform")
}
