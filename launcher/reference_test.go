package launcher_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thedavidweng/any2jb/launcher"
	"github.com/thedavidweng/any2jb/manifest"
)

// Optional source check against the pinned mac2jb clone, not a console test.
// It proves every recorded package verifies, unpacks and previews exactly the
// files it records, including its own core files.
func TestPinnedLauncherPackagesPrepare(t *testing.T) {
	root := os.Getenv("ANY2JB_REFERENCE_ROOT")
	if root == "" {
		t.Skip("set ANY2JB_REFERENCE_ROOT to the pinned mac2jb checkout")
	}
	m, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range launcher.IDs {
		c, err := m.ToolComponent(id)
		if err != nil {
			t.Fatal(err)
		}
		if c.Launcher.FullGame {
			// The full-game patch needs the user's own verified full-game
			// export, which no fixture or clone can supply.
			continue
		}
		t.Run(id, func(t *testing.T) {
			dest := filepath.Join(t.TempDir(), "save")
			preview, err := launcher.Prepare(context.Background(), m, c, root, dest, "", "")
			if err != nil {
				t.Fatal(err)
			}
			if len(preview.Files) != c.Launcher.Files {
				t.Fatalf("previewed %d files, recorded %d", len(preview.Files), c.Launcher.Files)
			}
			present := map[string]bool{}
			for _, file := range preview.Files {
				present[strings.TrimPrefix(file.Path, "/")] = true
			}
			for _, name := range c.Launcher.Core {
				if !present[name] {
					t.Errorf("the package is missing its core file %s", name)
				}
			}
		})
	}
}
