package manifest

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"slices"
	"sort"
	"strings"
)

// VerifyProfile checks the pinned distribution inventory, every source file,
// the source identity, provenance artifacts, and the selected bootstrap.
// root is an immutable staged runtime, using the paths in profiles.json.
func (m *Manifest) VerifyProfile(root, id string) error {
	_, err := m.ProfileFiles(root, id)
	return err
}

// ProfileFiles verifies a profile under root and returns the exact sorted list
// of root-relative paths that belong to it.
func (m *Manifest) ProfileFiles(root, id string) ([]string, error) {
	p, err := m.Profile(id)
	if err != nil {
		return nil, err
	}
	sums, err := verifyFile(root, id, p.DistributionManifest, p.DistributionManifestSHA256)
	if err != nil {
		return nil, err
	}
	files := []string{p.DistributionManifest, p.Bootstrap}
	recorded := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSuffix(string(sums), "\n"), "\n") {
		digest, name, ok := strings.Cut(line, "  ")
		_, duplicate := recorded[name]
		if !ok || !fs.ValidPath(name) || strings.ContainsAny(name, "\\\r") || duplicate {
			return nil, &IntegrityError{Component: id, Path: p.DistributionManifest, Err: errors.New("invalid or duplicate SHA256SUMS entry")}
		}
		rel := path.Join(p.SourceDirectory, name)
		if _, err := verifyFile(root, id, rel, digest); err != nil {
			return nil, err
		}
		recorded[name] = digest
		files = append(files, rel)
	}
	if err := verifyInventory(root, p, recorded); err != nil {
		return nil, err
	}
	// Match mac2jb's sourceDigest: sorted [relative path, file SHA-256] pairs
	// covering engine/ and entrypoints.
	names := make([]string, 0, len(recorded))
	for name := range recorded {
		if name == "run.cjs" || name == "bootstrap.bas" ||
			name == "rpc.cjs" || name == "dump-firmware.cjs" || strings.HasPrefix(name, "engine/") {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	pairs := make([][2]string, 0, len(names))
	for _, name := range names {
		pairs = append(pairs, [2]string{name, recorded[name]})
	}
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(pairs); err != nil {
		return nil, err
	}
	digest := sha256.Sum256(bytes.TrimSuffix(encoded.Bytes(), []byte("\n")))
	if hex.EncodeToString(digest[:]) != p.SourceSHA256 {
		return nil, &IntegrityError{Component: id, Path: p.SourceDirectory, Err: errors.New("source SHA-256 mismatch")}
	}
	for _, artifact := range []struct{ name, digest string }{
		{p.UpstreamManifest, p.UpstreamManifestSHA256},
		{p.LocalPatch, p.LocalPatchSHA256},
	} {
		if artifact.name != "" {
			if _, err := verifyFile(root, id, artifact.name, artifact.digest); err != nil {
				return nil, err
			}
			files = append(files, artifact.name)
		}
	}
	bootstrap, err := verifyFile(root, id, p.Bootstrap, p.BootstrapSHA256)
	if err != nil {
		return nil, err
	}
	source, err := readFile(root, path.Join(p.SourceDirectory, "bootstrap.bas"))
	if err != nil || !bytes.Equal(bootstrap, source) {
		return nil, &IntegrityError{Component: id, Path: p.Bootstrap, Err: errors.New("bootstrap differs from host source")}
	}
	slices.Sort(files)
	return slices.Compact(files), nil
}

func verifyInventory(root string, p Profile, recorded map[string]string) error {
	if !fs.ValidPath(p.SourceDirectory) || strings.Contains(p.SourceDirectory, `\`) {
		return &IntegrityError{Component: p.ID, Path: p.SourceDirectory, Err: errors.New("invalid source directory")}
	}
	dir, err := os.OpenRoot(root)
	if err != nil {
		return &IntegrityError{Component: p.ID, Path: p.SourceDirectory, Err: err}
	}
	defer dir.Close()
	err = fs.WalkDir(dir.FS(), p.SourceDirectory, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative := strings.TrimPrefix(name, p.SourceDirectory+"/")
		if !entry.Type().IsRegular() {
			return fmt.Errorf("non-regular source file: %s", relative)
		}
		if _, ok := recorded[relative]; !ok && name != p.DistributionManifest {
			return fmt.Errorf("unrecorded source file: %s", relative)
		}
		return nil
	})
	if err != nil {
		return &IntegrityError{Component: p.ID, Path: p.SourceDirectory, Err: err}
	}
	return nil
}
