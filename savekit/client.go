package savekit

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
)

// Client drives the SaveKit host module. Go owns policy and verification; the
// module only joins the pinned upstream tool scripts to JSON lines. A result
// must echo the requested action exactly: protocol drift, including a host
// claiming an action it was not asked for, is refused.
type Client struct {
	Node, Module, StagedRoot string
	// Scenario selects the structured fixture's behavior. It is a test seam and
	// stays empty in production.
	Scenario string
}

type hostEvent struct {
	Type      string   `json:"type"`
	Sequence  uint64   `json:"sequence"`
	Action    string   `json:"action,omitempty"`
	Message   string   `json:"message,omitempty"`
	Users     []string `json:"users,omitempty"`
	Titles    []string `json:"titles,omitempty"`
	Inventory []Entry  `json:"inventory,omitempty"`
	Output    string   `json:"output,omitempty"`
	Path      string   `json:"path,omitempty"`
	Files     int      `json:"files,omitempty"`
	Replaced  int      `json:"replaced,omitempty"`
}

var errProtocol = errors.New("invalid SaveKit host protocol")

// Run executes one operation against the held run directory. For a backup,
// destination is the empty export folder this app chose; the result must land
// exactly there and pass export verification. For a mutation, destination is
// the verified backup the host re-verifies before any console change.
func (c Client) Run(ctx context.Context, runDir, destination string, request Request) (result Result, err error) {
	if err := request.Validate(); err != nil {
		return Result{}, err
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		return Result{}, err
	}
	dest := "-"
	if destination != "" {
		dest = destination
	}
	args := []string{c.Module, runDir, dest}
	if c.StagedRoot != "" {
		args = []string{c.Module, c.StagedRoot, runDir, dest}
	}
	if c.Scenario != "" {
		args = append(args, c.Scenario)
	}
	cmd := exec.CommandContext(ctx, c.Node, args...)
	cmd.Stderr = io.Discard // raw tool stderr can contain console details
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return Result{}, err
	}
	defer stdin.Close()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Result{}, err
	}
	defer stdout.Close()
	if err := cmd.Start(); err != nil {
		return Result{}, err
	}
	defer func() {
		if err != nil {
			cmd.Process.Kill()
		}
		if wait := cmd.Wait(); err == nil && wait != nil {
			err = fmt.Errorf("the SaveKit host failed: %w", wait)
		}
	}()
	if _, err := stdin.Write(append(encoded, '\n')); err != nil {
		return Result{}, err
	}
	if err := stdin.Close(); err != nil {
		return Result{}, err
	}
	return c.observe(stdout, request, destination)
}

func (c Client) observe(output io.Reader, request Request, destination string) (Result, error) {
	scanner := bufio.NewScanner(output)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	var sequence uint64
	finished := false
	for scanner.Scan() {
		var event hostEvent
		decoder := json.NewDecoder(bytes.NewReader(scanner.Bytes()))
		decoder.DisallowUnknownFields()
		err := decoder.Decode(&event)
		if err == nil && decoder.Decode(new(any)) != io.EOF {
			err = errProtocol
		}
		if err != nil || event.Sequence != sequence+1 {
			return Result{}, errProtocol
		}
		sequence = event.Sequence
		switch event.Type {
		case "started", "progress":
			if finished || len(event.Message) > 512 {
				return Result{}, errProtocol
			}
		case "failed":
			if finished {
				return Result{}, errProtocol
			}
			return Result{}, errors.New("the SaveKit operation failed; see redacted diagnostics")
		case "result":
			if finished {
				return Result{}, errProtocol
			}
			finished = true
			return checkedResult(event, request, destination)
		default:
			return Result{}, errProtocol
		}
	}
	if err := scanner.Err(); err != nil {
		return Result{}, err
	}
	return Result{}, errors.New("the SaveKit host ended without a result")
}

func checkedResult(event hostEvent, request Request, destination string) (Result, error) {
	if event.Action != request.Action {
		// A host reporting any action but the requested one is protocol drift.
		return Result{}, errProtocol
	}
	result := Result{Action: event.Action}
	switch event.Action {
	case "accounts":
		if len(event.Users) == 0 || len(event.Users) > 8 {
			return Result{}, errProtocol
		}
		for _, user := range event.Users {
			if !uidPattern.MatchString(user) {
				return Result{}, errProtocol
			}
		}
		result.Users = event.Users
	case "titles":
		if len(event.Titles) == 0 || len(event.Titles) > 2048 {
			return Result{}, errProtocol
		}
		for _, title := range event.Titles {
			if !titlePattern.MatchString(title) {
				return Result{}, errProtocol
			}
		}
		result.Titles = event.Titles
	case "inventory":
		if len(event.Inventory) > 32768 {
			return Result{}, errProtocol
		}
		for _, entry := range event.Inventory {
			if !titlePattern.MatchString(entry.TitleID) || !uidPattern.MatchString(entry.UID) ||
				entry.Space > 255 || entry.Type > 7 || entry.Rank > 1 || entry.Index > 65535 {
				return Result{}, errProtocol
			}
		}
		result.Inventory = event.Inventory
	case "inspect", "filetime":
		if len(event.Output) == 0 || len(event.Output) > 1<<20 {
			return Result{}, errProtocol
		}
		result.Output = event.Output
	case "backup":
		if destination == "" || event.Path != destination {
			return Result{}, errProtocol
		}
		if err := VerifyExport(destination); err != nil {
			return Result{}, err
		}
		result.Path = event.Path
	case "write", "restore", "delete":
		// The host reports the pinned tool's own post-commit verification;
		// Go still reads the save back before calling the operation verified.
		if destination == "" || event.Files < 1 || event.Files > 2048 ||
			event.Replaced < 0 || event.Replaced > event.Files ||
			(request.Action == "delete" && (event.Files != 1 || event.Replaced != 0)) {
			return Result{}, errProtocol
		}
		result.Files = event.Files
		result.Replaced = event.Replaced
	default:
		return Result{}, errProtocol
	}
	return result, nil
}
