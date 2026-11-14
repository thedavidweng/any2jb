package civswitch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thedavidweng/any2jb/internal/manifest"
	"github.com/thedavidweng/any2jb/internal/session"
)

func TestBuildStageHexInflatesExactInNodeVM(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node required for VM inflate verification")
	}
	core := []byte("const greeting = 'hello civswitch';\n// non-ASCII comment: 测试\nawait log(greeting);\n")
	stageHex, want, err := buildStageHex(core)
	if err != nil {
		t.Fatal(err)
	}
	stageBytes, err := hex.DecodeString(stageHex)
	if err != nil {
		t.Fatal(err)
	}
	script := `
const vm = require('node:vm');
const stage = Buffer.from(process.argv[1], 'hex').toString('latin1');
const sb = { console };
vm.createContext(sb);
vm.runInContext('var __o; eval = x => __o = x;\n' + stage, sb, { timeout: 10000 });
process.stdout.write(sb.__o);
`
	if len(stageBytes) == 0 {
		t.Fatal("empty stage")
	}
	out, err := exec.Command(node, "-e", script, stageHex).Output()
	if err != nil {
		t.Fatalf("node vm decode failed: %v", err)
	}
	if string(out) != want {
		t.Fatalf("inflated payload mismatch:\ngot:  %q\nwant: %q", string(out), want)
	}
}

func TestPackagedNativeInstallerWritesTriggerWithoutPython(t *testing.T) {
	saves := map[string]string{"XSTRIG.Civ7Save": strings.Repeat("CIV7 save bytes ", 100)}
	backupResponder := consoleBackup(saves, false)
	loader := newFakeLoader(t, func(code string) string {
		switch {
		case strings.Contains(code, "STARTING INSTALLER"):
			return executionConfirmation + "\n"
		case strings.Contains(code, "window.__tpl=window.__tpl||[]"):
			return executionConfirmation + "\n"
		case strings.Contains(code, "INSTALL DONE - reboot, Load Game"):
			return installConfirmation + "\n" + executionConfirmation + "\n"
		default:
			return backupResponder(code)
		}
	})
	assets := t.TempDir()
	support := t.TempDir()
	m := &manifest.Manifest{}
	files := map[string]struct{ path, body string }{
		"civswitch-payload":    {"payload.txt", "fixture payload"},
		backupPayloadComponent: {"backup.js", "fixture backup"},
		coreComponent:          {"core.js", "await log('core ok');"},
		saveTemplateComponent:  {"save_template.bin", "CIV7" + strings.Repeat("T", 5000)},
	}
	for id, item := range files {
		full := filepath.Join(assets, item.path)
		if err := os.WriteFile(full, []byte(item.body), 0o600); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256([]byte(item.body))
		m.Components = append(m.Components, manifest.Component{
			ID:           id,
			Path:         item.path,
			SHA256:       fmt.Sprintf("%x", sum),
			Distribution: manifest.Distribution{Decision: "may-ship", Bundled: true},
		})
	}
	adapter := New(Options{Manifest: m, Assets: assets, Support: support})
	adapter.loader = loader.listener.Addr().String()
	sideEffect := false
	backup, err := adapter.InstallBootTrigger(context.Background(), "192.0.2.2", true, func(p session.Progress) {
		sideEffect = sideEffect || p.SideEffect
	})
	if err != nil {
		t.Fatalf("packaged native trigger install failed: %v", err)
	}
	if !sideEffect || backup == "" {
		t.Fatalf("missing side effect or backup path: sideEffect=%v backup=%q", sideEffect, backup)
	}
}
