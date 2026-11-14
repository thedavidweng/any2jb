package devtools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// nspBoxScript is the staged portable adapter that builds forwarder packages.
const nspBoxScript = "integrations/nsp-box/cli.py"

var titleIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{16}$`)

// NSPBoxRequest is one forwarder package build. Source is the decrypted dump
// folder and Output the new .nsp file; neither is ever overwritten.
type NSPBoxRequest struct {
	Source         string
	Output         string
	DLC            bool
	Title          string
	DisplayVersion string
	Artwork        string
	TitleID        string
	BaseGame       string
}

// NSPBoxEvent is one NDJSON event from the NSP-Box adapter: progress, result,
// cancelled, error, or a self-test notice. Report holds the raw build report.
type NSPBoxEvent struct {
	Type    string
	Stage   string
	Percent float64
	Message string
	Report  json.RawMessage
}

// NSPBoxResult is the verified build report. Cancelled records a build that
// stopped at the caller's request; the adapter removes the partial package.
type NSPBoxResult struct {
	Report    json.RawMessage
	Cancelled bool
}

// BuildNSP builds one .nsp package with the staged NSP-Box adapter. A
// cancelled context sends SIGTERM, which the adapter turns into a clean
// cancellation, and the returned error is context.Canceled.
func (c Config) BuildNSP(ctx context.Context, req NSPBoxRequest, onEvent func(NSPBoxEvent)) (NSPBoxResult, error) {
	script, err := c.pluginFile("nsp-box", nspBoxScript)
	if err != nil {
		return NSPBoxResult{}, err
	}
	if err := req.validate(); err != nil {
		return NSPBoxResult{}, err
	}
	python, err := ResolvePython(c.HomebrewPython, "PIL")
	if err != nil {
		return NSPBoxResult{}, fmt.Errorf("nsp-box needs Python with Pillow: %w", err)
	}
	args := []string{script, "--source", req.Source, "--output", req.Output}
	if req.DLC {
		args = append(args, "--dlc")
		if req.TitleID != "" {
			args = append(args, "--title-id", req.TitleID)
		}
		if req.BaseGame != "" {
			args = append(args, "--base-game", req.BaseGame)
		}
	} else {
		if req.Title != "" {
			args = append(args, "--title", req.Title)
		}
		if req.DisplayVersion != "" {
			args = append(args, "--display-version", req.DisplayVersion)
		}
		if req.Artwork != "" {
			args = append(args, "--artwork", req.Artwork)
		}
	}
	cmd := exec.Command(python, args...)
	cmd.Dir = c.ToolRoot
	cmd.Env = PythonEnv(c.HomebrewPython, python)
	var result NSPBoxResult
	var reported error
	_, runErr := runSupervised(ctx, cmd, func(line string) {
		event, ok := parseNSPBoxEvent(line)
		if !ok {
			return
		}
		if onEvent != nil {
			onEvent(event)
		}
		switch event.Type {
		case "result":
			result.Report = event.Report
		case "cancelled":
			result.Cancelled = true
		case "error":
			reported = errors.New(event.Message)
		}
	})
	if result.Cancelled {
		return result, context.Canceled
	}
	if reported != nil {
		return result, reported
	}
	if runErr != nil {
		return result, runErr
	}
	if result.Report == nil {
		return result, errors.New("nsp-box did not report a verified package")
	}
	return result, nil
}

// validate mirrors the adapter's own input checks so a bad request fails
// before a package folder is touched.
func (r NSPBoxRequest) validate() error {
	if r.Source == "" || !filepath.IsAbs(r.Source) {
		return errors.New("choose a source folder")
	}
	if info, err := os.Lstat(r.Source); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("the source folder is missing or unsafe")
	}
	if r.Output == "" || !filepath.IsAbs(r.Output) {
		return errors.New("choose an .nsp destination")
	}
	if !strings.EqualFold(filepath.Ext(r.Output), ".nsp") {
		return errors.New("choose an .nsp output filename")
	}
	if _, err := os.Lstat(r.Output); !errors.Is(err, os.ErrNotExist) {
		return errors.New("the selected NSP already exists; choose another filename")
	}
	if info, err := os.Lstat(filepath.Dir(r.Output)); err != nil || !info.IsDir() {
		return errors.New("the destination folder is missing")
	}
	if r.DLC {
		if r.TitleID != "" && !titleIDPattern.MatchString(r.TitleID) {
			return errors.New("the DLC title ID must contain exactly 16 hexadecimal digits")
		}
		if r.BaseGame != "" {
			if !filepath.IsAbs(r.BaseGame) {
				return errors.New("choose a base game folder")
			}
			if info, err := os.Lstat(r.BaseGame); err != nil || !info.IsDir() {
				return errors.New("the base game folder is missing")
			}
		}
		return nil
	}
	if len(r.Title) > 200 || strings.ContainsAny(r.Title, "\x00\n\r") {
		return errors.New("enter a title name of at most 200 characters")
	}
	if len(r.DisplayVersion) > 64 || strings.ContainsAny(r.DisplayVersion, "\x00\n\r") {
		return errors.New("enter a display version of at most 64 characters")
	}
	if r.Artwork != "" {
		if !filepath.IsAbs(r.Artwork) {
			return errors.New("choose an artwork image")
		}
		if info, err := os.Lstat(r.Artwork); err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("the artwork image is missing or unsafe")
		}
	}
	return nil
}

// parseNSPBoxEvent decodes one NDJSON event. Adapter tracebacks and other
// non-JSON lines are ignored; the exit status covers them.
func parseNSPBoxEvent(line string) (NSPBoxEvent, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "{") {
		return NSPBoxEvent{}, false
	}
	var raw struct {
		Type    string          `json:"type"`
		Stage   string          `json:"stage"`
		Percent float64         `json:"percent"`
		Message string          `json:"message"`
		Report  json.RawMessage `json:"report"`
	}
	if err := json.Unmarshal([]byte(line), &raw); err != nil || raw.Type == "" {
		return NSPBoxEvent{}, false
	}
	return NSPBoxEvent{Type: raw.Type, Stage: raw.Stage, Percent: min(max(raw.Percent, 0), 100),
		Message: raw.Message, Report: raw.Report}, true
}
