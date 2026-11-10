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

// savekitHost is the original any2jb read-only SaveKit host module, compiled
// into the verified binary and written to a private file only when a save
// operation runs. It never ships upstream tool code.
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
	m.mu.Lock()
	if m.state.Lost {
		m.mu.Unlock()
		return errors.New("the retained session was lost; restart the console before another attempt")
	}
	if m.active != nil {
		owner := m.active.tool
		m.mu.Unlock()
		return errors.New(owner + " already owns the retained session; stop it before starting another tool")
	}
	if m.savekitOwner {
		m.mu.Unlock()
		return errors.New("savekit-read already owns the retained session; wait for it to finish")
	}
	m.savekitOwner = true
	m.state = State{Tool: "savekit-read"}
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		m.savekitOwner = false
		m.state.Tool = ""
		m.mu.Unlock()
	}()
	ctx, cancel := context.WithCancel(ctx)
	m.mu.Lock()
	m.savekitCancel = cancel
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		m.savekitCancel = nil
		m.mu.Unlock()
		cancel()
	}()
	module, err := m.savekitModule()
	if err != nil {
		return err
	}
	destination := ""
	if request.SaveKit.Action == "backup" {
		// Exports live under support, outside every session workspace, and each
		// backup gets a fresh folder whose manifest keeps it verifiable later.
		parent := filepath.Join(m.options.Support, "savekit-exports")
		if err := os.MkdirAll(parent, 0o700); err != nil {
			return err
		}
		folder, err := os.MkdirTemp(parent, request.SaveKit.TitleID+"-")
		if err != nil {
			return err
		}
		destination = folder
	}
	report(session.Progress{SideEffect: true})
	client := savekit.Client{Node: m.options.Node, Module: module}
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
