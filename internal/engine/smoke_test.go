package engine_test

import (
	"context"
	"os"
	"testing"

	"github.com/thedavidweng/any2jb/internal/engine"
	"github.com/thedavidweng/any2jb/internal/manifest"
)

func TestSmokeRejectsMissingPayload(t *testing.T) {
	m, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.SelfTest(context.Background(), t.TempDir(), "0.1.0", m); err == nil {
		t.Fatal("missing packaged runtime accepted")
	}
}

func TestPackagedRuntimeSmoke(t *testing.T) {
	assets := os.Getenv("ANY2JB_PACKAGED_RESOURCES")
	if assets == "" {
		t.Skip("set ANY2JB_PACKAGED_RESOURCES to a built app's Contents/Resources")
	}
	m, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.SelfTest(context.Background(), assets, "0.1.0", m); err != nil {
		t.Fatal(err)
	}
}
