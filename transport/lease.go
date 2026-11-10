package transport

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/thedavidweng/any2jb/session"
)

func (d *Darwin) claim(run session.Run) (string, error) {
	support := d.options.Runtime.Support
	// Process fixtures may omit disk ownership; production authorization requires
	// an explicit app-support root.
	if d.trusted && support == "" {
		return "", nil
	}
	if !filepath.IsAbs(support) {
		return "", errors.New("USB input requires an absolute app-support path")
	}
	controller, _ := d.settings()
	name := filepath.Join(support, "usb-hid-"+controller+".lock")
	f, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", errors.New("USB controller lease unavailable; close its owner or inspect the stale lease before retrying")
	}
	_, writeErr := f.WriteString(run.ID)
	err = errors.Join(writeErr, f.Close())
	if err != nil {
		os.Remove(name)
		return "", err
	}
	return name, nil
}

func (p *helperProcess) releaseLease() error {
	if p.lease == "" {
		return nil
	}
	info, err := os.Lstat(p.lease)
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("USB controller lease changed")
	}
	owner, err := os.ReadFile(p.lease)
	if err != nil || string(owner) != p.run.ID {
		return errors.New("USB controller lease ownership changed")
	}
	return os.Remove(p.lease)
}
