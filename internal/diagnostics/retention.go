package diagnostics

import (
	"errors"
	"os"
	"path/filepath"
)

// RetainBackups moves intentionally retained artifacts out of a workspace before
// automatic teardown. Tools must use these directory names for retained data.
func (s Store) RetainBackups(workspace string) error {
	for _, name := range []string{"backups", "savekit-exports", "verified-exports", "dumps"} {
		source := filepath.Join(workspace, name)
		info, err := os.Lstat(source)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("retained artifact is not a real directory")
		}
		dest := filepath.Join(s.Support, "past-runs", filepath.Base(workspace))
		if err := os.MkdirAll(dest, 0o700); err != nil {
			return err
		}
		target := filepath.Join(dest, name)
		if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
			return errors.New("retained artifact destination already exists")
		}
		if err := os.Rename(source, target); err != nil {
			return err
		}
	}
	return nil
}
