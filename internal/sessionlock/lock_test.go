package sessionlock

import (
	"path/filepath"
	"testing"
)

func TestOneOwnerAndRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.lock")
	release, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	if other, err := Acquire(path); err == nil {
		other()
		t.Fatal("two presentations acquired the same session")
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	release, err = Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
}
