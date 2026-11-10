package manifest_test

import (
	"os"
	"testing"

	"github.com/thedavidweng/any2jb/manifest"
)

// Optional source check, not a console or hardware test. The ordinary tests
// remain self-contained; this checks the exact pinned mac2jb reference tree.
func TestPinnedSourceInventory(t *testing.T) {
	root := os.Getenv("ANY2JB_REFERENCE_ROOT")
	if root == "" {
		t.Skip("set ANY2JB_REFERENCE_ROOT to the pinned mac2jb checkout")
	}
	m, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range m.Profiles {
		t.Run(p.ID, func(t *testing.T) {
			if err := m.VerifyProfile(root, p.ID); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, c := range m.Components {
		if c.Upstream != "https://github.com/cpypwn/mac2jb" && c.ID != "browser-research" {
			continue
		}
		t.Run(c.ID, func(t *testing.T) {
			if err := m.Verify(root, c.ID); err != nil {
				t.Fatal(err)
			}
		})
	}
}
