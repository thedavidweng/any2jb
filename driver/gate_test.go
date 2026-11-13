package driver_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/thedavidweng/any2jb/driver"
	"github.com/thedavidweng/any2jb/manifest"
	"github.com/thedavidweng/any2jb/readiness"
	"github.com/thedavidweng/any2jb/session"
)

func TestDecliningDoesNotInspectOrInstall(t *testing.T) {
	called := false
	g := driver.New(driver.Options{Administrator: func(context.Context, string) error {
		called = true
		return nil
	}})
	result, err := g.Install(context.Background(), false)
	if err != nil || called || result.Attempted || result.RestartRequired {
		t.Fatalf("decline changed state: %+v, %v, called=%v", result, err, called)
	}
}

func TestDeveloperInstallVerifiesBeforeAdministrator(t *testing.T) {
	for _, tamper := range []bool{false, true} {
		t.Run(fmt.Sprint(tamper), func(t *testing.T) {
			root, m := fixture(t)
			names := []string{"drivers/usbgadget.kext/Contents/MacOS/usbgadget", "drivers/usbgadget.kext/Contents/Info.plist", "drivers/SHA256SUMS", "scripts/install-driver.sh"}
			if tamper {
				if err := os.WriteFile(filepath.Join(root, names[0]), []byte("changed"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			called := false
			g := driver.New(driver.Options{Manifest: m, DeveloperSourceRoot: root, Administrator: func(ctx context.Context, installer string) error {
				called = true
				if installer != filepath.Join(root, "scripts/install-driver.sh") {
					t.Fatalf("unexpected privileged action: %q", installer)
				}
				return nil
			}})
			result, err := g.Install(context.Background(), true)
			if tamper {
				if err == nil || called || result.Attempted {
					t.Fatalf("changed bytes accepted: %+v, %v", result, err)
				}
			} else if err != nil || !called || !result.Attempted || !result.RestartRequired || !result.DeveloperSource {
				t.Fatalf("verified developer install not surfaced: %+v, %v", result, err)
			}
		})
	}
}

func fixture(t *testing.T) (string, *manifest.Manifest) {
	t.Helper()
	root := t.TempDir()
	m := &manifest.Manifest{}
	names := []string{"drivers/usbgadget.kext/Contents/MacOS/usbgadget", "drivers/usbgadget.kext/Contents/Info.plist", "drivers/SHA256SUMS", "scripts/install-driver.sh"}
	ids := []string{"usbgadget-binary", "usbgadget-info", "usbgadget-checksums", "driver-installer"}
	for i, name := range names {
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("hello"), 0600); err != nil {
			t.Fatal(err)
		}
		m.Components = append(m.Components, manifest.Component{ID: ids[i], Path: name,
			SHA256:       "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
			Distribution: manifest.Distribution{Decision: "blocked", Reason: "fixture rights blocker"}})
	}
	return root, m
}

func TestRestartAfterFailedLoadRemainsRequired(t *testing.T) {
	root, m := fixture(t)
	marker := filepath.Join(t.TempDir(), ".mac2jb-usbgadget-restart-required")
	installed := time.Unix(1001, 0)
	g := driver.New(driver.Options{Manifest: m, DeveloperSourceRoot: root, Administrator: func(context.Context, string) error {
		if err := os.WriteFile(marker, nil, 0600); err != nil {
			return err
		}
		if err := os.Chtimes(marker, installed, installed); err != nil {
			return err
		}
		return errors.New("fake load rejected after installation")
	}})
	result, err := g.Install(context.Background(), true)
	if err == nil || !result.RestartRequired {
		t.Fatalf("failed load hid restart requirement: %+v %v", result, err)
	}
	for _, boot := range []int64{1000, 1002} {
		inspector := readiness.Inspector{Observe: func(context.Context) readiness.Observation {
			info, err := os.Stat(marker)
			if err != nil {
				t.Fatal(err)
			}
			return readiness.Observation{USBSupported: true, Driver: readiness.Driver{
				Present: true, Loaded: true, RestartPending: !info.ModTime().Before(time.Unix(boot, 0)),
			}}
		}}
		checks, err := inspector.Inspect(context.Background(), session.Run{})
		if err != nil {
			t.Fatal(err)
		}
		for _, check := range checks {
			if check.ID == "driver-restart" {
				if (check.State == session.RequiresAttention) != (boot == 1000) {
					t.Fatalf("restart state: %+v", check)
				}
				if check.Blocks(session.Transport{Capabilities: session.Capabilities{Manual: true}}) {
					t.Fatal("restart blocked manual input")
				}
			}
		}
	}
}

func TestUnexpectedBundleAndChangedInstallerNeverReachAdministrator(t *testing.T) {
	for _, name := range []string{"drivers/usbgadget.kext/extra", "scripts/install-driver.sh", "drivers/SHA256SUMS", "drivers/usbgadget.kext/Contents/Info.plist"} {
		t.Run(name, func(t *testing.T) {
			root, m := fixture(t)
			if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(name)), []byte("changed"), 0600); err != nil {
				t.Fatal(err)
			}
			g := driver.New(driver.Options{Manifest: m, DeveloperSourceRoot: root, Administrator: func(context.Context, string) error {
				t.Fatal("administrator invoked for unverified files")
				return nil
			}})
			if _, err := g.Install(context.Background(), true); err == nil {
				t.Fatal("unverified files accepted")
			}
		})
	}
}

