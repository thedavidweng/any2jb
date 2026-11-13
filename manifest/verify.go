package manifest

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"strings"
)

// IntegrityError identifies the component that must not be executed or sent.
// Unwrap preserves filesystem errors, including os.ErrNotExist.
type IntegrityError struct {
	Component string
	Path      string
	Err       error
}

func (e *IntegrityError) Error() string {
	return fmt.Sprintf("integrity check for %s (%s): %v", e.Component, e.Path, e.Err)
}

func (e *IntegrityError) Unwrap() error { return e.Err }

// Read returns the recorded artifact's verified bytes. Never read a component's
// file separately: an unverified second read may differ from the checked one.
func (m *Manifest) Read(root, componentID string) ([]byte, error) {
	for _, c := range m.Components {
		if c.ID == componentID {
			return verifyFile(root, componentID, c.Path, c.SHA256)
		}
	}
	return nil, &IntegrityError{Component: componentID, Err: errors.New("component is not recorded")}
}

// ReadAt verifies and reads a recorded component at an explicit relative path
// inside root (used when source resource overlays add a platform prefix).
func (m *Manifest) ReadAt(root, componentID, relPath string) ([]byte, error) {
	for _, c := range m.Components {
		if c.ID == componentID {
			return verifyFile(root, componentID, relPath, c.SHA256)
		}
	}
	return nil, &IntegrityError{Component: componentID, Path: relPath, Err: errors.New("component is not recorded")}
}

// Verify checks one recorded artifact, including helpers. Unknown IDs and
// artifacts without a pinned digest fail closed.
func (m *Manifest) Verify(root, componentID string) error {
	_, err := m.Read(root, componentID)
	return err
}

func verifyFile(root, component, name, digest string) ([]byte, error) {
	data, err := readFile(root, name)
	if err == nil {
		want, decodeErr := hex.DecodeString(digest)
		got := sha256.Sum256(data)
		if decodeErr != nil || len(want) != sha256.Size || digest != hex.EncodeToString(got[:]) {
			err = errors.New("SHA-256 mismatch or missing digest")
		}
	}
	if err != nil {
		return nil, &IntegrityError{Component: component, Path: name, Err: err}
	}
	return data, nil
}

func readFile(root, name string) ([]byte, error) {
	if !fs.ValidPath(name) || strings.Contains(name, `\`) {
		return nil, errors.New("invalid relative artifact path")
	}
	dir, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	parts := strings.Split(name, "/")
	for i := range parts {
		info, err := dir.Lstat(path.Join(parts[:i+1]...))
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("symlink artifact path")
		}
		if i == len(parts)-1 && !info.Mode().IsRegular() {
			return nil, errors.New("artifact is not a regular file")
		}
	}
	return dir.ReadFile(name)
}
