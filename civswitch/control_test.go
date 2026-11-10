package civswitch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/thedavidweng/any2jb/diagnostics"
	"github.com/thedavidweng/any2jb/session"
)

// consoleExport scripts the Control Center frames of the pinned upstream
// payload: a ready line, then the console-driven read-only save export.
func consoleExport(save string, alter func(frames []string) []string) func(string) string {
	return func(string) string {
		const chunk = 1024
		frames := []string{`CONTROL_CENTER_READY {"page":"home"}`}
		expected := (len(save) + chunk - 1) / chunk
		begin, _ := json.Marshal(map[string]any{"path": exportSource, "size": len(save),
			"expected_chunks": expected, "chunk_size": chunk, "policy": "read-only"})
		frames = append(frames, exportFramePrefix+"BEGIN|"+string(begin))
		for offset := 0; offset < len(save); offset += chunk {
			end := min(offset+chunk, len(save))
			frames = append(frames, fmt.Sprintf("%sCHUNK|%016x|%d|%s", exportFramePrefix, offset,
				end-offset, hex.EncodeToString([]byte(save[offset:end]))))
		}
		done, _ := json.Marshal(map[string]any{"complete": true, "chunks": expected, "bytes": len(save)})
		frames = append(frames, exportFramePrefix+"END|"+string(done))
		if alter != nil {
			frames = alter(frames)
		}
		return strings.Join(frames, "\n") + "\n"
	}
}

func observations(t *testing.T) (session.CivSwitchObserver, func() []session.CivSwitchObservation) {
	t.Helper()
	var mu sync.Mutex
	var seen []session.CivSwitchObservation
	return func(o session.CivSwitchObservation) {
			mu.Lock()
			defer mu.Unlock()
			seen = append(seen, o)
		}, func() []session.CivSwitchObservation {
			mu.Lock()
			defer mu.Unlock()
			return append([]session.CivSwitchObservation(nil), seen...)
		}
}

func TestControlCenterExportIsReadOnlyAndRetainedOutsideSessionWorkspaces(t *testing.T) {
	save := "CIV7" + strings.Repeat("save bytes ", 400)
	f := newPersistenceFixture(t, consoleExport(save, nil))
	observe, seen := observations(t)
	if err := f.adapter.ControlCenter(context.Background(), "192.0.2.2", observe); err != nil {
		t.Fatal(err)
	}
	export := ""
	ready := false
	for _, o := range seen() {
		ready = ready || o.Ready
		if o.ExportPath != "" {
			export = o.ExportPath
		}
	}
	if !ready || export == "" {
		t.Fatalf("observations: %+v", seen())
	}
	// The host sends the verified Control Center payload and nothing else: the
	// input monitor and inventories cannot write to the console.
	if sent := f.sent(t); len(sent) != 1 || sent[0] != "fixture "+controlCenterComponent {
		t.Fatalf("host sent code other than the verified Control Center payload: %q", sent)
	}
	if !strings.HasPrefix(export, filepath.Join(f.support, "civswitch", "save-exports")) {
		t.Fatalf("export is inside a session workspace: %s", export)
	}
	data, err := os.ReadFile(filepath.Join(export, exportName))
	if err != nil || string(data) != save {
		t.Fatalf("exported save: %v", err)
	}
	var record struct {
		SHA256   string `json:"sha256"`
		ReadOnly bool   `json:"readOnly"`
		Bytes    int    `json:"bytes"`
	}
	metadata, err := os.ReadFile(filepath.Join(export, "metadata.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(metadata, &record); err != nil {
		t.Fatal(err)
	}
	if record.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(save))) || !record.ReadOnly || record.Bytes != len(save) {
		t.Fatalf("export metadata: %+v", record)
	}
	// Retained exports survive the session cleanup the user can run.
	store := diagnostics.Store{Support: f.support}
	plan, err := store.PreviewCleanup()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range plan {
		if strings.Contains(path, "civswitch") {
			t.Fatalf("cleanup offered to delete a retained export: %s", path)
		}
	}
	if err := store.Cleanup(plan); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(export, exportName)); err != nil {
		t.Fatalf("cleanup removed the retained export: %v", err)
	}
}

func TestControlCenterRefusesUnverifiedExports(t *testing.T) {
	save := "CIV7" + strings.Repeat("x", 2000)
	cases := map[string]func([]string) []string{
		"wrong magic": nil,
		"missing end": func(frames []string) []string { return frames[:len(frames)-1] },
		"short count": func(frames []string) []string {
			return append(frames[:len(frames)-2], frames[len(frames)-1])
		},
		"foreign path": func(frames []string) []string {
			frames[1] = exportFramePrefix + `BEGIN|{"path":"save:/saves/Other","size":4,"expected_chunks":1,"chunk_size":1024}`
			return frames
		},
		"console error": func(frames []string) []string {
			return append(frames[:2], exportFramePrefix+`ERROR|{"message":"read failed"}`)
		},
	}
	for name, alter := range cases {
		t.Run(name, func(t *testing.T) {
			respond := consoleExport(save, alter)
			if name == "wrong magic" {
				respond = consoleExport("JUNK"+strings.Repeat("x", 2000), nil)
			}
			f := newPersistenceFixture(t, respond)
			observe, seen := observations(t)
			if err := f.adapter.ControlCenter(context.Background(), "192.0.2.2", observe); err == nil {
				t.Fatal("accepted an unverified save export")
			}
			for _, o := range seen() {
				if o.ExportPath != "" {
					t.Fatalf("kept an unverified export: %s", o.ExportPath)
				}
			}
			exports, err := os.ReadDir(filepath.Join(f.support, "civswitch", "save-exports"))
			if err == nil && len(exports) != 0 {
				t.Fatalf("retained %d unverified exports", len(exports))
			}
		})
	}
}

func TestControlCenterNeedsReadyAndStopsOnCancellation(t *testing.T) {
	t.Run("no ready line", func(t *testing.T) {
		f := newPersistenceFixture(t, func(string) string { return "loader busy\n" })
		observe, _ := observations(t)
		if err := f.adapter.ControlCenter(context.Background(), "192.0.2.2", observe); err == nil {
			t.Fatal("reported a Control Center that never became ready")
		}
	})
	t.Run("deliberate stop", func(t *testing.T) {
		f := newPersistenceFixture(t, func(string) string { return `CONTROL_CENTER_READY {"page":"home"}` + "\n" })
		f.loader.linger = make(chan struct{})
		t.Cleanup(func() { close(f.loader.linger) })
		observe, seen := observations(t)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- f.adapter.ControlCenter(ctx, "192.0.2.2", observe) }()
		for start := time.Now(); len(seen()) == 0; {
			if time.Since(start) > 5*time.Second {
				t.Fatal("Control Center never reported ready")
			}
			time.Sleep(5 * time.Millisecond)
		}
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatalf("a deliberate stop reported %v", err)
		}
	})
}
