// Package hwevidence defines the hardware acceptance evidence log: the
// machine-readable record a human tester files after running the staged
// protocol in docs/hardware-acceptance.md, the mapping from protocol steps to
// parity ledger rows, and the rules that keep hardware claims traceable.
package hwevidence

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/thedavidweng/any2jb/manifest"
)

const Schema = "any2jb/hardware-evidence@1"

const (
	RouteP1A       = "p1a"
	RouteCivSwitch = "civswitch"
)

const (
	TransportUSBHID       = "usb-hid"
	TransportGuidedManual = "guided-manual"
	TransportCompositeHID = "composite-hid"
)

const (
	OutcomePass    = "pass"
	OutcomeFail    = "fail"
	OutcomeBlocked = "blocked"
)

// StepDef is one staged protocol step. Optional steps may be omitted from a
// log; required steps must all be recorded. Rows returns the parity ledger
// row IDs a passing step verifies for the given combination.
type StepDef struct {
	ID       string
	Title    string
	Optional bool
	Rows     func(Combination) []string
}

// Step is one recorded protocol result.
type Step struct {
	ID      string `json:"id"`
	Outcome string `json:"outcome"`
	Notes   string `json:"notes,omitempty"`
	Log     string `json:"log"`
}

// Combination is the exact hardware/software combination under test.
type Combination struct {
	MacModel          string `json:"macModel"`
	MacOSVersion      string `json:"macosVersion"`
	StartupSecurity   string `json:"startupSecurity,omitempty"`
	HostProfile       string `json:"hostProfile,omitempty"`
	ConsoleGeneration string `json:"consoleGeneration,omitempty"`
	ConsoleSystem     string `json:"consoleSystemVersion"`
	GameEdition       string `json:"gameEdition,omitempty"`
	GameVersion       string `json:"gameVersion"`
	BootstrapSHA256   string `json:"bootstrapSHA256,omitempty"`
	SystemLanguage    string `json:"systemLanguage,omitempty"`
	Transport         string `json:"transport"`
	USBController     string `json:"usbController,omitempty"`
	Cable             string `json:"cable"`
	LANTopology       string `json:"lanTopology"`
}

// Log is one hardware acceptance evidence log.
type Log struct {
	Schema      string      `json:"schema"`
	Date        string      `json:"date"`
	Tester      string      `json:"tester"`
	Route       string      `json:"route"`
	Combination Combination `json:"combination"`
	Steps       []Step      `json:"steps"`

	Name string `json:"-"` // file base name, set by Load
}

func fixedRows(ids ...string) func(Combination) []string {
	return func(Combination) []string { return ids }
}

// p1aSteps is the staged P1a sequence for the Apple Silicon USB HID
// transport. The guided manual transport records only its last four steps.
var p1aSteps = []StepDef{
	{ID: "helper-dry-run", Title: "C helper --dry-run / --validate on the target Mac", Rows: fixedRows()},
	{ID: "usb-enumeration", Title: "USB keyboard enumeration", Rows: fixedRows("usb-role", "usb-probe")},
	{ID: "harmless-typing", Title: "Typing into a harmless target", Rows: fixedRows("keyboard-text")},
	{ID: "stop-key-release", Title: "Stop key-release check", Rows: fixedRows("keyboard-stop", "run-cancel", "run-stop")},
	{ID: "console-bootstrap-save", Title: "Console bootstrap save", Rows: bootstrapSaveRows},
	{ID: "host-connection", Title: "Host connection", Rows: hostConnectionRows},
	{ID: "verification-4of4", Title: "Structured 4/4 verification", Rows: fixedRows("run-progress")},
	{ID: "retained-rpc-readonly", Title: "Retained-RPC read-only check and Finish", Rows: fixedRows("host-hold", "rpc-check", "rpc-ownership", "system-dump", "run-finish")},
}

// usbHIDOnly is the number of leading p1a steps specific to the USB HID
// transport; guided manual starts at console-bootstrap-save.
const usbHIDOnly = 4

