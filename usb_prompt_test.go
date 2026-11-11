package main

import (
	"context"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/session"
	"github.com/thedavidweng/any2jb/transport"
)

// errContext reports an error while never signalling cancellation, so the
// confirmation's own result is the only thing the outcome can depend on.
type errContext struct{ context.Context }

func (errContext) Done() <-chan struct{} { return nil }
func (errContext) Err() error            { return context.Canceled }

func TestConsoleConfirmationSucceedsWhateverTheContextSays(t *testing.T) {
	p := &usbPrompt{}
	result := make(chan error, 1)
	go func() { result <- p.Wait(errContext{context.Background()}, session.Run{}, transport.LoadPrompt) }()
	tt := ui.NewTester(p.view, 640, 320)
	deadline := time.Now().Add(time.Minute)
	for !tt.HasText(consolePrompt(transport.LoadPrompt)) {
		if time.Now().After(deadline) {
			t.Fatal("the console confirmation never appeared")
		}
		tt.Frame()
	}
	if err := tt.Click("Console is ready, continue"); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatalf("an acknowledged confirmation reported %v", err)
	}
}
