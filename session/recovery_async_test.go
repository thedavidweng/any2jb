package session_test

import (
	"context"
	"testing"
	"time"

	"github.com/thedavidweng/any2jb/session"
)

type blockedRecoveryPlatform struct {
	session.PlatformServices
	entered chan struct{}
	release chan struct{}
}

func (p blockedRecoveryPlatform) ReleaseInterrupted(ctx context.Context) error {
	close(p.entered)
	select {
	case <-p.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestRecoveryLeaseIODoesNotBlockWindowSnapshot(t *testing.T) {
	env := session.NewFakeEnvironment().Environment()
	platform := blockedRecoveryPlatform{PlatformServices: env.Platform, entered: make(chan struct{}), release: make(chan struct{})}
	env.Platform = platform
	c := session.New(env, session.Profile{ID: "test"})
	c.MarkInterrupted()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := c.ConfirmConsoleRestart(ctx, true); done <- err }()
	<-platform.entered
	snapshots := make(chan session.Snapshot, 1)
	go func() { snapshots <- c.Snapshot() }()
	timer := time.NewTimer(time.Minute)
	defer timer.Stop()
	select {
	case s := <-snapshots:
		if s.Stage != session.Indeterminate {
			t.Fatal("lease check prematurely recovered")
		}
	case <-timer.C:
		t.Fatal("lease I/O blocked UI snapshot")
	}
	close(platform.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
