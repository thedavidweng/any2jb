package civswitch

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/netip"
	"strings"
	"time"
)

// A loader request is one connection: the loader executes the code it receives
// and answers until it closes. Response lines are only inspected here. Loader
// output is never executed, interpolated into a command, or sent back.
type loaderCall struct {
	deadline time.Duration
	limit    int64
	line     func(string) error
}

const executionConfirmation = "Executed successfully"

// A console is reachable on the LAN; a loopback or link-local answer is never
// the console the user confirmed.
func consoleAddress(address string) bool {
	ip, err := netip.ParseAddr(address)
	return err == nil && ip.Is4() && ip.IsGlobalUnicast() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast()
}

// send reports whether the loader confirmed that it executed the code.
func (call loaderCall) send(ctx context.Context, endpoint, code string) (bool, error) {
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", endpoint)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	deadline := time.Now().Add(call.deadline)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return false, err
	}
	if _, err := io.WriteString(conn, code); err != nil {
		return false, err
	}
	if err := conn.(*net.TCPConn).CloseWrite(); err != nil {
		return false, err
	}
	executed, read := false, int64(0)
	lines := bufio.NewScanner(conn)
	lines.Buffer(make([]byte, 0, 64*1024), 256*1024)
	for lines.Scan() {
		line := strings.TrimRight(lines.Text(), "\r")
		read += int64(len(line)) + 1
		if read > call.limit {
			return executed, errors.New("CivSwitch loader response exceeds its limit")
		}
		if strings.TrimSpace(line) == executionConfirmation {
			executed = true
		}
		if call.line == nil {
			continue
		}
		if err := call.line(line); err != nil {
			return executed, err
		}
	}
	return executed, lines.Err()
}
