package engine

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/thedavidweng/any2jb/internal/nativehelper"
	"github.com/thedavidweng/any2jb/manifest"
)

// FetchReferenceArchive downloads the pinned mac2jb archive into cache when
// absent and verifies its SHA-256 digest before returning its path.
func FetchReferenceArchive(ctx context.Context, cache string, m *manifest.Manifest) (string, error) {
	ref, err := m.ReferenceInfo()
	if err != nil {
		return "", err
	}
	wantURL := "https://codeload.github.com/cpypwn/mac2jb/tar.gz/" + ref.Revision
	if ref.Upstream != "https://github.com/cpypwn/mac2jb" || ref.Archive != wantURL || len(ref.ArchiveSHA256) != 64 {
		return "", errors.New("unapproved upstream reference archive")
	}
	if err := privateDir(cache); err != nil {
		return "", err
	}
	archivePath := filepath.Join(cache, "mac2jb-"+ref.Revision+".tar.gz")
	if info, err := os.Lstat(archivePath); err == nil {
		if !info.Mode().IsRegular() {
			return "", errors.New("cached upstream archive is not a regular file")
		}
		if verifyArchive(archivePath, ref.ArchiveSHA256) == nil {
			return archivePath, nil
		}
		if err := os.Remove(archivePath); err != nil {
			return "", err
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ref.Archive, nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("upstream archive download: %s", resp.Status)
	}
	tmp, err := os.CreateTemp(cache, ".mac2jb-")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	_, copyErr := io.Copy(tmp, resp.Body)
	closeErr := tmp.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		return "", err
	}
	if err := verifyArchive(tmp.Name(), ref.ArchiveSHA256); err != nil {
		return "", err
	}
	if err := os.Rename(tmp.Name(), archivePath); err != nil {
		return "", err
	}
	return archivePath, nil
}

