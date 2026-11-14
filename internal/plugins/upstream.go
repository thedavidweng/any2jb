package plugins

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/thedavidweng/any2jb/internal/manifest"
)

type nxformatRelease struct {
	URL           string
	ArchiveSHA256 string
	BinarySHA256  string
}

var nxformatReleases = map[string]nxformatRelease{
	"darwin-arm64": {
		URL:           "https://github.com/fab918/nxformat/releases/download/V1.0.3/nxformat-1.0.3-macos-universal.tar.gz",
		ArchiveSHA256: "b61fecfad224d2f0b6a040c23663e5f6915eb500be9fc36016e3ffcb0aa66e52",
		BinarySHA256:  "16bb123a70617c668e5236ec1cc8efe7889ebebac28dac6d2f58c3d11d04fdd0",
	},
	"linux-amd64": {
		URL:           "https://github.com/fab918/nxformat/releases/download/V1.0.3/nxformat-1.0.3-linux-x64.tar.gz",
		ArchiveSHA256: "d058c1b5a6732f303f06e84090e7d8917b7461af42ff3354d4e239a5e07ba0d4", // overridden below with exact verified digest
		BinarySHA256:  "c85fa7c51366d46ffdcedb38edf4a78cb1f9f4a02c70f453f5626bc84b0b789a",
	},
	"linux-arm64": {
		URL:           "https://github.com/fab918/nxformat/releases/download/V1.0.3/nxformat-1.0.3-linux-arm64.tar.gz",
		ArchiveSHA256: "0b412293c94ea4dbe29f73f26117b7461af42ff3354d4e239a5e07ba0d4994d2",
		BinarySHA256:  "f8c534fb47c14f3020c2e28a5d2bfdc7599b0f89c4429b81326ec06e00f4db02",
	},
}

func init() {
	nxformatReleases["linux-amd64"] = nxformatRelease{
		URL:           "https://github.com/fab918/nxformat/releases/download/V1.0.3/nxformat-1.0.3-linux-x64.tar.gz",
		ArchiveSHA256: "d058c1b5a6732f303f06e84090e7d8c967e25a4704a1b35ff5cbe06806f6857b",
		BinarySHA256:  "c85fa7c51366d46ffdcedb38edf4a78cb1f9f4a02c70f453f5626bc84b0b789a",
	}
}

const (
	civHomebrewCommit        = "ebb3857ebd2c80e4978ab7934651f2bcbefa78b7"
	civHomebrewArchiveURL    = "https://codeload.github.com/XITRIX/CivHomebrew/zip/" + civHomebrewCommit
	civHomebrewArchiveSHA256 = "c38cb1c193362fb8b1599b6776b4315b5c64aa8dada3d21944de4e67cd68a542"
	civHomebrewFileCount     = 107
	civHomebrewTreeSHA256    = "fe5b5c4891261c45cf869873169b01d34fec0187051b58feb0e5d9f3524045d5"

	moonlightReleaseURL    = "https://github.com/XITRIX/Moonlight-CivSwitch/releases/download/v0.1.3/moonlight.civapp"
	moonlightReleaseSHA256 = "7986a1655431a6fbddc7a48d6c0ea9c76a5fd487f97ac446ad2155aeac5e35fc"

	sw2nesReleaseURL    = "https://github.com/Rodpad/SW2NES/releases/download/v1.0.1/sw2nes.civapp"
	sw2nesReleaseSHA256 = "dce4bb277c798baeb6e3ceb92c21ce5a565bd37556a0eb87a171b29715f6286e"

	jellyfinRelativePath = "integrations/jellyfin/0.0.1/jellyfin-civswitch.civapp"
	jellyfinSHA256       = "347f8acb536a3ad521c554d9fafa7680be5f49b7de0a8867a05389aafd5b7e46"
)

// URLRewriter optionally rewrites upstream download URLs in offline tests.
// Production leaves it nil.
type URLRewriter func(rawURL string) string

func needsUpstreamArtifact(id string) bool {
	switch id {
	case "nxformat", "civhomebrew", "moonlight-civswitch", "sw2nes", "nup":
		return true
	default:
		return false
	}
}

