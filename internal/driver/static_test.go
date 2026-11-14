package driver_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Static guard: app code may inspect security state but never issue a
// command that changes it. The privileged installer itself is upstream code.
func TestAppIssuesNoSecurityReducingCommand(t *testing.T) {
	kmutil := regexp.MustCompile(`"/usr/bin/kmutil"[^\n]*`)
	err := filepath.WalkDir("../..", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && path != "../.." && (strings.HasPrefix(d.Name(), ".") || d.Name() == "build" || d.Name() == "dist" || d.Name() == "node_modules" || d.Name() == "vendor") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if m := securityCommand(filepath.ToSlash(path), string(data)); m != "" {
			t.Errorf("%s issues security command %s", path, m)
		}
		for _, call := range kmutil.FindAll(data, -1) {
			if !strings.Contains(string(call), `"showloaded"`) {
				t.Errorf("%s uses kmutil beyond read-only inspection: %s", path, call)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func securityCommand(path, source string) string {
	// Only this exact, fixed read-only invocation in the readiness adapter is permitted.
	if path == "../../internal/readiness/native_darwin.go" {
		source = strings.Replace(source, `capture(ctx, "/usr/bin/csrutil", "status")`, "readOnlySIPStatus", 1)
	}
	return regexp.MustCompile(`"(?:[^"\n]*/)?(csrutil|spctl|nvram|bputil|sudo|kextload|systemextensionsctl)"`).FindString(source)
}

func TestSecurityScanOnlyAllowsOneExactSIPStatusCall(t *testing.T) {
	status := `capture(ctx, "/usr/bin/csrutil", "status")`
	if got := securityCommand("../../internal/readiness/native_darwin.go", status); got != "" {
		t.Fatal(got)
	}
	for _, source := range []string{
		`capture(ctx, "/usr/bin/csrutil", "disable")`,
		`capture(ctx, "/usr/bin/csrutil", "status", "extra")`,
		`exec.CommandContext(ctx, "/usr/bin/csrutil", "status")`,
		`capture(ctx, "csrutil", "status")`,
		`capture(ctx, "/usr/sbin/bputil", "-d")`,
		`capture(ctx, "/usr/sbin/spctl", "--status")`,
		`capture(ctx, "sudo", "anything")`,
		status + "\n" + status,
	} {
		if securityCommand("../../internal/readiness/native_darwin.go", source) == "" {
			t.Errorf("security scan allowed %s", source)
		}
	}
	if securityCommand("../../cmd/any2jb/main.go", status) == "" {
		t.Fatal("exception applies outside readiness")
	}
}
