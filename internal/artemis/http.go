package artemis

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/thedavidweng/any2jb/internal/manifest"
	"github.com/thedavidweng/any2jb/internal/procgroup"
)

// The Artemis launcher serves its own HTTP API on port 8080 and accepts at most
// 28,672 bytes of request body, which mac2jb v0.8.2 enforces before any upload
// or Lua request leaves the Mac.
const (
	DefaultPort = 8080
	BodyLimit   = 0x7000
)

// maxToolOutput bounds what the pinned tool may print; the largest legitimate
// reply is one backup manifest.
const maxToolOutput = 4 << 20

// NodeBinary is the verified Node sidecar that runs pinned plugin scripts.
type NodeBinary string

// httpTool is the pinned Artemis HTTP CLI inside the savekit plugin.
const httpTool = "vendor/ns2-savekit/research/current/tools/artemis-http.cjs"

// Sentinels report the class of a refused or failed Artemis request.
var (
	ErrAddress    = errors.New("enter the console's private IPv4 address")
	ErrSavePath   = errors.New("use a flat save:/ name with letters, numbers, ., _ or -; launcher files are protected")
	ErrTooLarge   = errors.New("the Artemis launcher accepts at most 28,672 bytes per request")
	ErrConsent    = errors.New("explicit confirmation is required")
	ErrNotArtemis = errors.New("the console is not running a supported Artemis launcher")
	ErrConnect    = errors.New("cannot reach the Artemis launcher")
	ErrTimeout    = errors.New("the Artemis launcher did not answer in time")
	ErrLua        = errors.New("Lua returned an error")
	ErrHTTP       = errors.New("the Artemis launcher returned an HTTP error")
	ErrIntegrity  = errors.New("the transfer failed its integrity check")
	ErrProtocol   = errors.New("unexpected Artemis launcher response")
	ErrRefused    = errors.New("the Artemis launcher refused the request")
	ErrUsage      = errors.New("unsupported Artemis request")
)

// toolKinds maps the pinned tool's failure codes onto this package's sentinels.
var toolKinds = map[string]error{
	"ECONN": ErrConnect, "ETIMEDOUT": ErrTimeout, "ELUA": ErrLua, "EHTTP": ErrHTTP,
	"ESIZE": ErrIntegrity, "EHASH": ErrIntegrity, "ESTILL": ErrIntegrity,
	"EPROTO": ErrProtocol, "EPROTECTED": ErrRefused, "EEXIST": ErrRefused,
	"ENOENT": ErrRefused, "EPATH": ErrRefused, "EUSAGE": ErrUsage,
	"ERANGE": ErrUsage, "EATOMIC": ErrRefused,
}

// ToolError is one failed Artemis tool run. Unwrap reports the class of failure
// so a caller can tell an unreachable console from a refused write.
type ToolError struct {
	Kind    string
	Exit    int
	Message string
}

func (e *ToolError) Error() string {
	if e.Kind == "" {
		return "the Artemis HTTP tool failed: " + e.Message
	}
	return "the Artemis HTTP tool failed [" + e.Kind + "]: " + e.Message
}

func (e *ToolError) Unwrap() error { return toolKinds[e.Kind] }

// protectedFiles are the launcher's own files; reads may inspect them, writes
// never replace them.
var protectedFiles = map[string]bool{
	"core.lua": true, "inject.iet": true, "inject.lua": true, "http.lua": true,
	"proggy.ttf": true, "save9999.dat": true, "saveg.dat": true, "system.dat": true,
}

// ConsoleAddress accepts only a private-LAN IPv4 console address. mac2jb v0.8.2
// requires the Mac and the console on the same local network; loopback and
// link-local answers are never a console the user confirmed. allowLoopback
// selects a local fixture.
func ConsoleAddress(host string, allowLoopback bool) error {
	ip, err := netip.ParseAddr(host)
	if err != nil || !ip.Is4() || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() {
		return ErrAddress
	}
	if ip.IsLoopback() {
		if allowLoopback {
			return nil
		}
		return ErrAddress
	}
	if !ip.IsPrivate() {
		return ErrAddress
	}
	return nil
}

