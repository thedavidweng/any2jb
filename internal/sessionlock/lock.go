// Package sessionlock gives the GUI and CLI one shared, process-lifetime owner.
// The OS releases the advisory lock on exit, including an unclean exit. The
// lock file is never removed: unlinking it would allow a second inode owner.
// It must not share a name with the host's per-run lease, session.lock,
// whose mere presence means an interrupted run.
package sessionlock

import (
	"errors"
	"os"
	"path/filepath"
)

var ErrOwned = errors.New("another any2jb process owns this session")

// Acquire takes the owner lock of the app support folder.
func Acquire(support string) (func() error, error) {
	if err := os.MkdirAll(support, 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(support, "app-owner.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := lock(f); err != nil {
		f.Close()
		return nil, err
	}
	return f.Close, nil
}
