package sessionlock

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/thedavidweng/any2jb/internal/diagnostics"
)

func TestOneOwnerAndRelease(t *testing.T) {
	support := t.TempDir()
	release, err := Acquire(support)
	if err != nil {
		t.Fatal(err)
	}
	if other, err := Acquire(support); err == nil {
		other()
		t.Fatal("two presentations acquired the same session")
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	release, err = Acquire(support)
	if err != nil {
		t.Fatal(err)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
}

// The owner lock is not the host's per-run lease: holding it, or the file it
// leaves behind, must neither read as an interrupted session nor block a lease.
func TestOwnerLockIsNotTheRunLease(t *testing.T) {
	support := t.TempDir()
	release, err := Acquire(support)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if interrupted, err := (diagnostics.Store{Support: support}).Interrupted(); err != nil || interrupted {
		t.Fatalf("owner lock read as an interrupted session: %v %v", interrupted, err)
	}
	lease, err := os.OpenFile(filepath.Join(support, "session.lock"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("owner lock blocks the host lease: %v", err)
	}
	lease.Close()
}
