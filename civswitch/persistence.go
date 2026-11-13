package civswitch

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash"
	"hash/fnv"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/thedavidweng/any2jb/internal/procgroup"
	"github.com/thedavidweng/any2jb/session"
)

const (
	backupPayloadComponent = "civswitch-backup-payload"
	installerComponent     = "civswitch-installer"
	coreComponent          = "civswitch-core"
	saveTemplateComponent  = "civswitch-save-template"
	controlCenterComponent = "civswitch-control-center"
	installConfirmation    = "INSTALL DONE - reboot, Load Game"
	backupFramePrefix      = "MAC2JB_CIV_BACKUP "
	singleSaveDirectory    = "save:/saves/Single"
	maxBackupFiles         = 512
	maxBackupBytes         = 512 << 20
)

// Dependent actions re-probe the loader themselves. A loader verified earlier
// in the session is not proof that it is still listening and executing code.
func (a *Adapter) dependentLoader(ctx context.Context, address string, components ...string) (string, error) {
	if err := a.verifySource(); err != nil {
		return "", err
	}
	root := a.sourceRoot()
	for _, id := range components {
		if err := a.options.Manifest.Verify(root, id); err != nil {
			return "", err
		}
	}
	endpoint := a.loader
	if endpoint == "" {
		if !consoleAddress(address) {
			return "", errors.New("CivSwitch requires the console LAN IPv4 address")
		}
		endpoint = net.JoinHostPort(address, strconv.Itoa(LoaderPort))
	}
	if err := ProbeLoader(ctx, endpoint); err != nil {
		return "", err
	}
	return endpoint, nil
}