// SavePath validates a launcher save path that will be written, and returns the
// canonical save:/ form.
func SavePath(input string) (string, error) { return parseSavePath(input, true) }

// ReadPath validates a launcher save path that will only be read.
func ReadPath(input string) (string, error) { return parseSavePath(input, false) }

func parseSavePath(input string, write bool) (string, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" || len(trimmed) > 200 {
		return "", ErrSavePath
	}
	name := strings.TrimPrefix(trimmed, "save:/")
	if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, "..") ||
		strings.ContainsAny(name, `/\`) || (write && protectedFiles[name]) {
		return "", ErrSavePath
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '_', r == '-':
		default:
			return "", ErrSavePath
		}
	}
	return "save:/" + name, nil
}

// Recognized reports whether a /status body came from an Artemis launcher.
func Recognized(banner string) bool {
	if strings.HasPrefix(banner, "nora2hax/seedsow HTTP server\n") {
		return true
	}
	for _, game := range []string{"seedsow", "nora", "nora2", "hamidashi"} {
		if strings.HasPrefix(banner, "n2h HTTP server ("+game+")\n") {
			return true
		}
	}
	return false
}

// Status is the launcher banner and its HTTP status.
type Status struct {
	OK     bool   `json:"ok"`
	Code   int    `json:"status"`
	Banner string `json:"banner"`
	Raw    string `json:"raw"`
}

// Health is the launcher banner plus the Lua, platform and engine probes.
type Health struct {
	OK       bool   `json:"ok"`
	Code     int    `json:"status"`
	Banner   string `json:"banner"`
	Lua      string `json:"lua"`
	Platform string `json:"platform"`
	Engine   string `json:"engine"`
	Raw      string `json:"raw"`
}

// FileStat reports whether one launcher save file exists and its size.
type FileStat struct {
	Exists bool  `json:"exists"`
	Size   int64 `json:"size"`
}

// Download is one verified launcher save download.
type Download struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Upload is one read-back verified launcher save upload.
type Upload struct {
	Path        string `json:"path"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
	LocalSHA256 string `json:"localSha256"`
	Verified    bool   `json:"verified"`
	Atomic      bool   `json:"atomic"`
	// Backup is the pre-upload backup folder, always taken before the write.
	Backup string `json:"-"`
}

// Deletion is one confirmed launcher save delete.
type Deletion struct {
	Path    string `json:"path"`
	Deleted bool   `json:"deleted"`
	Reply   string `json:"reply"`
	// Backup is the pre-delete backup folder, always taken before the delete.
	Backup string `json:"-"`
}

// EvalResult is the parsed reply of one /eval request.
type EvalResult struct {
	OK     bool     `json:"ok"`
	Status string   `json:"status"`
	Result string   `json:"result"`
	Output []string `json:"output"`
	Raw    string   `json:"raw"`
}

// RenameProbe is the launcher's answer to an os.rename probe.
type RenameProbe struct {
	OK     bool   `json:"ok"`
	Reason string `json:"reason"`
}

// Probe reports whether the launcher can rename a save file atomically.
type Probe struct {
	Rename        RenameProbe `json:"rename"`
	ServerBodyCap int         `json:"serverBodyCap"`
}

// Found is one Artemis launcher that answered a subnet scan.
type Found struct {
	Host   string `json:"host"`
	Banner string `json:"banner"`
}

// BackupEntry records one explicit-file backup member.
type BackupEntry struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// BackupManifest describes an explicit-file backup. The upstream HTTP server has
// no listing endpoint, so a backup is never a complete archive.
type BackupManifest struct {
	Transport string        `json:"transport"`
	Source    string        `json:"source"`
	Scope     string        `json:"scope"`
	Complete  bool          `json:"complete"`
	Note      string        `json:"note"`
	Files     int           `json:"files"`
	Bytes     int64         `json:"bytes"`
	Entries   []BackupEntry `json:"entries"`
}

