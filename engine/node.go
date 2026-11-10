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

// PrepareNode downloads only the pinned official archive into cache, verifies
// its release digest, and extracts only Node and its full license. A corrupt
// cached archive is reported, not silently replaced by a new download.
func PrepareNode(cache, assets string, m *manifest.Manifest) error {
	r := m.Runtime
	if r.NodeArchive != "https://nodejs.org/dist/v"+r.NodeVersion+"/node-v"+r.NodeVersion+"-darwin-arm64.tar.gz" ||
		r.NodeVersion != "24.22.1" || r.Platform != "darwin-arm64" {
		return errors.New("unapproved Node archive")
	}
	if err := privateDir(cache); err != nil {
		return err
	}
	name := filepath.Join(cache, filepath.Base(r.NodeArchive))
	if _, err := os.Lstat(name); os.IsNotExist(err) {
		client := &http.Client{Timeout: 2 * time.Minute}
		response, err := client.Get(r.NodeArchive)
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
		if err := verifyArchive(tmp.Name(), r.NodeArchiveSHA256); err != nil {
			return err
		}
		if err := os.Rename(tmp.Name(), name); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if err := verifyArchive(name, r.NodeArchiveSHA256); err != nil {
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
	prefix := "node-v" + r.NodeVersion + "-darwin-arm64/"
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
			id = r.Node
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
		if id == r.Node {
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
	if !found[r.Node] || !found["node-notice"] {
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
