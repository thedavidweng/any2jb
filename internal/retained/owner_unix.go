//go:build unix

package retained

import (
	"io/fs"
	"os"
	"syscall"
)

func ownedByMe(info fs.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Getuid() {
		return errOwnership
	}
	return nil
}
