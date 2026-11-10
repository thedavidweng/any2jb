// Package launcher prepares pinned save packages; all console writes stay in SaveKit.
package launcher

import (
	"errors"
	"slices"

	"github.com/thedavidweng/any2jb/manifest"
	"github.com/thedavidweng/any2jb/savekit"
)

var IDs = []string{"launcher-seedsow-demo", "launcher-seedsow-full", "launcher-nora2-demo",
	"launcher-nora1-demo", "launcher-hamidashi-demo", "launcher-lua-editor",
	"launcher-balatro", "launcher-balatro-showcase", "launcher-gravity-circuit", "launcher-lua-editor-pro"}

type Request = savekit.LauncherRequest

func Validate(r Request, c manifest.Component, target savekit.Request) error {
	if !slices.Contains(IDs, r.ID) || c.ID != r.ID || c.Launcher == nil || target.TitleID != c.Launcher.TitleID {
		return errors.New("the launcher package does not support this game or edition")
	}
	if len(c.Launcher.Versions) > 0 && !slices.Contains(c.Launcher.Versions, r.Version) {
		return errors.New("Gravity Circuit requires version 1.0.0 or 1.2.1")
	}
	if c.Launcher.PhysicalCompatibility && !r.PhysicalCompatibility {
		return errors.New("Balatro requires the physical Switch 1 cartridge in Switch 2 compatibility mode, not the native Switch 2 edition")
	}
	return nil
}
