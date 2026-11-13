package transport

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/thedavidweng/any2jb/internal/session"
)

func (d *Darwin) claim(run session.Run) (*os.File, error) {
	support := d.options.Runtime.Support
	if d.trusted && support == "" {
		return nil, nil
	}
	if !filepath.IsAbs(support) {
		return nil, errors.New("USB input requires an absolute app-support path")
	}
	controller, _ := d.settings()
	root, err := os.OpenRoot(support)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	name := "usb-hid-" + controller + ".lock"
	if info, err := root.Lstat(name); err == nil && !info.Mode().IsRegular() {
		return nil, errors.New("USB controller lease is not a regular file")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	file, err := root.OpenFile(name, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if info, err := file.Stat(); err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		file.Close()
		return nil, errors.New("USB controller lease is not private")
	}
	if err := lockHelperLease(file); err != nil {
		file.Close()
		return nil, errors.New("USB controller is owned by another input operation")
	}
	if err := file.Truncate(0); err != nil {
		file.Close()
		return nil, err
	}
	if _, err := file.WriteString(run.ID); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}

func (p *helperProcess) releaseLease() error {
	if p.lease == nil {
		return nil
	}
	// Keep the inode stable. Removing it could let a concurrent opener lock a
	// different inode. The OS releases this advisory lease on owner death.
	return p.lease.Close()
}
