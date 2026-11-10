package session_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/thedavidweng/any2jb/savekit"
	"github.com/thedavidweng/any2jb/session"
)

func attachable(t *testing.T, f *session.FakeEnvironment) *session.SessionController {
	t.Helper()
	return session.New(f.Environment(), session.Profile{ID: "test", Revision: "pinned",
		RetainsRPC: true, Tools: []string{"rpc-status"}})
}

func TestAttachRequiresRetainedCapabilityAndAdapter(t *testing.T) {
	f := session.NewFakeEnvironment()
	noRPC := session.New(f.Environment(), session.Profile{ID: "test", Revision: "pinned"})
	if _, err := noRPC.AttachRetained("session-1"); err == nil {
		t.Fatal("attached to a host without the retained capability")
	}
	bare := session.NewFakeEnvironment()
	env := bare.Environment()
	env.Retained = nil
	c := session.New(env, session.Profile{ID: "test", Revision: "pinned", RetainsRPC: true})
	_, err := c.AttachRetained("session-1")
	rejection, ok := err.(*session.Rejection)
	if !ok || rejection.Reason != session.Unsupported || rejection.Instruction == "" {
		t.Fatalf("missing stated reason for an unsupported host: %v", err)
	}
}

func TestAttachIsDistinctFromLaunchAndDetachLeavesTheHostRunning(t *testing.T) {
	f := session.NewFakeEnvironment()
	f.RetainedSessions = []session.RetainedSession{{ID: "session-1", Description: "kurumuz · session-1"}}
	c := attachable(t, f)
	found, err := c.DiscoverRetained(context.Background())
	if err != nil || len(found) != 1 || found[0].ID != "session-1" {
		t.Fatalf("discovery did not list the owned verified session: %v %v", found, err)
	}
	if _, err := c.AttachRetained("session-1"); err != nil {
		t.Fatal(err)
	}
	s := await(t, c, session.RetainedRPC)
	if !s.Attached {
		t.Fatal("attached session is indistinguishable from a launched one")
	}
	trace := f.Trace()
	if slices.Contains(trace, "host.prepare") || slices.Contains(trace, "input.launch") {
		t.Fatalf("attaching rebuilt the exploit: %v", trace)
	}
	if !slices.Contains(trace, "retained.attach") {
		t.Fatalf("attach adapter was not used: %v", trace)
	}
	if _, err := c.Finish(); err == nil {
		t.Fatal("finished a host owned by another app")
	}
	if _, err := c.DetachRetained(); err != nil {
		t.Fatal(err)
	}
	s = await(t, c, session.Idle)
	if s.Attached {
		t.Fatal("detach kept the attachment")
	}
	trace = f.Trace()
	if !slices.Contains(trace, "retained.detach") {
		t.Fatalf("detach adapter was not used: %v", trace)
	}
	if slices.Contains(trace, "host.cleanup") {
		t.Fatalf("detach stopped the original host: %v", trace)
	}
}

func TestSaveKitMutationsNeedTheirGateBeforeAnyToolRuns(t *testing.T) {
	f := session.NewFakeEnvironment()
	f.RetainedSessions = []session.RetainedSession{{ID: "session-1"}}
	c := session.New(f.Environment(), session.Profile{ID: "test", Revision: "pinned",
		RetainsRPC: true, Tools: []string{"savekit-read", "savekit"}})
	if _, err := c.AttachRetained("session-1"); err != nil {
		t.Fatal(err)
	}
	await(t, c, session.RetainedRPC)
	// Without the individual confirmation and the preview binding, a mutation
	// is rejected before the tool adapter ever sees it.
	for _, action := range []string{"write", "restore", "delete"} {
		_, err := c.RunToolWith(session.ToolRequest{Tool: "savekit", SaveKit: &savekit.Request{
			Action: action, TitleID: "010012900d38a000", UID: strings.Repeat("1", 32)}})
		rejection, ok := err.(*session.Rejection)
		if !ok || rejection.Reason != session.InvalidConfiguration {
			t.Fatalf("ungated %s reached the tool adapter: %v", action, err)
		}
	}
	if slices.Contains(f.Trace(), "tool.run-with savekit") {
		t.Fatalf("an ungated mutation reached the adapter: %v", f.Trace())
	}
	if _, err := c.RunToolWith(session.ToolRequest{Tool: "savekit-read",
		SaveKit: &savekit.Request{Action: "accounts"}}); err != nil {
		t.Fatal(err)
	}
	await(t, c, session.RetainedRPC)
}
func TestAttachedToolNeedsTheLeaseAndALostSessionIsNotRetried(t *testing.T) {
	f := session.NewFakeEnvironment()
	f.RetainedSessions = []session.RetainedSession{{ID: "session-1"}}
	lost := make(chan struct{})
	f.RetainedLostGate = lost
	c := attachable(t, f)
	if _, err := c.RunTool("rpc-status"); err == nil {
		t.Fatal("tool ran without a retained lease")
	}
	if _, err := c.AttachRetained("session-1"); err != nil {
		t.Fatal(err)
	}
	await(t, c, session.RetainedRPC)
	if _, err := c.RunTool("savekit"); err == nil {
		t.Fatal("tool outside the verified profile was permitted")
	}
	if _, err := c.RunTool("rpc-status"); err != nil {
		t.Fatal(err)
	}
	await(t, c, session.RetainedRPC)
	close(lost)
	s := await(t, c, session.Indeterminate)
	if s.Recovery != session.RestartConsole || s.Attached {
		t.Fatalf("lost attachment was not reported as lost: %+v", s)
	}
	if _, err := c.RunTool("rpc-status"); err == nil {
		t.Fatal("tool accepted after the attachment was lost")
	}
}

func TestAttachRefusesIncompleteHeldEvidence(t *testing.T) {
	f := session.NewFakeEnvironment()
	f.RetainedSessions = []session.RetainedSession{{ID: "session-1"}}
	f.AttachOutcome = session.Outcome{Verified: true, FSVerified: true, Restored: true, RetainedReady: true}
	c := attachable(t, f)
	if _, err := c.AttachRetained("session-1"); err != nil {
		t.Fatal(err)
	}
	await(t, c, session.Failed)
	if c.Snapshot().Attached {
		t.Fatal("incomplete held evidence produced an attached session")
	}
}
