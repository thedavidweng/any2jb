package civswitch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/thedavidweng/any2jb/internal/manifest"
	"github.com/thedavidweng/any2jb/internal/session"
)

// fakeLoader answers on loopback like the upstream Remote JS Loader. No console
// is contacted and no USB device is used anywhere in these tests.
type fakeLoader struct {
	listener net.Listener
	mu       sync.Mutex
	received []string
	respond  func(code string) string
	// linger keeps an answered connection open, as the Control Center session
	// does while the user drives it on the console.
	linger chan struct{}
}

func newFakeLoader(t *testing.T, respond func(string) string) *fakeLoader {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	loader := &fakeLoader{listener: listener, respond: respond}
	t.Cleanup(func() { listener.Close() })
	go loader.serve()
	return loader
}

func (l *fakeLoader) serve() {
	for {
		conn, err := l.listener.Accept()
		if err != nil {
			return
		}
		go func() {
			defer conn.Close()
			code, err := io.ReadAll(conn)
			if err != nil {
				return
			}
			l.mu.Lock()
			l.received = append(l.received, string(code))
			l.mu.Unlock()
			io.WriteString(conn, l.answer(string(code)))
			if _, challenge := challengeMarker(string(code)); l.linger != nil && !challenge {
				<-l.linger
			}
		}()
	}
}

// answer replies to the loader challenge and delegates anything else to the
// scripted response of the test.
func (l *fakeLoader) answer(code string) string {
	if marker, ok := challengeMarker(code); ok {
		return marker + "\n" + executionConfirmation + "\n"
	}
	if l.respond == nil {
		return executionConfirmation + "\n"
	}
	return l.respond(code)
}

func challengeMarker(code string) (string, bool) {
	if !strings.HasPrefix(code, `log("any2jb-civswitch-`) {
		return "", false
	}
	var marker string
	if err := json.Unmarshal([]byte(strings.TrimSuffix(strings.TrimPrefix(code, "log("), ");")), &marker); err != nil {
		return "", false
	}
	return marker, true
}

func (l *fakeLoader) sent() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.received...)
}

func (l *fakeLoader) codeSent() int {
	count := 0
	for _, code := range l.sent() {
		if _, challenge := challengeMarker(code); !challenge {
			count++
		}
	}
	return count
}

type persistenceFixture struct {
	adapter   *Adapter
	loader    *fakeLoader
	support   string
	installer string
	progress  []session.Progress
}

func (f *persistenceFixture) report(p session.Progress) { f.progress = append(f.progress, p) }

func (f *persistenceFixture) sideEffectReported() bool {
	for _, p := range f.progress {
		if p.SideEffect {
			return true
		}
	}
	return false
}

// installerLog records the exact argument list the trigger installer received.
const installerScript = `#!/bin/sh
printf '%s\n' "$@" > "$CIVSWITCH_TEST_ARGS"
echo 'INSTALL DONE - reboot, Load Game'
`

func newPersistenceFixture(t *testing.T, respond func(string) string) *persistenceFixture {
	t.Helper()
	root, support := t.TempDir(), t.TempDir()
	m := &manifest.Manifest{}
	for id, file := range map[string]string{
		"civswitch-hid.c":      "civswitch-hid.c",
		"civswitch-payload":    "integrations/civswitch/payload.txt",
		backupPayloadComponent: "integrations/civswitch/backup-single.js",
		installerComponent:     "vendor/CivSwitch/installer-creator/install.py",
	} {
		data := "fixture " + id
		if err := os.MkdirAll(filepath.Join(root, filepath.Dir(file)), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, file), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		m.Components = append(m.Components, manifest.Component{ID: id, Path: file,
			SHA256:       fmt.Sprintf("%x", sha256.Sum256([]byte(data))),
			Distribution: manifest.Distribution{Decision: "blocked", Reason: "fixture distribution blocker"}})
	}
	python := filepath.Join(t.TempDir(), "python3")
	if err := os.WriteFile(python, []byte(installerScript), 0o700); err != nil {
		t.Fatal(err)
	}
	args := filepath.Join(support, "installer-args")
	t.Setenv("CIVSWITCH_TEST_ARGS", args)
	loader := newFakeLoader(t, respond)
	adapter := New(Options{Manifest: m, DeveloperSourceRoot: root, Support: support, DeveloperInstallerPython: python})
	adapter.loader = loader.listener.Addr().String()
	return &persistenceFixture{adapter: adapter, loader: loader, support: support, installer: args}
}

