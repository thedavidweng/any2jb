package parity

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
)

//go:embed engine_preload.cjs
var enginePreload []byte

// PreloadScript returns the deterministic Node --require harness used in
// hardware-free engine differential checks.
func PreloadScript() []byte {
	return bytes.Clone(enginePreload)
}

const (
	requestBytes    = 0x8080
	outputBytes     = 0x8000
	responseBytes   = 16 + outputBytes
	inputDataOffset = 0x40080
	outputOffset    = 0x4040

	mainBase       = uint64(0x3012300000)
	sdkBase        = uint64(0x4012300000)
	scratchBase    = uint64(0x5012340000)
	scratchBytes   = uint64(0x80000)
	gotAddr        = mainBase + 0x9c92f0
	memmoveAddr    = sdkBase + 0x5872f0
	backingBase    = uint64(0x5010100000)
	controllerAddr = uint64(0x5010100070)
	vtableAddr     = sdkBase + 0xccca90
	sdkCodeBytes   = uint64(0x5e1000)
	sdkCRC32       = uint64(3925528160)

	offCRC32          = uint64(0x71310)
	offQueryMemory    = uint64(0x158314)
	offGetController  = uint64(0x1aba00)
	offReportActivity = uint64(0x1b18a0)
	offMemcpy         = uint64(0x586fb0)
	offMemmove        = uint64(0x5872f0)
)

// EnginePacket summarizes one 0x8080-byte request written by the host.
type EnginePacket struct {
	Call0      [5]uint64 `json:"call0"`
	Call1      [5]uint64 `json:"call1"`
	DataSHA256 string    `json:"dataSha256"`
	Stop       bool      `json:"stop,omitempty"`
}

// EngineTranscript captures everything a host sends over the console TCP wire
// plus the digest of the staged workspace engine files.
type EngineTranscript struct {
	StartCommand       string         `json:"startCommand"`
	Packets            []EnginePacket `json:"packets"`
	WireBytes          int            `json:"wireBytes"`
	WireSHA256         string         `json:"wireSha256"`
	CopiedEngineSHA256 string         `json:"copiedEngineSha256,omitempty"`
	Stopped            bool           `json:"stopped"`
}

func (t EngineTranscript) Canonical() string {
	var b strings.Builder
	fmt.Fprintf(&b, "start=%s\nwireBytes=%d\nwireSha256=%s\ncopiedEngineSha256=%s\nstopped=%t\n",
		t.StartCommand, t.WireBytes, t.WireSHA256, t.CopiedEngineSHA256, t.Stopped)
	for i, p := range t.Packets {
		fmt.Fprintf(&b, "%d stop=%t c0=%x,%x,%x,%x,%x c1=%x,%x,%x,%x,%x data=%s\n",
			i, p.Stop,
			p.Call0[0], p.Call0[1], p.Call0[2], p.Call0[3], p.Call0[4],
			p.Call1[0], p.Call1[1], p.Call1[2], p.Call1[3], p.Call1[4],
			p.DataSHA256)
	}
	return b.String()
}

func (t EngineTranscript) Digest() string {
	sum := sha256.Sum256([]byte(t.Canonical()))
	return hex.EncodeToString(sum[:])
}

// DiffEngine describes the first wire-level or workspace difference between two
// engine runs, or returns "" when both are byte-identical.
func DiffEngine(wantName, gotName string, want, got EngineTranscript) string {
	if want.StartCommand != got.StartCommand {
		return fmt.Sprintf("START command: %s %q, %s %q", wantName, want.StartCommand, gotName, got.StartCommand)
	}
	if want.CopiedEngineSHA256 != got.CopiedEngineSHA256 {
		return fmt.Sprintf("copied workspace engine SHA-256: %s %s, %s %s",
			wantName, want.CopiedEngineSHA256, gotName, got.CopiedEngineSHA256)
	}
	for i := 0; i < len(want.Packets) || i < len(got.Packets); i++ {
		if i < len(want.Packets) && i < len(got.Packets) && want.Packets[i] == got.Packets[i] {
			continue
		}
		format := func(list []EnginePacket) string {
			if i >= len(list) {
				return "<end>"
			}
			p := list[i]
			return fmt.Sprintf("stop=%t c0=%#x c1=%#x data=%s", p.Stop, p.Call0, p.Call1, p.DataSHA256[:16])
		}
		return fmt.Sprintf("packet %d of %d/%d: %s %s, %s %s",
			i, len(want.Packets), len(got.Packets), wantName, format(want.Packets), gotName, format(got.Packets))
	}
	if want.Stopped != got.Stopped {
		return fmt.Sprintf("stopped flag: %s %t, %s %t", wantName, want.Stopped, gotName, got.Stopped)
	}
	if want.WireBytes != got.WireBytes || want.WireSHA256 != got.WireSHA256 {
		return fmt.Sprintf("raw TCP stream: %s %dB (%s), %s %dB (%s)",
			wantName, want.WireBytes, want.WireSHA256, gotName, got.WireBytes, got.WireSHA256)
	}
	return ""
}

