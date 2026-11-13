package engine

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/thedavidweng/any2jb/manifest"
)

func nodeArchiveSuffix(platform string) (string, bool) {
	switch platform {
	case "darwin-arm64":
		return "darwin-arm64", true
	case "linux-amd64":
		return "linux-x64", true
	case "linux-arm64":
		return "linux-arm64", true
	default:
		return "", false
	}
}

// PrepareNode downloads only the pinned official archives into cache, verifies
// their release digests, and extracts only Node and its full license for every
// pinned target in the manifest.
func PrepareNode(cache, assets string, m *manifest.Manifest) error {
	platforms := m.Runtime.Platforms()
	if len(platforms) == 0 {
		return errors.New("unapproved Node archive")
	}
	for _, platform := range platforms {
		if err := PrepareNodeForPlatform(cache, assets, platform, m); err != nil {
			return err
		}
	}
	return nil
}

// PrepareNodeForPlatform prepares the pinned official Node sidecar for one target.
// A corrupt cached archive is reported, not silently replaced by a new download.
func PrepareNodeForPlatform(cache, assets, platform string, m *manifest.Manifest) error {
	r := m.Runtime
	suffix, ok := nodeArchiveSuffix(platform)
	targetInfo, targetOK := r.Target(platform)
	wantURL := "https://nodejs.org/dist/v" + r.NodeVersion + "/node-v" + r.NodeVersion + "-" + suffix + ".tar.gz"
	if !ok || !targetOK || r.NodeVersion != "24.22.1" || targetInfo.NodeArchive != wantURL {
		return errors.New("unapproved Node archive")
	}
	if err := privateDir(cache); err != nil {
		return err
	}
	name := filepath.Join(cache, filepath.Base(targetInfo.NodeArchive))
	if _, err := os.Lstat(name); os.IsNotExist(err) {
		client := &http.Client{Timeout: 2 * time.Minute}
		response, err := client.Get(targetInfo.NodeArchive)
		if err != nil {
			return err
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return fmt.Errorf("Node download: %s", response.Status)
		}
		tmp, err := os.CreateTemp(cache, ".node-")
		if err != nil {
			return err
		}
		defer os.Remove(tmp.Name())
		_, copyErr := io.Copy(tmp, response.Body)
		closeErr := tmp.Close()
		if err := errors.Join(copyErr, closeErr); err != nil {
			return err
		}
		if err := verifyArchive(tmp.Name(), targetInfo.NodeArchiveSHA256); err != nil {
			return err
		}
		if err := os.Rename(tmp.Name(), name); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if err := verifyArchive(name, targetInfo.NodeArchiveSHA256); err != nil {
		return err
	}
	f, err := os.Open(name)
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
	prefix := "node-v" + r.NodeVersion + "-" + suffix + "/"
	found := map[string]bool{}
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		var id string
		switch header.Name {
		case prefix + "bin/node":
			id = targetInfo.Node
		case prefix + "LICENSE":
			id = "node-notice"
		default:
			continue
		}
		if header.Typeflag != tar.TypeReg || found[id] {
			return errors.New("invalid Node archive entry")
		}
		found[id] = true
		var c manifest.Component
		for _, candidate := range m.Components {
			if candidate.ID == id {
				c = candidate
				break
			}
		}
		if !c.Distribution.Available() {
			return fmt.Errorf("Node component %s unavailable", id)
		}
		data, err := io.ReadAll(reader)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != c.SHA256 {
			return fmt.Errorf("Node artifact %s digest mismatch", id)
		}
		if !filepath.IsLocal(c.Path) {
			return errors.New("invalid Node output path")
		}
		target := filepath.Join(assets, c.ResourcePlatform, filepath.FromSlash(c.Path))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		tmp, err := os.CreateTemp(filepath.Dir(target), ".node-")
		if err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if id == targetInfo.Node {
			mode = 0o755
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
		if err != nil {
			return err
		}
	}
	if !found[targetInfo.Node] || !found["node-notice"] {
		return errors.New("Node archive missing binary or license")
	}
	return nil
}

func verifyArchive(name, digest string) error {
	info, err := os.Lstat(name)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("Node archive is not a regular file")
	}
	f, err := os.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return err
	}
	if hex.EncodeToString(hash.Sum(nil)) != digest {
		return errors.New("Node archive SHA-256 mismatch")
	}
	return nil
}
