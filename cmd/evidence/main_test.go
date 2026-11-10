package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thedavidweng/any2jb/internal/hwevidence"
	"github.com/thedavidweng/any2jb/manifest"
)

func writeLog(t *testing.T, dir string) string {
	t.Helper()
	m, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	p, err := m.Profile("pepsipu")
	if err != nil {
		t.Fatal(err)
	}
	log := &hwevidence.Log{
		Schema: hwevidence.Schema, Date: "2026-11-20", Tester: "david", Route: hwevidence.RouteP1A,
		Combination: hwevidence.Combination{
			MacModel: "MacBook Pro (Apple Silicon)", MacOSVersion: "26.1 (25B78)",
			StartupSecurity: "Reduced Security", HostProfile: "pepsipu",
			ConsoleSystem: "23.0.1", GameEdition: "full", GameVersion: "4.4.9",
			BootstrapSHA256: p.BootstrapSHA256, Transport: hwevidence.TransportUSBHID,
			USBController: "usb-drd0", Cable: "USB-C data cable", LANTopology: "same LAN",
		},
		Steps: []hwevidence.Step{
			{ID: "helper-dry-run", Outcome: hwevidence.OutcomePass, Log: "dry-runs passed"},
			{ID: "usb-enumeration", Outcome: hwevidence.OutcomePass, Log: "READY on usb-drd0"},
			{ID: "harmless-typing", Outcome: hwevidence.OutcomePass, Log: "typed into TextEdit"},
			{ID: "stop-key-release", Outcome: hwevidence.OutcomePass, Log: "no held keys"},
			{ID: "console-bootstrap-save", Outcome: hwevidence.OutcomePass, Log: "SAVE confirmed"},
			{ID: "host-connection", Outcome: hwevidence.OutcomePass, Log: "paired"},
			{ID: "verification-4of4", Outcome: hwevidence.OutcomePass, Log: "4/4 structured result"},
			{ID: "retained-rpc-readonly", Outcome: hwevidence.OutcomeBlocked, Notes: "pepsipu has no held RPC", Log: "reason shown"},
		},
	}
	data, err := json.MarshalIndent(log, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "2026-11-20-p1a-pepsipu.json")
	if err := os.WriteFile(path, append(data, '\n'), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestValidateCommand(t *testing.T) {
	path := writeLog(t, t.TempDir())
	var out bytes.Buffer
	if err := run([]string{"validate", path}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "PASS "+path) {
		t.Errorf("missing PASS line: %s", out.String())
	}
}

func TestValidateRejectsBadLog(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte(`{"schema":"wrong"}`), 0644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run([]string{"validate", path}, &out); err == nil {
		t.Fatal("expected validation failure")
	}
}

func TestApplyAndReportCommands(t *testing.T) {
	dir := t.TempDir()
	logPath := writeLog(t, dir)
	ledger := filepath.Join(dir, "parity-ledger.md")
	content := "| ID | G | C | Tier | S | P | R | D | Evidence | E | Link | T | K |\n" +
		"|---|---|---|---|---|---|---|---|---|---|---|---|---|\n" +
		"| usb-probe | USB | Probe | P1a | s | p | input | gated | not verified | false | [not verified](#not-verified) | 09 | - |\n"
	if err := os.WriteFile(ledger, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run([]string{"apply", "-ledger", ledger, logPath}, &out); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(ledger)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "hardware-verified") {
		t.Error("ledger not advanced")
	}
	out.Reset()
	if err := run([]string{"report", logPath}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Date: 2026-11-20") || !strings.Contains(out.String(), "pepsipu") {
		t.Errorf("report missing release evidence fields: %s", out.String())
	}
}