// Backup is one explicit-file backup folder and its verified manifest.
type Backup struct {
	Dir      string         `json:"dir"`
	Manifest BackupManifest `json:"manifest"`
}

// GetRequest downloads one launcher save file.
type GetRequest struct {
	Host, Path, Out string
	// SHA256 and Size, when set, are verified against the downloaded bytes.
	SHA256 string
	Size   int64
	// Overwrite replaces an existing destination file.
	Overwrite bool
}

// EvalRequest runs Lua on the console. Arbitrary code needs explicit consent.
type EvalRequest struct {
	Host string
	// Code or File supplies the Lua source; Code wins when both are set.
	Code string
	File string
	// ConfirmedArbitraryCode is the user's explicit consent to run code.
	ConfirmedArbitraryCode bool
}

// PutRequest uploads one file into the launcher save. Writing needs explicit
// consent and always backs the target up first.
type PutRequest struct {
	Host, Path, In string
	Force          bool
	Atomic         bool
	// ConfirmedMutation is the user's explicit consent to change console data.
	ConfirmedMutation bool
}

// RemoveRequest deletes one launcher save file. Deleting needs explicit consent
// and always backs the target up first.
type RemoveRequest struct {
	Host, Path string
	// ConfirmedMutation is the user's explicit consent to change console data.
	ConfirmedMutation bool
}

// BackupRequest copies explicit launcher files into a private folder. An empty
// Dir selects a fresh 0700 folder under <Support>/artemis/backups.
type BackupRequest struct {
	Host  string
	Files []string
	Dir   string
}

// Client runs the pinned Artemis HTTP CLI from the verified savekit plugin.
// Every request is validated before Node starts, and nothing is retried here: a
// write must never be replayed blindly.
type Client struct {
	Node     NodeBinary
	ToolRoot string
	Manifest *manifest.Manifest
	// Support is the private app support root; temporary Lua sources and the
	// default backup folder live beneath it.
	Support string
	// Port is the launcher HTTP port; zero selects 8080.
	Port int
	// AllowLoopback permits 127.0.0.0/8 for local fixtures.
	AllowLoopback bool
}

// Status reports the launcher banner.
func (c Client) Status(ctx context.Context, host string) (Status, error) {
	hostArgs, err := c.hostArgs(host)
	if err != nil {
		return Status{}, err
	}
	out, err := c.run(ctx, append([]string{"status", "--json"}, hostArgs...)...)
	if err != nil {
		return Status{}, err
	}
	var status Status
	if err := decode(out, &status); err != nil {
		return Status{}, err
	}
	if !status.OK || !Recognized(status.Banner) {
		return Status{}, ErrNotArtemis
	}
	return status, nil
}

// Health reports the launcher banner plus the Lua, platform and engine probes.
func (c Client) Health(ctx context.Context, host string) (Health, error) {
	hostArgs, err := c.hostArgs(host)
	if err != nil {
		return Health{}, err
	}
	out, err := c.run(ctx, append([]string{"health", "--json"}, hostArgs...)...)
	if err != nil {
		return Health{}, err
	}
	var health Health
	if err := decode(out, &health); err != nil {
		return Health{}, err
	}
	if !health.OK || !Recognized(health.Banner) || health.Lua == "" || health.Platform == "" {
		return Health{}, ErrNotArtemis
	}
	return health, nil
}

// Probe checks whether the launcher can rename a save file atomically.
func (c Client) Probe(ctx context.Context, host string) (Probe, error) {
	hostArgs, err := c.hostArgs(host)
	if err != nil {
		return Probe{}, err
	}
	out, err := c.run(ctx, append([]string{"probe"}, hostArgs...)...)
	if err != nil {
		return Probe{}, err
	}
	var probe Probe
	if err := decode(out, &probe); err != nil {
		return Probe{}, err
	}
	return probe, nil
}

