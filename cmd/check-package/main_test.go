package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildHookAndResources(t *testing.T) {
	t.Chdir("../..")
	old := os.Args
	os.Args = []string{"check-package"}
	t.Cleanup(func() { os.Args = old })
	data, err := os.ReadFile("mygo.json")
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		BuildCommand string `json:"buildCommand"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	if config.BuildCommand != "go run ./cmd/prepare-runtime && go run ./cmd/check-package" {
		t.Fatalf("build must run the package gate: %q", config.BuildCommand)
	}
	if err := check(); err != nil && !(errors.Is(err, os.ErrNotExist) && strings.Contains(err.Error(), "node-darwin-arm64")) {
		t.Fatal(err)
	}
}

func TestBuildCannotAddUncheckedResourceRoutes(t *testing.T) {
	for _, config := range []string{
		`{"resources":["unchecked"]}`, `{"frontendDist":"unchecked"}`, `{"icon":"unchecked.png"}`,
	} {
		t.Run(config, func(t *testing.T) {
			root := t.TempDir()
			t.Chdir(root)
			old := os.Args
			os.Args = []string{"check-package"}
			t.Cleanup(func() { os.Args = old })
			if err := os.WriteFile(filepath.Join(root, "mygo.json"), []byte(config), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := check(); err == nil || !strings.Contains(err.Error(), "extra resources") {
				t.Fatalf("unchecked resource route not rejected at config boundary: %v", err)
			}
		})
	}
}