var civSteps = []StepDef{
	{ID: "civ-prerequisites", Title: "Game, cartridge, language and LAN prerequisites", Rows: fixedRows("civ-prerequisites")},
	{ID: "civ-composite-enumeration", Title: "Composite keyboard/gamepad HID enumeration", Rows: fixedRows()},
	{ID: "civ-chat-transfer", Title: "Chat transfer", Rows: fixedRows("civ-transfer")},
	{ID: "civ-stop-release", Title: "Stop releases both HID interfaces", Rows: fixedRows("civ-stop")},
	{ID: "civ-loader-verify", Title: "Loader verification on TCP 9026", Rows: fixedRows("civ-probe", "civ-attach")},
	{ID: "civ-save-backup", Title: "Verified save backup", Rows: fixedRows("civ-original")},
	{ID: "civ-trigger-install", Title: "Persistent boot trigger install", Optional: true, Rows: fixedRows("civ-trigger")},
	{ID: "civ-control-center", Title: "Control Center read and save export", Optional: true, Rows: fixedRows("civ-control-start", "civ-control-export", "civ-control-stop")},
	{ID: "civ-javascript", Title: "Explicit JavaScript send", Optional: true, Rows: fixedRows("civ-js")},
}

func bootstrapSaveRows(c Combination) []string {
	rows := []string{"bootstrap-open", "bootstrap-confirm", "bootstrap-reuse"}
	if c.GameEdition == "demo" {
		rows = append(rows, "bootstrap-save-demo")
	} else {
		rows = append(rows, "bootstrap-save-full")
	}
	if c.Transport == TransportUSBHID {
		rows = append(rows, "bootstrap-type", "keyboard-indent")
	}
	return rows
}

func hostConnectionRows(c Combination) []string {
	rows := []string{"host-only", "command-load", "command-run", "command-remove", "command-start"}
	if c.Transport == TransportGuidedManual {
		rows = append(rows, "manual-launch")
	} else {
		rows = append(rows, "start-jailbreak")
	}
	return rows
}

// Sequence returns the ordered protocol steps for a route and transport.
func Sequence(route, transport string) ([]StepDef, error) {
	switch route {
	case RouteP1A:
		switch transport {
		case TransportUSBHID:
			return p1aSteps, nil
		case TransportGuidedManual:
			return p1aSteps[usbHIDOnly:], nil
		}
	case RouteCivSwitch:
		if transport == TransportCompositeHID {
			return civSteps, nil
		}
	}
	return nil, fmt.Errorf("no protocol sequence for route %q with transport %q", route, transport)
}

// Load reads and validates one evidence log file.
func Load(path string) (*Log, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var log Log
	if err := json.Unmarshal(data, &log); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	log.Name = filepath.Base(path)
	if err := log.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	dir := filepath.Dir(path)
	for _, step := range log.Steps {
		if strings.HasSuffix(step.Log, ".log") || strings.HasSuffix(step.Log, ".txt") {
			if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(step.Log))); err != nil {
				return nil, fmt.Errorf("%s: step %s: redacted log file: %w", path, step.ID, err)
			}
		}
	}
	return &log, nil
}

// LoadDir loads every evidence log in a directory, excluding TEMPLATE.json.
func LoadDir(dir string) ([]*Log, error) {
	names, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	var logs []*Log
	for _, name := range names {
		if strings.HasPrefix(filepath.Base(name), "TEMPLATE") {
			continue
		}
		log, err := Load(name)
		if err != nil {
			return nil, err
		}
		logs = append(logs, log)
	}
	sort.Slice(logs, func(i, j int) bool { return logs[i].Name < logs[j].Name })
	return logs, nil
}

var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// pairingPattern catches unredacted START pairing commands, which must never
// be pasted into evidence (docs/guided-manual.md).
var pairingPattern = regexp.MustCompile(`START\(`)

// Validate checks a log against the protocol rules.
func (l *Log) Validate() error {
	if l.Schema != Schema {
		return fmt.Errorf("schema must be %q", Schema)
	}
	if _, err := time.Parse("2006-01-02", l.Date); err != nil {
		return fmt.Errorf("date must be ISO YYYY-MM-DD: %q", l.Date)
	}
	if strings.TrimSpace(l.Tester) == "" {
		return fmt.Errorf("tester is required")
	}
	seq, err := Sequence(l.Route, l.Combination.Transport)
	if err != nil {
		return err
	}
	if err := l.validateCombination(); err != nil {
		return err
	}
	return l.validateSteps(seq)
}