// ExtractReferenceFiles unpacks regular files selected by allow (or all regular
// files when allow is nil) from a verified mac2jb archive into dest.
func ExtractReferenceFiles(archivePath, revision, dest string, allow func(string) bool) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	prefix := "mac2jb-" + revision + "/"
	seen := make(map[string]bool)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeXGlobalHeader, tar.TypeXHeader, tar.TypeDir:
			continue
		case tar.TypeSymlink, tar.TypeLink:
			// Upstream developer sandboxes may contain symlinks outside our staged trees;
			// skip unrequested links, and reject any link inside a requested path.
			rel, ok := strings.CutPrefix(header.Name, prefix)
			if ok && (allow == nil || allow(rel)) {
				return fmt.Errorf("archive contains link at requested path %q", rel)
			}
			continue
		case tar.TypeReg:
		default:
			continue
		}
		rel, ok := strings.CutPrefix(header.Name, prefix)
		if !ok || !fs.ValidPath(rel) || strings.Contains(rel, `\`) {
			return fmt.Errorf("invalid archive entry %q", header.Name)
		}
		if allow != nil && !allow(rel) {
			continue
		}
		if seen[rel] {
			return fmt.Errorf("duplicate archive entry %q", rel)
		}
		seen[rel] = true
		target := filepath.Join(dest, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(out, reader)
		if err := errors.Join(copyErr, out.Close()); err != nil {
			return err
		}
	}
	return nil
}

func ensureExtractedReference(ctx context.Context, cache string, m *manifest.Manifest) (string, error) {
	ref, err := m.ReferenceInfo()
	if err != nil {
		return "", err
	}
	archivePath, err := FetchReferenceArchive(ctx, cache, m)
	if err != nil {
		return "", err
	}
	extracted := filepath.Join(cache, "mac2jb-"+ref.Revision)
	marker := filepath.Join(extracted, ".verified-"+ref.ArchiveSHA256)
	if info, err := os.Lstat(marker); err == nil && info.Mode().IsRegular() {
		return extracted, nil
	}
	if err := os.RemoveAll(extracted); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(cache, ".mac2jb-extract-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	// Extract all regular files (skipping any upstream symlinks outside staged subtrees);
	// manifest verification gates every file before copying into resources/.
	f, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", err
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	prefix := "mac2jb-" + ref.Revision + "/"
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		rel, ok := strings.CutPrefix(header.Name, prefix)
		if !ok || !fs.ValidPath(rel) || strings.Contains(rel, `\`) {
			return "", fmt.Errorf("invalid archive entry %q", header.Name)
		}
		target := filepath.Join(tmp, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return "", err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return "", err
		}
		_, copyErr := io.Copy(out, reader)
		if err := errors.Join(copyErr, out.Close()); err != nil {
			return "", err
		}
	}
	if err := os.WriteFile(filepath.Join(tmp, filepath.Base(marker)), []byte(ref.Revision+"\n"), 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, extracted); err != nil {
		return "", err
	}
	return extracted, nil
}

func writeAssetFile(target string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".asset-")
	if err != nil {
		return err
	}
	err = tmp.Chmod(mode)
	if err == nil {
		_, err = tmp.Write(data)
	}
	err = errors.Join(err, tmp.Close())
	if err == nil {
		err = os.Rename(tmp.Name(), target)
	}
	os.Remove(tmp.Name())
	return err
}

// PrepareRuntime fetches and verifies all pinned Node runtimes, bundled host
// profiles, session tools, launcher saves, notices, and platform USB sidecars.
func PrepareRuntime(ctx context.Context, cache, assets, platform string, m *manifest.Manifest) error {
	if platform != "" {
		if err := PrepareNodeForPlatform(cache, assets, platform, m); err != nil {
			return err
		}
	} else {
		if err := PrepareNode(cache, assets, m); err != nil {
			return err
		}
	}
	needUpstream := false
	for _, p := range m.Profiles {
		if p.Distribution.Available() {
			needUpstream = true
			break
		}
	}
	if !needUpstream {
		for _, c := range m.Components {
			if c.Distribution.Available() && c.Upstream != "any2jb" &&
				c.Upstream != "https://github.com/egoist/mygo" && c.Upstream != "https://nodejs.org" {
				needUpstream = true
				break
			}
		}
	}
	if !needUpstream {
		return nil
	}
	extracted, err := ensureExtractedReference(ctx, cache, m)
	if err != nil {
		return err
	}
	absCache, err := filepath.Abs(cache)
	if err != nil {
		return err
	}
	absExtracted, err := filepath.Abs(extracted)
	if err != nil {
		return err
	}
	sourceRoot, err := os.OpenRoot(absExtracted)
	if err != nil {
		return err
	}
	defer sourceRoot.Close()

	copyVerified := func(rel string, mode os.FileMode) error {
		data, err := sourceRoot.ReadFile(rel)
		if err != nil {
			return err
		}
		return writeAssetFile(filepath.Join(assets, filepath.FromSlash(rel)), data, mode)
	}

	for _, p := range m.Profiles {
		if !p.Distribution.Available() {
			continue
		}
		files, err := m.ProfileFiles(absExtracted, p.ID)
		if err != nil {
			return err
		}
		for _, f := range files {
			if err := copyVerified(f, 0o644); err != nil {
				return err
			}
		}
	}

	needDarwinHelpers := false
	for _, c := range m.Components {
		if !c.Distribution.Available() {
			continue
		}
		if c.Upstream == "any2jb" || c.Upstream == "https://github.com/egoist/mygo" || c.Upstream == "https://nodejs.org" {
			continue
		}
		if platform != "" && c.ResourcePlatform != "" && c.ResourcePlatform != platform {
			continue
		}
		if c.Source != nil {
			files, err := m.ToolSourceFiles(absExtracted, c.ID)
			if err != nil {
				return err
			}
			for _, f := range files {
				if err := copyVerified(f, 0o644); err != nil {
					return err
				}
			}
			continue
		}
		if c.ID == "libusbd-dylib" || slices.Contains(m.Runtime.Helpers, c.ID) {
			needDarwinHelpers = true
			continue
		}
		data, err := m.ReadAt(absExtracted, c.ID, c.Path)
		if err != nil {
			return err
		}
		if c.PackagedGzip != "" {
			var buf bytes.Buffer
			gz, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
			if err != nil {
				return err
			}
			if _, err := gz.Write(data); err != nil {
				return err
			}
			if err := gz.Close(); err != nil {
				return err
			}
			_ = os.Remove(filepath.Join(assets, c.ResourcePlatform, filepath.FromSlash(c.Path)))
			target := filepath.Join(assets, c.ResourcePlatform, filepath.FromSlash(c.PackagedGzip))
			if err := writeAssetFile(target, buf.Bytes(), 0o644); err != nil {
				return err
			}
			continue
		}
		mode := os.FileMode(0o644)
		if c.ID == "driver-installer" || c.ID == "usbgadget-binary" {
			mode = 0o755
		}
		target := filepath.Join(assets, c.ResourcePlatform, filepath.FromSlash(c.Path))
		if err := writeAssetFile(target, data, mode); err != nil {
			return err
		}
	}

	if !needDarwinHelpers {
		return nil
	}
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		return errors.New("building darwin-arm64 USB helpers requires Apple Silicon macOS (pass -platform linux-amd64 or linux-arm64 on Linux)")
	}
	stage, err := os.MkdirTemp(absCache, ".native-stage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err := nativehelper.Build(ctx, io.Discard, absExtracted, stage, m); err != nil {
		return err
	}
	for _, item := range []struct {
		id, stageRel string
	}{
		{"libusbd-dylib", "vendor/libusbd/libusbd.dylib"},
		{"sender", "sender"},
		{"commands", "commands"},
		{"keyboard", "keyboard"},
		{"civswitch-hid", "civswitch-hid"},
	} {
		var comp manifest.Component
		found := false
		for _, candidate := range m.Components {
			if candidate.ID == item.id {
				comp = candidate
				found = true
				break
			}
		}
		if !found || !comp.Distribution.Available() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(stage, filepath.FromSlash(item.stageRel)))
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != comp.SHA256 {
			return fmt.Errorf("%s compiled digest %x does not match manifest %s", comp.ID, sum, comp.SHA256)
		}
		if err := manifest.VerifyBinaryArchitecture("darwin-arm64", data); err != nil {
			return fmt.Errorf("%s: %w", comp.ID, err)
		}
		target := filepath.Join(assets, comp.ResourcePlatform, filepath.FromSlash(comp.Path))
		if err := writeAssetFile(target, data, 0o755); err != nil {
			return err
		}
	}
	return nil
}
