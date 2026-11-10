package retained

import (
	"context"
	_ "embed"
	"errors"
	"os"
	"path/filepath"

	"github.com/thedavidweng/any2jb/savekit"
	"github.com/thedavidweng/any2jb/session"
)

// savekitHost is the original any2jb SaveKit host module, compiled into the
// verified binary and written to a private file only when a save operation
// runs. It never ships upstream tool code.
//
//go:embed savekit-host.cjs
var savekitHost []byte

// savekitModule resolves the host module path: the explicit fixture module, or
// the embedded original module written under the private support root.
func (m *Manager) savekitModule() (string, error) {
	if m.options.SaveKitModule != "" {
		return m.options.SaveKitModule, nil
	}
	if m.options.Support == "" {
		return "", errors.New("no private support directory for the SaveKit host")
	}
	module := filepath.Join(m.options.Support, "savekit-host.cjs")
	if err := os.WriteFile(module, savekitHost, 0o600); err != nil {
		return "", err
	}
	return module, nil
}

// acquireSaveKit takes the SaveKit owner slot for one synchronous operation
// under the same lease rules as every tool: a live session, no active tool and
// no other SaveKit operation. The release function frees the slot and Stop
// cancellation.
func (m *Manager) acquireSaveKit(ctx context.Context, tool string) (context.Context, func(), error) {
	m.mu.Lock()
	if m.state.Lost {
		m.mu.Unlock()
		return nil, nil, errors.New("the retained session was lost; restart the console before another attempt")
	}
	if m.active != nil {
		owner := m.active.tool
		m.mu.Unlock()
		return nil, nil, errors.New(owner + " already owns the retained session; stop it before starting another tool")
	}
	if m.savekitOwner {
		owner := m.state.Tool
		m.mu.Unlock()
		return nil, nil, errors.New(owner + " already owns the retained session; wait for it to finish")
	}
	m.savekitOwner = true
	m.state.Tool = tool
	m.mu.Unlock()
	ctx, cancel := context.WithCancel(ctx)
	m.mu.Lock()
	m.savekitCancel = cancel
	m.mu.Unlock()
	release := func() {
		m.mu.Lock()
		m.savekitOwner = false
		m.savekitCancel = nil
		m.state.Tool = ""
		m.mu.Unlock()
		cancel()
	}
	return ctx, release, nil
}

// freshExportFolder allocates a new verified-export folder under support,
// outside every session workspace.
func (m *Manager) freshExportFolder(titleID string) (string, error) {
	parent := filepath.Join(m.options.Support, "savekit-exports")
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return "", err
	}
	return os.MkdirTemp(parent, titleID+"-")
}

// runSaveKit executes one read-only save operation under the same lease rules
// as every tool: fresh held evidence, the recorded distribution decision and a
// single owner. The operation is synchronous; Stop cancels its child.
func (m *Manager) runSaveKit(ctx context.Context, run session.Run, request session.ToolRequest, workspace string, report session.Report) error {
	if request.SaveKit == nil {
		return errors.New("missing read-only SaveKit request")
	}
	if err := request.SaveKit.Validate(); err != nil {
		return err
	}
	ctx, release, err := m.acquireSaveKit(ctx, "savekit-read")
	if err != nil {
		return err
	}
	defer release()
	module, err := m.savekitModule()
	if err != nil {
		return err
	}
	destination := ""
	if request.SaveKit.Action == "backup" {
		// Exports live under support, outside every session workspace, and each
		// backup gets a fresh folder whose manifest keeps it verifiable later.
		folder, err := m.freshExportFolder(request.SaveKit.TitleID)
		if err != nil {
			return err
		}
		destination = folder
	}
	report(session.Progress{SideEffect: true})
	client := savekit.Client{Node: m.options.Node, Module: module, Scenario: m.options.SaveKitScenario}
	result, err := client.Run(ctx, workspace, destination, *request.SaveKit)
	if err != nil {
		return err
	}
	m.mu.Lock()
	observed := m.state.SaveKit
	if observed == nil {
		observed = &SaveKitState{}
		m.state.SaveKit = observed
	}
	observed.Action = result.Action
	if result.Users != nil {
		observed.Users = result.Users
	}
	if result.Titles != nil {
		observed.Titles = result.Titles
	}
	if result.Inventory != nil {
		observed.Inventory = result.Inventory
	}
	if result.Output != "" {
		observed.Output = result.Output
	}
	if result.Path != "" {
		observed.Path = result.Path
		m.state.Export = result.Path
	}
	m.mu.Unlock()
	return nil
}
