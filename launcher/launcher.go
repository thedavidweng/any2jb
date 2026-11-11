// Package launcher prepares pinned save packages; all console writes stay in SaveKit.
package launcher

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/thedavidweng/any2jb/manifest"
	"github.com/thedavidweng/any2jb/savekit"
)

func IDs(m *manifest.Manifest) []string {
	var ids []string
	for _, c := range m.Components {
		if c.Launcher != nil {
			ids = append(ids, c.ID)
		}
	}
	return ids
}

type Request = savekit.LauncherRequest

func Validate(r Request, c manifest.Component, target savekit.Request) error {
	if c.ID != r.ID || c.Launcher == nil || target.TitleID != c.Launcher.TitleID {
		return errors.New("the launcher package does not support this game or edition")
	}
	if len(c.Launcher.Versions) > 0 && !slices.Contains(c.Launcher.Versions, r.Version) {
		return fmt.Errorf("%s requires version %s", c.ID, strings.Join(c.Launcher.Versions, " or "))
	}
	if c.Launcher.PhysicalCompatibility && !r.PhysicalCompatibility {
		return fmt.Errorf("%s requires the physical Switch 1 cartridge in Switch 2 compatibility mode, not the native Switch 2 edition", c.ID)
	}
	return nil
}