// Discover scans one private /24 for Artemis launchers.
func (c Client) Discover(ctx context.Context, subnet string) ([]Found, error) {
	prefix := strings.TrimSuffix(strings.TrimSpace(subnet), ".")
	parts := strings.Split(prefix, ".")
	if len(parts) != 3 {
		return nil, errors.New("discovery needs a private subnet prefix such as 192.168.1")
	}
	for _, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 || number > 255 || strconv.Itoa(number) != part {
			return nil, errors.New("discovery needs a private subnet prefix such as 192.168.1")
		}
	}
	// A scan is only ever local: the subnet must address the private LAN.
	if err := ConsoleAddress(prefix+".1", false); err != nil {
		return nil, fmt.Errorf("%w: discovery stays on the local private network", ErrAddress)
	}
	out, err := c.run(ctx, append([]string{"discover", "--json", "--subnet", prefix}, c.portArgs()...)...)
	if err != nil {
		return nil, err
	}
	var found []Found
	if err := decode(out, &found); err != nil {
		return nil, err
	}
	return found, nil
}

// Stat reports existence and size of one launcher save file.
func (c Client) Stat(ctx context.Context, host, savePath string) (FileStat, error) {
	hostArgs, err := c.hostArgs(host)
	if err != nil {
		return FileStat{}, err
	}
	path, err := ReadPath(savePath)
	if err != nil {
		return FileStat{}, err
	}
	out, err := c.run(ctx, append([]string{"stat", "--json", "--path", path}, hostArgs...)...)
	if err != nil {
		return FileStat{}, err
	}
	var stat FileStat
	if err := decode(out, &stat); err != nil {
		return FileStat{}, err
	}
	return stat, nil
}

// Get downloads one launcher save file to an absolute destination and returns
// the verified size and SHA-256.
func (c Client) Get(ctx context.Context, request GetRequest) (Download, error) {
	hostArgs, err := c.hostArgs(request.Host)
	if err != nil {
		return Download{}, err
	}
	path, err := ReadPath(request.Path)
	if err != nil {
		return Download{}, err
	}
	if !filepath.IsAbs(request.Out) {
		return Download{}, errors.New("the download destination must be an absolute path")
	}
	if info, err := os.Stat(filepath.Dir(request.Out)); err != nil || !info.IsDir() {
		return Download{}, fmt.Errorf("the download folder %s is not a directory", filepath.Dir(request.Out))
	}
	if _, err := os.Lstat(request.Out); err == nil && !request.Overwrite {
		return Download{}, fmt.Errorf("%s already exists; choose another destination", request.Out)
	}
	args := []string{"get", "--json", "--path", path, "--out", request.Out}
	if request.SHA256 != "" {
		args = append(args, "--sha256", request.SHA256)
	}
	if request.Size > 0 {
		args = append(args, "--size", strconv.FormatInt(request.Size, 10))
	}
	out, err := c.run(ctx, append(args, hostArgs...)...)
	if err != nil {
		return Download{}, err
	}
	var download Download
	if err := decode(out, &download); err != nil {
		return Download{}, err
	}
	if download.Size < 0 || len(download.SHA256) != 64 {
		return Download{}, fmt.Errorf("%w: the download reply is incomplete", ErrProtocol)
	}
	return download, nil
}

