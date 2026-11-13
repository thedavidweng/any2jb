package engine_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/thedavidweng/any2jb/engine"
	"github.com/thedavidweng/any2jb/internal/parity"
	"github.com/thedavidweng/any2jb/manifest"
	"github.com/thedavidweng/any2jb/session"
)

var updateParity = flag.Bool("update-parity", false, "rewrite testdata/mac2jb-parity.json from the pinned mac2jb reference")

const engineParityFile = "testdata/mac2jb-parity.json"

type engineParityRecord struct {
	StartCommand       string `json:"startCommand"`
	Packets            int    `json:"packets"`
	WireBytes          int    `json:"wireBytes"`
	WireSHA256         string `json:"wireSha256"`
	CopiedEngineSHA256 string `json:"copiedEngineSha256"`
	TranscriptSHA256   string `json:"transcriptSha256"`
}

type engineParityFileFormat struct {
	Format   string                        `json:"format"`
	Profiles map[string]engineParityRecord `json:"profiles"`
}

func readParityMeta(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading parity metadata %s: %v", path, err)
	}
	var meta struct {
		CopiedEngineSHA256 string `json:"copiedEngineSha256"`
	}
	if err := json.Unmarshal(raw, &meta); err != nil || len(meta.CopiedEngineSHA256) != 64 {
		t.Fatalf("invalid parity metadata %s: %v", string(raw), err)
	}
	return meta.CopiedEngineSHA256
}

func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(cur string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, cur)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unexpected non-regular file %s", cur)
		}
		data, err := os.ReadFile(cur)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm()|0o600)
	})
	if err != nil {
		t.Fatalf("copying %s -> %s: %v", src, dst, err)
	}
}

func unsealSupport(support string) {
	_ = filepath.WalkDir(support, func(name string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			_ = os.Chmod(name, 0o700)
		}
		return nil
	})
}

func runNs2jbHost(ctx context.Context, t *testing.T, node, reference, preload, seed string, p manifest.Profile) parity.EngineTranscript {
	t.Helper()
	work := t.TempDir()
	hostDir := filepath.Join(work, "host")
	copyTree(t, filepath.Join(reference, filepath.FromSlash(p.SourceDirectory)), hostDir)
	metaFile := filepath.Join(work, "meta.json")

	args := []string{"run.cjs"}
	if p.SupportsHeldRPC {
		args = append(args, "--rpc")
	}
	args = append(args, "127.0.0.1")

	stopOnStdin := "0"
	if p.SupportsHeldRPC {
		stopOnStdin = "1"
	}
	cmd := exec.CommandContext(ctx, node, args...)
	cmd.Dir = hostDir
	cmd.Env = append(os.Environ(),
		"NODE_OPTIONS=--require="+preload,
		"PARITY_SEED="+seed,
		"PARITY_META_FILE="+metaFile,
		"PARITY_STOP_ON_STDIN="+stopOnStdin,
		"NS2_BASIC_ARITY="+strconv.Itoa(p.StartArgumentCount),
	)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	type consoleOutcome struct {
		tx  parity.EngineTranscript
		err error
	}
	consoleCh := make(chan consoleOutcome, 1)
	var runFolder string
	startedConsole := false
	sentStop := false

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "V=START(") && !startedConsole {
			startedConsole = true
			startCmd := line
			go func() {
				tx, err := parity.ServeConsole(ctx, startCmd)
				consoleCh <- consoleOutcome{tx: tx, err: err}
			}()
		}
		if after, ok := strings.CutPrefix(line, "Run folder: "); ok {
			runFolder = after
		}
		if p.SupportsHeldRPC && strings.HasPrefix(line, "Stop with Ctrl-C;") && !sentStop {
			sentStop = true
			statusCmd := exec.CommandContext(ctx, node, filepath.Join(hostDir, "rpc.cjs"), runFolder, "status")
			out, err := statusCmd.CombinedOutput()
			if err != nil || !bytes.Contains(out, []byte(`"ready": true`)) && !bytes.Contains(out, []byte(`"ready":true`)) {
				t.Fatalf("ns2jb rpc.cjs status failed (%v): %s", err, string(out))
			}
			if _, err := io.WriteString(stdin, "stop\n"); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		runLog, _ := os.ReadFile(filepath.Join(runFolder, "run.log"))
		t.Fatalf("ns2jb run.cjs failed (%v): %s\nrun.log:\n%s", err, stderr.String(), string(runLog))
	}
	_ = stdin.Close()
	if !startedConsole {
		t.Fatalf("ns2jb never emitted V=START command; stderr: %s", stderr.String())
	}
	co := <-consoleCh
	if co.err != nil {
		t.Fatalf("ns2jb fake console failed: %v", co.err)
	}
	co.tx.CopiedEngineSHA256 = readParityMeta(t, metaFile)

	rawResult, err := os.ReadFile(filepath.Join(runFolder, "result.json"))
	if err != nil {
		t.Fatalf("reading ns2jb result.json: %v", err)
	}
	var res struct {
		Verified      bool  `json:"verified"`
		CleanupErrors []any `json:"cleanupErrors"`
		FS            struct {
			Verified bool `json:"verified"`
			Restored bool `json:"restored"`
			Cleanup  struct {
				CleanedUp bool `json:"cleanedUp"`
			} `json:"cleanup"`
		} `json:"fs"`
	}
	if err := json.Unmarshal(rawResult, &res); err != nil {
		t.Fatal(err)
	}
	if !res.Verified || !res.FS.Verified || !res.FS.Restored || !res.FS.Cleanup.CleanedUp || len(res.CleanupErrors) != 0 {
		t.Fatalf("ns2jb result.json did not verify cleanly: %s", string(rawResult))
	}
	return co.tx
}