func (l *Log) validateCombination() error {
	c := l.Combination
	required := map[string]string{
		"macModel":             c.MacModel,
		"macosVersion":         c.MacOSVersion,
		"consoleSystemVersion": c.ConsoleSystem,
		"gameVersion":          c.GameVersion,
		"cable":                c.Cable,
		"lanTopology":          c.LANTopology,
	}
	for field, value := range required {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("combination.%s is required", field)
		}
	}
	hid := c.Transport == TransportUSBHID || c.Transport == TransportCompositeHID
	if hid {
		if strings.TrimSpace(c.StartupSecurity) == "" {
			return fmt.Errorf("combination.startupSecurity is required for driver-based transports")
		}
		if strings.TrimSpace(c.USBController) == "" {
			return fmt.Errorf("combination.usbController is required for driver-based transports")
		}
	}
	switch l.Route {
	case RouteP1A:
		m, err := manifest.Load()
		if err != nil {
			return err
		}
		p, err := m.Profile(c.HostProfile)
		if err != nil {
			return fmt.Errorf("combination.hostProfile: %w", err)
		}
		supported := false
		for _, edition := range p.SupportedSmileBASIC {
			if edition == c.GameVersion+" "+c.GameEdition {
				supported = true
			}
		}
		if !supported {
			return fmt.Errorf("profile %s does not support SmileBASIC %s %s", c.HostProfile, c.GameVersion, c.GameEdition)
		}
		if !sha256Pattern.MatchString(c.BootstrapSHA256) {
			return fmt.Errorf("combination.bootstrapSHA256 must be a lowercase SHA-256 hex digest")
		}
		if c.BootstrapSHA256 != p.BootstrapSHA256 {
			return fmt.Errorf("combination.bootstrapSHA256 does not match the pinned %s bootstrap", c.HostProfile)
		}
	case RouteCivSwitch:
		if strings.TrimSpace(c.ConsoleGeneration) == "" {
			return fmt.Errorf("combination.consoleGeneration is required (expected Switch 1)")
		}
		if strings.TrimSpace(c.SystemLanguage) == "" {
			return fmt.Errorf("combination.systemLanguage is required (expected English (US))")
		}
	}
	return nil
}

func (l *Log) validateSteps(seq []StepDef) error {
	byID := map[string]int{}
	for i, def := range seq {
		byID[def.ID] = i
	}
	seen := map[string]bool{}
	position := -1
	failed := false
	for _, step := range l.Steps {
		index, ok := byID[step.ID]
		if !ok {
			return fmt.Errorf("step %q is not part of the %s/%s protocol", step.ID, l.Route, l.Combination.Transport)
		}
		def := seq[index]
		if seen[step.ID] {
			return fmt.Errorf("step %q recorded twice", step.ID)
		}
		seen[step.ID] = true
		if index < position {
			return fmt.Errorf("step %q is out of protocol order", step.ID)
		}
		position = index
		switch step.Outcome {
		case OutcomePass:
			if failed && !def.Optional {
				return fmt.Errorf("step %q cannot pass after an earlier required step did not", step.ID)
			}
		case OutcomeFail, OutcomeBlocked:
			if strings.TrimSpace(step.Notes) == "" {
				return fmt.Errorf("step %q needs notes explaining the %s outcome", step.ID, step.Outcome)
			}
			if !def.Optional {
				failed = true
			}
		default:
			return fmt.Errorf("step %q has invalid outcome %q", step.ID, step.Outcome)
		}
		if strings.TrimSpace(step.Log) == "" {
			return fmt.Errorf("step %q needs a redacted log reference", step.ID)
		}
		for _, text := range []string{step.Notes, step.Log} {
			if pairingPattern.MatchString(text) {
				return fmt.Errorf("step %q pastes a START pairing command; redact it", step.ID)
			}
		}
	}
	for _, def := range seq {
		if !def.Optional && !seen[def.ID] {
			return fmt.Errorf("required step %q is not recorded", def.ID)
		}
	}
	return nil
}

// VerifiedRows maps each parity ledger row verified by a passing step to the
// names of the logs that verify it. Failed or blocked steps verify nothing.
func VerifiedRows(logs []*Log) (map[string][]string, error) {
	verified := map[string][]string{}
	for _, log := range logs {
		seq, err := Sequence(log.Route, log.Combination.Transport)
		if err != nil {
			return nil, err
		}
		byID := map[string]StepDef{}
		for _, def := range seq {
			byID[def.ID] = def
		}
		for _, step := range log.Steps {
			if step.Outcome != OutcomePass {
				continue
			}
			for _, row := range byID[step.ID].Rows(log.Combination) {
				verified[row] = append(verified[row], log.Name)
			}
		}
	}
	return verified, nil
}
