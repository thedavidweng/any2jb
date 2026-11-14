package devtools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	browserProbesScript       = "scripts/gui-bundled-browser-probes.cjs"
	browserProbesReadyTimeout = 20 * time.Second
)

// BrowserProbes lists the seven bundled probes in catalog order.
var BrowserProbes = []string{"runtime", "media", "network", "boundaries", "fonts", "webkit", "fontenv"}

// BrowserProbesRequest is one probe run. Port 0 asks the kernel for an
// ephemeral port; Source stays empty to use the verified bundled pages.
type BrowserProbesRequest struct {
	Address string
	Port    int
	Probe   string
	Source  string
}

// BrowserProbesEvent is one MAC2JB_BROWSER frame: ready, stopped or failed.
// Raw keeps the whole frame for fields a future helper adds.
type BrowserProbesEvent struct {
	Type         string
	URL          string
	ReportFolder string
	Probe        string
	Message      string
	Raw          json.RawMessage
}

// BrowserProbesSession is a running probe server owned by this process.
type BrowserProbesSession struct {
	proc     *supervised
	consumed chan struct{}
	ready    BrowserProbesEvent
}

// URL is the private probe page URL, including its access token.
func (s *BrowserProbesSession) URL() string { return s.ready.URL }

// ReportFolder is the private directory reports are written to.
func (s *BrowserProbesSession) ReportFolder() string { return s.ready.ReportFolder }

// Probe is the probe the page opens with.
func (s *BrowserProbesSession) Probe() string { return s.ready.Probe }

// Stop closes the probe server and waits for the helper to exit.
func (s *BrowserProbesSession) Stop(ctx context.Context) error {
	return s.proc.Stop(ctx)
}

// StartBrowserProbes serves the bundled probe page for one probe and returns
// once the helper reports ready.
func (c Config) StartBrowserProbes(ctx context.Context, req BrowserProbesRequest, onEvent func(BrowserProbesEvent)) (*BrowserProbesSession, error) {
	script, err := c.pluginFile("browser-probes", browserProbesScript)
	if err != nil {
		return nil, err
	}
	source, err := c.browserProbeSource(req.Source)
	if err != nil {
		return nil, err
	}
	if err := req.validate(); err != nil {
		return nil, err
	}
	reports, err := c.privateDir("browser-probe-reports")
	if err != nil {
		return nil, err
	}
	node, err := c.nodeBinary()
	if err != nil {
		return nil, err
	}
	port := req.Port
	cmd := exec.Command(node, script, "serve",
		"--address", req.Address,
		"--probe", req.Probe,
		"--reports", reports,
		"--source", source,
		"--port", strconv.Itoa(port))
	cmd.Dir = c.ToolRoot
	proc, err := startSupervised(cmd)
	if err != nil {
		return nil, err
	}
	session := &BrowserProbesSession{proc: proc, consumed: make(chan struct{})}
	ready := make(chan BrowserProbesEvent, 1)
	failed := make(chan error, 1)
	go func() {
		defer close(session.consumed)
		for line := range proc.lines {
			event, ok := parseBrowserProbesLine(line)
			if !ok {
				continue
			}
			switch event.Type {
			case "ready":
				select {
				case ready <- event:
				default:
				}
			case "failed", "error":
				select {
				case failed <- errors.New(event.Message):
				default:
				}
			}
			if onEvent != nil {
				onEvent(event)
			}
		}
	}()
	timeout := time.NewTimer(browserProbesReadyTimeout)
	defer timeout.Stop()
	select {
	case event := <-ready:
		session.ready = event
		return session, nil
	case failure := <-failed:
		return nil, c.abortBrowserProbes(session, failure)
	case <-proc.done:
		if proc.err == nil {
			return nil, c.abortBrowserProbes(session, errors.New("the browser probe server exited before reporting ready"))
		}
		return nil, c.abortBrowserProbes(session, proc.exitError())
	case <-ctx.Done():
		return nil, c.abortBrowserProbes(session, ctx.Err())
	case <-timeout.C:
		return nil, c.abortBrowserProbes(session, errors.New("the browser probe server did not report ready in time"))
	}
}

func (c Config) abortBrowserProbes(session *BrowserProbesSession, failure error) error {
	_ = session.proc.Stop(context.Background())
	<-session.consumed
	return failure
}

// browserProbeSource returns the verified bundled probe pages unless the
// caller pinned its own source folder.
func (c Config) browserProbeSource(override string) (string, error) {
	if override != "" {
		info, err := os.Lstat(override)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("the probe source must be a regular folder")
		}
		return override, nil
	}
	dir, err := c.pluginDir("browser-probes")
	if err != nil {
		return "", err
	}
	source := filepath.Join(dir, "integrations", "browser-probes")
	if info, err := os.Lstat(source); err != nil || !info.IsDir() {
		return "", &PluginMissingError{ID: "browser-probes", Cause: errors.New("the bundled probe pages are missing from the staged plugin")}
	}
	return source, nil
}

// validate mirrors the helper's option checks before a process is started.
func (r BrowserProbesRequest) validate() error {
	ip := net.ParseIP(r.Address)
	if ip == nil || ip.To4() == nil {
		return errors.New("choose a Mac IPv4 address")
	}
	if !slices.Contains(BrowserProbes, r.Probe) {
		return fmt.Errorf("choose one of the seven bundled browser probes: %s", strings.Join(BrowserProbes, ", "))
	}
	if r.Port < 0 || r.Port > 65535 {
		return errors.New("choose a port from 0 to 65535")
	}
	return nil
}

// parseBrowserProbesLine decodes one MAC2JB_BROWSER frame.
func parseBrowserProbesLine(line string) (BrowserProbesEvent, bool) {
	payload, ok := strings.CutPrefix(strings.TrimSpace(line), "MAC2JB_BROWSER ")
	if !ok {
		return BrowserProbesEvent{}, false
	}
	var raw struct {
		Type         string `json:"type"`
		URL          string `json:"url"`
		ReportFolder string `json:"reportFolder"`
		Probe        string `json:"probe"`
		Message      string `json:"message"`
	}
	if err := decodeJSON([]byte(payload), &raw); err != nil || raw.Type == "" {
		return BrowserProbesEvent{}, false
	}
	return BrowserProbesEvent{
		Type: raw.Type, URL: raw.URL, ReportFolder: raw.ReportFolder,
		Probe: raw.Probe, Message: raw.Message, Raw: json.RawMessage(payload),
	}, true
}
