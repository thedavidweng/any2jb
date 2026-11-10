package main

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/session"
	"github.com/thedavidweng/any2jb/session/sessiontest"
)

type restartPlatform struct {
	session.PlatformServices
	releaseError error
}

func (p restartPlatform) ReleaseInterrupted(context.Context) error { return p.releaseError }

func TestConsoleRestartUIKeepsLiveLeaseBlockedAndShowsError(t *testing.T) {
	for _, releaseError := range []error{nil, errors.New("session owner is still alive")} {
		env := sessiontest.NewFakeEnvironment().Environment()
		env.Platform = restartPlatform{PlatformServices: env.Platform, releaseError: releaseError}
		a := &app{controller: session.New(env, session.Profile{ID: "fixture", Revision: "pinned"})}
		a.controller.MarkInterrupted()
		tt := ui.NewTester(a.view, 640, 720)
		clickManual(t, tt, "I restarted the console, prepare a new attempt")
		timer := time.NewTimer(time.Minute)
		for {
			tt.Frame()
			if releaseError == nil && a.controller.Snapshot().Stage == session.Idle {
				break
			}
			if releaseError != nil && tt.HasText("session owner is still alive") {
				s := a.controller.Snapshot()
				if s.Stage != session.Indeterminate || s.CanStart() {
					t.Fatal("failed lease release allowed launch")
				}
				break
			}
			select {
			case <-timer.C:
				t.Fatal("console recovery did not finish")
			default:
				runtime.Gosched()
			}
		}
		timer.Stop()
		tt.Frame()
		if releaseError == nil && !tt.HasText("Home") {
			t.Fatal("recovery did not return to Home")
		}
	}
}
