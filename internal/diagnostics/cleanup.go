package diagnostics

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Only these known temporary files may be deleted. Unknown entries, backups,
// exports, runtime assets and support bundles are intentionally retained.
var temporary = []string{"host.log", "run.log", "usb.log", "diagnostics-id", "connect.txt", "connection.json", "result.json", "switch2/pipeline.json", "switch2/acquisition.log"}

func (s Store) Interrupted() (bool, error) {
	if _, err := os.Lstat(filepath.Join(s.Support, "session-interrupted")); err == nil {
		return true, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if _, err := os.Lstat(filepath.Join(s.Support, "session.lock")); err == nil {
		return true, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	entries, err := os.ReadDir(filepath.Join(s.Support, "runs"))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return len(entries) > 0, nil
}

func (s Store) PreviewCleanup() ([]string, error) {
	if _, err := os.Lstat(filepath.Join(s.Support, "session.lock")); !errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("session lease exists; stop its owner or confirm recovery first")
	}
	var plan []string
	for _, parent := range []string{"runs", "past-runs"} {
		entries, err := os.ReadDir(filepath.Join(s.Support, parent))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "run-") {
				continue
			}
			for _, name := range temporary {
				path := filepath.Join(parent, entry.Name(), name)
				if safeTemporary(s.Support, path) {
					plan = append(plan, path)
				}
			}
		}
	}
	return plan, nil
}

func safeTemporary(support, path string) bool {
	parts := strings.Split(filepath.ToSlash(path), "/")
	if len(parts) < 3 || (parts[0] != "runs" && parts[0] != "past-runs") || !strings.HasPrefix(parts[1], "run-") || !slices.Contains(temporary, strings.Join(parts[2:], "/")) {
		return false
	}
	current := support
	for i, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || (i < len(parts)-1 && !info.IsDir()) {
			return false
		}
		if i == len(parts)-1 {
			return info.Mode().IsRegular()
		}
	}
	return false
}

// Cleanup requires the exact preview. A new lease or changed file set cancels it.
func (s Store) Cleanup(plan []string) error {
	current, err := s.PreviewCleanup()
	if err != nil {
		return err
	}
	if !slices.Equal(current, plan) {
		return errors.New("cleanup preview changed; review it again")
	}
	lease, err := os.OpenFile(filepath.Join(s.Support, "session.lock"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	lease.Close()
	defer os.Remove(filepath.Join(s.Support, "session.lock"))
	root, err := os.OpenRoot(s.Support)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, path := range plan {
		if !safeTemporary(s.Support, path) {
			return errors.New("temporary path changed")
		}
		if err := root.Remove(path); err != nil {
			return err
		}
	}
	return nil
}
