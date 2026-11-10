package session_test

import (
	"fmt"
	"testing"

	"github.com/thedavidweng/any2jb/session"
)

func retainedHomebrew(t *testing.T) *session.SessionController {
	t.Helper()
	f := session.NewFakeEnvironment()
	f.RetainedSessions = []session.RetainedSession{{ID: "session-1"}}
	c := session.New(f.Environment(), session.Profile{ID: "test", Revision: "pinned", RetainsRPC: true,
		Tools: []string{"rpc-status", "mintbrew", "native-hblauncher", "izlauncher"}})
	if _, err := c.AttachRetained("session-1"); err != nil {
		t.Fatal(err)
	}
	await(t, c, session.RetainedRPC)
	return c
}

func TestHomebrewToolRequestsValidateTheirInput(t *testing.T) {
	c := retainedHomebrew(t)
	many := make([]string, 16)
	for i := range many {
		many[i] = fmt.Sprintf("module-%02d", i)
	}
	bad := []session.ToolRequest{
		{Tool: "izlauncher", Modules: []string{"UPPER"}},
		{Tool: "izlauncher", Modules: []string{"blitbun", "blitbun"}},
		{Tool: "izlauncher", Modules: many},
		{Tool: "mintbrew", Modules: []string{"blitbun"}},
		{Tool: "native-hblauncher", SDRomFolder: "sd/roms"},
		{Tool: "native-hblauncher", SDRomFolder: "/sd/roms/"},
		{Tool: "native-hblauncher", SDRomFolder: "/sd/../roms"},
		{Tool: "native-hblauncher", AppFolder: "relative/apps"},
		{Tool: "native-hblauncher", AppFolder: "/definitely/missing-any2jb-fixture"},
		{Tool: "mintbrew", SDRomFolder: "/sd/roms/nes"},
	}
	for _, request := range bad {
		_, err := c.RunToolWith(request)
		rejection, ok := err.(*session.Rejection)
		if !ok || rejection.Reason != session.InvalidConfiguration {
			t.Fatalf("invalid request must state InvalidConfiguration: %+v %v", request, err)
		}
	}
	if _, err := c.RunToolWith(session.ToolRequest{Tool: "izlauncher",
		Modules: []string{"iz-exchange", "blitbun"}}); err != nil {
		t.Fatal(err)
	}
	await(t, c, session.RetainedRPC)
	if _, err := c.RunToolWith(session.ToolRequest{Tool: "native-hblauncher",
		SDRomFolder: "/sd/roms/nes", AppFolder: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	await(t, c, session.RetainedRPC)
}
