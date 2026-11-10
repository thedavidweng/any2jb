package transport

import (
	"context"
	"errors"
	"sync"

	"github.com/thedavidweng/any2jb/engine"
	"github.com/thedavidweng/any2jb/session"
)

// Prompt is ephemeral console input. Never persist it or include it in diagnostics.
type Prompt struct {
	RunID    string
	Sequence uint64
	Kind     string
	Text     string
}

type Manual struct {
	mu        sync.Mutex
	bootstrap func(session.Run) (string, error)
	prompt    Prompt
	sequence  uint64
	ack       chan struct{}
	changes   chan struct{}
}

func NewManual(bootstrap func(session.Run) (string, error)) *Manual {
	return &Manual{bootstrap: bootstrap, changes: make(chan struct{}, 1)}
}

func (m *Manual) Changes() <-chan struct{} { return m.changes }

func (m *Manual) changed() {
	select {
	case m.changes <- struct{}{}:
	default:
	}
}

func (m *Manual) Prompt() Prompt {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.prompt
}

func (m *Manual) Acknowledge(runID string, sequence uint64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ack == nil || runID != m.prompt.RunID || sequence != m.prompt.Sequence {
		return false
	}
	close(m.ack)
	m.ack = nil
	m.prompt = Prompt{}
	m.changed()
	return true
}

func (m *Manual) ask(ctx context.Context, run session.Run, kind, text string, report session.Report) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	report(session.Progress{SideEffect: true})
	m.mu.Lock()
	m.sequence++
	m.prompt = Prompt{RunID: run.ID, Sequence: m.sequence, Kind: kind, Text: text}
	ack := make(chan struct{})
	m.ack = ack
	m.changed()
	m.mu.Unlock()
	select {
	case <-ctx.Done():
		m.Release(context.Background(), run)
		return ctx.Err()
	case <-ack:
		return ctx.Err()
	}
}

func (m *Manual) Send(ctx context.Context, run session.Run, action session.InputAction, host session.PreparedHost, report session.Report) error {
	if !run.Transport.Capabilities.Manual {
		return errors.New("guided input requires manual transport")
	}
	switch action {
	case session.Bootstrap:
		source, err := m.bootstrap(run)
		if err != nil {
			return err
		}
		for _, p := range []Prompt{{Kind: "slot"}, {Kind: "bootstrap", Text: source}, {Kind: "direct"}} {
			if err := m.ask(ctx, run, p.Kind, p.Text, report); err != nil {
				return err
			}
		}
		return nil
	case session.Save:
		command := `SAVE "` + run.Profile.SaveName + `"`
		if run.Edition == "demo" {
			command = `PRGEDIT 0:C$="":FOR I=0 TO PRGSIZE(0)-1:C$=C$+PRGGET$() :NEXT:SAVEV "TXT:` + run.Profile.SaveName + `",C$`
		}
		return m.ask(ctx, run, "save", command, report)
	case session.Launch:
		pairing, err := engine.PairingFor(host, run)
		if err != nil {
			return err
		}
		load := `LOAD "TXT:` + run.Profile.SaveName + `"`
		if run.Edition == "demo" {
			load = `PRGEDIT 0:LOADV "TXT:` + run.Profile.SaveName + `" OUT C$:PRGSET C$`
		}
		for _, p := range []Prompt{
			{Kind: "load", Text: load}, {Kind: "load-confirm"},
			{Kind: "run", Text: "RUN"}, {Kind: "remove", Text: "REMOVE NSR,&H60000000,&H3FFFFFFF"},
			{Kind: "start", Text: pairing.Command},
		} {
			if err := m.ask(ctx, run, p.Kind, p.Text, report); err != nil {
				return err
			}
		}
		return nil
	default:
		return errors.New("unsupported guided input action")
	}
}

func (m *Manual) Release(_ context.Context, run session.Run) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.prompt.RunID == run.ID {
		m.prompt = Prompt{}
		m.ack = nil
		m.changed()
	}
	return nil
}
