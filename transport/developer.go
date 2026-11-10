package transport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/thedavidweng/any2jb/internal/nativehelper"
	"github.com/thedavidweng/any2jb/manifest"
)

// DeveloperHelpers is an in-memory authorization from a successful local build.
// A directory or a provenance file alone never authorizes a helper launch.
type DeveloperHelpers struct {
	root    string
	digests map[string][32]byte
}

// BuildDeveloperHelpers uses the regression harness's single native build path.
// It only compiles; it never initializes USB. The caller owns the returned
// directory until all transports are released, then calls Close.
func BuildDeveloperHelpers(ctx context.Context, source, support string, m *manifest.Manifest, out io.Writer) (*DeveloperHelpers, error) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		return nil, errors.New("developer USB builds require Darwin arm64")
	}
	if err := os.MkdirAll(support, 0700); err != nil {
		return nil, err
	}
	root, err := os.MkdirTemp(support, "usb-developer-")
	if err != nil {
		return nil, err
	}
	if err := nativehelper.Build(ctx, out, source, root, m); err != nil {
		os.RemoveAll(root)
		return nil, err
	}
	h := &DeveloperHelpers{root: root, digests: make(map[string][32]byte)}
	for _, name := range []string{"sender", "commands", "keyboard", "civswitch-hid", "vendor/libusbd/libusbd.dylib"} {
		if err := os.Chmod(h.path(name), 0500); err != nil {
			h.Close()
			return nil, err
		}
		data, err := os.ReadFile(h.path(name))
		if err != nil {
			h.Close()
			return nil, err
		}
		h.digests[name] = sha256.Sum256(data)
	}
	return h, nil
}
func (h *DeveloperHelpers) path(name string) string {
	return filepath.Join(h.root, filepath.FromSlash(name))
}
func (h *DeveloperHelpers) verify() error {
	if !filepath.IsAbs(h.root) || len(h.digests) != 5 {
		return errors.New("missing successful developer USB build authorization")
	}
	info, err := os.Lstat(h.root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("developer USB root is not a real directory")
	}
	root, err := os.OpenRoot(h.root)
	if err != nil {
		return err
	}
	defer root.Close()
	for name, want := range h.digests {
		info, err := root.Lstat(name)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0222 != 0 {
			return fmt.Errorf("developer USB helper is not a sealed regular file: %s", name)
		}
		data, err := root.ReadFile(name)
		if err != nil {
			return err
		}
		if sha256.Sum256(data) != want {
			return fmt.Errorf("developer USB helper digest mismatch: %s", name)
		}
	}
	return nil
}
func (h *DeveloperHelpers) Close() error { return os.RemoveAll(h.root) }

// CompositeHelper exposes the same sealed build to the separate CivSwitch route.
// The receiving adapter must verify this digest again immediately before use.
func (h *DeveloperHelpers) CompositeHelper() (string, string, error) {
	if err := h.verify(); err != nil {
		return "", "", err
	}
	sum := h.digests["civswitch-hid"]
	return h.path("civswitch-hid"), hex.EncodeToString(sum[:]), nil
}
