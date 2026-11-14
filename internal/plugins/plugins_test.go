package plugins

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/thedavidweng/any2jb/internal/manifest"
)

func TestCatalogMatchesManifestExactly(t *testing.T) {
	m, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyCatalogMatchesManifest(m) {
		t.Fatalf("plugin catalog (%d) does not match manifest OptionalToolIDs (%v)", len(All()), m.OptionalToolIDs())
	}
	for _, spec := range All() {
		if spec.TitleKey == "" || spec.DescKey == "" || spec.UpstreamURL == "" || spec.License == "" {
			t.Fatalf("incomplete spec for %s: %+v", spec.ID, spec)
		}
	}
}

func TestExpandTargetsResolvesDependenciesAndPlatforms(t *testing.T) {
	got, err := ExpandTargets("darwin-arm64", "modloader")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{"ouncecake", "modloader"}) {
		t.Fatalf("ExpandTargets(modloader) = %v, want [ouncecake modloader]", got)
	}

	gotMoon, err := ExpandTargets("linux-amd64", "moonlight-civswitch")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(gotMoon, []string{"civhomebrew", "moonlight-civswitch"}) {
		t.Fatalf("ExpandTargets(moonlight-civswitch) = %v, want [civhomebrew moonlight-civswitch]", gotMoon)
	}

	gotHB, err := ExpandTargets("linux-amd64", "mintbrew")
	if err != nil {
		t.Fatal(err)
	}
	// On Linux, homebrew-runtime is darwin-arm64-only and skipped when pulled as a dependency.
	if !slices.Equal(gotHB, []string{"mintbrew"}) {
		t.Fatalf("ExpandTargets(linux-amd64, mintbrew) = %v, want [mintbrew]", gotHB)
	}

	if _, err := ExpandTargets("linux-amd64", "homebrew-runtime"); err == nil {
		t.Fatal("expected direct homebrew-runtime install on linux-amd64 to report UnsupportedPlatformError")
	}

	if _, err := ExpandTargets("darwin-arm64", "nonexistent-plugin"); err == nil {
		t.Fatal("expected unknown plugin error")
	}

	gotTrigger, err := ExpandTargets("darwin-arm64", "civswitch-trigger")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(gotTrigger, []string{"civswitch-usb", "civswitch-trigger"}) {
		t.Fatalf("ExpandTargets(civswitch-trigger) = %v, want [civswitch-usb civswitch-trigger]", gotTrigger)
	}

	allDarwin, err := ExpandTargets("darwin-arm64", "all")
	if err != nil || len(allDarwin) != 29 {
		t.Fatalf("ExpandTargets(darwin-arm64, all) = %d (%v), want 29", len(allDarwin), err)
	}
	allLinux, err := ExpandTargets("linux-amd64", "all")
	if err != nil || len(allLinux) != 28 {
		t.Fatalf("ExpandTargets(linux-amd64, all) = %d (%v), want 28", len(allLinux), err)
	}
}

func TestArchiveHelpersRejectTraversalAndVerifyDigests(t *testing.T) {
	dir := t.TempDir()

	// Build a zip with a traversal path and ensure extractZipTree rejects it.
	var zbuf bytes.Buffer
	zw := zip.NewWriter(&zbuf)
	w, err := zw.Create("repo-abc/../escape.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("bad"))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	badZip := filepath.Join(dir, "bad.zip")
	if err := os.WriteFile(badZip, zbuf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := extractZipTree(badZip, "repo-abc/", filepath.Join(dir, "out"), nil); err == nil {
		t.Fatal("expected path traversal zip entry to fail")
	}

	// Build a tar.gz member and test HTTP fetch + digest check.
	var tbuf bytes.Buffer
	gzw := gzip.NewWriter(&tbuf)
	tw := tar.NewWriter(gzw)
	payload := []byte("#!/bin/sh\necho ok\n")
	if err := tw.WriteHeader(&tar.Header{Name: "pkg/bin/nxconvert", Mode: 0o755, Size: int64(len(payload)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(payload); err != nil {
		t.Fatal(err)
	}
	_ = tw.Close()
	_ = gzw.Close()
	archiveBytes := tbuf.Bytes()
	sum := sha256.Sum256(archiveBytes)
	wantArchiveSHA := hex.EncodeToString(sum[:])

	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		_, _ = rw.Write(archiveBytes)
	}))
	defer srv.Close()

	cached, err := fetchVerifiedFile(context.Background(), filepath.Join(dir, "cache"), "sample.tar.gz", srv.URL+"/sample.tar.gz", wantArchiveSHA)
	if err != nil {
		t.Fatal(err)
	}
	binDest := filepath.Join(dir, "extracted", "bin", "nxconvert")
	if err := extractTarGzMember(cached, "/bin/nxconvert", binDest, 0o500); err != nil {
		t.Fatal(err)
	}
	payloadSum := sha256.Sum256(payload)
	if err := verifyFileDigest(binDest, hex.EncodeToString(payloadSum[:])); err != nil {
		t.Fatal(err)
	}
}
