package savekit_test

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/thedavidweng/any2jb/internal/savekit"
)

func client(t *testing.T, scenario string) savekit.Client {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node is required for the structured SaveKit fixture")
	}
	module, err := filepath.Abs("testdata/fake-savekit-host.cjs")
	if err != nil {
		t.Fatal(err)
	}
	return savekit.Client{Node: node, Module: module, Scenario: scenario}
}

func TestClientListsAccountsTitlesAndInventory(t *testing.T) {
	run := t.TempDir()
	c := client(t, "ok")
	accounts, err := c.Run(context.Background(), run, "", savekit.Request{Action: "accounts"})
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts.Users) != 2 {
		t.Fatalf("accounts: %+v", accounts)
	}
	titles, err := c.Run(context.Background(), run, "", savekit.Request{Action: "titles"})
	if err != nil {
		t.Fatal(err)
	}
	if len(titles.Titles) != 2 {
		t.Fatalf("titles: %+v", titles)
	}
	inventory, err := c.Run(context.Background(), run, "", savekit.Request{Action: "inventory"})
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.Inventory) != 2 || inventory.Inventory[0].TitleID != "010012900d38a000" {
		t.Fatalf("inventory: %+v", inventory)
	}
	// An archive from the inventory selects its own title, account and archive.
	entry := inventory.Inventory[1]
	inspect, err := c.Run(context.Background(), run, "", savekit.Request{
		Action:  "inspect",
		TitleID: entry.TitleID, UID: entry.UID, Archive: &entry.Archive,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(inspect.Output, "/savegame.bin") {
		t.Fatalf("inspect output: %q", inspect.Output)
	}
}

func TestClientProtocolErrorStopsAndReapsChild(t *testing.T) {
	c := client(t, "protocol-hang")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	started := time.Now()
	_, err := c.Run(ctx, t.TempDir(), "", savekit.Request{Action: "accounts"})
	if err == nil || !strings.Contains(err.Error(), "protocol") {
		t.Fatalf("protocol error not reported: %v", err)
	}
	if time.Since(started) > 2*time.Second {
		t.Fatal("protocol failure waited for context cancellation instead of stopping the child")
	}
}

func TestClientBackupKeepsAVerifiedExport(t *testing.T) {
	run := t.TempDir()
	destination := filepath.Join(t.TempDir(), "export")
	c := client(t, "ok")
	result, err := c.Run(context.Background(), run, destination, savekit.Request{
		Action:  "backup",
		TitleID: "010012900d38a000", UID: "11111111111111111111111111111111",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Path != destination {
		t.Fatalf("the export moved: %q", result.Path)
	}
	if err := savekit.VerifyExport(destination); err != nil {
		t.Fatal(err)
	}
}

func TestClientRejectsAForeignExportPathAndProtocolDrift(t *testing.T) {
	run := t.TempDir()
	c := client(t, "foreign-path")
	_, err := c.Run(context.Background(), run, filepath.Join(t.TempDir(), "export"),
		savekit.Request{Action: "backup", TitleID: "010012900d38a000", UID: "11111111111111111111111111111111"})
	if err == nil {
		t.Fatal("an export outside the chosen destination was accepted")
	}
	c = client(t, "write-action")
	_, err = c.Run(context.Background(), run, "", savekit.Request{Action: "accounts"})
	if err == nil {
		t.Fatal("a host attempting a write action was accepted")
	}
	c = client(t, "bad-sequence")
	if _, err = c.Run(context.Background(), run, "", savekit.Request{Action: "accounts"}); err == nil {
		t.Fatal("out-of-order events were accepted")
	}
}
