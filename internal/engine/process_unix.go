//go:build darwin || linux

package engine

import (
	"os"
	"syscall"
)

func ownedFile(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Getuid()) && info.Mode().Perm()&0o077 == 0
}
