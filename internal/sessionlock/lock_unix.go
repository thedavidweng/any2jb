//go:build unix

package sessionlock

import (
	"errors"
	"os"
	"syscall"
)

func lock(f *os.File) error {
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return ErrOwned
	}
	return err
}
