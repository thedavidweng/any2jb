package retained

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/thedavidweng/any2jb/launcher"
	"github.com/thedavidweng/any2jb/manifest"
	"github.com/thedavidweng/any2jb/savekit"
)

// PrepareLauncher is local-only and must run off the UI thread. The write gate
// prepares the package again, so editing a preview never authorizes other data.
func (m *Manager) PrepareLauncher(ctx context.Context, r launcher.Request, target savekit.Request) (string, savekit.Preview, error) {
	if reason := m.Unavailable(r.ID, ""); reason != "" {
		return "", savekit.Preview{}, errors.New(reason)
	}
	if m.options.Manifest == nil {
		return "", savekit.Preview{}, errors.New("launcher package is not recorded")
	}
	c, err := m.options.Manifest.ToolComponent(r.ID)
	if err != nil {
		return "", savekit.Preview{}, err
	}
	if err := launcher.Validate(r, c, target); err != nil {
		return "", savekit.Preview{}, err
	}
	if c.Launcher.FullGame {
		export, err := savekit.ReadExport(r.BackupFolder)
		if err != nil {
			return "", savekit.Preview{}, err
		}
		if err := export.MatchesTarget(target); err != nil {
			return "", savekit.Preview{}, err
		}
		if export.UID != target.UID {
			return "", savekit.Preview{}, errors.New("the full-game export belongs to a different account")
		}
	}
	parent := filepath.Join(m.options.Support, "launcher-previews")
	if err := os.MkdirAll(parent, 0700); err != nil {
		return "", savekit.Preview{}, err
	}
	dir, err := os.MkdirTemp(parent, r.ID+"-")
	if err != nil {
		return "", savekit.Preview{}, err
	}
	folder := filepath.Join(dir, "save")
	preview, err := launcher.Prepare(ctx, m.options.Manifest, c, filepath.Join(m.options.ToolRoot, "tools", r.ID), folder, m.options.Node, r.BackupFolder)
	if err != nil {
		return "", savekit.Preview{}, errors.Join(err, os.RemoveAll(dir))
	}
	return folder, preview, nil
}

func (m *Manager) launcherGuard(r launcher.Request, c manifest.Component, live savekit.Export, preview savekit.Preview, target savekit.Request) error {
	if live.UID != target.UID {
		return errors.New("the launcher target must belong to the selected console account")
	}
	if c.Launcher.FullGame {
		base, err := savekit.ReadExport(r.BackupFolder)
		if err != nil {
			return err
		}
		if err := savekit.CompareExports(live, base); err != nil {
			return errors.New("the full-game save changed since the patch preview; export and preview it again")
		}
	}
	return launcher.CheckExisting(c, live, preview)
}