func runMac2jbHost(ctx context.Context, t *testing.T, node, reference, preload, seed string, p manifest.Profile) parity.EngineTranscript {
	t.Helper()
	macRoot := t.TempDir()
	copyTree(t, filepath.Join(reference, "scripts"), filepath.Join(macRoot, "scripts"))
	copyTree(t, filepath.Join(reference, "packaging"), filepath.Join(macRoot, "packaging"))
	copyTree(t, filepath.Join(reference, filepath.FromSlash(p.SourceDirectory)), filepath.Join(macRoot, filepath.FromSlash(p.SourceDirectory)))
	bootstrapDst := filepath.Join(macRoot, filepath.FromSlash(p.Bootstrap))
	if err := os.MkdirAll(filepath.Dir(bootstrapDst), 0o700); err != nil {
		t.Fatal(err)
	}
	bootstrapBytes, err := os.ReadFile(filepath.Join(reference, filepath.FromSlash(p.Bootstrap)))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bootstrapDst, bootstrapBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	selectCmd := exec.CommandContext(ctx, node, "-e",
		"require(process.argv[1]).selectHost(process.argv[2], process.argv[3])",
		filepath.Join(macRoot, "scripts/prepare-host-profiles.cjs"), macRoot, p.ID)
	if out, err := selectCmd.CombinedOutput(); err != nil {
		t.Fatalf("selectHost(%s) failed (%v): %s", p.ID, err, string(out))
	}

	metaFile := filepath.Join(macRoot, "meta.json")
	args := []string{filepath.Join(macRoot, "scripts/gui-host.cjs"), "host", "127.0.0.1"}
	if p.SupportsHeldRPC {
		args = append(args, "--post-run-options")
	}
	cmd := exec.CommandContext(ctx, node, args...)
	cmd.Dir = macRoot
	cmd.Env = append(os.Environ(),
		"NODE_OPTIONS=--require="+preload,
		"PARITY_SEED="+seed,
		"PARITY_META_FILE="+metaFile,
	)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	type consoleOutcome struct {
		tx  parity.EngineTranscript
		err error
	}
	consoleCh := make(chan consoleOutcome, 1)
	startedConsole := false
	sawVerified := false
	var stages []int

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 4096), 256*1024)
	for scanner.Scan() {
		var ev struct {
			Type         string `json:"type"`
			StartCommand string `json:"startCommand"`
			Stage        string `json:"stage"`
			Completed    int    `json:"completed"`
			Ready        bool   `json:"ready"`
			Message      string `json:"message"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			t.Fatalf("invalid mac2jb JSON event %q: %v", scanner.Text(), err)
		}
		switch ev.Type {
		case "host-ready":
			if startedConsole {
				t.Fatal("duplicate mac2jb host-ready")
			}
			startedConsole = true
			startCmd := ev.StartCommand
			go func() {
				tx, err := parity.ServeConsole(ctx, startCmd)
				consoleCh <- consoleOutcome{tx: tx, err: err}
			}()
		case "host-stage":
			if ev.Stage == "pipeline" {
				stages = append(stages, ev.Completed+1)
			}
		case "post-run-ready":
			if _, err := io.WriteString(stdin, "{\"action\":\"rpc-status\"}\n"); err != nil {
				t.Fatal(err)
			}
		case "rpc-status":
			if !ev.Ready {
				t.Fatalf("mac2jb rpc-status not ready: %s", ev.Message)
			}
			if _, err := io.WriteString(stdin, "{\"action\":\"post-run\",\"keepRPC\":false,\"dumpFirmware\":false}\n"); err != nil {
				t.Fatal(err)
			}
		case "verified":
			sawVerified = true
		case "failed":
			t.Fatalf("mac2jb reported failure: %s (stderr: %s)", ev.Message, stderr.String())
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("mac2jb gui-host failed (%v): %s", err, stderr.String())
	}
	_ = stdin.Close()
	if !startedConsole || !sawVerified {
		t.Fatalf("mac2jb incomplete session (started=%t verified=%t)", startedConsole, sawVerified)
	}
	wantStages := []int{1, 2, 3, 4}
	if p.SupportsHeldRPC {
		// Upstream run.cjs prints "4/4 verified..." after pipeline's "4/4 ...", so
		// mac2jb's gui-session.cjs emits a second stage-4 progress event.
		wantStages = []int{1, 2, 3, 4, 4}
	}
	if !slices.Equal(stages, wantStages) {
		t.Fatalf("mac2jb pipeline stages = %v, want %v", stages, wantStages)
	}
	co := <-consoleCh
	if co.err != nil {
		t.Fatalf("mac2jb fake console failed: %v", co.err)
	}
	co.tx.CopiedEngineSHA256 = readParityMeta(t, metaFile)
	return co.tx
}

func runAny2jbHost(ctx context.Context, t *testing.T, assets, reference, preload, seed string, m *manifest.Manifest, p manifest.Profile) parity.EngineTranscript {
	t.Helper()
	support := t.TempDir()
	t.Cleanup(func() { unsealSupport(support) })
	metaFile := filepath.Join(t.TempDir(), "meta.json")

	t.Setenv("NODE_OPTIONS", "--require="+preload)
	t.Setenv("PARITY_SEED", seed)
	t.Setenv("PARITY_META_FILE", metaFile)
	t.Setenv("PARITY_STOP_ON_STDIN", "")

	options := engine.Options{
		Assets:              assets,
		Support:             support,
		Version:             "parity-check",
		Manifest:            m,
		DeveloperSourceRoot: reference,
		DeveloperProfileID:  p.ID,
	}
	run := session.Run{
		ID:      "parity-" + p.ID,
		Edition: "full",
		LANIPv4: "127.0.0.1",
		Profile: p.SessionProfile(),
	}
	runner := engine.NewHostRunner(options)
	defer runner.Cleanup(context.Background(), run)

	var stages []int
	report := func(pr session.Progress) {
		if pr.Total == 4 && pr.Current > 0 {
			stages = append(stages, pr.Current)
		}
	}
	host, err := runner.Prepare(ctx, run, report)
	if err != nil {
		t.Fatalf("any2jb Prepare(%s): %v", p.ID, err)
	}
	details, err := engine.PairingFor(host, run)
	if err != nil {
		t.Fatalf("any2jb PairingFor(%s): %v", p.ID, err)
	}

	type consoleOutcome struct {
		tx  parity.EngineTranscript
		err error
	}
	consoleCh := make(chan consoleOutcome, 1)
	go func() {
		tx, err := parity.ServeConsole(ctx, details.Command)
		consoleCh <- consoleOutcome{tx: tx, err: err}
	}()

	outcome, err := runner.Run(ctx, run, host, report)
	if err != nil {
		t.Fatalf("any2jb Run(%s): %v", p.ID, err)
	}
	if !slices.Equal(stages, []int{1, 2, 3, 4}) {
		t.Fatalf("any2jb stages = %v, want [1 2 3 4]", stages)
	}
	if p.SupportsHeldRPC {
		if !outcome.Verified || !outcome.FSVerified || !outcome.Restored || !outcome.RetainedReady ||
			!outcome.CleanupDeferred || !outcome.DonorCleanedUp || !outcome.RPCListening || outcome.CleanedUp {
			t.Fatalf("unexpected held outcome: %+v", outcome)
		}
		workspace, err := runner.Workspace(run)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := engine.VerifyHeld(ctx, workspace); err != nil {
			t.Fatalf("VerifyHeld(%s): %v", p.ID, err)
		}
		finalOutcome, err := runner.Finish(ctx, run)
		if err != nil {
			t.Fatalf("Finish(%s): %v", p.ID, err)
		}
		if !finalOutcome.Verified || !finalOutcome.FSVerified || !finalOutcome.Restored || !finalOutcome.CleanedUp {
			t.Fatalf("unexpected finished outcome: %+v", finalOutcome)
		}
	} else if !outcome.Verified || !outcome.FSVerified || !outcome.Restored || !outcome.CleanedUp {
		t.Fatalf("unexpected non-held outcome: %+v", outcome)
	}
	if err := runner.Cleanup(ctx, run); err != nil {
		t.Fatalf("Cleanup(%s): %v", p.ID, err)
	}

	co := <-consoleCh
	if co.err != nil {
		t.Fatalf("any2jb fake console failed: %v", co.err)
	}
	co.tx.CopiedEngineSHA256 = readParityMeta(t, metaFile)
	return co.tx
}

func resolveRuntimeAssets(t *testing.T) (string, string) {
	t.Helper()
	platform := runtime.GOOS + "-" + runtime.GOARCH
	checkCandidate := func(dir string) (string, string) {
		merged := filepath.Join(dir, "runtime/node")
		if _, err := os.Stat(merged); err == nil {
			return dir, merged
		}
		overlay := filepath.Join(dir, platform, "runtime/node")
		if _, err := os.Stat(overlay); err == nil {
			return dir, overlay
		}
		return "", ""
	}
	if env := os.Getenv("ANY2JB_RUNTIME_ASSETS"); env != "" {
		return checkCandidate(env)
	}
	if candidate, err := filepath.Abs("../resources"); err == nil {
		return checkCandidate(candidate)
	}
	return "", ""
}

func TestEngineOutputMatchesMac2jbAndNs2jb(t *testing.T) {
	reference := os.Getenv("ANY2JB_REFERENCE_ROOT")
	assets, node := resolveRuntimeAssets(t)
	if reference == "" || assets == "" || node == "" {
		if *updateParity {
			t.Fatal("-update-parity requires ANY2JB_REFERENCE_ROOT and runtime assets")
		}
		t.Skip("set ANY2JB_REFERENCE_ROOT (and prepare runtime assets) for live 3-way engine differential check")
	}

	m, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	preloadDir := t.TempDir()
	preload := filepath.Join(preloadDir, "engine_preload.cjs")
	if err := os.WriteFile(preload, parity.PreloadScript(), 0o600); err != nil {
		t.Fatal(err)
	}

	var committed engineParityFileFormat
	if !*updateParity {
		raw, err := os.ReadFile(engineParityFile)
		if err != nil {
			t.Fatalf("reading %s: %v", engineParityFile, err)
		}
		if err := json.Unmarshal(raw, &committed); err != nil {
			t.Fatal(err)
		}
	} else {
		committed = engineParityFileFormat{
			Format:   "any2jb-engine-parity-v1",
			Profiles: make(map[string]engineParityRecord),
		}
	}

	for _, p := range m.Profiles {
		if p.Engine == "izmysh" {
			t.Run("refuses-"+p.ID, func(t *testing.T) {
				support := t.TempDir()
				t.Cleanup(func() { unsealSupport(support) })
				runner := engine.NewHostRunner(engine.Options{
					Assets:              assets,
					Support:             support,
					Version:             "parity-izmysh",
					Manifest:            m,
					DeveloperSourceRoot: reference,
					DeveloperProfileID:  p.ID,
				})
				run := session.Run{ID: "parity-izmysh", Edition: "full", LANIPv4: "127.0.0.1", Profile: p.SessionProfile()}
				_, err := runner.Prepare(context.Background(), run, func(session.Progress) {})
				if err == nil || !strings.Contains(err.Error(), "IZMYSH") {
					t.Fatalf("expected explicit IZMYSH refusal, got %v", err)
				}
			})
			continue
		}

		t.Run(p.ID, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			seed := "any2jb-engine-parity-v1:" + p.ID

			ns2jbTx := runNs2jbHost(ctx, t, node, reference, preload, seed, p)
			mac2jbTx := runMac2jbHost(ctx, t, node, reference, preload, seed, p)
			if diff := parity.DiffEngine("ns2jb", "mac2jb", ns2jbTx, mac2jbTx); diff != "" {
				t.Fatalf("mac2jb diverged from upstream ns2jb: %s", diff)
			}

			any2jbTx := runAny2jbHost(ctx, t, assets, reference, preload, seed, m, p)
			if diff := parity.DiffEngine("mac2jb", "any2jb", mac2jbTx, any2jbTx); diff != "" {
				t.Fatalf("any2jb diverged from mac2jb: %s", diff)
			}
			if diff := parity.DiffEngine("ns2jb", "any2jb", ns2jbTx, any2jbTx); diff != "" {
				t.Fatalf("any2jb diverged from upstream ns2jb: %s", diff)
			}

			rec := engineParityRecord{
				StartCommand:       any2jbTx.StartCommand,
				Packets:            len(any2jbTx.Packets),
				WireBytes:          any2jbTx.WireBytes,
				WireSHA256:         any2jbTx.WireSHA256,
				CopiedEngineSHA256: any2jbTx.CopiedEngineSHA256,
				TranscriptSHA256:   any2jbTx.Digest(),
			}
			if *updateParity {
				committed.Profiles[p.ID] = rec
				return
			}
			want, ok := committed.Profiles[p.ID]
			if !ok {
				t.Fatalf("missing committed parity record for profile %s", p.ID)
			}
			if want != rec {
				t.Fatalf("committed digest mismatch for %s:\nwant %+v\ngot  %+v", p.ID, want, rec)
			}
		})
	}

	if *updateParity {
		if err := os.MkdirAll(filepath.Dir(engineParityFile), 0o755); err != nil {
			t.Fatal(err)
		}
		out, err := json.MarshalIndent(committed, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(engineParityFile, append(out, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCommittedEngineParityDigests(t *testing.T) {
	raw, err := os.ReadFile(engineParityFile)
	if err != nil {
		t.Fatalf("reading %s: %v", engineParityFile, err)
	}
	var committed engineParityFileFormat
	if err := json.Unmarshal(raw, &committed); err != nil {
		t.Fatal(err)
	}
	if committed.Format != "any2jb-engine-parity-v1" {
		t.Fatalf("unexpected format %q", committed.Format)
	}
	m, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	expected := make(map[string]manifest.Profile)
	for _, p := range m.Profiles {
		if p.Engine != "izmysh" {
			expected[p.ID] = p
		}
	}
	if len(committed.Profiles) != len(expected) {
		t.Fatalf("recorded %d profiles, want %d", len(committed.Profiles), len(expected))
	}
	for id, p := range expected {
		rec, ok := committed.Profiles[id]
		if !ok {
			t.Fatalf("missing profile %s in %s", id, engineParityFile)
		}
		args := strings.Split(strings.TrimSuffix(strings.TrimPrefix(rec.StartCommand, "V=START("), ")"), ",")
		if len(args) != p.StartArgumentCount {
			t.Fatalf("%s START arg count = %d, want %d (%q)", id, len(args), p.StartArgumentCount, rec.StartCommand)
		}
		if rec.Packets < 25 || rec.WireBytes != rec.Packets*0x8080 {
			t.Fatalf("%s invalid packet/byte counts: packets=%d bytes=%d", id, rec.Packets, rec.WireBytes)
		}
		for _, digest := range []string{rec.WireSHA256, rec.CopiedEngineSHA256, rec.TranscriptSHA256} {
			if len(digest) != 64 {
				t.Fatalf("%s invalid SHA-256 %q", id, digest)
			}
		}
	}
}

func TestFakeConsoleAndDiffEngine(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	startCmd := fmt.Sprintf("V=START(%d,127,0,0,1,11,22,33,44)", port)

	done := make(chan struct {
		tx  parity.EngineTranscript
		err error
	}, 1)
	go func() {
		tx, err := parity.ServeConsole(ctx, startCmd)
		done <- struct {
			tx  parity.EngineTranscript
			err error
		}{tx, err}
	}()

	conn, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	var hello [128]byte
	if _, err := io.ReadFull(conn, hello[:]); err != nil {
		t.Fatal(err)
	}
	if magic := binary.LittleEndian.Uint64(hello[0:8]); magic != 0x31545344 {
		t.Fatalf("unexpected magic %#x", magic)
	}
	for i, want := range []uint64{11, 22, 33, 44} {
		if got := binary.LittleEndian.Uint64(hello[(11+i)*8:]); got != want {
			t.Fatalf("hello token[%d] = %d, want %d", i, got, want)
		}
	}
	sdk := binary.LittleEndian.Uint64(hello[6*8:])
	scratch := binary.LittleEndian.Uint64(hello[3*8:])
	outAddr := scratch + 0x4040

	// Send one CRC32 call + dummy memmove, then native.stop() zero frame.
	var req [0x8080]byte
	binary.LittleEndian.PutUint64(req[0:], sdk+0x71310)
	binary.LittleEndian.PutUint64(req[8:], 0)
	binary.LittleEndian.PutUint64(req[16:], sdk)
	binary.LittleEndian.PutUint64(req[24:], 0x5e1000)
	binary.LittleEndian.PutUint64(req[40:], sdk+0x5872f0)
	binary.LittleEndian.PutUint64(req[48:], outAddr)
	binary.LittleEndian.PutUint64(req[56:], outAddr)
	if _, err := conn.Write(req[:]); err != nil {
		t.Fatal(err)
	}
	var resp [0x8010]byte
	if _, err := io.ReadFull(conn, resp[:]); err != nil {
		t.Fatal(err)
	}
	if crc := binary.LittleEndian.Uint64(resp[8:16]); crc != 3925528160 {
		t.Fatalf("unexpected CRC32 %d", crc)
	}

	var stop [0x8080]byte
	if _, err := conn.Write(stop[:]); err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()

	res := <-done
	if res.err != nil {
		t.Fatal(res.err)
	}
	if !res.tx.Stopped || len(res.tx.Packets) != 2 || res.tx.WireBytes != 2*0x8080 {
		t.Fatalf("unexpected transcript: %+v", res.tx)
	}
	if diff := parity.DiffEngine("a", "b", res.tx, res.tx); diff != "" {
		t.Fatalf("expected empty diff, got %s", diff)
	}

	mutated := res.tx
	mutated.Packets = slices.Clone(res.tx.Packets)
	mutated.Packets[0].Call0[3] ^= 1
	if diff := parity.DiffEngine("mac2jb", "any2jb", res.tx, mutated); !strings.Contains(diff, "packet 0") {
		t.Fatalf("expected packet 0 diff, got %q", diff)
	}
}
