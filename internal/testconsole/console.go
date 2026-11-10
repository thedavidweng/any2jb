// Package testconsole simulates the console side of the pinned pepsipu TCP
// protocol. It does not execute native calls, emulate an exploit or use USB.
package testconsole

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
)

type Console struct{ net.Conn }

// Connect accepts a nine-argument V=START command and sends a fresh binary
// greeting. Only loopback is allowed: test pairing must never reach a console.
func Connect(ctx context.Context, command string) (*Console, error) {
	if !strings.HasPrefix(command, "V=START(") || !strings.HasSuffix(command, ")") {
		return nil, fmt.Errorf("expected V=START pairing command")
	}
	args := strings.Split(strings.TrimSuffix(strings.TrimPrefix(command, "V=START("), ")"), ",")
	if len(args) != 9 {
		return nil, fmt.Errorf("expected nine START arguments")
	}
	values := make([]uint64, len(args))
	for i, arg := range args {
		value, err := strconv.ParseUint(arg, 10, 48)
		if err != nil {
			return nil, fmt.Errorf("START argument %d: %w", i, err)
		}
		values[i] = value
	}
	if values[0] < 1024 || values[0] > 65535 {
		return nil, fmt.Errorf("invalid START port")
	}
	if values[1] != 127 || values[2] != 0 || values[3] != 0 || values[4] != 1 {
		return nil, fmt.Errorf("fake console requires loopback pairing")
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp4", net.JoinHostPort("127.0.0.1", args[0]))
	if err != nil {
		return nil, err
	}
	console := &Console{conn}
	if deadline, ok := ctx.Deadline(); ok {
		conn.SetDeadline(deadline)
	}
	// Addresses/layout from the pinned bootstrap's protocol-v1 test vector.
	const main, sdk = uint64(0x3012300000), uint64(0x4012300000)
	words := []uint64{0x31545344, 1, 2, 0x5012340000, 0x80000, main, sdk,
		main + 0x9c92f0, sdk + 0x5872f0, 0x5010100000, 0x5010100070,
		values[5], values[6], values[7], values[8], 3925528160}
	hello := make([]byte, 128)
	for i, word := range words {
		binary.LittleEndian.PutUint64(hello[i*8:], word)
	}
	if _, err := conn.Write(hello); err != nil {
		conn.Close()
		return nil, err
	}
	return console, nil
}

// DisconnectPartialReply receives exactly one native request, sends half of
// the response header, and disconnects. It never retries a native operation.
func (c *Console) DisconnectPartialReply(ctx context.Context) error {
	if deadline, ok := ctx.Deadline(); ok {
		c.SetDeadline(deadline)
	}
	if _, err := io.ReadFull(c, make([]byte, 0x8080)); err != nil {
		return err
	}
	if _, err := c.Write(make([]byte, 8)); err != nil {
		return err
	}
	return c.Close()
}