// sent returns the code the host sent, without the loader challenges that every
// dependent action performs first.
func (f *persistenceFixture) sent(t *testing.T) []string {
	t.Helper()
	var code []string
	for _, line := range f.loader.sent() {
		if _, challenge := challengeMarker(line); !challenge {
			code = append(code, line)
		}
	}
	return code
}

func (f *persistenceFixture) installerArgs(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(f.installer)
	if err != nil {
		return nil
	}
	return strings.Fields(strings.TrimSpace(string(data)))
}

// consoleBackup scripts the upstream save-backup frames of backup-single.js.
func consoleBackup(files map[string]string, corrupt bool) func(string) string {
	return func(string) string {
		var out strings.Builder
		frame := func(kind string, value any) {
			data, _ := json.Marshal(value)
			fmt.Fprintf(&out, "MAC2JB_CIV_BACKUP %s %s\n", kind, data)
		}
		frame("BEGIN", map[string]any{"version": 1, "directory": "save:/saves/Single", "count": len(files), "chunkSize": 512})
		for name, content := range files {
			frame("FILE", map[string]any{"nameHex": hex.EncodeToString([]byte(name)), "size": len(content)})
			for offset := 0; offset < len(content); offset += 512 {
				end := min(offset+512, len(content))
				frame("CHUNK", map[string]any{"offset": offset, "hex": hex.EncodeToString([]byte(content[offset:end]))})
			}
			sum := fnv.New32a()
			io.WriteString(sum, content)
			checksum := fmt.Sprintf("%08x", sum.Sum32())
			if corrupt {
				checksum = "deadbeef"
			}
			frame("FILE_END", map[string]any{"size": len(content), "fnv32": checksum})
		}
		frame("END", map[string]any{"count": len(files)})
		out.WriteString(executionConfirmation + "\n")
		return out.String()
	}
}

func retained(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		files[entry.Name()] = string(data)
	}
	return files
}

func TestBootTriggerInstallNeedsConsentLiveLoaderAndVerifiedBackup(t *testing.T) {
	saves := map[string]string{"XSTRIG.Civ7Save": strings.Repeat("CIV7 save bytes ", 100)}
	t.Run("unconfirmed save replacement", func(t *testing.T) {
		f := newPersistenceFixture(t, consoleBackup(saves, false))
		if _, err := f.adapter.InstallBootTrigger(context.Background(), "192.0.2.2", false, f.report); err == nil {
			t.Fatal("installed the persistent trigger without confirming that it replaces the game's saves")
		}
		if len(f.loader.sent()) != 0 || f.installerArgs(t) != nil {
			t.Fatalf("touched the console before confirmation: %v", f.loader.sent())
		}
	})
	t.Run("unverified backup", func(t *testing.T) {
		f := newPersistenceFixture(t, consoleBackup(saves, true))
		if _, err := f.adapter.InstallBootTrigger(context.Background(), "192.0.2.2", true, f.report); err == nil {
			t.Fatal("ran the installer after an unverified backup")
		}
		if f.installerArgs(t) != nil {
			t.Fatal("the installer replaced saves that were not backed up")
		}
		if f.sideEffectReported() {
			t.Fatal("reported a console side effect for a failed read-only backup")
		}
		if entries, err := os.ReadDir(filepath.Join(f.support, "civswitch", "backups")); err == nil && len(entries) != 0 {
			t.Fatalf("kept an unverified backup: %v", entries[0].Name())
		}
	})
	t.Run("verified backup then install", func(t *testing.T) {
		f := newPersistenceFixture(t, consoleBackup(saves, false))
		backup, err := f.adapter.InstallBootTrigger(context.Background(), "192.0.2.2", true, f.report)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(backup, filepath.Join(f.support, "civswitch", "backups")) {
			t.Fatalf("backup is not retained outside the session workspace: %s", backup)
		}
		if got := retained(t, filepath.Join(backup, "files")); got["XSTRIG.Civ7Save"] != saves["XSTRIG.Civ7Save"] {
			t.Fatalf("backup contents: %q", got)
		}
		var record struct {
			Files []struct{ Name, SHA256 string }
		}
		data, err := os.ReadFile(filepath.Join(backup, "manifest.json"))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &record); err != nil {
			t.Fatal(err)
		}
		digest := fmt.Sprintf("%x", sha256.Sum256([]byte(saves["XSTRIG.Civ7Save"])))
		if len(record.Files) != 1 || record.Files[0].Name != "XSTRIG.Civ7Save" || record.Files[0].SHA256 != digest {
			t.Fatalf("backup manifest: %+v", record)
		}
		if args := f.installerArgs(t); len(args) != 2 || !strings.HasSuffix(args[0], "install.py") || args[1] != "192.0.2.2" {
			t.Fatalf("installer arguments: %v", args)
		}
		if !f.sideEffectReported() {
			t.Fatal("installing the trigger did not report a console side effect")
		}
	})
	t.Run("unconfirmed install", func(t *testing.T) {
		f := newPersistenceFixture(t, consoleBackup(saves, false))
		if err := os.WriteFile(filepath.Join(filepath.Dir(f.adapter.options.DeveloperInstallerPython), "python3"),
			[]byte("#!/bin/sh\necho 'installer crashed'\nexit 1\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := f.adapter.InstallBootTrigger(context.Background(), "192.0.2.2", true, f.report); err == nil {
			t.Fatal("an unconfirmed installer run was reported as installed")
		}
	})
	t.Run("loader not answering", func(t *testing.T) {
		f := newPersistenceFixture(t, consoleBackup(saves, false))
		f.loader.listener.Close()
		if _, err := f.adapter.InstallBootTrigger(context.Background(), "192.0.2.2", true, f.report); err == nil {
			t.Fatal("installed without validating the loader connection")
		}
		if f.installerArgs(t) != nil {
			t.Fatal("ran the installer without a live loader")
		}
	})
}

