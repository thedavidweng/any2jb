package artemis

import (
	"errors"
	"strings"
	"testing"
)

func TestParseFrameRejectsWhatThePinnedHostNeverSends(t *testing.T) {
	frame, ok, err := parseFrame(`MAC2JB_ARTEMIS_FS {"type":"held-ready","message":"verified",` +
		`"runPath":"/x/runs/switch2-abc","resultPath":"/x/result.json"}`)
	if err != nil || !ok || frame.Type != "held-ready" || frame.RunPath != "/x/runs/switch2-abc" ||
		frame.ResultPath != "/x/result.json" {
		t.Fatalf("held-ready frame: %+v %t %v", frame, ok, err)
	}
	// The pinned host prints the run folder as a plain log line as well.
	if _, ok, err := parseFrame("Run folder: /x/runs/switch2-abc"); ok || err != nil {
		t.Fatalf("a log line must stay a log line: %t %v", ok, err)
	}
	if _, _, err := parseFrame(`MAC2JB_ARTEMIS_FS {"type":`); !errors.Is(err, ErrProtocol) {
		t.Fatalf("a malformed frame must be refused: %v", err)
	}
	if _, _, err := parseFrame(`MAC2JB_ARTEMIS_FS {"type":"teleport"}`); !errors.Is(err, ErrProtocol) ||
		!strings.Contains(err.Error(), "teleport") {
		t.Fatalf("an unknown frame must be refused: %v", err)
	}
}

func TestTruncateKeepsWholeCharacters(t *testing.T) {
	message := strings.Repeat("持", 4)
	if got := truncate(message, 7); got != "持持..." {
		t.Fatalf("truncate split a character: %q", got)
	}
	if got := truncate("short", 7); got != "short" {
		t.Fatalf("truncate changed a short message: %q", got)
	}
}
