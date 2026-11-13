package diagnostics

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRealLogSourcesAreSeparatedAndFollowNewestOutput(t *testing.T) {
	s := Store{Support: t.TempDir()}
	workspace := t.TempDir()
	for name, value := range map[string]string{"host.log": "host connected", "usb.log": "typing failed", "run.log": "pipeline restored", "switch2/acquisition.log": "acquisition timeout"} {
		p := filepath.Join(workspace, name)
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Capture(workspace, "test"); err != nil {
		t.Fatal(err)
	}
	logs, err := s.Read("test")
	if err != nil {
		t.Fatal(err)
	}
	if logs.Host != "host connected" || logs.USB != "typing failed" || logs.Pipeline != "pipeline restored" || logs.Acquisition != "acquisition timeout" {
		t.Fatal(logs)
	}
	if err := os.WriteFile(filepath.Join(workspace, "run.log"), []byte(strings.Repeat("old log line\n", 30000)+"new failure"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.Capture(workspace, "test"); err != nil {
		t.Fatal(err)
	}
	logs, err = s.Read("test")
	if err != nil || !strings.HasSuffix(logs.Pipeline, "new failure") || len(logs.Pipeline) > 256*1024 {
		t.Fatal("did not follow newest bounded output", err)
	}
}
