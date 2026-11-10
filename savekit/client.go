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

// Client drives the read-only SaveKit host module. Go owns policy and
// verification; the module only joins the pinned upstream tool scripts to JSON
// lines. There is no write path: Request cannot express one and any host event
// claiming a mutation action is refused.
type Client struct {
	Node, Module string
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
}

var errProtocol = errors.New("invalid read-only SaveKit host protocol")

// Run executes one read-only operation against the held run directory. For a
// backup, destination is the empty export folder this app chose; the result
// must land exactly there and pass export verification.
func (c Client) Run(ctx context.Context, runDir, destination string, request Request) (Result, error) {
	if err := request.Validate(); err != nil {
		return Result{}, err
	}
	args := []string{c.Module, runDir, "-"}
	if destination != "" {
		args[2] = destination
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
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Result{}, err
	}
	if err := cmd.Start(); err != nil {
		return Result{}, err
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		return Result{}, err
	}
	if _, err := stdin.Write(append(encoded, '\n')); err != nil {
		return Result{}, err
	}
	if err := stdin.Close(); err != nil {
		return Result{}, err
	}
	result, err := c.observe(stdout, request, destination)
	wait := cmd.Wait()
	if err != nil {
		return Result{}, err
	}
	if wait != nil {
		return Result{}, fmt.Errorf("the read-only SaveKit host failed: %w", wait)
	}
	return result, nil
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
			return Result{}, errors.New("the read-only SaveKit operation failed; see redacted diagnostics")
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
	return Result{}, errors.New("the read-only SaveKit host ended without a result")
}

func checkedResult(event hostEvent, request Request, destination string) (Result, error) {
	if event.Action != request.Action {
		// A host reporting any action but the requested read is protocol drift;
		// this is also what refuses a host attempting a mutation.
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
	default:
		return Result{}, errProtocol
	}
	return result, nil
}
