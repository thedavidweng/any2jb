//go:build darwin || linux

package diagnostics

import (
	"errors"
	"os"
	"syscall"
)

func ownerDead(pid int) (bool, error) {
	err := syscall.Kill(pid, 0)
	if errors.Is(err, syscall.ESRCH) {
		return true, nil
	}
	if err == nil || errors.Is(err, syscall.EPERM) {
		return false, nil
	}
	return false, err
}

// Advisory locking is released by the OS even if recovery itself crashes.
func recoveryLock(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, err
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
}
