package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// Only main opts into this local launch configuration. Adapters never read it.
type developerLaunch struct {
	SourceRoot      string `json:"sourceRoot"`
	Assets          string `json:"assets"`
	InstallerPython string `json:"installerPython,omitempty"`
}

func loadDeveloperLaunch(path string, allowed bool) (developerLaunch, error) {
	var config developerLaunch
	if !allowed || path == "" {
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
			return developerLaunch{}, errors.New("developer sourceRoot and assets must be absolute directories")
		}
		info, err := os.Stat(path)
		if err != nil {
			return developerLaunch{}, err
		}
		if !info.IsDir() {
			return developerLaunch{}, errors.New("developer sourceRoot and assets must be directories")
		}
	}
	if config.InstallerPython != "" {
		if !filepath.IsAbs(config.InstallerPython) {
			return developerLaunch{}, errors.New("developer installerPython must be absolute")
		}
		info, err := os.Stat(config.InstallerPython)
		if err != nil {
			return developerLaunch{}, err
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
			return developerLaunch{}, errors.New("developer installerPython must be an executable file")
		}
	}
	return config, nil
}