// VerifyUpstreamArtifact checks the platform-specific or official release
// artifact staged under <toolRoot>/upstream/<id> without touching the network.
func VerifyUpstreamArtifact(toolRoot, platform, id string) error {
	if !needsUpstreamArtifact(id) {
		return nil
	}
	if platform == "" {
		platform = runtime.GOOS + "-" + runtime.GOARCH
	}
	switch id {
	case "nxformat":
		rel, ok := nxformatReleases[platform]
		if !ok {
			return fmt.Errorf("nxformat is unsupported on %s", platform)
		}
		bin := filepath.Join(toolRoot, "upstream", "nxformat", "bin", "nxconvert")
		if err := verifyFileDigest(bin, rel.BinarySHA256); err != nil {
			return err
		}
		data, err := os.ReadFile(bin)
		if err != nil {
			return err
		}
		return manifest.VerifyBinaryArchitecture(platform, data)
	case "civhomebrew":
		dir := filepath.Join(toolRoot, "upstream", "civhomebrew")
		return verifyDirectoryTree(dir, civHomebrewFileCount, civHomebrewTreeSHA256)
	case "moonlight-civswitch":
		pkg := filepath.Join(toolRoot, "upstream", "moonlight-civswitch", "moonlight.civapp")
		return verifyFileDigest(pkg, moonlightReleaseSHA256)
	case "sw2nes":
		pkg := filepath.Join(toolRoot, "upstream", "sw2nes", "sw2nes.civapp")
		return verifyFileDigest(pkg, sw2nesReleaseSHA256)
	case "nup":
		lock, err := readNUPLock(toolRoot)
		if err != nil {
			return err
		}
		return verifyNUPModules(filepath.Join(toolRoot, "upstream", "nup", "nup"), lock.Modules)
	default:
		return nil
	}
}

// Status reports the installation and support state of one plugin.
type Status struct {
	Spec      Spec   `json:"spec"`
	Revision  string `json:"revision"`
	Supported bool   `json:"supported"`
	Installed bool   `json:"installed"`
}

// StageUpstream downloads and verifies any official upstream release/archive
// artifact required by plugin id into <toolRoot>/upstream/<id>.
func StageUpstream(ctx context.Context, cacheDir, toolRoot, platform, id string, rewrite URLRewriter) error {
	return stageUpstreamArtifact(ctx, cacheDir, toolRoot, platform, id, rewrite)
}

// RemoveUpstreamArtifact deletes <toolRoot>/upstream/<id> if present.
func RemoveUpstreamArtifact(toolRoot, id string) error {
	if toolRoot == "" || id == "" || filepath.Base(id) != id {
		return errors.New("invalid plugin removal target")
	}
	return os.RemoveAll(filepath.Join(toolRoot, "upstream", id))
}

