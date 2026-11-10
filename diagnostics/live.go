package diagnostics

import (
	"errors"
	"os"
	"path/filepath"
)

// Refresh captures this run's current workspace, or reads its retained diagnostics
// after teardown. A run is identified by an app-written marker, never pairing data.
func (s Store) Refresh(id string) (Logs, string, error) {
	if id == "" {
		data, err := os.ReadFile(filepath.Join(s.Support, "session-interrupted"))
		if errors.Is(err, os.ErrNotExist) {
			return Logs{}, "", nil
		}
		if err != nil {
			return Logs{}, "", err
		}
		id = string(data)
	}
	if !validID(id) {
		return Logs{}, "", nil
	}
	for _, parent := range []string{"runs", "past-runs"} {
		entries, err := os.ReadDir(filepath.Join(s.Support, parent))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return Logs{}, "", err
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			path := filepath.Join(s.Support, parent, entry.Name())
			data, err := os.ReadFile(filepath.Join(path, "diagnostics-id"))
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return Logs{}, "", err
			}
			if string(data) != id {
				continue
			}
			if err := s.Capture(path, id); err != nil && !errors.Is(err, os.ErrNotExist) {
				return Logs{}, "", err
			}
			logs, err := s.Read(id)
			return logs, path, err
		}
	}
	logs, err := s.Read(id)
	return logs, filepath.Join(s.Support, "diagnostics", id), err
}
