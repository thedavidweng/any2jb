package engine

import (
	"errors"
	"os"
	"os/exec"
)

// Windows is a compile gate, not a supported process supervisor yet. Refuse to
// launch rather than pretending Process.Kill terminates a whole process tree.
func configureGroup(cmd *exec.Cmd) error {
	return errors.New("Windows process-group supervision is not implemented")
}
func signalGroup(cmd *exec.Cmd, force bool) error {
	return errors.New("Windows process-group supervision is not implemented")
}
func ownedFile(info os.FileInfo) bool { return false }
