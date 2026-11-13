package procgroup

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"time"
)

// ErrProtocol indicates framing, unknown-field, trailing-token, or sequence
// drift on a supervised child's NDJSON stream.
var ErrProtocol = errors.New("invalid supervised child protocol")

// Start configures process-group ownership, discards raw untrusted stderr,
// opens stdin/stdout pipes, and starts cmd, closing pipes if startup fails.
func Start(cmd *exec.Cmd) (io.WriteCloser, io.ReadCloser, error) {
	if err := Configure(cmd); err != nil {
		return nil, nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	// Raw child stderr can contain pairing, native, or console addresses.
	cmd.Stderr = io.Discard
	stdin, err := cmd.StdinPipe()
	if err != nil {
		_ = stdout.Close()
		return nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		_ = stdout.Close()
		_ = stdin.Close()
		return nil, nil, err
	}
	return stdin, stdout, nil
}

// NDJSONReader reads newline-delimited JSON objects with strict unknown-field
// rejection, bounded line length, and monotonic 1-based sequence verification.
type NDJSONReader[T any] struct {
	scanner  *bufio.Scanner
	seqOf    func(T) uint64
	sequence uint64
}

// NewNDJSONReader constructs a bounded strict NDJSON reader. When maxLineBytes
// is <= 0, 64 KiB is used. Partial trailing lines without '\n' fail with ErrProtocol.
func NewNDJSONReader[T any](r io.Reader, maxLineBytes int, seqOf func(T) uint64) *NDJSONReader[T] {
	if maxLineBytes <= 0 {
		maxLineBytes = 65536
	}
	initBuf := 4096
	if initBuf > maxLineBytes {
		initBuf = maxLineBytes
	}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, initBuf), maxLineBytes)
	scanner.Split(func(data []byte, eof bool) (int, []byte, error) {
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			return i + 1, data[:i], nil
		}
		if eof && len(data) != 0 {
			return 0, nil, ErrProtocol
		}
		return 0, nil, nil
	})
	return &NDJSONReader[T]{scanner: scanner, seqOf: seqOf}
}

// Next decodes the next event, returning io.EOF when the stream ends cleanly
// or ErrProtocol when framing, schema, or sequence invariants are violated.
func (r *NDJSONReader[T]) Next() (T, error) {
	var zero T
	if !r.scanner.Scan() {
		if err := r.scanner.Err(); err != nil {
			return zero, ErrProtocol
		}
		return zero, io.EOF
	}
	var event T
	decoder := json.NewDecoder(bytes.NewReader(r.scanner.Bytes()))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&event); err != nil {
		return zero, ErrProtocol
	}
	if decoder.Decode(new(any)) != io.EOF {
		return zero, ErrProtocol
	}
	if r.seqOf != nil {
		seq := r.seqOf(event)
		if seq != r.sequence+1 {
			return zero, ErrProtocol
		}
		r.sequence = seq
	}
	return event, nil
}

// Stop terminates an owned process group cleanly within grace, escalating
// through SIGTERM (after grace when initialSIGTERM is false) and SIGKILL, and
// sweeps any remaining group members after the leader exits.
func Stop(ctx context.Context, cmd *exec.Cmd, done <-chan struct{}, initialSIGTERM bool, grace, killDelay time.Duration) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	if initialSIGTERM {
		if err := Signal(cmd, false); err != nil {
			return err
		}
	}
	timer := time.NewTimer(grace)
	defer timer.Stop()
	select {
	case <-done:
	case <-ctx.Done():
		if err := Signal(cmd, true); err != nil {
			return err
		}
		<-done
	case <-timer.C:
		if !initialSIGTERM && killDelay > 0 {
			if err := Signal(cmd, false); err != nil {
				return err
			}
			killTimer := time.NewTimer(killDelay)
			select {
			case <-done:
				killTimer.Stop()
			case <-ctx.Done():
				killTimer.Stop()
				if err := Signal(cmd, true); err != nil {
					return err
				}
				<-done
			case <-killTimer.C:
				if err := Signal(cmd, true); err != nil {
					return err
				}
				<-done
			}
		} else {
			if err := Signal(cmd, true); err != nil {
				return err
			}
			<-done
		}
	}
	return Signal(cmd, true)
}
