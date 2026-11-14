package diagnostics

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/thedavidweng/any2jb/locales"
)

type Owner struct {
	RunID string `json:"runID"`
	PID   int    `json:"pid"`
}

func (s Store) RecordOwner(id string) error {
	data, err := json.Marshal(Owner{RunID: id, PID: os.Getpid()})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.Support, "session-owner.json"), data, 0o600)
}

// ReleaseInterrupted never signals a process. Unknown ownership or a live PID
// blocks recovery, including PID reuse. The caller must confirm console restart.
func (s Store) ReleaseInterrupted(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	unlock, err := recoveryLock(filepath.Join(s.Support, "session-recovery.lock"))
	if err != nil {
		return err
	}
	defer unlock()
	lock := filepath.Join(s.Support, "session.lock")
	info, err := os.Lstat(lock)
	if err == nil {
		if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
			return locales.Key("dynRecInvalidLease")
		}
		id, err := os.ReadFile(lock)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(filepath.Join(s.Support, "session-owner.json"))
		if err != nil {
			return err
		}
		var owner Owner
		if json.Unmarshal(data, &owner) != nil || owner.RunID != string(id) || owner.PID <= 0 {
			return locales.Key("dynRecOwnerUnproven")
		}
		dead, err := ownerDead(owner.PID)
		if err != nil {
			return err
		}
		if !dead {
			return locales.Key("dynRecOwnerAlive")
		}
	} else if errors.Is(err, os.ErrNotExist) {
		// Reserve the same lease used by HostRunner while relocating orphaned runs.
		f, err := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		f.Close()
		defer os.Remove(lock)
	} else {
		return err
	}
	entries, err := os.ReadDir(filepath.Join(s.Support, "runs"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if len(entries) > 0 {
		if err := os.MkdirAll(filepath.Join(s.Support, "past-runs"), 0o700); err != nil {
			return err
		}
		for _, entry := range entries {
			dest := filepath.Join(s.Support, "past-runs", entry.Name())
			if _, err := os.Lstat(dest); !errors.Is(err, os.ErrNotExist) {
				return errors.New("past session destination already exists")
			}
			if err := os.Rename(filepath.Join(s.Support, "runs", entry.Name()), dest); err != nil {
				return err
			}
		}
	}
	if err := os.Remove(filepath.Join(s.Support, "session-owner.json")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Remove(filepath.Join(s.Support, "session-interrupted")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Remove(lock)
}
