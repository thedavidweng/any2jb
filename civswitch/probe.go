// Package civswitch integrates the separate Civilization VII composite-HID
// route. A verified loader is not an ns2jb verified session or held RPC lease.
package civswitch

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

const LoaderPort = 9026

// ProbeLoader performs the upstream Remote JS Loader challenge, not just a TCP
// connect. The endpoint parameter permits loopback fixtures without consoles.
func ProbeLoader(ctx context.Context, endpoint string) error {
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", endpoint)
	if err != nil {
		return err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	deadline := time.Now().Add(10 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return err
	}
	marker := "any2jb-civswitch-" + rand.Text()
	if _, err := io.WriteString(conn, fmt.Sprintf("log(%q);", marker)); err != nil {
		return err
	}
	if err := conn.(*net.TCPConn).CloseWrite(); err != nil {
		return err
	}
	response, err := io.ReadAll(io.LimitReader(conn, 65537))
	if err != nil {
		return err
	}
	if len(response) > 65536 {
		return errors.New("CivSwitch loader response exceeds limit")
	}
	foundMarker, executed := false, false
	for _, line := range strings.Split(string(response), "\n") {
		switch strings.TrimSpace(line) {
		case marker:
			foundMarker = true
		case "Executed successfully":
			executed = true
		}
	}
	if !foundMarker || !executed {
		return errors.New("port 9026 did not confirm the CivSwitch loader challenge")
	}
	return nil
}

func probe(ctx context.Context, address string, once bool) error {
	wait, attempt, interval := 90*time.Second, 10*time.Second, 2*time.Second
	if once {
		wait, attempt, interval = 3*time.Second, 2500*time.Millisecond, 250*time.Millisecond
	}
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	for {
		attemptCtx, done := context.WithTimeout(ctx, attempt)
		err := ProbeLoader(attemptCtx, net.JoinHostPort(address, "9026"))
		done()
		if err == nil {
			return nil
		}
		var op *net.OpError
		if !errors.As(err, &op) {
			return err
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