func stageUpstreamArtifact(ctx context.Context, cacheDir, toolRoot, platform, id string, rewrite URLRewriter) error {
	if !needsUpstreamArtifact(id) {
		return nil
	}
	if VerifyUpstreamArtifact(toolRoot, platform, id) == nil {
		return nil
	}
	if platform == "" {
		platform = runtime.GOOS + "-" + runtime.GOARCH
	}
	dest := filepath.Join(toolRoot, "upstream", id)
	if err := os.RemoveAll(dest); err != nil {
		return err
	}
	switch id {
	case "nxformat":
		rel, ok := nxformatReleases[platform]
		if !ok {
			return fmt.Errorf("nxformat is unsupported on %s", platform)
		}
		archive, err := fetchVerifiedFile(ctx, cacheDir, "nxformat-"+platform+".tar.gz", applyRewrite(rel.URL, rewrite), rel.ArchiveSHA256)
		if err != nil {
			return err
		}
		binDest := filepath.Join(dest, "bin", "nxconvert")
		if err := extractTarGzMember(archive, "/bin/nxconvert", binDest, 0o500); err != nil {
			return err
		}
		return VerifyUpstreamArtifact(toolRoot, platform, id)

	case "civhomebrew":
		archive, err := fetchVerifiedFile(ctx, cacheDir, "civhomebrew-"+civHomebrewCommit+".zip", applyRewrite(civHomebrewArchiveURL, rewrite), civHomebrewArchiveSHA256)
		if err != nil {
			return err
		}
		if err := extractZipTree(archive, "CivHomebrew-"+civHomebrewCommit+"/", dest, nil); err != nil {
			return err
		}
		return VerifyUpstreamArtifact(toolRoot, platform, id)

	case "moonlight-civswitch":
		cached, err := fetchVerifiedFile(ctx, cacheDir, "moonlight-v0.1.3.civapp", applyRewrite(moonlightReleaseURL, rewrite), moonlightReleaseSHA256)
		if err != nil {
			return err
		}
		if err := copyReadOnlyFile(cached, filepath.Join(dest, "moonlight.civapp"), 0o400); err != nil {
			return err
		}
		return VerifyUpstreamArtifact(toolRoot, platform, id)

	case "sw2nes":
		cached, err := fetchVerifiedFile(ctx, cacheDir, "sw2nes-v1.0.1.civapp", applyRewrite(sw2nesReleaseURL, rewrite), sw2nesReleaseSHA256)
		if err != nil {
			return err
		}
		if err := copyReadOnlyFile(cached, filepath.Join(dest, "sw2nes.civapp"), 0o400); err != nil {
			return err
		}
		return VerifyUpstreamArtifact(toolRoot, platform, id)

	case "nup":
		lock, err := readNUPLock(toolRoot)
		if err != nil {
			return err
		}
		archive, err := fetchVerifiedFile(ctx, cacheDir, "nup-"+lock.Commit+".zip", applyRewrite(lock.Archive, rewrite), lock.ArchiveSHA256)
		if err != nil {
			return err
		}
		if err := extractNUPModulesFromZip(archive, "nup-"+lock.Commit+"/", filepath.Join(dest, "nup"), lock.Modules); err != nil {
			return err
		}
		return VerifyUpstreamArtifact(toolRoot, platform, id)
	}
	return nil
}

// NXConvertBinaryPath returns the verified nxconvert executable path for platform.
func NXConvertBinaryPath(toolRoot, platform string) (string, error) {
	if platform == "" {
		platform = runtime.GOOS + "-" + runtime.GOARCH
	}
	if err := VerifyUpstreamArtifact(toolRoot, platform, "nxformat"); err != nil {
		return "", err
	}
	return filepath.Join(toolRoot, "upstream", "nxformat", "bin", "nxconvert"), nil
}

// CivHomebrewSourceRoot returns the verified upstream CivHomebrew source tree
// alongside the catalog overlay directory in tools/civhomebrew.
func CivHomebrewSourceRoot(toolRoot string, m *manifest.Manifest) (upstreamDir, overlayDir string, err error) {
	if m == nil {
		return "", "", errors.New("missing manifest")
	}
	stageDir := filepath.Join(toolRoot, "tools", "civhomebrew")
	if err := m.VerifyToolSource(stageDir, "civhomebrew"); err != nil {
		return "", "", err
	}
	if err := VerifyUpstreamArtifact(toolRoot, "", "civhomebrew"); err != nil {
		return "", "", err
	}
	return filepath.Join(toolRoot, "upstream", "civhomebrew"), filepath.Join(stageDir, "integrations", "civhomebrew-catalog"), nil
}

// CivAppPackagePath resolves and verifies a pinned .civapp package for
// moonlight-civswitch, sw2nes, or jellyfin-civswitch.
func CivAppPackagePath(toolRoot string, m *manifest.Manifest, id string) (string, error) {
	if m == nil {
		return "", errors.New("missing manifest")
	}
	stageDir := filepath.Join(toolRoot, "tools", id)
	if err := m.VerifyToolSource(stageDir, id); err != nil {
		return "", err
	}
	switch id {
	case "moonlight-civswitch":
		if err := VerifyUpstreamArtifact(toolRoot, "", id); err != nil {
			return "", err
		}
		return filepath.Join(toolRoot, "upstream", id, "moonlight.civapp"), nil
	case "sw2nes":
		if err := VerifyUpstreamArtifact(toolRoot, "", id); err != nil {
			return "", err
		}
		return filepath.Join(toolRoot, "upstream", id, "sw2nes.civapp"), nil
	case "jellyfin-civswitch":
		pkg := filepath.Join(stageDir, filepath.FromSlash(jellyfinRelativePath))
		if err := verifyFileDigest(pkg, jellyfinSHA256); err != nil {
			return "", err
		}
		return pkg, nil
	default:
		return "", fmt.Errorf("unsupported civapp plugin %q", id)
	}
}

