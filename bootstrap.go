package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/thedavidweng/any2jb/session"
)

func (a *app) bootstrapPath() string {
	return filepath.Join(filepath.Dir(a.preferencesPath), "bootstrap.json")
}

func (a *app) restoreBootstrap() error {
	data, err := os.ReadFile(a.bootstrapPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var record session.BootstrapRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return err
	}
	if record.SavedAt.IsZero() {
		return errors.New("saved bootstrap identity has no confirmation timestamp")
	}
	// A valid but no-longer-matching record must not prevent preparing a new one.
	a.command(a.controller.UseSavedBootstrap(record))
	return nil
}

func (a *app) confirmExistingBootstrap() error {
	s := a.controller.Snapshot()
	record := session.BootstrapRecord{
		ProfileID: s.Profile.ID, Revision: s.Profile.Revision,
		SourceSHA256: s.Profile.SourceSHA256, BootstrapSHA256: s.Profile.BootstrapSHA256,
		SaveName: s.Profile.SaveName, Edition: s.Edition, Confirmed: true, SavedAt: time.Now().UTC(),
	}
	if _, err := a.controller.UseSavedBootstrap(record); err != nil {
		return err
	}
	return a.persistBootstrap(a.bootstrapPath())
}

func (a *app) persistBootstrap(path string) error {
	a.bootstrapMu.Lock()
	defer a.bootstrapMu.Unlock()
	// Read after acquiring the writer lock, so an older redraw cannot replace
	// a newer explicit identity confirmation.
	s := a.controller.Snapshot()
	if !s.BootstrapSaved {
		return nil
	}
	return saveBootstrapRecord(path, s.SavedBootstrap)
}

func saveBootstrapRecord(path string, record session.BootstrapRecord) error {
	if !record.Confirmed || record.SavedAt.IsZero() {
		return errors.New("saved bootstrap requires explicit confirmation")
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".bootstrap-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
