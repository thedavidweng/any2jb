package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDeveloperLaunchConfigIsExplicitAndUnavailableInPackages(t *testing.T) {
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
	config, err := loadDeveloperLaunch(path, true)
	if err != nil || config.SourceRoot != root || config.InstallerPython != python {
		t.Fatal(config, err)
	}
	if c, err := loadDeveloperLaunch(path, false); err != nil || c.SourceRoot != "" {
		t.Fatal("packaged build read developer launch", c, err)
	}
	if _, err := loadDeveloperLaunch(filepath.Join(root, "missing.json"), true); err == nil {
		t.Fatal("missing config accepted")
	}
	if err := os.WriteFile(path, []byte(`{"sourceRoot":"relative","assets":"/assets"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadDeveloperLaunch(path, true); err == nil {
		t.Fatal("relative developer root accepted")
	}
	if _, err := loadDeveloperLaunch("relative.json", true); err == nil {
		t.Fatal("relative developer config accepted")
	}
}
