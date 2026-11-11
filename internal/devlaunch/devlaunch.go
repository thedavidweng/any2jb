// Package devlaunch is the local developer-launch boundary. Only main reads it,
// and only in a build that may: a development build that is not a packaged
// bundle. Adapters never see this configuration.
package devlaunch

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
)

// Config names the local sources a developer build stages instead of the
// bundled runtime.
type Config struct {
	SourceRoot      string `json:"sourceRoot"`
	Assets          string `json:"assets"`
	InstallerPython string `json:"installerPython,omitempty"`
}

// Allowed reports whether this build may read the developer configuration.
func Allowed() bool {
	return mygo.IsDev() && !mygo.App.IsPackaged()
}

// Load reads the configuration at path. A closed gate returns the zero Config
// without touching the file or the environment.
func Load(path string) (Config, error) {
	var config Config
	if !Allowed() || path == "" {
		return config, nil
	}
	if !filepath.IsAbs(path) {
		return config, errors.New("developer config path must be absolute")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return config, err
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return config, err
	}
	for _, path := range []string{config.SourceRoot, config.Assets} {
		if !filepath.IsAbs(path) {
			return Config{}, errors.New("developer sourceRoot and assets must be absolute directories")
		}
		info, err := os.Stat(path)
		if err != nil {
			return Config{}, err
		}
		if !info.IsDir() {
			return Config{}, errors.New("developer sourceRoot and assets must be directories")
		}
	}
	if config.InstallerPython != "" {
		if !filepath.IsAbs(config.InstallerPython) {
			return Config{}, errors.New("developer installerPython must be absolute")
		}
		info, err := os.Stat(config.InstallerPython)
		if err != nil {
			return Config{}, err
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
			return Config{}, errors.New("developer installerPython must be an executable file")
		}
	}
	return config, nil
}
