package engine_test

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/thedavidweng/any2jb/internal/engine"
	"github.com/thedavidweng/any2jb/internal/manifest"
	"github.com/thedavidweng/any2jb/internal/testconsole"
)

// This exercises the real upstream TCP/native-call protocol, not exploit
// execution. The peer intentionally disconnects during its first native reply.
func TestPinnedConsolePartialDisconnect(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("pinned Node protocol test requires Darwin arm64")
	}
	source, assets := os.Getenv("ANY2JB_REFERENCE_ROOT"), os.Getenv("ANY2JB_RUNTIME_ASSETS")
	if source == "" || assets == "" {
		t.Skip("real engine TCP suite requires ANY2JB_REFERENCE_ROOT and ANY2JB_RUNTIME_ASSETS (pinned Node)")
	}
	m, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	options := fixture(t)
	options.Assets, options.Manifest = assets, m
	options.DeveloperSourceRoot, options.DeveloperProfileID = source, "pepsipu"
	root, err := engine.Stage(options)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.VerifyProfile(root, "pepsipu"); err != nil {
		t.Fatal(err)
	}
	p, _ := m.Profile("pepsipu")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Use the engine's actual listener, pairing formatter and native client.
	// No pipeline fixtures or modified engine files are involved in this suite.
	script := `
const path = require('node:path');
const root = process.argv[1];
const {listen, pairingCode} = require(path.join(root, 'engine/lib/connection.cjs'));
const {NativeClient} = require(path.join(root, 'engine/lib/native.cjs'));
const {connectCommand} = require(path.join(root, 'run.cjs'));
const token = pairingCode();
(async () => {
  const connected = await listen({host:'127.0.0.1',port:39380,token,onWaiting() {
    console.log(connectCommand('127.0.0.1',token));
  }});
  const native = new NativeClient(connected.connection,connected.addresses);
  console.log('PAIRED');
  try {
    await native.call(0x5872f0,native.output,native.output,0);
    throw Error('partial native response accepted');
  } catch(error) {
    if (!/disconnected|closed/.test(error.message)) throw error;
    console.log('PARTIAL_REPLY_REJECTED');
  } finally {connected.connection.close();}
})().catch(error => {console.error(error.message);process.exitCode=1;});
`
	cmd := exec.CommandContext(ctx, filepath.Join(root, "runtime/node"), "-e", script, filepath.Join(root, p.SourceDirectory))
	var stderr strings.Builder
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			cmd.Process.Kill()
			cmd.Wait()
		}
	})
	lines := bufio.NewScanner(stdout)
	if !lines.Scan() {
		t.Fatalf("missing pairing: %s", stderr.String())
	}
	console, err := testconsole.Connect(ctx, lines.Text())
	if err != nil {
		t.Fatal(err)
	}
	defer console.Close()
	if err := console.DisconnectPartialReply(ctx); err != nil {
		t.Fatal(err)
	}
	var events []string
	for lines.Scan() {
		events = append(events, lines.Text())
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("engine: %v: %s", err, stderr.String())
	}
	if strings.Join(events, ",") != "PAIRED,PARTIAL_REPLY_REJECTED" {
		t.Fatalf("unexpected engine observations: %v", events)
	}
}
