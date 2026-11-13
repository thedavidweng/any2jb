// Package civswitch integrates the separate Civilization VII composite-HID
// route. A verified loader is not an ns2jb verified session or held RPC lease.
package civswitch

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

const LoaderPort = 9026

// ProbeLoader performs the upstream Remote JS Loader challenge, not just a TCP
// connect. The endpoint parameter permits loopback fixtures without consoles.
func ProbeLoader(ctx context.Context, endpoint string) error {
	marker := "any2jb-civswitch-" + rand.Text()
	found := false
	executed, err := loaderCall{deadline: 10 * time.Second, limit: 64 << 10, line: func(line string) error {
		if strings.TrimSpace(line) == marker {
			found = true
		}
		return nil
	}}.send(ctx, endpoint, fmt.Sprintf("log(%q);", marker))
	if err != nil {
		return err
	}
	if !found || !executed {
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
