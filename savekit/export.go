package savekit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// exportManifest is the identity and integrity record the pull leaves in every
// export folder. The folder stays verifiable after the session ends.
type exportManifest struct {
	ProgramID string `json:"programId"`
	TitleID   string `json:"titleId"`
	UID       string `json:"uid"`
	Archive   string `json:"archive"`
	Space     int    `json:"space"`
	Type      int    `json:"type"`
	Rank      int    `json:"rank"`
	Index     int    `json:"index"`
	Files     int    `json:"files"`
	Bytes     int64  `json:"bytes"`
	Partial   bool   `json:"partial"`
	Failed    []any  `json:"failed"`
	Entries   []struct {
		Path   string `json:"path"`
		Size   int64  `json:"size"`
		SHA256 string `json:"sha256"`
	} `json:"entries"`
}

// VerifyExport re-checks a saved export against its own manifest: complete,
// every recorded file present with its size and SHA-256, and no unrecorded,
// special or escaping files. A failed check means the export cannot be trusted
// as a backup; it is kept for diagnosis, never silently repaired.
func VerifyExport(dir string) error {
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return err
	}
	var m exportManifest
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&m); err != nil {
		return fmt.Errorf("the save export manifest is unreadable: %w", err)
	}
	if m.Partial || len(m.Failed) > 0 {
		return errors.New("the save export is partial; it is not a verified backup")
	}
	if len(m.Entries) != m.Files || m.Files < 0 || m.Files > 65536 {
		return errors.New("the save export manifest is incomplete")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	seen := map[string]bool{}
	var total int64
	for _, entry := range m.Entries {
		name, err := safeExportPath(entry.Path)
		if err != nil {
			return err
		}
		if seen[name] {
			return fmt.Errorf("the save export manifest repeats a file: %s", name)
		}
		seen[name] = true
		info, err := root.Lstat(name)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() != entry.Size || entry.Size < 0 {
			return fmt.Errorf("the save export contains an invalid file: %s", name)
		}
		file, err := root.Open(name)
		if err != nil {
			return err
		}
		hash := sha256.New()
		_, copyErr := io.Copy(hash, file)
		closeErr := file.Close()
		if copyErr != nil || closeErr != nil {
			return errors.Join(copyErr, closeErr)
		}
		if hex.EncodeToString(hash.Sum(nil)) != entry.SHA256 {
			return fmt.Errorf("the save export failed SHA-256 verification: %s", name)
		}
		total += info.Size()
	}
	if total != m.Bytes {
		return errors.New("the save export byte count differs from its manifest")
	}
	return filepath.WalkDir(dir, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(dir, name)
		if err != nil {
			return err
		}
		if entry.Type().IsRegular() {
			if relative == "manifest.json" {
				return nil
			}
			if !seen[filepath.ToSlash(relative)] {
				return fmt.Errorf("the save export contains a file missing from its manifest: %s", relative)
			}
			return nil
		}
		return fmt.Errorf("the save export contains a link or special file: %s", relative)
	})
}

func safeExportPath(value string) (string, error) {
	if !strings.HasPrefix(value, "/") || strings.Contains(value, "\\") {
		return "", fmt.Errorf("the save manifest contains an unsafe path: %q", value)
	}
	name := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(filepath.FromSlash(value))), "/")
	if name == "" || name == "." || name == "manifest.json" || strings.HasPrefix(name, "../") ||
		strings.Contains(name, "/../") {
		return "", fmt.Errorf("the save manifest contains an unsafe path: %q", value)
	}
	return name, nil
}
