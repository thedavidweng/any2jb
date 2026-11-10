package civswitch_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/thedavidweng/any2jb/civswitch"
	"github.com/thedavidweng/any2jb/manifest"
	"github.com/thedavidweng/any2jb/readiness"
	"github.com/thedavidweng/any2jb/session"
)

func TestCompositeHelperUsesMeasuredDryRunAndCompletedLines(t *testing.T) {
	script := `#!/bin/sh
if [ "$4" != "usb-drd0" ]; then exit 2; fi
if [ "$5" = "--dry-run" ]; then
  echo 'MAC2JB_CIVSWITCH {"type":"dry-run","total":87,"characters":21675,"estimatedMs":1299100}'
else
  echo 'MAC2JB_CIVSWITCH {"type":"ready","total":87}'
  echo 'MAC2JB_CIVSWITCH {"type":"line-start","completed":0,"total":87}'
  echo 'MAC2JB_CIVSWITCH {"type":"line-complete","completed":1,"total":87}'
  echo 'MAC2JB_CIVSWITCH {"type":"complete","completed":87,"total":87}'
fi
`
	options := helperFixture(t, script)
	a := civswitch.New(options)
	estimate, err := a.Prepare(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if estimate != 1299100*time.Millisecond {
		t.Fatal(estimate)
	}
	var completed []int
	if err := a.Transfer(context.Background(), func(p session.Progress) { completed = append(completed, p.Current) }); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(completed) != "[1 87]" {
		t.Fatal(completed)
	}
	if err := a.Release(context.Background()); err != nil {
		t.Fatal(err)
	}
	options.DeveloperSourceRoot = ""
	if err := civswitch.New(options).Availability(); err == nil {
		t.Fatal("packaged blocked path was available")
	}
}

func helperFixture(t *testing.T, script string) civswitch.Options {
	t.Helper()
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("Darwin helper fixture")
	}
	root := t.TempDir()
	m := &manifest.Manifest{Components: []manifest.Component{}}
	for id, data := range map[string]string{"civswitch-hid.c": "fixture source", "civswitch-payload": "fixture payload"} {
		if err := os.WriteFile(filepath.Join(root, id), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		m.Components = append(m.Components, manifest.Component{ID: id, Path: id, SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(data))), Distribution: manifest.Distribution{Decision: "blocked", Reason: "fixture distribution blocker"}})
	}
	helper := filepath.Join(root, "mac2jb-civswitch-hid")
	if err := os.WriteFile(helper, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	inspector := readiness.Inspector{Observe: func(context.Context) readiness.Observation {
		return readiness.Observation{Addresses: []readiness.Address{{Interface: "en0", IPv4: "192.168.1.2"}}, USBSupported: true, Driver: readiness.Driver{Present: true, Loaded: true}, Controllers: []string{"usb-drd0"}}
	}}
	return civswitch.Options{Manifest: m, DeveloperSourceRoot: root, DeveloperHelperSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(script))), Inspector: &inspector}
}

func TestCompositeHelperCancellationWaitsForNeutralReportConfirmation(t *testing.T) {
	options := helperFixture(t, `#!/bin/sh
trap 'echo '\''MAC2JB_CIVSWITCH {"type":"cancelled"}'\''; exit 130' TERM
echo 'MAC2JB_CIVSWITCH {"type":"line-complete","completed":1,"total":87}'
while :; do :; done
`)
	a := civswitch.New(options)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := a.Transfer(ctx, func(session.Progress) { cancel() })
	if err == nil {
		t.Fatal("cancelled helper reported success")
	}
	if err := a.Release(context.Background()); err != nil {
		t.Fatalf("helper cancellation did not confirm release: %v", err)
	}
}

func TestCompositeHelperRefusesTamperedAssetsAndFalseCompletion(t *testing.T) {
	options := helperFixture(t, "#!/bin/sh\necho 'MAC2JB_CIVSWITCH {\"type\":\"complete\",\"completed\":1,\"total\":87}'\n")
	a := civswitch.New(options)
	if err := a.Transfer(context.Background(), func(session.Progress) {}); err == nil {
		t.Fatal("false completion accepted")
	}
	if err := a.Release(context.Background()); err == nil {
		t.Fatal("release was assumed from process exit")
	}
	options.DeveloperHelperSHA256 = ""
	if err := civswitch.New(options).Transfer(context.Background(), func(session.Progress) {}); err == nil {
		t.Fatal("unverified binary executed")
	}
	if err := os.WriteFile(filepath.Join(options.DeveloperSourceRoot, "civswitch-payload"), []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Prepare(context.Background()); err == nil {
		t.Fatal("tampered payload accepted")
	}
}
