//go:build darwin || linux

package engine_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/thedavidweng/any2jb/internal/engine"
	"github.com/thedavidweng/any2jb/internal/session"
	"github.com/thedavidweng/any2jb/internal/session/sessiontest"
)

func TestCleanupKillsGrandchildThatIgnoresTermination(t *testing.T) {
	runner, run := protocolRunner(t, "grandchild"), testRun()
	host, err := runner.Prepare(context.Background(), run, func(session.Progress) {})
	if err != nil {
		t.Fatal(err)
	}
	details, _ := engine.Pairing(host)
	data, err := os.ReadFile(filepath.Join(details.Workspace, "grandchild.json"))
	if err != nil {
		t.Fatal(err)
	}
	var child struct {
		PID int `json:"pid"`
	}
	if err := json.Unmarshal(data, &child); err != nil {
		t.Fatal(err)
	}
	if err := runner.Cleanup(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	timeout := time.NewTimer(time.Minute)
	defer timeout.Stop()
	for {
		if errors.Is(syscall.Kill(child.PID, 0), syscall.ESRCH) {
			break
		}
		select {
		case <-timeout.C:
			t.Fatal("grandchild survived owned process-group cleanup")
		default:
			runtime.Gosched()
		}
	}
}

func TestRetainedProcessCrashInvalidatesController(t *testing.T) {
	runner := protocolRunner(t, "held-verified")
	f := sessiontest.NewFakeEnvironment()
	env := f.Environment()
	env.Host = runner
	profile := testRun().Profile
	profile.Revision, profile.RetainsRPC = "fixture", true
	c := session.New(env, profile)
	c.ConfigureTransport(session.Transport{Mode: "manual", Capabilities: session.Capabilities{Manual: true}}, "192.0.2.1")
	c.Inspect()
	awaitSnapshot(t, c, func(s session.Snapshot) bool { return s.Stage == session.Ready && !s.Busy })
	c.Start()
	s := awaitSnapshot(t, c, func(s session.Snapshot) bool { return s.Stage == session.RetainedRPC && !s.Busy })
	process, err := os.FindProcess(s.Processes[0].PID)
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Kill(); err != nil {
		t.Fatal(err)
	}
	s = awaitSnapshot(t, c, func(s session.Snapshot) bool { return s.Stage == session.Indeterminate && !s.Busy })
	if s.Recovery != session.RestartConsole || len(s.Processes) != 0 {
		t.Fatal("crash retained a usable session")
	}
}
