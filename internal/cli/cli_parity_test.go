package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/thedavidweng/any2jb/internal/parity"
	"github.com/thedavidweng/any2jb/locales"
)

// TestCLIParity keeps every GUI control reachable from the command line: each
// control has a docs/cli-parity.md row, and each row's command and flags exist.
func TestCLIParity(t *testing.T) {
	repoRoot := filepath.Join("..", "..")
	data, err := os.ReadFile(filepath.Join(repoRoot, "docs", "cli-parity.md"))
	if err != nil {
		t.Fatal(err)
	}
	controls, err := parity.GUIControls(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	root := newCommandLine(strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}, false).root()
	rows := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "| ") || strings.HasPrefix(line, "| UI key ") || strings.HasPrefix(line, "| ---") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		if len(cells) != 2 {
			t.Fatalf("row needs a UI key and a CLI cell: %s", line)
		}
		key, cli := strings.TrimSpace(cells[0]), strings.TrimSpace(cells[1])
		if rows[key] {
			t.Errorf("duplicate row %q", key)
		}
		rows[key] = true
		if _, ok := controls[key]; !ok {
			t.Errorf("%s: no such GUI control", key)
		}
		if reason, na := strings.CutPrefix(cli, "n/a: "); na {
			if strings.TrimSpace(reason) == "" {
				t.Errorf("%s: n/a needs a reason", key)
			}
			continue
		}
		if err := cliPath(root, strings.Trim(cli, "`")); err != "" {
			t.Errorf("%s: %s", key, err)
		}
	}
	for key, position := range controls {
		if !rows[key] {
			t.Errorf("%s (%s) has no command-line equivalent in docs/cli-parity.md", key, position)
		}
	}
}

// TestCLICommandsLocalized requires every command's description in both
// catalogs, so a new command cannot ship untranslated.
func TestCLICommandsLocalized(t *testing.T) {
	root := newCommandLine(strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}, false).root()
	var walk func(*cobra.Command)
	walk = func(parent *cobra.Command) {
		for _, cmd := range parent.Commands() {
			key := cliCommandKey(cmd)
			en, zh := locales.Catalog.Format("en", key, nil), locales.Catalog.Format("zh-CN", key, nil)
			if en == "" || en == key || zh == en {
				t.Errorf("%s: %s needs an English message and its own zh-CN translation", cmd.CommandPath(), key)
			}
			walk(cmd)
		}
	}
	walk(root)
}

func cliPath(root *cobra.Command, path string) string {
	cmd := root
	for _, word := range strings.Fields(path) {
		if name, ok := strings.CutPrefix(word, "--"); ok {
			if cmd.Flags().Lookup(name) == nil && cmd.InheritedFlags().Lookup(name) == nil {
				return cmd.CommandPath() + " has no --" + name
			}
			continue
		}
		var next *cobra.Command
		for _, child := range cmd.Commands() {
			if child.Name() == word {
				next = child
			}
		}
		if next == nil {
			return cmd.CommandPath() + " has no subcommand " + word
		}
		cmd = next
	}
	return ""
}