// Retained artifacts live beside the app's support data, outside every session
// workspace, so automatic and user cleanup cannot remove them.
func (a *Adapter) retainedDirectory(kind string) (string, error) {
	if a.options.Support == "" {
		return "", errors.New("no retained CivSwitch directory is configured")
	}
	dir := filepath.Join(a.options.Support, "civswitch", kind)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func (a *Adapter) componentPath(id string) (string, error) {
	root := a.sourceRoot()
	for _, c := range a.options.Manifest.Components {
		if c.ID == id {
			return filepath.Join(root, c.Path), nil
		}
	}
	return "", errors.New("unrecorded CivSwitch component " + id)
}

// InstallBootTrigger replaces the game's single-player save files with the
// persistent CivSwitch trigger. It first takes a complete backup of
// save:/saves/Single and verifies it on disk; without that the console is
// left untouched.
func (a *Adapter) InstallBootTrigger(ctx context.Context, address string, confirmedSaveReplacement bool, report session.Report) (string, error) {
	if !confirmedSaveReplacement {
		return "", errors.New("installing the CivSwitch boot trigger replaces the Civilization VII single-player save files and needs explicit confirmation")
	}
	backups, err := a.retainedDirectory("backups")
	if err != nil {
		return "", err
	}
	required := []string{backupPayloadComponent}
	if a.options.DeveloperInstallerPython != "" {
		required = append(required, installerComponent)
	} else {
		required = append(required, coreComponent, saveTemplateComponent)
	}
	endpoint, err := a.dependentLoader(ctx, address, required...)
	if err != nil {
		return "", err
	}
	payload, err := a.options.Manifest.Read(a.sourceRoot(), backupPayloadComponent)
	if err != nil {
		return "", err
	}
	backup, err := a.backupSingleSaves(ctx, endpoint, address, backups, string(payload), report)
	if err != nil {
		return "", err
	}
	// Reading the saves is read-only; the installer is the first console write.
	report(session.Progress{SideEffect: true})
	if err := a.runInstaller(ctx, endpoint, address); err != nil {
		return backup, err
	}
	return backup, nil
}

func (a *Adapter) backupSingleSaves(ctx context.Context, endpoint, address, backups, payload string, report session.Report) (string, error) {
	receiver := &backupReceiver{backups: backups, address: address, report: report}
	defer receiver.discard()
	executed, err := loaderCall{deadline: 10 * time.Minute, limit: 2 * maxBackupBytes, line: receiver.accept}.send(ctx, endpoint, payload)
	if err != nil {
		return "", err
	}
	if !executed {
		return "", errors.New("the CivSwitch loader did not confirm the save backup")
	}
	return receiver.commit()
}

// The installer runs either the verified pure-Go stage/template uploader or,
// when explicitly requested in developer mode, the pinned upstream Python
// script with a fixed argument list and no shell.
func (a *Adapter) runInstaller(ctx context.Context, endpoint, address string) error {
	if a.options.DeveloperInstallerPython == "" {
		return a.runNativeInstaller(ctx, endpoint)
	}
	if !filepath.IsAbs(a.options.DeveloperInstallerPython) {
		return errors.New("the CivSwitch trigger installer needs an explicit Python 3 interpreter path; none is bundled")
	}
	script, err := a.componentPath(installerComponent)
	if err != nil {
		return err
	}
	confirmation := &markerWriter{marker: installConfirmation}
	cmd := exec.CommandContext(ctx, a.options.DeveloperInstallerPython, script, address)
	if err := procgroup.Configure(cmd); err != nil {
		return err
	}
	cmd.Cancel = func() error { return procgroup.Signal(cmd, true) }
	cmd.Dir = filepath.Dir(script)
	cmd.Stdout, cmd.Stderr = confirmation, confirmation
	cmd.Env = append(os.Environ(), "PYTHONUNBUFFERED=1")
	if err := cmd.Run(); err != nil {
		return err
	}
	if !confirmation.found {
		return errors.New("the installer did not confirm that the CivSwitch trigger was written")
	}
	return nil
}

// markerWriter confirms a fixed phrase in a stream without retaining the
// process output or passing it on.
type markerWriter struct {
	marker string
	carry  string
	found  bool
}

func (w *markerWriter) Write(p []byte) (int, error) {
	text := w.carry + string(p)
	if strings.Contains(text, w.marker) {
		w.found = true
	}
	if len(text) >= len(w.marker) {
		text = text[len(text)-len(w.marker)+1:]
	}
	w.carry = text
	return len(p), nil
}

type backupFrame struct {
	Version   int    `json:"version"`
	Directory string `json:"directory"`
	Count     int    `json:"count"`
	ChunkSize int    `json:"chunkSize"`
	NameHex   string `json:"nameHex"`
	Size      int    `json:"size"`
	Offset    int    `json:"offset"`
	Hex       string `json:"hex"`
	FNV32     string `json:"fnv32"`
}

type backupFile struct {
	Name   string `json:"name"`
	Size   int    `json:"size"`
	SHA256 string `json:"sha256"`
	FNV32  string `json:"fnv32"`
}

// backupReceiver validates the upstream save-backup frames of the pinned
// backup payload. Frames are data: nothing here is executed, and a backup is
// only kept once every declared file matched its checksum on disk.
type backupReceiver struct {
	backups, address string
	report           session.Report
	temp             string
	root             *os.Root
	expected         int
	chunkSize        int
	started          bool
	complete         bool
	total            int
	files            []backupFile
	current          *os.File
	name             string
	size, written    int
	digest           hash.Hash
	checksum         hash.Hash32
}

func (r *backupReceiver) accept(line string) error {
	if !strings.HasPrefix(line, backupFramePrefix) {
		return nil
	}
	kind, body, found := strings.Cut(strings.TrimPrefix(line, backupFramePrefix), " ")
	if !found {
		return errors.New("malformed CivSwitch save backup frame")
	}
	var frame backupFrame
	if err := json.Unmarshal([]byte(body), &frame); err != nil {
		return errors.New("invalid CivSwitch save backup frame")
	}
	if r.complete {
		return errors.New("unexpected CivSwitch save backup frame after completion")
	}
	switch kind {
	case "BEGIN":
		return r.begin(frame)
	case "FILE":
		return r.openFile(frame)
	case "CHUNK":
		return r.chunk(frame)
	case "FILE_END":
		return r.closeFile(frame)
	case "END":
		return r.end(frame)
	case "ERROR":
		return errors.New("the console reported that the Civilization VII save backup failed")
	}
	return errors.New("unknown CivSwitch save backup frame")
}

func (r *backupReceiver) begin(frame backupFrame) error {
	if r.started || frame.Version != 1 || frame.Directory != singleSaveDirectory ||
		frame.Count < 0 || frame.Count > maxBackupFiles || frame.ChunkSize != 512 {
		return errors.New("invalid CivSwitch save backup inventory")
	}
	temp, err := os.MkdirTemp(r.backups, ".pending-")
	if err != nil {
		return err
	}
	r.temp, r.started, r.expected, r.chunkSize = temp, true, frame.Count, frame.ChunkSize
	if err := os.Mkdir(filepath.Join(temp, "files"), 0o700); err != nil {
		return err
	}
	r.root, err = os.OpenRoot(temp)
	return err
}

// Save names are console-supplied bytes. They are stored literally inside the
// backup directory, never interpreted, and never allowed to escape it.
func (r *backupReceiver) openFile(frame backupFrame) error {
	if !r.started || r.current != nil || len(r.files) >= r.expected ||
		frame.Size < 0 || frame.Size > maxBackupBytes {
		return errors.New("invalid CivSwitch save backup file")
	}
	raw, err := hex.DecodeString(frame.NameHex)
	if err != nil {
		return errors.New("invalid CivSwitch save filename encoding")
	}
	name := string(raw)
	if name == "" || name == "." || name == ".." || len(name) > 255 || !utf8.ValidString(name) ||
		strings.ContainsAny(name, `/\`) || strings.ContainsFunc(name, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return errors.New("unsafe CivSwitch save filename")
	}
	file, err := r.root.OpenFile(filepath.Join("files", name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	r.current, r.name, r.size, r.written = file, name, frame.Size, 0
	r.digest, r.checksum = sha256.New(), fnv.New32a()
	return nil
}

func (r *backupReceiver) chunk(frame backupFrame) error {
	if r.current == nil || frame.Offset != r.written || len(frame.Hex) > 2*r.chunkSize {
		return errors.New("invalid CivSwitch save backup chunk")
	}
	data, err := hex.DecodeString(frame.Hex)
	if err != nil {
		return errors.New("invalid CivSwitch save backup chunk encoding")
	}
	if r.written+len(data) > r.size || r.total+len(data) > maxBackupBytes {
		return errors.New("CivSwitch save backup exceeds its declared size")
	}
	if _, err := r.current.Write(data); err != nil {
		return err
	}
	r.digest.Write(data)
	r.checksum.Write(data)
	r.written += len(data)
	r.total += len(data)
	return nil
}

func (r *backupReceiver) closeFile(frame backupFrame) error {
	if r.current == nil || frame.Size != r.size || r.written != r.size || frame.FNV32 != padded(r.checksum.Sum32()) {
		return errors.New("CivSwitch save backup length or checksum mismatch")
	}
	if err := r.current.Sync(); err != nil {
		return err
	}
	if err := r.current.Close(); err != nil {
		return err
	}
	r.current = nil
	r.files = append(r.files, backupFile{Name: r.name, Size: r.size,
		SHA256: hex.EncodeToString(r.digest.Sum(nil)), FNV32: frame.FNV32})
	r.report(session.Progress{Current: len(r.files), Total: r.expected})
	return nil
}

func padded(sum uint32) string {
	text := strconv.FormatUint(uint64(sum), 16)
	return strings.Repeat("0", 8-len(text)) + text
}

func (r *backupReceiver) end(frame backupFrame) error {
	if !r.started || r.current != nil || frame.Count != r.expected || len(r.files) != r.expected {
		return errors.New("incomplete CivSwitch save backup")
	}
	r.complete = true
	return nil
}

// commit re-reads every file from disk before the backup is accepted. A backup
// that cannot be proven on disk does not authorize the trigger install.
func (r *backupReceiver) commit() (string, error) {
	if !r.complete {
		return "", errors.New("the console did not complete the Civilization VII save backup")
	}
	for _, file := range r.files {
		data, err := r.root.ReadFile(filepath.Join("files", file.Name))
		if err != nil {
			return "", err
		}
		if len(data) != file.Size || hex.EncodeToString(sha256Sum(data)) != file.SHA256 {
			return "", errors.New("the retained CivSwitch backup does not match the console save")
		}
	}
	record, err := json.MarshalIndent(map[string]any{"version": 1, "source": singleSaveDirectory,
		"consoleAddress": r.address, "capturedAt": time.Now().UTC().Format(time.RFC3339), "files": r.files}, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(r.temp, "manifest.json"), append(record, '\n'), 0o600); err != nil {
		return "", err
	}
	final := filepath.Join(r.backups, time.Now().UTC().Format("2006-01-02T150405Z")+"-"+strings.ToLower(rand.Text()[:8]))
	if err := os.Rename(r.temp, final); err != nil {
		return "", err
	}
	r.temp = ""
	return final, nil
}

func sha256Sum(data []byte) []byte {
	sum := sha256.Sum256(data)
	return sum[:]
}

func (r *backupReceiver) discard() {
	if r.current != nil {
		r.current.Close()
		r.current = nil
	}
	if r.root != nil {
		r.root.Close()
	}
	if r.temp != "" {
		os.RemoveAll(r.temp)
		r.temp = ""
	}
}
