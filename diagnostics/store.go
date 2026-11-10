// Package diagnostics keeps shareable evidence separate from private run artifacts.
package diagnostics

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"

	"github.com/thedavidweng/any2jb/manifest"
	"github.com/thedavidweng/any2jb/session"
)

type Store struct{ Support string }
type Logs struct{ Host, USB, Pipeline, Acquisition string }

var sensitive = regexp.MustCompile(`(?i)token|pairing|secret|password|authorization|cookie|account|user.?id|private.?key|access.?key|api.?key|credential|save.?data|\bkey\b|START\s*\(`)
var identifiers = regexp.MustCompile(`(?i)\b(?:0x)?[0-9a-f]{16,}\b|\b[0-9]{8,}\b|(?:/Users/|/home/)[^\s"'<>]+`)

// Redact omits unknown JSON fields, whole sensitive lines and opaque identifiers.
// Raw binary/private save artifacts are never read.
func Redact(raw string) string {
	var out []string
	for _, line := range strings.Split(raw, "\n") {
		var object map[string]json.RawMessage
		if json.Unmarshal([]byte(line), &object) == nil && object != nil {
			safe := map[string]any{}
			for _, key := range []string{"error", "message", "stage", "status", "verified", "restored", "cleanedUp"} {
				if value, ok := object[key]; ok {
					var v any
					if json.Unmarshal(value, &v) == nil {
						switch x := v.(type) {
						case string:
							if !sensitive.MatchString(x) {
								safe[key] = identifiers.ReplaceAllString(x, "[redacted]")
							}
						case bool:
							safe[key] = x
						}
					}
				}
			}
			data, _ := json.Marshal(safe)
			out = append(out, string(data))
		} else if sensitive.MatchString(line) || strings.HasPrefix(strings.TrimSpace(line), `"`) || strings.HasPrefix(strings.TrimSpace(line), "{") || strings.HasPrefix(strings.TrimSpace(line), "[") {
			out = append(out, "[sensitive line omitted]")
		} else {
			out = append(out, identifiers.ReplaceAllString(line, "[redacted]"))
		}
	}
	return strings.Join(out, "\n")
}

var sources = []struct{ name, path string }{
	{"host", "host.log"}, {"usb", "usb.log"},
	{"pipeline", "run.log"}, {"acquisition", "switch2/acquisition.log"},
}

func validID(id string) bool {
	return id != "" && id != "." && id != ".." && filepath.Base(id) == id && !strings.ContainsAny(id, `/\`)
}

// Capture reads only known log files beneath an os.Root, never arbitrary artifacts.
// Sanitized copies outlive the private workspace's automatic teardown.
func (s Store) Capture(workspace, id string) error {
	if !validID(id) {
		return errors.New("invalid diagnostic run identity")
	}
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return err
	}
	defer root.Close()
	dest := filepath.Join(s.Support, "diagnostics", id)
	if err := os.MkdirAll(dest, 0700); err != nil {
		return err
	}
	for _, source := range sources {
		info, err := root.Lstat(source.path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errors.New("diagnostic source is not a regular file")
		}
		f, err := root.Open(source.path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		info, err = f.Stat()
		if err != nil {
			f.Close()
			return err
		}
		if !info.Mode().IsRegular() {
			f.Close()
			return errors.New("diagnostic source is not a regular file")
		}
		const limit = 256 * 1024
		if info.Size() > limit {
			if _, err := f.Seek(-limit, io.SeekEnd); err != nil {
				f.Close()
				return err
			}
		}
		raw, err := io.ReadAll(io.LimitReader(f, limit))
		f.Close()
		if err != nil {
			return err
		}
		if info.Size() > limit {
			if i := strings.IndexByte(string(raw), '\n'); i >= 0 {
				raw = raw[i+1:]
			} else {
				raw = nil
			}
		}
		if err := replaceLog(dest, source.name+".log", Redact(string(raw))); err != nil {
			return err
		}
	}
	return nil
}

func (s Store) Read(id string) (Logs, error) {
	if !validID(id) {
		return Logs{}, nil
	}
	var logs Logs
	for i, target := range []*string{&logs.Host, &logs.USB, &logs.Pipeline, &logs.Acquisition} {
		data, err := os.ReadFile(filepath.Join(s.Support, "diagnostics", id, sources[i].name+".log"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return Logs{}, err
		}
		*target = Redact(string(data))
	}
	return logs, nil
}

// Export uses an explicit schema, not a serialized snapshot or raw log archive.
// Free-form messages, paths, IDs, network addresses and unknown fields are excluded.
func (s Store) Export(ctx context.Context, snapshot session.Snapshot) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	logs, _, err := s.Refresh(snapshot.RunID)
	if err != nil {
		return "", err
	}
	m, err := manifest.Load()
	if err != nil {
		return "", err
	}
	host, revision := "", ""
	if p, err := m.Profile(snapshot.Profile.ID); err == nil {
		host, revision = p.ID, p.Revision
	}
	stages := []session.Stage{session.Idle, session.Inspecting, session.NeedsSetup, session.Ready, session.BootstrapSetup, session.PreparingHost, session.CivSwitchFetching, session.WaitingForInput, session.Inputting, session.WaitingForConsole, session.RunningExploit, session.Verifying, session.Verified, session.RetainedRPC, session.RunningTool, session.Finished, session.Failed, session.Indeterminate}
	if !slices.Contains(stages, snapshot.Stage) {
		snapshot.Stage = ""
	}
	if !slices.Contains([]session.Recovery{session.NoRecovery, session.CorrectSetup, session.RetrySafe, session.RestartConsole}, snapshot.Recovery) {
		snapshot.Recovery = ""
	}
	dir := filepath.Join(s.Support, "support-bundles")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(dir, "support-*.zip")
	if err != nil {
		return "", err
	}
	z := zip.NewWriter(f)
	entry, err := z.Create("diagnostics.json")
	if err == nil {
		err = json.NewEncoder(entry).Encode(struct {
			Schema                                 int              `json:"schema"`
			Stage                                  session.Stage    `json:"stage"`
			Recovery                               session.Recovery `json:"recovery"`
			Outcome                                session.Outcome  `json:"verification"`
			Current, Total                         int
			Platform, Architecture, Host, Revision string
			Sources                                map[string]logSummary
		}{1, snapshot.Stage, snapshot.Recovery, snapshot.Outcome, snapshot.Current, snapshot.Total,
			runtime.GOOS, runtime.GOARCH, host, revision, map[string]logSummary{
				"host": summarize(logs.Host), "usb": summarize(logs.USB), "pipeline": summarize(logs.Pipeline), "acquisition": summarize(logs.Acquisition),
			}})
	}
	err = errors.Join(err, z.Close(), f.Close())
	if err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

func replaceLog(dir, name, value string) error {
	f, err := os.CreateTemp(dir, ".log-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, writeErr := f.WriteString(value)
	if err := errors.Join(writeErr, f.Close()); err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(dir, name))
}

type logSummary struct {
	Lines        int
	Observations []string
}

// Fixed classifications preserve useful failure evidence without exporting text.
func summarize(log string) logSummary {
	summary := logSummary{}
	if strings.TrimSpace(log) == "" {
		return summary
	}
	summary.Lines = len(strings.Split(strings.TrimSpace(log), "\n"))
	for _, word := range []string{"failed", "error", "timeout", "closed", "connected", "restored", "verified"} {
		if strings.Contains(strings.ToLower(log), word) {
			summary.Observations = append(summary.Observations, word)
		}
	}
	return summary
}