// Eval runs Lua on the console. It refuses to start Node without explicit
// consent to execute arbitrary code.
func (c Client) Eval(ctx context.Context, request EvalRequest) (EvalResult, error) {
	if !request.ConfirmedArbitraryCode {
		return EvalResult{}, fmt.Errorf("%w: running Lua on the console needs ConfirmedArbitraryCode", ErrConsent)
	}
	hostArgs, err := c.hostArgs(request.Host)
	if err != nil {
		return EvalResult{}, err
	}
	code := request.Code
	if code == "" {
		if request.File == "" {
			return EvalResult{}, errors.New("enter Lua code to run")
		}
		info, err := os.Lstat(request.File)
		if err != nil || !info.Mode().IsRegular() {
			return EvalResult{}, fmt.Errorf("%s is not a regular Lua source file", request.File)
		}
		if info.Size() > BodyLimit {
			return EvalResult{}, ErrTooLarge
		}
		source, err := os.ReadFile(request.File)
		if err != nil {
			return EvalResult{}, err
		}
		code = string(source)
	}
	if strings.TrimSpace(code) == "" {
		return EvalResult{}, errors.New("enter Lua code to run")
	}
	if len(code) > BodyLimit {
		return EvalResult{}, ErrTooLarge
	}
	// The source travels in a private file, never in the process arguments.
	source, cleanup, err := c.privateFile("eval", []byte(code))
	if err != nil {
		return EvalResult{}, err
	}
	defer cleanup()
	out, err := c.run(ctx, append([]string{"eval", "--json", "--file", source}, hostArgs...)...)
	if err != nil {
		return EvalResult{}, err
	}
	var parsed EvalResult
	if err := decode(out, &parsed); err != nil {
		return EvalResult{}, err
	}
	if !parsed.OK {
		return EvalResult{}, fmt.Errorf("%w: %s", ErrLua, parsed.Result)
	}
	return parsed, nil
}

// Put uploads one file into the launcher save. It refuses to start Node without
// explicit consent, and backs the target up before any byte is written.
func (c Client) Put(ctx context.Context, request PutRequest) (Upload, error) {
	if !request.ConfirmedMutation {
		return Upload{}, fmt.Errorf("%w: uploading a save file needs ConfirmedMutation", ErrConsent)
	}
	hostArgs, err := c.hostArgs(request.Host)
	if err != nil {
		return Upload{}, err
	}
	path, err := SavePath(request.Path)
	if err != nil {
		return Upload{}, err
	}
	info, err := os.Lstat(request.In)
	if err != nil || !info.Mode().IsRegular() {
		return Upload{}, fmt.Errorf("%s is not a regular upload source file", request.In)
	}
	if info.Size() > BodyLimit {
		return Upload{}, ErrTooLarge
	}
	backup, err := c.backupFile(ctx, hostArgs, path, "upload")
	if err != nil {
		return Upload{}, fmt.Errorf("the pre-upload backup failed: %w", err)
	}
	args := []string{"put", "--json", "--path", path, "--in", request.In}
	if request.Force {
		args = append(args, "--force")
	}
	if request.Atomic {
		args = append(args, "--atomic")
	}
	out, err := c.run(ctx, append(args, hostArgs...)...)
	if err != nil {
		return Upload{}, fmt.Errorf("the upload failed; the pre-upload backup is %s: %w", backup.Dir, err)
	}
	var upload Upload
	if err := decode(out, &upload); err != nil {
		return Upload{}, err
	}
	if !upload.Verified || len(upload.SHA256) != 64 {
		return Upload{}, fmt.Errorf("%w: the upload was not verified by read-back", ErrIntegrity)
	}
	upload.Backup = backup.Dir
	return upload, nil
}

// Remove deletes one launcher save file. It refuses to start Node without
// explicit consent, and backs the file up before the delete.
func (c Client) Remove(ctx context.Context, request RemoveRequest) (Deletion, error) {
	if !request.ConfirmedMutation {
		return Deletion{}, fmt.Errorf("%w: deleting a save file needs ConfirmedMutation", ErrConsent)
	}
	hostArgs, err := c.hostArgs(request.Host)
	if err != nil {
		return Deletion{}, err
	}
	path, err := SavePath(request.Path)
	if err != nil {
		return Deletion{}, err
	}
	backup, err := c.backupFile(ctx, hostArgs, path, "delete")
	if err != nil {
		return Deletion{}, fmt.Errorf("the pre-delete backup failed: %w", err)
	}
	out, err := c.run(ctx, append([]string{"rm", "--json", "--path", path}, hostArgs...)...)
	if err != nil {
		return Deletion{}, fmt.Errorf("the delete failed; the pre-delete backup is %s: %w", backup.Dir, err)
	}
	var deletion Deletion
	if err := decode(out, &deletion); err != nil {
		return Deletion{}, err
	}
	if !deletion.Deleted {
		return Deletion{}, fmt.Errorf("%w: the launcher did not confirm the delete", ErrProtocol)
	}
	deletion.Backup = backup.Dir
	return deletion, nil
}