type memRegion struct {
	base, size          uint64
	memType, attr, perm uint32
}

type consoleState struct {
	regions []memRegion
	pages   map[uint64]*[4096]byte
}

func newConsoleState() *consoleState {
	s := &consoleState{
		regions: []memRegion{
			{base: mainBase, size: 0xa00000, memType: 4, attr: 0, perm: 3},
			{base: sdkBase, size: sdkCodeBytes, memType: 3, attr: 0, perm: 5},
			{base: sdkBase + sdkCodeBytes, size: 0x900000, memType: 4, attr: 0, perm: 1},
			{base: backingBase, size: 0x1000, memType: 4, attr: 0, perm: 3},
			{base: scratchBase, size: scratchBytes, memType: 4, attr: 0, perm: 3},
		},
		pages: make(map[uint64]*[4096]byte),
	}
	s.putU64(gotAddr, memmoveAddr)
	activityCode, _ := hex.DecodeString("0020009121088052e6401014")
	s.write(sdkBase+offReportActivity, activityCode)
	s.putU64(vtableAddr+0x120, sdkBase+offReportActivity)
	s.putU64(controllerAddr, vtableAddr)
	seedScratch := make([]byte, 64)
	for i := range seedScratch {
		seedScratch[i] = byte(i + 1)
	}
	s.write(scratchBase+0x10000, seedScratch)
	return s
}

func (s *consoleState) region(addr uint64) (memRegion, bool) {
	for _, r := range s.regions {
		if addr >= r.base && addr < r.base+r.size {
			return r, true
		}
	}
	return memRegion{}, false
}

func (s *consoleState) read(addr uint64, length int) []byte {
	out := make([]byte, length)
	for i := range length {
		cur := addr + uint64(i)
		if p := s.pages[cur&^4095]; p != nil {
			out[i] = p[cur&4095]
		}
	}
	return out
}

func (s *consoleState) write(addr uint64, data []byte) {
	for i, b := range data {
		cur := addr + uint64(i)
		page := s.pages[cur&^4095]
		if page == nil {
			page = new([4096]byte)
			s.pages[cur&^4095] = page
		}
		page[cur&4095] = b
	}
}

func (s *consoleState) putU64(addr, val uint64) {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], val)
	s.write(addr, buf[:])
}

func (s *consoleState) putU32(addr uint64, val uint32) {
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], val)
	s.write(addr, buf[:])
}

func (s *consoleState) execCall(words [5]uint64) (uint64, error) {
	target, a0, a1, a2 := words[0], words[1], words[2], words[3]
	switch target {
	case sdkBase + offQueryMemory:
		r, ok := s.region(a2)
		if !ok {
			r = memRegion{base: a2 &^ 4095, size: 4096, memType: 0, attr: 0, perm: 0}
		}
		s.putU64(a0, r.base)
		s.putU64(a0+8, r.size)
		s.putU32(a0+16, r.memType)
		s.putU32(a0+20, r.attr)
		s.putU32(a0+24, r.perm)
		s.putU32(a0+28, 0)
		s.putU32(a0+32, 0)
		s.putU32(a0+36, 0)
		s.putU32(a1, 0)
		return 0, nil
	case sdkBase + offCRC32:
		if a0 != 0 || a1 != sdkBase || a2 != sdkCodeBytes {
			return 0, fmt.Errorf("unexpected CRC32 args: %#x %#x %#x", a0, a1, a2)
		}
		return sdkCRC32, nil
	case sdkBase + offMemcpy, sdkBase + offMemmove:
		if a2 > scratchBytes {
			return 0, fmt.Errorf("excessive copy length: %#x", a2)
		}
		if a2 > 0 {
			s.write(a0, s.read(a1, int(a2)))
		}
		return a0, nil
	case sdkBase + offGetController:
		return controllerAddr, nil
	case sdkBase + offReportActivity:
		if a0 != controllerAddr {
			return 1, fmt.Errorf("unexpected controller arg: %#x", a0)
		}
		return 0, nil
	default:
		return 0, fmt.Errorf("unsupported console call target %#x", target)
	}
}

func decodeCall(raw []byte) [5]uint64 {
	var out [5]uint64
	for i := range out {
		out[i] = binary.LittleEndian.Uint64(raw[i*8:])
	}
	return out
}

