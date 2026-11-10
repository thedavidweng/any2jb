package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestShell(t *testing.T) {
	a := &app{}
	tt := ui.NewTester(a.view, 640, 400)
	for _, text := range []string{"any2jb", "No console session is running."} {
		if !tt.HasText(text) {
			t.Errorf("missing %q in shell: %q", text, tt.Texts())
		}
	}
	if err := tt.Click("About this build"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("Development shell. Console workflows are not available yet.") {
		t.Errorf("missing build limitations: %q", tt.Texts())
	}
}
