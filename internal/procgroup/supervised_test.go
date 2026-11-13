package procgroup_test

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/thedavidweng/any2jb/internal/procgroup"
)

type sampleEvent struct {
	Type     string `json:"type"`
	Sequence uint64 `json:"sequence"`
}

func TestNDJSONReaderEnforcesFramingUnknownFieldsAndSequence(t *testing.T) {
	valid := `{"type":"started","sequence":1}` + "\n" + `{"type":"done","sequence":2}` + "\n"
	r := procgroup.NewNDJSONReader(strings.NewReader(valid), 1024, func(e sampleEvent) uint64 { return e.Sequence })
	first, err := r.Next()
	if err != nil || first.Type != "started" || first.Sequence != 1 {
		t.Fatalf("first: %+v, err=%v", first, err)
	}
	second, err := r.Next()
	if err != nil || second.Type != "done" || second.Sequence != 2 {
		t.Fatalf("second: %+v, err=%v", second, err)
	}
	if _, err := r.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("expected EOF, got %v", err)
	}

	for name, bad := range map[string]string{
		"partial-line":   `{"type":"started","sequence":1}`,
		"unknown-field":  `{"type":"started","sequence":1,"extra":true}` + "\n",
		"trailing-token": `{"type":"started","sequence":1}{}` + "\n",
		"bad-sequence":   `{"type":"started","sequence":2}` + "\n",
		"over-limit":     `{"type":"` + strings.Repeat("x", 256) + `","sequence":1}` + "\n",
	} {
		reader := procgroup.NewNDJSONReader(strings.NewReader(bad), 128, func(e sampleEvent) uint64 { return e.Sequence })
		if _, err := reader.Next(); !errors.Is(err, procgroup.ErrProtocol) {
			t.Errorf("%s: expected ErrProtocol, got %v", name, err)
		}
	}
}
