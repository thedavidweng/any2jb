package main

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/locales"
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
		a := testApp(t, session.New(env, session.Profile{ID: "fixture", Revision: "pinned"}))
		a.controller.MarkInterrupted()
		published := make(chan func(), 4)
		a.restart.update = func(apply func()) { published <- apply }
		tt := ui.NewTester(a.view, 640, 720)
		clickManual(t, tt, "I restarted the console, prepare a new attempt")
		timer := time.NewTimer(time.Minute)
		for {
			select {
			case apply := <-published:
				apply()
			default:
			}
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
		if releaseError == nil && (a.flow.stage != flowDetection || !tt.HasText(locales.FlowCheckFirst())) {
			t.Fatal("recovery did not return to the first stage")
		}
	}
}

type unconfirmedInput struct{ session.InputTransport }

func (unconfirmedInput) Release(context.Context, session.Run) error {
	return session.ErrReleaseUnconfirmed
}

func TestConsoleRestartPanelShowsTheUnconfirmedReleaseInstruction(t *testing.T) {
	withLocale(t, "zh-CN")
	f := sessiontest.NewFakeEnvironment()
	f.InputGate = make(chan struct{})
	env := f.Environment()
	env.Input = unconfirmedInput{env.Input}
	a := testApp(t, session.New(env, session.Profile{ID: "fixture", Revision: "pinned"}))
	tt := ui.NewTester(a.view, 640, 1200)
	clickManual(t, tt, locales.CheckAgain())
	uiStage(t, a, tt, session.Ready)
	advanceToLaunch(t, a, tt)
	clickManual(t, tt, locales.GuidedManualLaunch())
	deadline := time.After(time.Second)
	for {
		s := a.controller.Snapshot()
		if s.Stage == session.WaitingForConsole && s.SideEffects {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("console input did not start: %+v", s)
		case <-time.After(time.Millisecond):
		}
	}
	tt.Frame()
	clickManual(t, tt, locales.Stop())
	uiStage(t, a, tt, session.Indeterminate)
	if !tt.HasText(locales.DynReleaseUnconfirmed()) {
		t.Fatal(tt.Texts())
	}
}

// Recovery states its status where every observed row does: at the end of
// the line that names it.
func TestRecoveryStatusSharesItsHeadingLine(t *testing.T) {
	a := newTestApp(t)
	a.controller.MarkInterrupted()
	tt := ui.NewTester(a.view, 960, 900)
	tt.Frame()
	heading, ok := tt.Access(locales.RecoveryRequired())
	status, found := tt.Access(locales.CheckAttention())
	if !ok || !found {
		t.Fatal("recovery heading or status missing")
	}
	middle := status.Bounds.Y + status.Bounds.H/2
	if middle < heading.Bounds.Y || middle > heading.Bounds.Y+heading.Bounds.H || status.Bounds.X <= heading.Bounds.X+heading.Bounds.W {
		t.Fatalf("recovery status %v is not at the end of its heading %v", status.Bounds, heading.Bounds)
	}
}
