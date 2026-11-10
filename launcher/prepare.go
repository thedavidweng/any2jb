package launcher

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/thedavidweng/any2jb/manifest"
	"github.com/thedavidweng/any2jb/savekit"
)

// Prepare extracts only a verified, pinned package into a new private folder.
// Full-game Seedsow patches the user's verified full-game export, not demo data.
func Prepare(ctx context.Context, m *manifest.Manifest, c manifest.Component, root, dest, node, backup string) (savekit.Preview, error) {
	if c.Launcher == nil {
		return savekit.Preview{}, errors.New("not a launcher save package")
	}
	if err := m.VerifyToolSource(root, c.ID); err != nil {
		return savekit.Preview{}, err
	}
	name := filepath.Join(root, filepath.FromSlash(c.Path))
	info, err := os.Lstat(name)
	if err != nil {
		return savekit.Preview{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > 8<<20 {
		return savekit.Preview{}, errors.New("unsupported launcher archive")
	}
	data, err := os.ReadFile(name)
	if err != nil {
		return savekit.Preview{}, err
	}
	if fmt.Sprintf("%x", sha256.Sum256(data)) != c.SHA256 {
		return savekit.Preview{}, errors.New("launcher package digest mismatch")
	}
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return savekit.Preview{}, err
	}
	seen := map[string]bool{}
	var total uint64
	count := 0
	for _, f := range z.File {
		p := strings.TrimSuffix(f.Name, "/")
		if !fs.ValidPath(p) || strings.Contains(p, `\`) || seen[p] {
			return savekit.Preview{}, errors.New("unsafe launcher archive path")
		}
		seen[p] = true
		if f.FileInfo().IsDir() {
			continue
		}
		if !f.Mode().IsRegular() || f.UncompressedSize64 > 64<<20 {
			return savekit.Preview{}, errors.New("unsupported launcher archive entry")
		}
		for _, part := range strings.Split(p, "/") {
			if !savekit.SafeRootName(part) {
				return savekit.Preview{}, errors.New("unsafe launcher archive path")
			}
		}
		total += f.UncompressedSize64
		if total > 256<<20 {
			return savekit.Preview{}, errors.New("launcher archive is too large")
		}
		if p != ".nx_save_meta.bin" {
			count++
		}
	}
	if count != c.Launcher.Files || count == 0 {
		return savekit.Preview{}, errors.New("launcher package file count mismatch")
	}
	if err := checkSaveMetadata(z, c.Launcher.TitleID); err != nil {
		return savekit.Preview{}, err
	}
	if err := os.Mkdir(dest, 0700); err != nil {
		return savekit.Preview{}, err
	}
	for _, f := range z.File {
		if f.FileInfo().IsDir() || f.Name == ".nx_save_meta.bin" {
			continue
		}
		name := filepath.Join(dest, filepath.FromSlash(f.Name))
		if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
			return savekit.Preview{}, err
		}
		in, err := f.Open()
		if err != nil {
			return savekit.Preview{}, err
		}
		out, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			in.Close()
			return savekit.Preview{}, err
		}
		_, copyErr := io.Copy(out, io.LimitReader(in, 64<<20+1))
		err = errors.Join(copyErr, out.Close(), in.Close())
		if err != nil {
			return savekit.Preview{}, err
		}
	}
	if c.Launcher.FullGame {
		export, err := savekit.ReadExport(backup)
		if err != nil {
			return savekit.Preview{}, err
		}
		if export.TitleID != c.Launcher.TitleID || export.Archive != (savekit.Archive{Space: 1, Type: 1}) {
			return savekit.Preview{}, errors.New("the full-game patch requires a verified full-game account save")
		}
		ctx, cancel := context.WithTimeout(ctx, time.Minute)
		defer cancel()
		// This pinned upstream helper only patches local save bytes. It does
		// not receive a workspace, lease or console address.
		cmd := exec.CommandContext(ctx, node, "-e", `require(process.argv[1]).stageFullSeedsow(process.argv[2],process.argv[3],{preferUpdate:false}).catch(()=>process.exitCode=1)`,
			filepath.Join(root, "scripts/seedsow-full-save.cjs"), dest, backup)
		if err := cmd.Run(); err != nil {
			return savekit.Preview{}, errors.New("the full-game autosave is not compatible with the pinned Seedsow BOWS v1003 patch")
		}
	}
	return savekit.PreviewWrite(dest)
}

// checkSaveMetadata verifies the archive's own save metadata, when it carries
// any, names the recorded title: a package exported from another game is
// refused before it reaches the write gate.
func checkSaveMetadata(z *zip.Reader, titleID string) error {
	meta, err := z.Open(".nx_save_meta.bin")
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer meta.Close()
	header := make([]byte, 13)
	if _, err := io.ReadFull(meta, header); err != nil {
		return errors.New("the launcher package has unreadable save metadata")
	}
	if fmt.Sprintf("%016x", binary.LittleEndian.Uint64(header[5:])) != titleID {
		return errors.New("the launcher package belongs to another game than its recorded title")
	}
	return nil
}

// CheckExisting refuses partial or unrecognised core files before any write.
func CheckExisting(c manifest.Component, live savekit.Export, preview savekit.Preview) error {
	current := map[string]string{}
	for _, f := range live.Files {
		current[strings.TrimPrefix(f.Path, "/")] = f.SHA256
	}
	expected := map[string]string{}
	for _, f := range preview.Files {
		expected[strings.TrimPrefix(f.Path, "/")] = f.SHA256
	}
	found := 0
	for _, name := range c.Launcher.Core {
		if _, ok := current[name]; ok {
			found++
		}
	}
	if found == 0 {
		return nil
	}
	for _, name := range c.Launcher.Core {
		if current[name] == "" || current[name] != expected[name] {
			return errors.New("the existing launcher has unrecognised or partial core files; the verified backup is kept and no files were changed")
		}
	}
	return nil
}
