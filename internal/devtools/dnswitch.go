package devtools

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultDNSwitchHostname is the captive-portal hostname the console resolves.
const DefaultDNSwitchHostname = "ctest.p01.ctest.srv.nintendo.net"

const (
	dnsWitchScript       = "scripts/gui-dnswitch.cjs"
	dnsWitchReadyTimeout = 20 * time.Second
)

var dnsHostnameLabel = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$`)

// DNSwitchRequest is one captive-portal run. The GUI supplies the console's
// DNS port 53 and HTTP port 80, which need administrator privileges; port 0
// asks the kernel for an ephemeral port, as tests do.
type DNSwitchRequest struct {
	Address  string
	Hostname string
	URL      string
	DNSPort  int
	HTTPPort int
}

// DNSwitchState is the state the helper records for `status` and the GUI.
type DNSwitchState struct {
	Phase             string   `json:"phase"`
	Address           string   `json:"address"`
	Hostname          string   `json:"hostname"`
	AcceptedHostnames []string `json:"acceptedHostnames"`
	DNSPort           int      `json:"dnsPort"`
	HTTPPort          int      `json:"httpPort"`
	ControlPort       int      `json:"controlPort"`
	PID               int      `json:"pid"`
	StartedAt         string   `json:"startedAt"`
	ExpiresAt         string   `json:"expiresAt"`
	Message           string   `json:"message"`
}

// DNSwitchEvent is one MAC2JB_DNSWITCH frame or the helper's ready state.
type DNSwitchEvent struct {
	Type    string
	At      time.Time
	State   DNSwitchState
	Name    string
	QType   int
	RCode   int
	Method  string
	Status  int
	Message string
}

// DNSwitchSession is a running captive-portal helper owned by this process.
type DNSwitchSession struct {
	proc     *supervised
	consumed chan struct{}
	mu       sync.Mutex
	state    DNSwitchState
}

// State returns the last state the helper reported.
func (s *DNSwitchSession) State() DNSwitchState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// Stop closes the listeners and waits for the helper to exit.
func (s *DNSwitchSession) Stop(ctx context.Context) error {
	return s.proc.Stop(ctx)
}

// StartDNSwitch starts the captive-portal DNS and HTTP listeners and returns
// once the helper reports ready. Events keep flowing to onEvent until the
// helper exits.
func (c Config) StartDNSwitch(ctx context.Context, req DNSwitchRequest, onEvent func(DNSwitchEvent)) (*DNSwitchSession, error) {
	script, err := c.pluginFile("dnswitch", dnsWitchScript)
	if err != nil {
		return nil, err
	}
	if err := req.validate(); err != nil {
		return nil, err
	}
	node, stateFile, tokenFile, err := c.dnswitchState()
	if err != nil {
		return nil, err
	}
	args := []string{script, "serve", "--address", req.Address}
	if req.Hostname != "" {
		args = append(args, "--hostname", req.Hostname)
	}
	args = append(args, "--url", req.URL,
		"--dns-port", strconv.Itoa(req.DNSPort),
		"--http-port", strconv.Itoa(req.HTTPPort),
		"--state", stateFile, "--token-file", tokenFile)
	cmd := exec.Command(node, args...)
	cmd.Dir = c.ToolRoot
	proc, err := startSupervised(cmd)
	if err != nil {
		return nil, err
	}
	session := &DNSwitchSession{proc: proc, consumed: make(chan struct{})}
	ready := make(chan DNSwitchState, 1)
	failed := make(chan error, 1)
	go func() {
		defer close(session.consumed)
		for line := range proc.lines {
			event, ok := parseDNSwitchLine(line)
			if !ok {
				continue
			}
			switch event.Type {
			case "ready":
				session.set(event.State)
				select {
				case ready <- event.State:
				default:
				}
			case "failed":
				select {
				case failed <- dnsWitchFailure(event.Message):
				default:
				}
			}
			if onEvent != nil {
				onEvent(event)
			}
		}
	}()
	timeout := time.NewTimer(dnsWitchReadyTimeout)
	defer timeout.Stop()
	select {
	case state := <-ready:
		session.set(state)
		return session, nil
	case failure := <-failed:
		return nil, c.abortDNSwitch(session, failure)
	case <-proc.done:
		if proc.err == nil {
			return nil, c.abortDNSwitch(session, errors.New("DNSwitch exited before reporting ready"))
		}
		return nil, c.abortDNSwitch(session, proc.exitError())
	case <-ctx.Done():
		return nil, c.abortDNSwitch(session, ctx.Err())
	case <-timeout.C:
		return nil, c.abortDNSwitch(session, errors.New("DNSwitch did not report ready in time"))
	}
}

func (c Config) abortDNSwitch(session *DNSwitchSession, failure error) error {
	_ = session.proc.Stop(context.Background())
	<-session.consumed
	return failure
}

func (s *DNSwitchSession) set(state DNSwitchState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = state
}

// StatusDNSwitch reports the state the helper recorded in the private session
// directory, including a stopped or failed phase.
func (c Config) StatusDNSwitch(ctx context.Context) (DNSwitchState, error) {
	node, script, stateFile, tokenFile, err := c.dnswitchPaths()
	if err != nil {
		return DNSwitchState{}, err
	}
	output, err := runSupervised(ctx, exec.Command(node, script, "status",
		"--state", stateFile, "--token-file", tokenFile), nil)
	if err != nil {
		return DNSwitchState{}, err
	}
	var state DNSwitchState
	if err := decodeLastJSON(output, &state); err != nil || state.Phase == "" {
		return DNSwitchState{}, errors.New("DNSwitch reported an unreadable state")
	}
	return state, nil
}

// StopDNSwitch asks a running helper to close its listeners and exit.
func (c Config) StopDNSwitch(ctx context.Context) error {
	node, script, stateFile, tokenFile, err := c.dnswitchPaths()
	if err != nil {
		return err
	}
	_, err = runSupervised(ctx, exec.Command(node, script, "stop",
		"--state", stateFile, "--token-file", tokenFile), nil)
	return err
}

// dnswitchPaths resolves the helper, its private session directory, the state
// file and a 0600 control token.
func (c Config) dnswitchPaths() (node, script, stateFile, tokenFile string, err error) {
	if script, err = c.pluginFile("dnswitch", dnsWitchScript); err != nil {
		return "", "", "", "", err
	}
	if node, stateFile, tokenFile, err = c.dnswitchState(); err != nil {
		return "", "", "", "", err
	}
	return node, script, stateFile, tokenFile, nil
}

// dnswitchState resolves the interpreter and the private session files.
func (c Config) dnswitchState() (node, stateFile, tokenFile string, err error) {
	if node, err = c.nodeBinary(); err != nil {
		return "", "", "", err
	}
	dir, err := c.privateDir("dnswitch")
	if err != nil {
		return "", "", "", err
	}
	stateFile = filepath.Join(dir, "dnswitch-state.json")
	tokenFile = filepath.Join(dir, "dnswitch-token")
	if err := ensureToken(tokenFile); err != nil {
		return "", "", "", err
	}
	return node, stateFile, tokenFile, nil
}

// ensureToken creates the 64-character control token the helper requires. An
// existing token is left untouched and validated by the helper itself.
func ensureToken(path string) error {
	if _, err := os.Lstat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.WriteString(hex.EncodeToString(buffer)); err != nil {
		return err
	}
	return file.Chmod(0o600)
}

// validate mirrors the helper's option checks before a process is started.
func (r DNSwitchRequest) validate() error {
	ip := net.ParseIP(r.Address)
	if ip == nil {
		return errors.New("choose an IPv4 address assigned to this Mac")
	}
	ipv4 := ip.To4()
	if ipv4 == nil || ipv4.String() != r.Address {
		return errors.New("choose an IPv4 address assigned to this Mac")
	}
	if ipv4[0] == 0 || ipv4[0] >= 224 || r.Address == "255.255.255.255" {
		return errors.New("choose a usable Mac LAN IPv4 address")
	}
	if r.Hostname != "" {
		if len(r.Hostname) > 253 || r.Hostname != strings.ToLower(r.Hostname) {
			return errors.New("the captive portal hostname is invalid")
		}
		for _, label := range strings.Split(r.Hostname, ".") {
			if len(label) == 0 || len(label) > 63 || !dnsHostnameLabel.MatchString(label) {
				return errors.New("the captive portal hostname is invalid")
			}
		}
	}
	if r.URL != "" {
		if len(r.URL) > 2048 || strings.ContainsFunc(r.URL, func(char rune) bool { return char < 0x20 || char == 0x7f }) {
			return errors.New("enter a valid HTTP or HTTPS redirect URL")
		}
		parsed, err := url.Parse(r.URL)
		if err != nil || parsed.Scheme != "http" && parsed.Scheme != "https" || parsed.Host == "" {
			return errors.New("enter a valid HTTP or HTTPS redirect URL")
		}
		if parsed.User != nil {
			return errors.New("enter a valid HTTP or HTTPS redirect URL without credentials")
		}
	}
	if err := checkPort(r.DNSPort, "DNS"); err != nil {
		return err
	}
	return checkPort(r.HTTPPort, "HTTP")
}

func checkPort(port int, label string) error {
	if port < 0 || port > 65535 {
		return fmt.Errorf("%s port must be between 0 and 65535", label)
	}
	return nil
}

// dnsWitchFailure adds the privilege hint the caller needs when a listener
// cannot bind port 53 or 80 without administrator rights.
func dnsWitchFailure(message string) error {
	lower := strings.ToLower(message)
	if !strings.Contains(lower, "eacces") && !strings.Contains(lower, "permission denied") && !strings.Contains(lower, "operation not permitted") {
		return errors.New(message)
	}
	if os.Geteuid() == 0 {
		return errors.New(message)
	}
	return fmt.Errorf("%s; ports 53 and 80 need administrator privileges, so stop DNSwitch and choose a test port above 1023", message)
}

// parseDNSwitchLine reads the helper's stdout: either one MAC2JB_DNSWITCH
// traffic frame or the bare JSON ready state printed by `serve`.
func parseDNSwitchLine(line string) (DNSwitchEvent, bool) {
	line = strings.TrimSpace(line)
	if payload, ok := strings.CutPrefix(line, "MAC2JB_DNSWITCH "); ok {
		var raw struct {
			At      time.Time `json:"at"`
			Type    string    `json:"type"`
			Name    string    `json:"name"`
			QType   int       `json:"qtype"`
			RCode   int       `json:"rcode"`
			Method  string    `json:"method"`
			Status  int       `json:"status"`
			Message string    `json:"message"`
		}
		if err := decodeJSON([]byte(payload), &raw); err != nil || raw.Type == "" {
			return DNSwitchEvent{}, false
		}
		return DNSwitchEvent{
			Type: raw.Type, At: raw.At, Name: raw.Name, QType: raw.QType,
			RCode: raw.RCode, Method: raw.Method, Status: raw.Status, Message: raw.Message,
		}, true
	}
	var state DNSwitchState
	if err := decodeJSON([]byte(line), &state); err != nil || state.Phase == "" {
		return DNSwitchEvent{}, false
	}
	event := DNSwitchEvent{Type: state.Phase, State: state, Message: state.Message}
	return event, true
}
