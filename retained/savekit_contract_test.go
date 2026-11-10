package retained_test

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/thedavidweng/any2jb/savekit"
)

// The structured fixture cannot catch drift between the Go request and the
// real host's schema, so this drives the real host module's validator and
// preview check with exactly what the Go client sends.
func TestRealHostAcceptsGoMutationRequestsAndPreviews(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	host, err := filepath.Abs("savekit-host.cjs")
	if err != nil {
		t.Fatal(err)
	}
	source, preview := writeSourceFolder(t, map[string]string{
		"b.bin": "b\n", "dir/a.bin": "a\n", "Z.bin": "z\n", "é.bin": "e\n"})
	restoreSource := exportFolder(t, map[string]string{"savegame.bin": "restored\n"})
	restorePreview, err := savekit.PreviewRestore(restoreSource)
	if err != nil {
		t.Fatal(err)
	}
	remove := savekit.PreviewDelete("savegame.bin")
	cases := []struct {
		request savekit.Request
		files   []savekit.ExportFile
	}{
		{savekit.Request{Action: "write", TitleID: writeTitle, UID: writeUID, SourceFolder: source,
			AllowReplace: true, Confirm: true, PreviewDigest: preview.Digest}, preview.Files},
		{savekit.Request{Action: "restore", TitleID: writeTitle, UID: writeUID, SourceFolder: restoreSource,
			Archive: &savekit.Archive{Space: 1, Type: 3}, Confirm: true, PreviewDigest: restorePreview.Digest}, restorePreview.Files},
		{savekit.Request{Action: "delete", TitleID: writeTitle, UID: writeUID, FileName: "savegame.bin",
			Confirm: true, PreviewDigest: remove.Digest}, nil},
	}
	for _, c := range cases {
		if err := c.request.Validate(); err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(c.request)
		if err != nil {
			t.Fatal(err)
		}
		type file struct {
			Path   string `json:"path"`
			Size   int64  `json:"size"`
			SHA256 string `json:"sha256"`
		}
		files := []file{}
		for _, f := range c.files {
			files = append(files, file{f.Path, f.Size, f.SHA256})
		}
		staged, err := json.Marshal(files)
		if err != nil {
			t.Fatal(err)
		}
		script := `const h = require(process.argv[1]);
const parsed = h.request(JSON.parse(process.argv[2]));
if (parsed.action === 'delete') h.checkPreview(null, parsed.previewDigest, parsed.fileName);
else h.checkPreview(JSON.parse(process.argv[3]).reverse(), parsed.previewDigest);`
		out, err := exec.Command(node, "-e", script, host, string(encoded), string(staged)).CombinedOutput()
		if err != nil {
			t.Fatalf("%s: the real host refused the Go request: %v\n%s", c.request.Action, err, out)
		}
	}
}
