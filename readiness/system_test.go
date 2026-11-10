package readiness_test

import (
	"context"
	"os"
	"testing"

	"github.com/thedavidweng/any2jb/readiness"
	"github.com/thedavidweng/any2jb/session"
)

func TestReadOnlySystemInspection(t *testing.T) {
	if os.Getenv("ANY2JB_READINESS_INSPECTION") != "1" {
		t.Skip("opt-in local OS inspection; no console or USB input")
	}
	checks, err := readiness.New().Inspect(context.Background(), session.Run{})
	if err != nil {
		t.Fatal(err)
	}
	if len(checks) < 2 || checks[0].ID != "network" {
		t.Fatal("missing independent checks")
	}
	for _, check := range checks {
		switch check.State {
		case session.CheckVerified, session.Available, session.RequiresAttention:
		default:
			t.Fatal("unstructured check state")
		}
	}
	t.Logf("Collected %d structured checks. No console connectivity, USB enumeration or input was tested.", len(checks))
}
