// Package driver owns informed consent and the privileged installer boundary.
package driver

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/thedavidweng/any2jb/manifest"
)

type Administrator func(context.Context, string) error

type Options struct {
	Assets, DeveloperSourceRoot string
	Manifest                    *manifest.Manifest
	Administrator               Administrator
}

type Gate struct{ options Options }

func (g *Gate) DeveloperSource() bool { return g.options.DeveloperSourceRoot != "" }

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

func (g *Gate) Install(ctx context.Context, approved bool) (Result, error) {
	if !approved {
		return Result{}, nil
	}
	if err := g.Availability(); err != nil {
		return Result{}, err
	}
	root := g.options.Assets
	if g.options.DeveloperSourceRoot != "" {
		root = g.options.DeveloperSourceRoot
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
	for _, id := range componentIDs {
		if err := g.options.Manifest.Verify(root, id); err != nil {
			return Result{}, err
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
		if entry.Type()&os.ModeSymlink != 0 || (name != "drivers/usbgadget.kext/Contents/Info.plist" && name != "drivers/usbgadget.kext/Contents/MacOS/usbgadget") {
			return fmt.Errorf("unexpected driver bundle file")
		}
		return nil
	})
	if err != nil {
		return Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	result := Result{Attempted: true, RestartRequired: true, DeveloperSource: g.options.DeveloperSourceRoot != ""}
	// Even a failed load may follow an installed bundle and restart marker.
	err = g.options.Administrator(ctx, filepath.Join(root, "scripts/install-driver.sh"))
	if err != nil {
		return result, fmt.Errorf("driver installation did not complete; refresh readiness before retrying: %w", err)
	}
	return result, nil
}

var componentIDs = []string{"usbgadget-binary", "usbgadget-info", "usbgadget-checksums", "driver-installer"}

// Availability is the packaging gate, not a claim of hardware or security readiness.
func (g *Gate) Availability() error {
	if g.options.Manifest == nil {
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
	return nil
}