func TestBackupRefusesUnsafeAndUnexecutedLoaderOutput(t *testing.T) {
	cases := map[string]func(string) string{
		"escaping filename": consoleBackup(map[string]string{"../../../etc/passwd": "x"}, false),
		"unknown frame": func(string) string {
			return "MAC2JB_CIV_BACKUP SHELL {\"command\":\"rm -rf /\"}\n" + executionConfirmation + "\n"
		},
		"missing execution": consoleBackupWithoutConfirmation(map[string]string{"XSTRIG.Civ7Save": "CIV7"}),
		"console error": func(string) string {
			return "MAC2JB_CIV_BACKUP ERROR {\"message\":\"save directory changed during backup\"}\n"
		},
		"incomplete backup": func(string) string {
			return "MAC2JB_CIV_BACKUP BEGIN {\"version\":1,\"directory\":\"save:/saves/Single\",\"count\":2,\"chunkSize\":512}\n" + executionConfirmation + "\n"
		},
		"unexpected chunk": func(string) string {
			return "MAC2JB_CIV_BACKUP CHUNK {\"offset\":0,\"hex\":\"00\"}\n" + executionConfirmation + "\n"
		},
	}
	for name, respond := range cases {
		t.Run(name, func(t *testing.T) {
			f := newPersistenceFixture(t, respond)
			if _, err := f.adapter.InstallBootTrigger(context.Background(), "192.0.2.2", true, f.report); err == nil {
				t.Fatal("accepted an unusable backup response")
			}
			if f.installerArgs(t) != nil {
				t.Fatal("the installer ran without a verified backup")
			}
			if entries, err := os.ReadDir(filepath.Join(f.support, "civswitch", "backups")); err == nil && len(entries) != 0 {
				t.Fatalf("retained an unverified backup: %v", entries[0].Name())
			}
		})
	}
}

// A hostile filename is stored as literal bytes inside the backup directory and
// never reaches a shell or the installer's arguments.
func TestLoaderFilenamesAreNeverInterpolatedIntoCommands(t *testing.T) {
	name := "$(touch pwned).Civ7Save"
	f := newPersistenceFixture(t, consoleBackup(map[string]string{name: "CIV7 data"}, false))
	backup, err := f.adapter.InstallBootTrigger(context.Background(), "192.0.2.2", true, f.report)
	if err != nil {
		t.Fatal(err)
	}
	if got := retained(t, filepath.Join(backup, "files")); got[name] != "CIV7 data" {
		t.Fatalf("hostile filename was not stored literally: %q", got)
	}
	if _, err := os.Lstat(filepath.Join(f.support, "pwned")); err == nil {
		t.Fatal("loader output was executed by a shell")
	}
	args := f.installerArgs(t)
	for _, arg := range args {
		if strings.Contains(arg, "touch") || strings.Contains(arg, "Civ7Save") {
			t.Fatalf("loader output reached the installer command: %v", args)
		}
	}
	if len(args) != 2 {
		t.Fatalf("installer arguments: %v", args)
	}
}

func consoleBackupWithoutConfirmation(files map[string]string) func(string) string {
	complete := consoleBackup(files, false)
	return func(code string) string {
		return strings.TrimSuffix(complete(code), executionConfirmation+"\n")
	}
}
