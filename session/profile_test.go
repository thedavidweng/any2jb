package session_test

import (
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/thedavidweng/any2jb/manifest"
	"github.com/thedavidweng/any2jb/session"
	"github.com/thedavidweng/any2jb/session/sessiontest"
)

func TestProfileEditionAndSavedBootstrap(t *testing.T) {
	m, _ := manifest.Load()
	for _, p := range m.Profiles {
		t.Run(p.ID, func(t *testing.T) {
			c := session.New(sessiontest.NewFakeEnvironment().Environment(), p.SessionProfile())
			if _, err := c.Inspect(); err != nil {
				t.Fatal(err)
			}
			deadline := time.After(time.Second)
			for c.Snapshot().Busy {
				select {
				case <-deadline:
					t.Fatal("inspection hung")
				default:
					runtime.Gosched()
				}
			}
			if c.Snapshot().CanStart() {
				t.Fatal("unconfirmed bootstrap is launchable")
			}
			if _, err := c.Start(); err == nil {
				t.Fatal("unconfirmed bootstrap accepted")
			}
			_, err := c.SelectProfile(p.SessionProfile(), "demo")
			if (err == nil) != p.SupportsDemo {
				t.Fatalf("demo: %v", err)
			}
			if _, err := c.SelectProfile(p.SessionProfile(), "full"); err != nil {
				t.Fatal(err)
			}
			record := session.BootstrapRecord{ProfileID: p.ID, Revision: p.Revision, SourceSHA256: p.SourceSHA256,
				BootstrapSHA256: p.BootstrapSHA256, SaveName: p.SaveName, Edition: "full", Confirmed: true}
			if _, err := c.UseSavedBootstrap(record); err != nil {
				t.Fatal(err)
			}
			if !c.Snapshot().BootstrapSaved {
				t.Fatal("matching bootstrap not accepted")
			}
			for _, other := range m.Profiles {
				if other.ID == p.ID {
					continue
				}
				if _, err := c.SelectProfile(other.SessionProfile(), "full"); err != nil {
					t.Fatal(err)
				}
				if c.Snapshot().BootstrapSaved {
					t.Fatal("switch retained saved assumption")
				}
				if _, err := c.UseSavedBootstrap(record); err == nil || !strings.Contains(err.Error(), "Prepare") {
					t.Fatalf("missing recovery: %v", err)
				}
			}
		})
	}
}
