package civswitch

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/thedavidweng/any2jb/session"
)

const (
	controlCenterReady   = "CONTROL_CENTER_READY "
	exportFramePrefix    = "SAVE_DUMP|"
	exportSource         = "save:/saves/Single/XSTRIG.Civ7Save"
	exportName           = "XSTRIG.Civ7Save"
	maxExportBytes       = 128 << 20
	controlCenterSession = 30 * time.Minute
)

// ControlCenter runs the pinned Control Center payload on a verified loader and
// stays connected while the user drives it with the controller. It is
// read-only: after that verified script the host sends nothing, so the input
// monitor and the inventories cannot write to the console. The console's own
// save export is the only data kept, in retained storage.
func (a *Adapter) ControlCenter(ctx context.Context, address string, observe session.CivSwitchObserver) error {
	exports, err := a.retainedDirectory("save-exports")
	if err != nil {
		return err
	}
	endpoint, err := a.dependentLoader(ctx, address, controlCenterComponent)
	if err != nil {
		return err
	}
	script, err := a.options.Manifest.Read(a.sourceRoot(), controlCenterComponent)
	if err != nil {
		return err
	}
	receiver := &exportReceiver{exports: exports, address: address, observe: observe}
	ready := false
	_, err = loaderCall{deadline: controlCenterSession, limit: 3 * maxExportBytes, line: func(line string) error {
		switch {
		case strings.HasPrefix(line, controlCenterReady):
			ready = true
			observe(session.CivSwitchObservation{Ready: true})
		case strings.HasPrefix(line, exportFramePrefix):
			return receiver.accept(line)
		}
		// Other console text is observational only. It is never displayed,
		// executed, or sent back to the loader.
		return nil
	}}.send(ctx, endpoint, string(script))
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	if !ready {
		return errors.New("the CivSwitch Control Center did not report ready")
	}
	return receiver.pending()
}

type exportFrame struct {
	Path     string `json:"path"`
	Size     int    `json:"size"`
	Chunks   int    `json:"expected_chunks"`
	Chunk    int    `json:"chunk_size"`
	Complete bool   `json:"complete"`
	Done     int    `json:"chunks"`
	Bytes    int    `json:"bytes"`
}

// exportReceiver validates the console's read-only save export. An incomplete
// or altered transfer is refused instead of being kept as a backup.
type exportReceiver struct {
	exports, address string
	observe          session.CivSwitchObserver
	size, chunkSize  int
	expected, chunks int
	data             []byte
	active, complete bool
}

func (r *exportReceiver) accept(line string) error {
	fields := strings.SplitN(line, "|", 5)
	if len(fields) < 3 {
		return errors.New("malformed CivSwitch save export frame")
	}
	switch fields[1] {
	case "BEGIN":
		return r.begin(fields[2])
	case "CHUNK":
		return r.chunk(fields)
	case "END":
		return r.end(fields[2])
	case "ERROR":
		return errors.New("the console reported that the save export failed")
	}
	return errors.New("unknown CivSwitch save export frame")
}

func (r *exportReceiver) decode(body string) (exportFrame, error) {
	var frame exportFrame
	if err := json.Unmarshal([]byte(body), &frame); err != nil {
		return frame, errors.New("invalid CivSwitch save export frame")
	}
	return frame, nil
}

func (r *exportReceiver) begin(body string) error {
	frame, err := r.decode(body)
	if err != nil {
		return err
	}
	if r.active || frame.Path != exportSource || frame.Size <= 0 || frame.Size > maxExportBytes ||
		frame.Chunk <= 0 || frame.Chunk > 64<<10 || frame.Chunks != (frame.Size+frame.Chunk-1)/frame.Chunk {
		return errors.New("invalid CivSwitch save export inventory")
	}
	r.size, r.chunkSize, r.expected = frame.Size, frame.Chunk, frame.Chunks
	r.data, r.chunks, r.active = make([]byte, frame.Size), 0, true
	return nil
}

func (r *exportReceiver) chunk(fields []string) error {
	if !r.active || len(fields) != 5 {
		return errors.New("unexpected CivSwitch save export chunk")
	}
	offset, offsetErr := strconv.ParseInt(fields[2], 16, 64)
	length, lengthErr := strconv.Atoi(fields[3])
	data, decodeErr := hex.DecodeString(fields[4])
	if offsetErr != nil || lengthErr != nil || decodeErr != nil || len(data) != length ||
		int(offset) != r.chunks*r.chunkSize || int(offset)+length > r.size {
		return errors.New("invalid CivSwitch save export chunk")
	}
	copy(r.data[offset:], data)
	r.chunks++
	return nil
}

func (r *exportReceiver) end(body string) error {
	frame, err := r.decode(body)
	if err != nil {
		return err
	}
	if !r.active || r.complete || !frame.Complete || frame.Done != r.expected ||
		r.chunks != r.expected || frame.Bytes != r.size || string(r.data[:4]) != "CIV7" {
		return errors.New("the CivSwitch save export did not complete and verify")
	}
	digest := hex.EncodeToString(sha256Sum(r.data))
	dir := filepath.Join(r.exports, time.Now().UTC().Format("2006-01-02T150405Z")+"-"+digest[:8])
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, exportName), r.data, 0o600); err != nil {
		return err
	}
	record, err := json.MarshalIndent(map[string]any{"capturedAt": time.Now().UTC().Format(time.RFC3339),
		"consoleAddress": r.address, "source": exportSource, "bytes": r.size, "chunks": r.chunks,
		"chunkSize": r.chunkSize, "sha256": digest, "readOnly": true}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), append(record, '\n'), 0o600); err != nil {
		return err
	}
	r.active, r.complete, r.data = false, true, nil
	r.observe(session.CivSwitchObservation{ExportPath: dir})
	return nil
}

func (r *exportReceiver) pending() error {
	if r.active {
		return errors.New("the console save export ended before it was complete")
	}
	return nil
}
