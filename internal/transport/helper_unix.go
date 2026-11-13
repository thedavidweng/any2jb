//go:build darwin || linux

package transport

import (
	"os"
	"syscall"
)

func lockHelperLease(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}