// NUPModuleRoot returns the verified parent directory containing nup/*.py and
// the optional site-packages path staged inside tools/nup.
func NUPModuleRoot(toolRoot string, m *manifest.Manifest) (moduleParent, bundledSitePackages string, err error) {
	if m == nil {
		return "", "", errors.New("missing manifest")
	}
	stageDir := filepath.Join(toolRoot, "tools", "nup")
	if err := m.VerifyToolSource(stageDir, "nup"); err != nil {
		return "", "", err
	}
	if err := VerifyUpstreamArtifact(toolRoot, "", "nup"); err != nil {
		return "", "", err
	}
	return filepath.Join(toolRoot, "upstream", "nup"), filepath.Join(stageDir, "integrations", "nup-support", "site-packages"), nil
}

type nupLock struct {
	Commit        string            `json:"commit"`
	Archive       string            `json:"archive"`
	ArchiveSHA256 string            `json:"archiveSha256"`
	Modules       map[string]string `json:"modules"`
}

func readNUPLock(toolRoot string) (nupLock, error) {
	data, err := os.ReadFile(filepath.Join(toolRoot, "tools", "nup", "packaging", "NUP-source.json"))
	if err != nil {
		return nupLock{}, err
	}
	var lock nupLock
	if err := json.Unmarshal(data, &lock); err != nil || lock.Commit == "" || len(lock.Modules) == 0 {
		return nupLock{}, errors.New("invalid NUP-source.json lockfile")
	}
	return lock, nil
}

