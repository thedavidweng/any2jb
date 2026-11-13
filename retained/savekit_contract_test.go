package retained_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/thedavidweng/any2jb/engine"
	"github.com/thedavidweng/any2jb/savekit"
)

func TestHeldProofContractAcrossGoAndBothHosts(t *testing.T) {
	runner, run, _ := heldSession(t)
	workspace := mustWorkspace(t, runner, run)
	resultPath := filepath.Join(workspace, "result.json")
	original, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatal(err)
	}
	toolHost, _ := filepath.Abs("../resources/runtime/toolhost.cjs")
	saveHost, _ := filepath.Abs("savekit-host.cjs")
	for _, tc := range []struct {
		name   string
		change func(map[string]any)
		valid  bool
	}{
		{"held", func(map[string]any) {}, true},
		{"unverified", func(r map[string]any) { r["verified"] = false }, false},
		{"completed", func(r map[string]any) { r["completed"] = "fixture" }, false},
		{"cleanup errors", func(r map[string]any) { r["cleanupErrors"] = []string{"fixture"} }, false},
		{"already cleaned", func(r map[string]any) { r["fs"].(map[string]any)["cleanup"].(map[string]any)["cleanedUp"] = true }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var result map[string]any
			if err := json.Unmarshal(original, &result); err != nil {
				t.Fatal(err)
			}
			tc.change(result)
			body, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(resultPath, body, 0600); err != nil {
				t.Fatal(err)
			}
			outcome, err := engine.VerifyHeld(context.Background(), workspace)
			if got := err == nil && outcome.Valid(true); got != tc.valid {
				t.Errorf("Go held proof = %t, want %t: %v", got, tc.valid, err)
			}
			script := `Promise.all([process.argv[1],process.argv[2]].map(async file=>{try{await require(file).heldProof(process.argv[3]);return true;}catch{return false;}})).then(value=>console.log(JSON.stringify(value)));`
			output, err := exec.Command(nodePath(t), "-e", script, toolHost, saveHost, workspace).CombinedOutput()
			if err != nil {
				t.Fatalf("host proof probe: %v %s", err, output)
			}
			var results []bool
			if err := json.Unmarshal(output, &results); err != nil {
				t.Fatal(err)
			}
			if len(results) != 2 || results[0] != tc.valid || results[1] != tc.valid {
				t.Errorf("JS held proofs = %v, want both %t", results, tc.valid)
			}
		})
	}
}

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
