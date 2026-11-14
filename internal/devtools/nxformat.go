package devtools

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// NXFormatAction is one NXFormat operation. The values match the nxconvert
// subcommands mac2jb drives, except "status", which only verifies availability.
type NXFormatAction string

const (
	NXFormatInfo     NXFormatAction = "info"
	NXFormatContents NXFormatAction = "contents"
	NXFormatVerify   NXFormatAction = "verify"
	NXFormatPack     NXFormatAction = "pack"
	NXFormatStatus   NXFormatAction = "status"
)

// Progress is one @PROGRESS report from nxconvert.
type Progress struct {
	Stage   string
	Percent float64
	Message string
}

// NXFormatRequest is one NXFormat invocation. Input is an absolute .nx file, or
// for NXFormatPack an extracted dump folder or a .nup file. Output is the
// absolute .nx destination and is required for NXFormatPack only.
type NXFormatRequest struct {
	Action NXFormatAction
	Input  string
	Output string
}

// NXFormatResult carries captured output and, after a pack, the saved path.
type NXFormatResult struct {
	Output string
	Saved  string
}

// RunNXFormat inspects or packs one .nx archive. Only NXFormatPack writes, and
// it never overwrites an existing file or its own input.
func (c Config) RunNXFormat(ctx context.Context, req NXFormatRequest, onProgress func(Progress)) (NXFormatResult, error) {
	binary, err := c.nxconvert()
	if err != nil {
		return NXFormatResult{}, err
	}
	report := func(line string) {
		if onProgress == nil {
			return
		}
		if progress, ok := parseProgress(line); ok {
			onProgress(progress)
		}
	}
	switch req.Action {
	case NXFormatStatus:
		return NXFormatResult{Output: "nxconvert is ready"}, nil
	case NXFormatInfo, NXFormatContents, NXFormatVerify:
		input, err := nxArchive(req.Input)
		if err != nil {
			return NXFormatResult{}, err
		}
		output, err := c.execNXFormat(ctx, binary, []string{string(req.Action), input}, report)
		return NXFormatResult{Output: output}, err
	case NXFormatPack:
		return c.packNXFormat(ctx, binary, req, report)
	default:
		return NXFormatResult{}, fmt.Errorf("unknown NXFormat operation %q; choose info, contents, verify or pack", req.Action)
	}
}

// packNXFormat writes a new .nx archive beside the caller's destination,
// verifies it, and only then links it into place.
func (c Config) packNXFormat(ctx context.Context, binary string, req NXFormatRequest, report func(string)) (NXFormatResult, error) {
	source, err := filepath.Abs(req.Input)
	if err != nil {
		return NXFormatResult{}, err
	}
	destination, err := filepath.Abs(req.Output)
	if err != nil {
		return NXFormatResult{}, err
	}
	if source == destination {
		return NXFormatResult{}, errors.New("the .nx destination must differ from the source")
	}
	if !strings.EqualFold(filepath.Ext(destination), ".nx") {
		return NXFormatResult{}, errors.New("choose a .nx destination")
	}
	if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		return NXFormatResult{}, errors.New("the .nx destination already exists")
	}
	parent, err := os.Lstat(filepath.Dir(destination))
	if err != nil || !parent.IsDir() {
		return NXFormatResult{}, errors.New("the destination folder is missing")
	}
	stat, err := os.Lstat(source)
	if err != nil {
		return NXFormatResult{}, errors.New("choose an extracted dump folder or a .nup file")
	}
	var command []string
	switch {
	case stat.IsDir() && stat.Mode()&os.ModeSymlink == 0:
		command = []string{"pack-tree", source}
	case stat.Mode().IsRegular() && strings.EqualFold(filepath.Ext(source), ".nup"):
		command = []string{"pack-nup", source}
	default:
		return NXFormatResult{}, errors.New("choose an extracted dump folder or a .nup file")
	}
	suffix, err := randomSuffix()
	if err != nil {
		return NXFormatResult{}, err
	}
	temporary := destination + ".mac2jb-" + suffix + ".partial.nx"
	defer os.Remove(temporary)
	command = append(command, temporary, "--platform", "switch2", "--no-trail", "--frontend")
	if _, err := c.execNXFormat(ctx, binary, command, report); err != nil {
		return NXFormatResult{}, err
	}
	if info, err := os.Lstat(temporary); err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		return NXFormatResult{}, errors.New("nxconvert did not produce an archive")
	}
	if _, err := c.execNXFormat(ctx, binary, []string{"verify", temporary}, report); err != nil {
		return NXFormatResult{}, err
	}
	if err := os.Link(temporary, destination); err != nil {
		if errors.Is(err, os.ErrExist) {
			return NXFormatResult{}, errors.New("the .nx destination already exists")
		}
		return NXFormatResult{}, err
	}
	return NXFormatResult{Saved: destination}, nil
}

func (c Config) execNXFormat(ctx context.Context, binary string, args []string, report func(string)) (string, error) {
	cmd := exec.Command(binary, args...)
	cmd.Dir = c.ToolRoot
	return runSupervised(ctx, cmd, report)
}

// nxArchive validates an existing .nx file.
func nxArchive(path string) (string, error) {
	if path == "" || !filepath.IsAbs(path) || !strings.EqualFold(filepath.Ext(path), ".nx") {
		return "", errors.New("choose an existing .nx file")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("the selected .nx file is missing or unsafe")
	}
	return path, nil
}

// parseProgress reads one "@PROGRESS" line. nxconvert reports either
// key=value pairs ("@PROGRESS stage=pack percent=42 message=romfs") or a
// positional form ("@PROGRESS pack 42 romfs"); both are accepted.
func parseProgress(line string) (Progress, bool) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(line), "@PROGRESS")
	if !ok {
		return Progress{}, false
	}
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return Progress{}, false
	}
	var progress Progress
	if strings.Contains(fields[0], "=") {
		var message []string
		for _, field := range fields {
			key, value, ok := strings.Cut(field, "=")
			if !ok {
				continue
			}
			switch strings.ToLower(key) {
			case "stage":
				progress.Stage = value
			case "percent":
				progress.Percent = parsePercent(value)
			case "message":
				message = append(message, value)
			}
		}
		progress.Message = strings.Join(message, " ")
		return progress, true
	}
	progress.Stage = fields[0]
	if len(fields) > 1 {
		progress.Percent = parsePercent(fields[1])
		fields = fields[1:]
	}
	progress.Message = strings.Join(fields[1:], " ")
	return progress, true
}

func parsePercent(value string) float64 {
	percent, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(value), "%"), 64)
	if err != nil {
		return 0
	}
	return min(max(percent, 0), 100)
}

func randomSuffix() (string, error) {
	buffer := make([]byte, 5)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}
