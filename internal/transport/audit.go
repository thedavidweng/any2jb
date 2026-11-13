package transport

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/thedavidweng/any2jb/internal/diagnostics"
	"github.com/thedavidweng/any2jb/internal/engine"
	"github.com/thedavidweng/any2jb/internal/session"
)

type usbAudit struct {
	file                      *os.File
	support, workspace, runID string
}

// Called with d.mu held. Standalone bootstrap/live operations keep one private
// workspace for the run; launch appends to the already-owned host workspace.
func (d *Darwin) audit(run session.Run, workspace string) (*usbAudit, error) {
	support := d.options.Runtime.Support
	if d.trusted && support == "" {
		return nil, nil
	}
	if !filepath.IsAbs(support) {
		return nil, errors.New("USB diagnostics requires absolute app support")
	}
	if workspace == "" {
		workspace = d.workspaces[run.ID]
		if workspace == "" {
			var err error
			workspace, err = engine.NewWorkspace(support)
			if err != nil {
				return nil, err
			}
			if d.workspaces == nil {
				d.workspaces = make(map[string]string)
			}
			d.workspaces[run.ID] = workspace
			if err := os.WriteFile(filepath.Join(workspace, "diagnostics-id"), []byte(run.ID), 0600); err != nil {
				return nil, err
			}
		}
	}
	if filepath.Dir(workspace) != filepath.Join(support, "runs") || !strings.HasPrefix(filepath.Base(workspace), "run-") {
		return nil, errors.New("USB log workspace is not an owned run")
	}
	info, err := os.Lstat(workspace)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("USB log workspace is not private")
	}
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	id, err := root.ReadFile("diagnostics-id")
	if err != nil || string(id) != run.ID {
		return nil, errors.New("USB log workspace does not match run")
	}
	if info, err := root.Lstat("usb.log"); err == nil && (!info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0) {
		return nil, errors.New("USB log is not a private regular file")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	file, err := root.OpenFile("usb.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, err
	}
	return &usbAudit{file: file, support: support, workspace: workspace, runID: run.ID}, nil
}

// Callers provide only fixed statuses, validated controller names, counters and
// measured durations. Never pass source text, reports, START values or stderr.
func (a *usbAudit) event(status, message string) error {
	if a == nil {
		return nil
	}
	return json.NewEncoder(a.file).Encode(struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	}{status, message})
}

func (p *helperProcess) auditLine(current, total int, remaining time.Duration) error {
	return p.audit.event("typing_line", fmt.Sprintf("completed=%d total=%d remaining_ms=%d", current, total, remaining.Milliseconds()))
}

func (a *usbAudit) close() error {
	if a == nil {
		return nil
	}
	return errors.Join(a.file.Close(), (diagnostics.Store{Support: a.support}).Capture(a.workspace, a.runID))
}

func (d *Darwin) releaseWorkspace(run session.Run) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	workspace := d.workspaces[run.ID]
	if workspace == "" {
		return nil
	}
	if err := (diagnostics.Store{Support: d.options.Runtime.Support}).Capture(workspace, run.ID); err != nil {
		return err
	}
	if err := os.RemoveAll(workspace); err != nil {
		return err
	}
	delete(d.workspaces, run.ID)
	return nil
}
