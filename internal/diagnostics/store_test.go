package diagnostics

import (
	"archive/zip"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thedavidweng/any2jb/internal/session"
)

func TestRedactedLogsAndAllowlistedBundle(t *testing.T) {
	for _, field := range []string{"token", "pairingToken", "key", "privateKey", "accessKey", "apiKey", "credentials", "accountId", "user_id", "authorization", "password", "saveData", "futureSecret"} {
		t.Run(field, func(t *testing.T) {
			store := Store{Support: t.TempDir()}
			workspace := filepath.Join(store.Support, "runs", "run-test")
			if err := os.MkdirAll(workspace, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(workspace, "host.log"), []byte(`{"error":"connection closed","`+field+`":"sensitive-value"}`+"\nV=START(39380,192,0,2,1,1234)\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := store.Capture(workspace, "test"); err != nil {
				t.Fatal(err)
			}
			logs, err := store.Read("test")
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(logs.Host, "sensitive-value") || strings.Contains(logs.Host, "1234") || !strings.Contains(logs.Host, "connection closed") {
				t.Fatal(logs)
			}
			path, err := store.Export(context.Background(), session.Snapshot{
				RunID: "test", LastMessage: "sensitive-value", LANIPv4: "192.0.2.1",
				Stage: session.Stage("sensitive-value"), Recovery: session.Recovery("sensitive-value"),
				Profile:   session.Profile{ID: "pepsipu", Revision: "sensitive-value"},
				Checks:    []session.Check{{ID: "sensitive-value", Message: "sensitive-value"}},
				Processes: []session.Process{{Name: "sensitive-value"}},
			})
			if err != nil {
				t.Fatal(err)
			}
			z, err := zip.OpenReader(path)
			if err != nil {
				t.Fatal(err)
			}
			defer z.Close()
			for _, f := range z.File {
				r, _ := f.Open()
				data, _ := io.ReadAll(r)
				r.Close()
				if strings.Contains(string(data), "sensitive-value") || strings.Contains(string(data), "192.0.2.1") {
					t.Fatalf("leak in %s", f.Name)
				}
			}
		})
	}
}

func TestMultilineAndMalformedJSONFailClosed(t *testing.T) {
	for _, raw := range []string{`{"unrecognized": "private-value"`, "{\n\"unrecognized\": \"private-value\"\n}", "apiKey=private-value", "accountId=private-value", "privateKey=private-value", "credentials=private-value", "V=START(private-value)"} {
		if strings.Contains(Redact(raw), "private-value") {
			t.Fatal("leaked malformed or sensitive field")
		}
	}
}

func TestRedactNetworkAddressesAndSerials(t *testing.T) {
	for _, value := range []string{"192.0.2.50", "02:00:5e:10:00:00", "02-00-5E-10-00-00", "TEST12345678"} {
		for _, raw := range []string{"connected to " + value, `{"message":"connected to ` + value + `"}`} {
			got := Redact(raw)
			if strings.Contains(got, value) || !strings.Contains(got, "[redacted]") {
				t.Fatalf("identifier leaked: %q", got)
			}
		}
	}
}

func TestCaptureRefusesSymlinkToPrivateData(t *testing.T) {
	s := Store{Support: t.TempDir()}
	workspace := t.TempDir()
	private := filepath.Join(workspace, "save.bin")
	if err := os.WriteFile(private, []byte("private save"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(private, filepath.Join(workspace, "host.log")); err != nil {
		t.Fatal(err)
	}
	if err := s.Capture(workspace, "test"); err == nil {
		t.Fatal("read private save through log symlink")
	}
}
