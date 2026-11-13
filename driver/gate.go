// Package driver owns informed consent and the privileged installer boundary.
package driver

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"

	"github.com/thedavidweng/any2jb/manifest"
)

type Administrator func(context.Context, string) error

type Options struct {
	Assets, DeveloperSourceRoot string
	Manifest                    *manifest.Manifest
	Administrator               Administrator
}

type Gate struct{ options Options }

func (g *Gate) DeveloperSource() bool { return g != nil && g.options.DeveloperSourceRoot != "" }

func New(options Options) *Gate {
	if options.Administrator == nil {
		options.Administrator = administrator
	}
	return &Gate{options: options}
}

type Result struct {
	Attempted, RestartRequired bool
	DeveloperSource            bool
}

func (g *Gate) resolveRoot() (string, error) {
	if g.options.DeveloperSourceRoot != "" {
		return g.options.DeveloperSourceRoot, nil
	}
	if g.options.Assets == "" {
		return "", fmt.Errorf("Bundled USB driver files are missing from this build.")
	}
	if _, err := os.Lstat(filepath.Join(g.options.Assets, "drivers/usbgadget.kext")); err == nil {
		return g.options.Assets, nil
	}
	overlay := filepath.Join(g.options.Assets, "darwin-arm64")
	if _, err := os.Lstat(filepath.Join(overlay, "drivers/usbgadget.kext")); err == nil {
		return overlay, nil
	}
	return "", fmt.Errorf("Bundled USB driver files are missing from this build.")
}

func (g *Gate) Install(ctx context.Context, approved bool) (Result, error) {
	if !approved {
		return Result{}, nil
	}
	if err := g.Availability(); err != nil {
		return Result{}, err
	}
	root, err := g.resolveRoot()
	if err != nil {
		return Result{}, err
	}
	if !filepath.IsAbs(root) {
		return Result{}, fmt.Errorf("driver source root must be absolute")
	}
	info, err := os.Lstat(root)
	if err != nil {
		return Result{}, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return Result{}, fmt.Errorf("driver source root is not a real directory")
	}
	verified := make(map[string][]byte, len(componentIDs))
	for _, id := range componentIDs {
		data, err := g.options.Manifest.Read(root, id)
		if err != nil {
			return Result{}, err
		}
		verified[id] = data
	}
	wantBinary := "drivers/usbgadget.kext/Contents/MacOS/usbgadget"
	gzipped := false
	if g.options.DeveloperSourceRoot == "" {
		if _, err := os.Lstat(filepath.Join(root, "drivers/usbgadget.kext/Contents/MacOS/usbgadget.gz")); err == nil {
			if _, rawErr := os.Lstat(filepath.Join(root, wantBinary)); os.IsNotExist(rawErr) {
				wantBinary = "drivers/usbgadget.kext/Contents/MacOS/usbgadget.gz"
				gzipped = true
			}
		}
	}
	// The pinned installer permits exactly these two regular bundle files.
	err = filepath.WalkDir(filepath.Join(root, "drivers/usbgadget.kext"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)
		if entry.Type()&os.ModeSymlink != 0 || (name != "drivers/usbgadget.kext/Contents/Info.plist" && name != wantBinary) {
			return fmt.Errorf("unexpected driver bundle file")
		}
		return nil
	})
	if err != nil {
		return Result{}, err
	}
	installerPath := filepath.Join(root, "scripts/install-driver.sh")
	if gzipped {
		stage, err := os.MkdirTemp("", "any2jb-driver-*")
		if err != nil {
			return Result{}, err
		}
		defer os.RemoveAll(stage)
		for _, item := range []struct {
			id, rel string
			mode    os.FileMode
		}{
			{"usbgadget-binary", "drivers/usbgadget.kext/Contents/MacOS/usbgadget", 0o755},
			{"usbgadget-info", "drivers/usbgadget.kext/Contents/Info.plist", 0o644},
			{"usbgadget-checksums", "drivers/SHA256SUMS", 0o644},
			{"driver-installer", "scripts/install-driver.sh", 0o755},
		} {
			dst := filepath.Join(stage, filepath.FromSlash(item.rel))
			if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
				return Result{}, err
			}
			if err := os.WriteFile(dst, verified[item.id], item.mode); err != nil {
				return Result{}, err
			}
		}
		installerPath = filepath.Join(stage, "scripts/install-driver.sh")
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	result := Result{Attempted: true, RestartRequired: true, DeveloperSource: g.options.DeveloperSourceRoot != ""}
	// Even a failed load may follow an installed bundle and restart marker.
	err = g.options.Administrator(ctx, installerPath)
	if err != nil {
		return result, fmt.Errorf("driver installation did not complete; refresh readiness before retrying: %w", err)
	}
	return result, nil
}

var componentIDs = []string{"usbgadget-binary", "usbgadget-info", "usbgadget-checksums", "driver-installer"}

// Availability is the packaging gate, not a claim of hardware or security readiness.
func (g *Gate) Availability() error {
	if g == nil || g.options.Manifest == nil {
		return fmt.Errorf("missing driver manifest")
	}
	for _, id := range componentIDs {
		d, err := g.options.Manifest.DistributionFor(id)
		if err != nil {
			return err
		}
		if !d.Available() && g.options.DeveloperSourceRoot == "" {
			return fmt.Errorf("%s", d.Reason)
		}
		if g.options.DeveloperSourceRoot != "" && d.Decision == "excluded" {
			return fmt.Errorf("%s", d.Reason)
		}
	}
	if g.options.DeveloperSourceRoot == "" {
		if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
			return fmt.Errorf("driver installation requires Apple Silicon macOS")
		}
		if _, err := g.resolveRoot(); err != nil {
			return err
		}
	}
	return nil
}
