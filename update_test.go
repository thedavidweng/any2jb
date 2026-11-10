package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/egoist/mygo"
)

func TestMyGoUpdateSignatureBoundary(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("isolated macOS .app updater boundary")
	}
	root := t.TempDir()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "throwaway.key"), private, 0o600); err != nil {
		t.Fatal(err)
	}
	// A signed archive with no .app must reach bundle validation but never
	// replace anything. This isolates authentication from macOS's exit helper.
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "not-an-app.txt", Mode: 0o600, Size: 4}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("test")); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	data := archive.Bytes()
	sum := sha256.Sum256(data)
	signature := base64.StdEncoding.EncodeToString(ed25519.Sign(private, sum[:]))
	binary := filepath.Join(root, "Probe.app/Contents/MacOS/probe")
	if err := os.MkdirAll(filepath.Dir(binary), 0o700); err != nil {
		t.Fatal(err)
	}
	flags := "-X github.com/egoist/mygo.production=1 -X github.com/egoist/mygo.packageVersion=0.1.0" +
		" -X github.com/egoist/mygo.packageUpdateFeed=https://invalid.example/update.json" +
		" -X github.com/egoist/mygo.packageUpdateKey=" + base64.StdEncoding.EncodeToString(public)
	cmd := exec.Command("go", "build", "-ldflags", flags, "-o", binary, "./testdata/updater")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build isolated updater: %v\n%s", err, out)
	}
	for _, fault := range []string{"valid signature", "tampered archive", "wrong key", "unsigned"} {
		t.Run(fault, func(t *testing.T) {
			payload := append([]byte(nil), data...)
			sig := signature
			want := "the update holds no app bundle"
			switch fault {
			case "tampered archive":
				payload[0] ^= 1
				want = "the update is not signed with the app's key"
			case "wrong key":
				_, other, err := ed25519.GenerateKey(rand.Reader)
				if err != nil {
					t.Fatal(err)
				}
				sig = base64.StdEncoding.EncodeToString(ed25519.Sign(other, sum[:]))
				want = "the update is not signed with the app's key"
			case "unsigned":
				sig = ""
				want = "incomplete manifest"
			}
			fixture := t.TempDir()
			manifestPath, archivePath := filepath.Join(fixture, "update.json"), filepath.Join(fixture, "update.tar.gz")
			document, err := json.Marshal(map[string]any{
				"version": "0.2.0", "url": "fixture", "size": len(payload), "signature": sig,
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(manifestPath, document, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(archivePath, payload, 0o600); err != nil {
				t.Fatal(err)
			}
			out, err := exec.Command(binary, manifestPath, archivePath).CombinedOutput()
			if err != nil || !strings.Contains(string(out), want) {
				t.Fatalf("signature boundary: %v %s; want %q", err, out, want)
			}
			if _, err := os.Stat(binary); err != nil {
				t.Fatal("updater modified its isolated installed app")
			}
		})
	}
}

func TestUnconfiguredUpdatesFailClosed(t *testing.T) {
	// The real app's configuration must not contain a dummy key or guessed
	// channel. An absent updates object makes MyGo omit both linker settings.
	data, err := os.ReadFile("mygo.json")
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]json.RawMessage
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	if _, configured := config["updates"]; configured {
		t.Fatal("production key/channel activation needs release review")
	}
	if mygo.Updater.Enabled() {
		t.Fatal("unconfigured updater enabled")
	}
	if _, err := mygo.Updater.Check(context.Background()); !errors.Is(err, mygo.ErrUpdatesDisabled) {
		t.Fatalf("unconfigured update check: %v", err)
	}
}
