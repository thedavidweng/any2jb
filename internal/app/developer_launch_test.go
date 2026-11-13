package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thedavidweng/any2jb/internal/devlaunch"
)

func validDeveloperConfig(t *testing.T) (string, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "developer.json")
	root := t.TempDir()
	python := filepath.Join(root, "python")
	if err := os.WriteFile(python, []byte("fixture"), 0700); err != nil {
		t.Fatal(err)
	}
	data := `{"sourceRoot":"` + root + `","assets":"` + root + `","installerPython":"` + python + `"}`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	return path, root
}

func TestDeveloperLaunchConfigIsExplicitAndValidated(t *testing.T) {
	path, root := validDeveloperConfig(t)
	if !devlaunch.Allowed() {
		t.Skip("developer launch is closed in this build")
	}
	config, err := devlaunch.Load(path)
	if err != nil || config.SourceRoot != root {
		t.Fatal(config, err)
	}
	if _, err := devlaunch.Load(filepath.Join(root, "missing.json")); err == nil {
		t.Fatal("missing config accepted")
	}
	if err := os.WriteFile(path, []byte(`{"sourceRoot":"relative","assets":"/assets"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := devlaunch.Load(path); err == nil {
		t.Fatal("relative developer root accepted")
	}
	if _, err := devlaunch.Load("relative.json"); err == nil {
		t.Fatal("relative developer config accepted")
	}
}

// TestProductionBuildIgnoresDeveloperLaunch reads the gate from a binary built
// with the release ldflag: it must ignore a valid developer configuration
// rather than stage it.
func TestProductionBuildIgnoresDeveloperLaunch(t *testing.T) {
	path, _ := validDeveloperConfig(t)
	binary := filepath.Join(t.TempDir(), "devlaunch-probe")
	build := exec.Command("go", "build", "-ldflags",
		"-X github.com/egoist/mygo.production=1", "-o", binary, "./testdata/devlaunch")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build production probe: %v\n%s", err, out)
	}
	cmd := exec.Command(binary)
	cmd.Env = append(os.Environ(), "ANY2JB_DEVELOPER_CONFIG="+path, "MYGO_ENV=production")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("production probe: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "not staged" {
		t.Fatalf("a production build staged the developer launch: %q", got)
	}
}

// TestDevelopmentBuildHonoursDeveloperLaunch reads the same gate from a binary
// built without the release ldflag, so the probe cannot pass by always refusing.
func TestDevelopmentBuildHonoursDeveloperLaunch(t *testing.T) {
	path, root := validDeveloperConfig(t)
	binary := filepath.Join(t.TempDir(), "devlaunch-probe")
	if out, err := exec.Command("go", "build", "-o", binary, "./testdata/devlaunch").CombinedOutput(); err != nil {
		t.Fatalf("build development probe: %v\n%s", err, out)
	}
	cmd := exec.Command(binary)
	cmd.Env = append(os.Environ(), "ANY2JB_DEVELOPER_CONFIG="+path, "MYGO_ENV=development")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("development probe: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "staged "+root {
		t.Fatalf("a development build ignored the developer launch: %q", got)
	}
}
