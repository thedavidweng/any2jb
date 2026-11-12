// Package sessionlock gives the GUI and CLI one shared, process-lifetime owner.
// The OS releases the advisory lock on exit, including an unclean exit. The
// lock file is never removed: unlinking it would allow a second inode owner.
package sessionlock

import (
	"errors"
	"os"
	"path/filepath"
)

var ErrOwned = errors.New("another any2jb process owns this session")

func Acquire(path string) (func() error, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := lock(f); err != nil {
		f.Close()
		return nil, err
	}
	return f.Close, nil
}