// Backup copies explicit launcher files into a private folder and returns the
// verified manifest. An empty request.Dir selects a fresh 0700 folder under
// <Support>/artemis/backups.
func (c Client) Backup(ctx context.Context, request BackupRequest) (Backup, error) {
	hostArgs, err := c.hostArgs(request.Host)
	if err != nil {
		return Backup{}, err
	}
	names := make([]string, 0, len(request.Files))
	for _, file := range request.Files {
		path, err := ReadPath(file)
		if err != nil {
			return Backup{}, err
		}
		names = append(names, strings.TrimPrefix(path, "save:/"))
	}
	dir := request.Dir
	if dir == "" {
		dir, err = c.freshBackupFolder("backup")
	} else {
		if !filepath.IsAbs(dir) {
			return Backup{}, errors.New("the backup folder must be an absolute path")
		}
		err = os.MkdirAll(dir, 0o700)
	}
	if err != nil {
		return Backup{}, err
	}
	return c.backupInto(ctx, hostArgs, dir, names)
}

// backupFile is the mandatory pre-mutation backup of one launcher save file.
func (c Client) backupFile(ctx context.Context, hostArgs []string, path, prefix string) (Backup, error) {
	name, err := ReadPath(path)
	if err != nil {
		return Backup{}, err
	}
	dir, err := c.freshBackupFolder(prefix)
	if err != nil {
		return Backup{}, err
	}
	return c.backupInto(ctx, hostArgs, dir, []string{strings.TrimPrefix(name, "save:/")})
}

func (c Client) backupInto(ctx context.Context, hostArgs []string, dir string, names []string) (Backup, error) {
	args := []string{"backup", "--json", "--out", dir}
	if len(names) > 0 {
		args = append(args, "--files", strings.Join(names, ","))
	}
	out, err := c.run(ctx, append(args, hostArgs...)...)
	if err != nil {
		return Backup{}, err
	}
	var made Backup
	if err := decode(out, &made.Manifest); err != nil {
		return Backup{}, err
	}
	made.Dir = dir
	if made.Manifest.Scope != "explicit-files" || made.Manifest.Complete {
		return Backup{}, fmt.Errorf("%w: the backup is not an explicit-file backup", ErrProtocol)
	}
	for _, entry := range made.Manifest.Entries {
		if len(entry.SHA256) != 64 || entry.Size < 0 {
			return Backup{}, fmt.Errorf("%w: the backup manifest is incomplete", ErrProtocol)
		}
	}
	return made, nil
}

// freshBackupFolder allocates a new 0700 backup folder under support, outside
// every run and export workspace.
func (c Client) freshBackupFolder(prefix string) (string, error) {
	if c.Support == "" {
		return "", errors.New("no private support directory for Artemis backups")
	}
	parent := filepath.Join(c.Support, "artemis", "backups")
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return "", err
	}
	return os.MkdirTemp(parent, prefix+"-")
}

// privateFile writes body into a new 0600 file the pinned tool reads, and
// returns its path with the cleanup that removes it.
func (c Client) privateFile(prefix string, body []byte) (string, func(), error) {
	parent := filepath.Join(c.Support, "artemis")
	if c.Support == "" {
		parent = ""
	} else if err := os.MkdirAll(parent, 0o700); err != nil {
		return "", nil, err
	}
	dir, err := os.MkdirTemp(parent, prefix+"-")
	if err != nil {
		return "", nil, err
	}
	file := filepath.Join(dir, prefix+".lua")
	cleanup := func() { _ = os.RemoveAll(dir) }
	if err := os.WriteFile(file, body, 0o600); err != nil {
		cleanup()
		return "", nil, err
	}
	return file, cleanup, nil
}

