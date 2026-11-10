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
	forbidden := regexp.MustCompile(`"(csrutil|spctl|nvram|bputil|sudo|kextload|systemextensionsctl|/usr/bin/csrutil|/usr/sbin/spctl)"`)
	kmutil := regexp.MustCompile(`"/usr/bin/kmutil"[^\n]*`)
	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && path != ".." && (strings.HasPrefix(d.Name(), ".") || d.Name() == "build" || d.Name() == "vendor") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if m := forbidden.Find(data); m != nil {
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
