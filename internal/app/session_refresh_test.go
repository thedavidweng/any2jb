package app

import (
	"testing"
	"time"
)

func TestWindowRefreshFollowsControllerAndManualEventsUntilClose(t *testing.T) {
	closed, controller, manual := make(chan struct{}), make(chan struct{}), make(chan struct{})
	updates, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		RefreshSession(closed, controller, manual, func() { updates <- struct{}{} })
	}()
	timer := time.NewTimer(time.Minute)
	defer timer.Stop()
	for _, events := range []chan struct{}{controller, manual} {
		events <- struct{}{}
		select {
		case <-updates:
		case <-timer.C:
			t.Fatal("window did not receive a background refresh")
		}
	}
	close(closed)
	select {
	case <-done:
	case <-timer.C:
		t.Fatal("window refresh watcher outlived the window")
	}
}