// hostArgs validates the console address and returns the tool's endpoint flags.
func (c Client) hostArgs(host string) ([]string, error) {
	if err := ConsoleAddress(host, c.AllowLoopback); err != nil {
		return nil, err
	}
	return append([]string{"--host", host}, c.portArgs()...), nil
}

func (c Client) portArgs() []string {
	if c.Port == 0 || c.Port == DefaultPort {
		return nil
	}
	return []string{"--port", strconv.Itoa(c.Port)}
}

// script resolves and verifies the pinned tool before it may run.
func (c Client) script() (string, error) {
	root, err := PluginRoot(c.ToolRoot, c.Manifest, SaveKitPlugin)
	if err != nil {
		return "", err
	}
	if _, err := readRecorded(root, c.Manifest, SaveKitPlugin, httpTool); err != nil {
		return "", err
	}
	return filepath.Join(root, filepath.FromSlash(httpTool)), nil
}

func (c Client) run(ctx context.Context, args ...string) ([]byte, error) {
	script, err := c.script()
	if err != nil {
		return nil, err
	}
	if c.Node == "" {
		return nil, errors.New("no verified Node binary for the Artemis HTTP tool")
	}
	cmd := exec.CommandContext(ctx, string(c.Node), append([]string{script}, args...)...)
	if err := procgroup.Configure(cmd); err != nil {
		return nil, err
	}
	stdout, stderr := &bounded{limit: maxToolOutput}, &bounded{limit: 64 << 10}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	if err := cmd.Wait(); err != nil {
		_ = procgroup.Signal(cmd, true)
		if stopped := ctx.Err(); stopped != nil {
			return nil, fmt.Errorf("the Artemis HTTP tool was stopped: %w", stopped)
		}
		if stdout.err != nil || stderr.err != nil {
			return nil, errors.Join(stdout.err, stderr.err)
		}
		return nil, toolError(cmd, err, stderr.buf.String())
	}
	if stdout.err != nil {
		return nil, stdout.err
	}
	return stdout.buf.Bytes(), nil
}

var failedLine = regexp.MustCompile(`FAILED(?:\[([A-Z]+)\])?: (.*)`)

// toolError keeps the pinned tool's failure code and bounded message. A raw
// Node stack is truncated: it is diagnostics, never something to show whole.
func toolError(cmd *exec.Cmd, wait error, stderr string) error {
	failure := &ToolError{Exit: -1, Message: strings.TrimSpace(stderr)}
	if cmd.ProcessState != nil {
		failure.Exit = cmd.ProcessState.ExitCode()
	}
	for _, line := range strings.Split(stderr, "\n") {
		if match := failedLine.FindStringSubmatch(strings.TrimRight(line, "\r")); match != nil {
			failure.Kind, failure.Message = match[1], match[2]
		}
	}
	if failure.Message == "" {
		failure.Message = wait.Error()
	}
	if len(failure.Message) > 512 {
		failure.Message = failure.Message[:512]
	}
	return failure
}

func decode(output []byte, target any) error {
	trimmed := bytes.TrimSpace(output)
	if len(trimmed) == 0 {
		return fmt.Errorf("%w: the Artemis HTTP tool printed nothing", ErrProtocol)
	}
	if err := json.Unmarshal(trimmed, target); err != nil {
		return fmt.Errorf("%w: %v", ErrProtocol, err)
	}
	return nil
}

// bounded captures child output up to a limit and fails the run beyond it.
type bounded struct {
	buf   bytes.Buffer
	limit int
	err   error
}

func (b *bounded) Write(p []byte) (int, error) {
	if b.err != nil {
		return 0, b.err
	}
	if b.buf.Len()+len(p) > b.limit {
		b.err = fmt.Errorf("the Artemis HTTP tool printed more than %d bytes", b.limit)
		return 0, b.err
	}
	return b.buf.Write(p)
}
