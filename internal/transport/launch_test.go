package transport_test

import (
	"context"
	"testing"

	"github.com/thedavidweng/any2jb/internal/engine"
	"github.com/thedavidweng/any2jb/internal/manifest"
	"github.com/thedavidweng/any2jb/internal/session"
	guided "github.com/thedavidweng/any2jb/internal/transport"
)

func TestManualLaunchUsesExactProfileAndEditionCommands(t *testing.T) {
	profiles, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, profile := range profiles.Profiles {
		for _, edition := range profile.SessionProfile().Editions {
			t.Run(profile.ID+"/"+edition, func(t *testing.T) {
				m := guided.NewManual(nil)
				run := session.Run{ID: "launch", Profile: profile.SessionProfile(), Edition: edition, LANIPv4: "192.0.2.1",
					Transport: session.Transport{Mode: "manual", Capabilities: session.Capabilities{Manual: true}}}
				start := "V=START(39380,192,0,2,1)"
				if profile.StartArgumentCount == 9 {
					start = "V=START(39380,192,0,2,1,1,2,3,4)"
				}
				host := session.PreparedHost{Listening: true, PairingValid: true,
					Opaque: engine.PairingDetails{RunID: run.ID, Command: start}}
				done := make(chan error, 1)
				go func() { done <- m.Send(context.Background(), run, session.Launch, host, func(session.Progress) {}) }()
				load := `LOAD "TXT:NS2"`
				if edition == "demo" {
					load = `PRGEDIT 0:LOADV "TXT:NS2" OUT C$:PRGSET C$`
				}
				for _, text := range []string{load, "", "RUN", "REMOVE NSR,&H60000000,&H3FFFFFFF", start} {
					p := prompt(t, m)
					if p.Text != text {
						t.Fatalf("wrong command: %q, want %q", p.Text, text)
					}
					m.Acknowledge(run.ID, p.Sequence)
					if m.Acknowledge(run.ID, p.Sequence) {
						t.Fatal("duplicate acknowledgement accepted")
					}
				}
				if err := <-done; err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestManualRejectsUnreadyOrWrongSessionPairing(t *testing.T) {
	for _, host := range []session.PreparedHost{
		{PairingValid: true}, {Listening: true},
		{Listening: true, PairingValid: true, Opaque: engine.PairingDetails{RunID: "other", Command: "V=START(39380,192,0,2,1)"}},
		{Listening: true, PairingValid: true, Opaque: engine.PairingDetails{RunID: "launch", Command: "V=START(39380,192,0,2,9)"}},
	} {
		m := guided.NewManual(nil)
		run := session.Run{ID: "launch", LANIPv4: "192.0.2.1", Profile: session.Profile{StartPort: 39380, StartArgumentCount: 5},
			Transport: session.Transport{Capabilities: session.Capabilities{Manual: true}}}
		if err := m.Send(context.Background(), run, session.Launch, host, func(session.Progress) {}); err == nil {
			t.Fatal("untrusted pairing accepted")
		}
		if m.Prompt().Kind != "" {
			t.Fatal("command exposed without valid listening host")
		}
	}
}

func TestManualDemoSaveCommand(t *testing.T) {
	m := guided.NewManual(nil)
	run := session.Run{ID: "demo", Edition: "demo", Profile: session.Profile{SaveName: "NS2"},
		Transport: session.Transport{Capabilities: session.Capabilities{Manual: true}}}
	done := make(chan error, 1)
	go func() {
		done <- m.Send(context.Background(), run, session.Save, session.PreparedHost{}, func(session.Progress) {})
	}()
	p := prompt(t, m)
	if p.Text != `PRGEDIT 0:C$="":FOR I=0 TO PRGSIZE(0)-1:C$=C$+PRGGET$() :NEXT:SAVEV "TXT:NS2",C$` {
		t.Fatal(p.Text)
	}
	m.Acknowledge(run.ID, p.Sequence)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