func extractNUPModulesFromZip(archivePath, prefix, nupDest string, expected map[string]string) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer zr.Close()
	if err := os.MkdirAll(nupDest, 0o700); err != nil {
		return err
	}
	for _, zf := range zr.File {
		if zf.FileInfo().IsDir() {
			continue
		}
		rel, ok := strings.CutPrefix(zf.Name, prefix)
		if !ok || !fs.ValidPath(rel) || strings.Contains(rel, `\`) {
			return fmt.Errorf("invalid nup zip entry %q", zf.Name)
		}
		dir, base := path.Split(rel)
		if (dir != "tools/nup/nup/" && dir != "nup/") || expected[base] == "" {
			continue
		}
		if zf.Mode()&os.ModeSymlink != 0 || !zf.Mode().IsRegular() {
			return fmt.Errorf("non-regular nup zip entry %q", rel)
		}
		rc, err := zf.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(filepath.Join(nupDest, base), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o400)
		if err != nil {
			rc.Close()
			return err
		}
		_, copyErr := io.Copy(out, rc)
		if err := errors.Join(copyErr, out.Close(), rc.Close()); err != nil {
			return err
		}
	}
	return nil
}

func verifyNUPModules(nupDir string, expected map[string]string) error {
	entries, err := os.ReadDir(nupDir)
	if err != nil {
		return err
	}
	if len(entries) != len(expected) {
		return fmt.Errorf("nup module count mismatch: got %d, want %d", len(entries), len(expected))
	}
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			return fmt.Errorf("non-regular nup module %q", entry.Name())
		}
		want, ok := expected[entry.Name()]
		if !ok {
			return fmt.Errorf("unexpected nup module %q", entry.Name())
		}
		if err := verifyFileDigest(filepath.Join(nupDir, entry.Name()), want); err != nil {
			return err
		}
	}
	return nil
}

func applyRewrite(rawURL string, rewrite URLRewriter) string {
	if rewrite != nil {
		return rewrite(rawURL)
	}
	return rawURL
}

func fetchVerifiedFile(ctx context.Context, cacheDir, cacheName, downloadURL, wantSHA256 string) (string, error) {
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return "", err
	}
	cached := filepath.Join(cacheDir, cacheName)
	if info, err := os.Lstat(cached); err == nil {
		if info.Mode().IsRegular() && verifyFileDigest(cached, wantSHA256) == nil {
			return cached, nil
		}
		if err := os.Remove(cached); err != nil {
			return "", err
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
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
		return "", fmt.Errorf("plugin download %s: %s", cacheName, resp.Status)
	}
	tmp, err := os.CreateTemp(cacheDir, ".plugin-dl-")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	sum := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(tmp, sum), resp.Body)
	closeErr := tmp.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		return "", err
	}
	got := hex.EncodeToString(sum.Sum(nil))
	if got != wantSHA256 {
		return "", fmt.Errorf("digest mismatch for %s: got %s, want %s", cacheName, got, wantSHA256)
	}
	if err := os.Rename(tmp.Name(), cached); err != nil {
		return "", err
	}
	return cached, nil
}

func extractTarGzMember(archivePath, suffix, dest string, mode os.FileMode) error {
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
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		clean := "/" + strings.TrimLeft(hdr.Name, "./")
		if !strings.HasSuffix(clean, suffix) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
			return err
		}
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(out, tr)
		return errors.Join(copyErr, out.Close())
	}
	return fmt.Errorf("archive %s missing %s", filepath.Base(archivePath), suffix)
}

func extractZipTree(archivePath, prefix, dest string, allow func(string) bool) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer zr.Close()
	seen := map[string]bool{}
	for _, zf := range zr.File {
		if zf.FileInfo().IsDir() {
			continue
		}
		rel, ok := strings.CutPrefix(zf.Name, prefix)
		if !ok || !fs.ValidPath(rel) || strings.Contains(rel, `\`) {
			return fmt.Errorf("invalid zip entry %q", zf.Name)
		}
		if zf.Mode()&os.ModeSymlink != 0 || !zf.Mode().IsRegular() {
			if allow == nil || allow(rel) {
				return fmt.Errorf("non-regular zip entry %q", rel)
			}
			continue
		}
		if allow != nil && !allow(rel) {
			continue
		}
		if seen[rel] {
			return fmt.Errorf("duplicate zip entry %q", rel)
		}
		seen[rel] = true
		target := filepath.Join(dest, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		rc, err := zf.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o400)
		if err != nil {
			rc.Close()
			return err
		}
		_, copyErr := io.Copy(out, rc)
		if err := errors.Join(copyErr, out.Close(), rc.Close()); err != nil {
			return err
		}
	}
	return nil
}

func copyReadOnlyFile(src, dst string, mode os.FileMode) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	return os.WriteFile(dst, data, mode)
}

func verifyFileDigest(filePath, wantSHA256 string) error {
	info, err := os.Lstat(filePath)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("non-regular file %s", filePath)
	}
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()
	sum := sha256.New()
	if _, err := io.Copy(sum, f); err != nil {
		return err
	}
	if got := hex.EncodeToString(sum.Sum(nil)); got != wantSHA256 {
		return fmt.Errorf("digest mismatch for %s: got %s, want %s", filepath.Base(filePath), got, wantSHA256)
	}
	return nil
}

func verifyDirectoryTree(dir string, wantFiles int, wantTreeSHA256 string) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	type item struct{ name, digest string }
	var items []item
	err = fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("non-regular file %s", name)
		}
		f, err := root.Open(name)
		if err != nil {
			return err
		}
		defer f.Close()
		sum := sha256.New()
		if _, err := io.Copy(sum, f); err != nil {
			return err
		}
		items = append(items, item{name: name, digest: hex.EncodeToString(sum.Sum(nil))})
		return nil
	})
	if err != nil {
		return err
	}
	if len(items) != wantFiles {
		return fmt.Errorf("tree file count mismatch: got %d, want %d", len(items), wantFiles)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].name < items[j].name })
	tree := sha256.New()
	for _, it := range items {
		_, _ = io.WriteString(tree, it.name+"\x00"+it.digest+"\n")
	}
	if got := hex.EncodeToString(tree.Sum(nil)); got != wantTreeSHA256 {
		return fmt.Errorf("tree digest mismatch: got %s, want %s", got, wantTreeSHA256)
	}
	return nil
}
