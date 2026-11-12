package manifest_test

import (
	"os"
	"strings"
	"testing"

	"github.com/thedavidweng/any2jb/manifest"
)

// The external projects page is for outside review, so every pinned source
// the app relies on must be named there with its exact revision.
func TestExternalProjectsDocumentListsEveryPinnedSource(t *testing.T) {
	doc, err := os.ReadFile("../docs/external-projects.md")
	if err != nil {
		t.Fatal(err)
	}
	page := string(doc)
	m, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{m.Runtime.NodeArchive}
	for _, p := range m.Profiles {
		want = append(want, p.Upstream, p.Revision)
	}
	for _, c := range m.Components {
		if strings.HasPrefix(c.Upstream, "https://") {
			want = append(want, c.Upstream, c.Revision, "`"+c.ID+"`")
		}
	}
	mod, err := os.ReadFile("../go.mod")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(mod), "\n") {
		if f := strings.Fields(line); len(f) >= 2 && strings.Contains(f[0], ".") && strings.HasPrefix(f[1], "v") {
			want = append(want, f[0], f[1])
		}
	}
	for _, s := range want {
		if !strings.Contains(page, s) {
			t.Errorf("docs/external-projects.md does not mention %q", s)
		}
	}
}