func TestPackagedDriverBlockedWithCanonicalReason(t *testing.T) {
	m, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	blocked := *m
	blocked.Components = append([]manifest.Component(nil), m.Components...)
	const blocker = "USB driver distribution is blocked in this test fixture."
	for i := range blocked.Components {
		if blocked.Components[i].ID == "usbgadget-binary" {
			blocked.Components[i].Distribution = manifest.Distribution{Decision: "blocked", Reason: blocker}
		}
	}
	called := false
	g := driver.New(driver.Options{Manifest: &blocked, Administrator: func(context.Context, string) error {
		called = true
		return nil
	}})
	if err := g.Availability(); err == nil || !strings.Contains(err.Error(), blocker) {
		t.Fatalf("missing canonical blocker: %v", err)
	}
	if _, err := g.Install(context.Background(), true); err == nil || called {
		t.Fatalf("blocked package invoked administrator: %v, called=%v", err, called)
	}
	missingAssets := driver.New(driver.Options{Manifest: m, Assets: t.TempDir(), Administrator: func(context.Context, string) error {
		called = true
		return nil
	}})
	if err := missingAssets.Availability(); err == nil {
		t.Fatal("missing bundled driver files accepted")
	}
}

func TestPackagedDriverInstallVerifiesBundledAndOverlayAssets(t *testing.T) {
	root, m := fixture(t)
	for i := range m.Components {
		m.Components[i].Distribution = manifest.Distribution{Decision: "may-ship", Bundled: true}
	}
	overlay := t.TempDir()
	for _, c := range m.Components {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(c.Path)))
		if err != nil {
			t.Fatal(err)
		}
		dst := filepath.Join(overlay, "darwin-arm64", filepath.FromSlash(c.Path))
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, assets := range []string{root, overlay} {
		called := false
		g := driver.New(driver.Options{Manifest: m, Assets: assets, Administrator: func(_ context.Context, installer string) error {
			called = true
			if !strings.HasSuffix(filepath.ToSlash(installer), "/scripts/install-driver.sh") {
				t.Fatalf("unexpected installer path: %q", installer)
			}
			return nil
		}})
		if err := g.Availability(); err != nil {
			t.Fatalf("bundled driver unavailable for %s: %v", assets, err)
		}
		result, err := g.Install(context.Background(), true)
		if err != nil || !called || !result.Attempted || !result.RestartRequired || result.DeveloperSource {
			t.Fatalf("packaged driver install failed: %+v %v", result, err)
		}
	}
}

func TestPackagedGzipDriverInstallMaterializesVerifiedKext(t *testing.T) {
	root, m := fixture(t)
	wantBinary, err := os.ReadFile(filepath.Join(root, "drivers/usbgadget.kext/Contents/MacOS/usbgadget"))
	if err != nil {
		t.Fatal(err)
	}
	assets := t.TempDir()
	for i := range m.Components {
		m.Components[i].Distribution = manifest.Distribution{Decision: "may-ship", Bundled: true}
		if m.Components[i].ID == "usbgadget-binary" {
			m.Components[i].PackagedGzip = "drivers/usbgadget.kext/Contents/MacOS/usbgadget.gz"
			var buf bytes.Buffer
			gz := gzip.NewWriter(&buf)
			if _, err := gz.Write(wantBinary); err != nil {
				t.Fatal(err)
			}
			if err := gz.Close(); err != nil {
				t.Fatal(err)
			}
			dst := filepath.Join(assets, filepath.FromSlash(m.Components[i].PackagedGzip))
			if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(dst, buf.Bytes(), 0o600); err != nil {
				t.Fatal(err)
			}
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(m.Components[i].Path)))
		if err != nil {
			t.Fatal(err)
		}
		dst := filepath.Join(assets, filepath.FromSlash(m.Components[i].Path))
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	called := false
	g := driver.New(driver.Options{Manifest: m, Assets: assets, Administrator: func(_ context.Context, installer string) error {
		called = true
		stage := filepath.Dir(filepath.Dir(installer))
		got, err := os.ReadFile(filepath.Join(stage, "drivers/usbgadget.kext/Contents/MacOS/usbgadget"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, wantBinary) {
			t.Fatalf("staged kext bytes %q != %q", got, wantBinary)
		}
		info, err := os.Stat(filepath.Join(stage, "drivers/usbgadget.kext/Contents/MacOS/usbgadget"))
		if err != nil || info.Mode().Perm()&0o111 == 0 {
			t.Fatalf("staged kext missing executable mode: %v", err)
		}
		return nil
	}})
	result, err := g.Install(context.Background(), true)
	if err != nil || !called || !result.Attempted || !result.RestartRequired || result.DeveloperSource {
		t.Fatalf("packaged gzip driver install failed: %+v %v", result, err)
	}
}
