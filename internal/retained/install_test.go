package retained_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/thedavidweng/any2jb/internal/manifest"
	"github.com/thedavidweng/any2jb/internal/retained"
	"github.com/thedavidweng/any2jb/internal/savekit"
	"github.com/thedavidweng/any2jb/internal/session"
)

func buildReferenceTarball(t *testing.T, dir, revision string, files map[string]string) string {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	prefix := "mac2jb-" + revision + "/"
	for name, body := range files {
		hdr := &tar.Header{
			Name:     prefix + name,
			Mode:     0o600,
			Size:     int64(len(body)),
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(dir, "mac2jb-"+revision+".tar.gz")
	if err := os.WriteFile(archive, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(buf.Bytes())
	return hex.EncodeToString(sum[:])
}

func shaHex(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

func TestBundledStageAndOnDemandHomebrewInstall(t *testing.T) {
	node := nodePath(t)
	toolHost, err := filepath.Abs("../../resources/runtime/toolhost.cjs")
	if err != nil {
		t.Fatal(err)
	}
	const revision = "717022b8a91a713be41a02b5e131d3229dd8b00a"
	files := map[string]string{
		"vendor/OunceCake/tool.cjs":             "// ouncecake\n",
		"vendor/MintBrew/manifest.json":         "{}\n",
		"integrations/mintbrew-native/host.cjs": "// native\n",
		"packaging/homebrew-runtime.lock.json":  "{\"lock\":1}\n",
		"packaging/homebrew/sitecustomize.py":   "# font adapter\n",
		"scripts/prepare-homebrew-runtime.py":   "import sys; sys.exit(1)\n",
	}

	embeddedComponent := func(id, anchor string, bundled bool, paths ...string) manifest.Component {
		embedded := map[string]string{anchor: shaHex(files[anchor])}
		for _, p := range paths {
			embedded[p] = shaHex(files[p])
		}
		dir := filepath.ToSlash(filepath.Dir(anchor))
		return manifest.Component{
			ID:     id,
			Path:   anchor,
			SHA256: shaHex(files[anchor]),
			Source: &manifest.ToolSource{
				Directory: dir,
				Format:    "embedded",
				Files:     len(embedded),
				Embedded:  embedded,
			},
			Distribution: manifest.Distribution{
				Decision: "may-ship",
				Bundled:  bundled,
				Reason:   "optional component " + id,
			},
		}
	}

	support := t.TempDir()
	archiveSHA := buildReferenceTarball(t, filepath.Join(support, "cache"), revision, files)
	refJSON, err := json.Marshal(manifest.ReferenceSource{
		Upstream:      "https://github.com/cpypwn/mac2jb",
		Revision:      revision,
		Archive:       "https://codeload.github.com/cpypwn/mac2jb/tar.gz/" + revision,
		ArchiveSHA256: archiveSHA,
	})
	if err != nil {
		t.Fatal(err)
	}
	m := &manifest.Manifest{
		Reference: refJSON,
		Profiles: []manifest.Profile{{
			ID:              "kurumuz",
			Engine:          "ns2jb",
			Revision:        revision,
			SourceDirectory: "vendor/kurumuz",
			Distribution:    manifest.Distribution{Decision: "may-ship", Bundled: true, Reason: "bundled"},
		}},
		Components: []manifest.Component{
			embeddedComponent("ouncecake", "vendor/OunceCake/tool.cjs", false),
			embeddedComponent("mintbrew", "vendor/MintBrew/manifest.json", false),
			embeddedComponent("mintbrew-native", "integrations/mintbrew-native/host.cjs", false),
			{
				ID:     "homebrew-runtime",
				Path:   "packaging/homebrew-runtime.lock.json",
				SHA256: shaHex(files["packaging/homebrew-runtime.lock.json"]),
				Source: &manifest.ToolSource{
					Directory: "packaging/homebrew",
					Format:    "selected-files",
					Files:     2,
					Extra: map[string]string{
						"packaging/homebrew/sitecustomize.py": shaHex(files["packaging/homebrew/sitecustomize.py"]),
						"scripts/prepare-homebrew-runtime.py": shaHex(files["scripts/prepare-homebrew-runtime.py"]),
					},
				},
				Distribution: manifest.Distribution{Decision: "may-ship", Bundled: false, Reason: "optional runtime"},
			},
		},
	}

	toolRoot, staged, err := retained.StageTools(retained.StageOptions{
		Support:  support,
		Version:  "0.1.0",
		Bundled:  true,
		Manifest: m,
	})
	if err != nil || len(staged) != 0 {
		t.Fatalf("empty packaged StageTools failed: %v %v", staged, err)
	}
	record, err := os.ReadFile(filepath.Join(toolRoot, "stage.json"))
	if err != nil || string(record) != `{"source":"bundled","redistributable":true}` {
		t.Fatalf("unexpected bundled stage.json: %q %v", record, err)
	}

	runner, run, heldSupport := heldSession(t)
	workspace := mustWorkspace(t, runner, run)
	_ = heldSupport

	mgr := retained.New(retained.Options{
		Support:      support,
		Version:      "0.1.0",
		Bundled:      true,
		Node:         node,
		Host:         toolHost,
		ToolRoot:     toolRoot,
		Staged:       staged,
		EngineStaged: true,
		EngineRoot:   t.TempDir(),
		Manifest:     m,
		Runs:         runner,
	})
	profile := session.Profile{ID: "kurumuz", Tools: []string{"ouncecake-webdav", "mintbrew", "native-hblauncher"}}
	if !mgr.HomebrewInstallable(profile) {
		t.Fatal("HomebrewInstallable should be true before optional components are downloaded")
	}
	if got := mgr.Unavailable("ouncecake-webdav", "kurumuz"); got == "" {
		t.Fatal("ouncecake-webdav should be unavailable before plugin install")
	}
	if got := mgr.Unavailable("native-hblauncher", "kurumuz"); got == "" {
		t.Fatal("native-hblauncher should be unavailable before on-demand install")
	}

	if _, err := mgr.InstallPlugins(context.Background(), "ouncecake"); err != nil {
		t.Fatalf("InstallPlugins(ouncecake): %v", err)
	}
	if got := mgr.Unavailable("ouncecake-webdav", "kurumuz"); got != "" {
		t.Fatalf("ouncecake-webdav should be ready after InstallPlugins, got %q", got)
	}

	// InstallHomebrew stages all homebrew plugins (and ouncecake dependency);
	// fixture prepare-homebrew-runtime.py exits 1 so Native HBLauncher becomes
	// ready while GPU/Python launchers report the actionable runtime note.
	installErr := mgr.InstallHomebrew(context.Background())
	if installErr == nil {
		t.Fatal("expected portable runtime note from failing fixture prepare script")
	}
	if got := mgr.Unavailable("native-hblauncher", "kurumuz"); got != "" {
		t.Fatalf("native-hblauncher should be ready after InstallHomebrew, got %q", got)
	}
	if got := mgr.Unavailable("ouncecake-webdav", "kurumuz"); got != "" {
		t.Fatalf("InstallHomebrew lost installed ouncecake: %q", got)
	}
	restored := retained.VerifyInstalledPlugins(toolRoot, m)
	for _, want := range []string{"ouncecake", "mintbrew", "mintbrew-native"} {
		if !slices.Contains(restored, want) {
			t.Fatalf("VerifyInstalledPlugins did not restore %s: %v", want, restored)
		}
	}

	if _, err := mgr.UninstallPlugins(context.Background(), "mintbrew"); err != nil {
		t.Fatalf("UninstallPlugins(mintbrew): %v", err)
	}
	if got := mgr.Unavailable("mintbrew", "kurumuz"); got == "" {
		t.Fatal("mintbrew remained available after UninstallPlugins")
	}

	// Verify real savekit-host.cjs accepts the bundled stage.json record up to
	// its pinned-tool presence check (proving argv + bundled stage gate match).
	saveHost, err := filepath.Abs("savekit-host.cjs")
	if err != nil {
		t.Fatal(err)
	}
	client := savekit.Client{Node: node, Module: saveHost, StagedRoot: toolRoot}
	_, saveErr := client.Run(context.Background(), workspace, "", savekit.Request{Action: "accounts"})
	if saveErr == nil {
		t.Fatal("expected missing savekit tool files error after passing stage.json check")
	}
}
