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

	"github.com/thedavidweng/any2jb/manifest"
	"github.com/thedavidweng/any2jb/retained"
	"github.com/thedavidweng/any2jb/savekit"
	"github.com/thedavidweng/any2jb/session"
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
	toolHost, err := filepath.Abs("../resources/runtime/toolhost.cjs")
	if err != nil {
		t.Fatal(err)
	}
	izFixture, err := filepath.Abs("testdata/fake-iz-prepare.cjs")
	if err != nil {
		t.Fatal(err)
	}
	izPrepareBytes, err := os.ReadFile(izFixture)
	if err != nil {
		t.Fatal(err)
	}

	const revision = "ce1ce5f4f40b412cf83ba0a4f10ac53a850ed308"
	files := map[string]string{
		"vendor/bundled/tool.cjs":               "// bundled\n",
		"vendor/MintBrew/manifest.json":         "{}\n",
		"integrations/mintbrew-native/host.cjs": "// native\n",
		"scripts/prepare-izlauncher.cjs":        string(izPrepareBytes),
		"vendor/izlauncher-core/core.cjs":       "// iz core\n",
		"vendor/izlauncher-blitbun/mod.cjs":     "// blitbun\n",
		"vendor/izlauncher-shell/mod.cjs":       "// shell\n",
		"vendor/izlauncher-vacootube/mod.cjs":   "// vacootube\n",
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
			embeddedComponent("ouncecake", "vendor/bundled/tool.cjs", true),
			embeddedComponent("mintbrew", "vendor/MintBrew/manifest.json", false),
			embeddedComponent("mintbrew-native", "integrations/mintbrew-native/host.cjs", false),
			{
				ID:     "izlauncher",
				Path:   "vendor/izlauncher-core/core.cjs",
				SHA256: shaHex(files["vendor/izlauncher-core/core.cjs"]),
				Source: &manifest.ToolSource{
					Directory: "vendor/izlauncher-core",
					Format:    "embedded",
					Files:     1,
					Embedded:  map[string]string{"vendor/izlauncher-core/core.cjs": shaHex(files["vendor/izlauncher-core/core.cjs"])},
					Extra:     map[string]string{"scripts/prepare-izlauncher.cjs": shaHex(files["scripts/prepare-izlauncher.cjs"])},
				},
				Distribution: manifest.Distribution{Decision: "may-ship", Bundled: false, Reason: "optional izlauncher"},
			},
			embeddedComponent("izlauncher-blitbun", "vendor/izlauncher-blitbun/mod.cjs", false),
			embeddedComponent("izlauncher-shell", "vendor/izlauncher-shell/mod.cjs", false),
			embeddedComponent("izlauncher-vacootube", "vendor/izlauncher-vacootube/mod.cjs", false),
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

	assets := t.TempDir()
	assetFile := filepath.Join(assets, "vendor/bundled/tool.cjs")
	if err := os.MkdirAll(filepath.Dir(assetFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(assetFile, []byte(files["vendor/bundled/tool.cjs"]), 0o600); err != nil {
		t.Fatal(err)
	}

	toolRoot, staged, err := retained.StageTools(retained.StageOptions{
		Support:    support,
		Version:    "0.1.0",
		SourceRoot: assets,
		Bundled:    true,
		Manifest:   m,
		Components: []string{"ouncecake"},
	})
	if err != nil || !slices.Equal(staged, []string{"ouncecake"}) {
		t.Fatalf("bundled StageTools failed: %v %v", staged, err)
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
	profile := session.Profile{ID: "kurumuz", Tools: []string{"ouncecake-webdav", "mintbrew", "native-hblauncher", "izlauncher"}}
	if !mgr.HomebrewInstallable(profile) {
		t.Fatal("HomebrewInstallable should be true before optional components are downloaded")
	}
	if got := mgr.Unavailable("ouncecake-webdav", "kurumuz"); got != "" {
		t.Fatalf("bundled ouncecake should be available offline, got %q", got)
	}
	if got := mgr.Unavailable("native-hblauncher", "kurumuz"); got == "" {
		t.Fatal("native-hblauncher should be unavailable before on-demand install")
	}

	// InstallHomebrew stages all source components and IZlauncher; the fixture
	// prepare-homebrew-runtime.py exits 1 so Native HBLauncher becomes ready
	// while GPU/Python launchers report the actionable runtime note.
	installErr := mgr.InstallHomebrew(context.Background())
	if installErr == nil {
		t.Fatal("expected portable runtime note from failing fixture prepare script")
	}
	if got := mgr.Unavailable("native-hblauncher", "kurumuz"); got != "" {
		t.Fatalf("native-hblauncher should be ready after InstallHomebrew, got %q", got)
	}
	if got := mgr.Unavailable("ouncecake-webdav", "kurumuz"); got != "" {
		t.Fatalf("InstallHomebrew lost previously bundled ouncecake: %q", got)
	}
	if err := retained.CheckIZLauncher(node, filepath.Join(toolRoot, "tools", "izlauncher")); err != nil {
		t.Fatalf("IZlauncher was not prepared by InstallHomebrew: %v", err)
	}
	restored := retained.VerifyStagedComponents(toolRoot, m, retained.OptionalHomebrewComponents)
	for _, want := range []string{"mintbrew", "mintbrew-native", "izlauncher", "izlauncher-blitbun", "izlauncher-shell", "izlauncher-vacootube"} {
		if !slices.Contains(restored, want) {
			t.Fatalf("VerifyStagedComponents did not restore %s: %v", want, restored)
		}
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