// ServeConsole dials a loopback V=START(port,127,0,0,1[,t0,t1,t2,t3]) listener,
// sends the protocol-v1 greeting, answers native qualification and bridge RPC
// packets deterministically, and records every byte received from the host.
func ServeConsole(ctx context.Context, startCommand string) (EngineTranscript, error) {
	if !strings.HasPrefix(startCommand, "V=START(") || !strings.HasSuffix(startCommand, ")") {
		return EngineTranscript{}, fmt.Errorf("expected V=START command, got %q", startCommand)
	}
	fields := strings.Split(strings.TrimSuffix(strings.TrimPrefix(startCommand, "V=START("), ")"), ",")
	if len(fields) != 5 && len(fields) != 9 {
		return EngineTranscript{}, fmt.Errorf("expected 5 or 9 START arguments, got %d", len(fields))
	}
	values := make([]uint64, len(fields))
	for i, f := range fields {
		v, err := strconv.ParseUint(f, 10, 48)
		if err != nil {
			return EngineTranscript{}, fmt.Errorf("START arg %d: %w", i, err)
		}
		values[i] = v
	}
	if values[0] < 1024 || values[0] > 65535 {
		return EngineTranscript{}, fmt.Errorf("invalid START port %d", values[0])
	}
	if values[1] != 127 || values[2] != 0 || values[3] != 0 || values[4] != 1 {
		return EngineTranscript{}, fmt.Errorf("fake console only dials 127.0.0.1")
	}

	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp4", net.JoinHostPort("127.0.0.1", fields[0]))
	if err != nil {
		return EngineTranscript{}, err
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		conn.SetDeadline(deadline)
	}
	stopCancel := context.AfterFunc(ctx, func() { conn.Close() })
	defer stopCancel()

	var tokens [4]uint64
	if len(values) == 9 {
		copy(tokens[:], values[5:9])
	}
	words := [16]uint64{
		0x31545344, 1, 2,
		scratchBase, scratchBytes, mainBase, sdkBase,
		gotAddr, memmoveAddr, backingBase, controllerAddr,
		tokens[0], tokens[1], tokens[2], tokens[3],
		sdkCRC32,
	}
	var hello [128]byte
	for i, w := range words {
		binary.LittleEndian.PutUint64(hello[i*8:], w)
	}
	if _, err := conn.Write(hello[:]); err != nil {
		return EngineTranscript{}, err
	}

	state := newConsoleState()
	wireHash := sha256.New()
	var wireBytes int
	var packets []EnginePacket
	var zeroPacket [requestBytes]byte
	req := make([]byte, requestBytes)
	resp := make([]byte, responseBytes)

	for {
		if _, err := io.ReadFull(conn, req); err != nil {
			return EngineTranscript{}, fmt.Errorf("reading packet %d: %w", len(packets), err)
		}
		wireHash.Write(req)
		wireBytes += len(req)
		dataSum := sha256.Sum256(req[0x80:])

		if bytes.Equal(req, zeroPacket[:]) {
			packets = append(packets, EnginePacket{
				DataSHA256: hex.EncodeToString(dataSum[:]),
				Stop:       true,
			})
			var extra [1]byte
			n, err := conn.Read(extra[:])
			if n != 0 || (err != io.EOF && (err == nil || !strings.Contains(strings.ToLower(err.Error()), "reset"))) {
				return EngineTranscript{}, fmt.Errorf("expected socket close after stop frame, got n=%d err=%v", n, err)
			}
			return EngineTranscript{
				StartCommand: startCommand,
				Packets:      packets,
				WireBytes:    wireBytes,
				WireSHA256:   hex.EncodeToString(wireHash.Sum(nil)),
				Stopped:      true,
			}, nil
		}

		c0 := decodeCall(req[0:40])
		c1 := decodeCall(req[40:80])
		packets = append(packets, EnginePacket{
			Call0:      c0,
			Call1:      c1,
			DataSHA256: hex.EncodeToString(dataSum[:]),
		})
		state.write(scratchBase+inputDataOffset, req[0x80:])
		r0, err := state.execCall(c0)
		if err != nil {
			return EngineTranscript{}, fmt.Errorf("packet %d call0: %w", len(packets)-1, err)
		}
		r1, err := state.execCall(c1)
		if err != nil {
			return EngineTranscript{}, fmt.Errorf("packet %d call1: %w", len(packets)-1, err)
		}
		binary.LittleEndian.PutUint64(resp[0:8], r1)
		binary.LittleEndian.PutUint64(resp[8:16], r0)
		copy(resp[16:], state.read(scratchBase+outputOffset, outputBytes))
		if _, err := conn.Write(resp); err != nil {
			return EngineTranscript{}, fmt.Errorf("writing response %d: %w", len(packets)-1, err)
		}
	}
}
