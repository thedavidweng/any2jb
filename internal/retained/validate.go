package retained

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/thedavidweng/any2jb/internal/session"
)

// The modloader and custom homebrew layout rules mirror the pinned mac2jb
// collector (integrations/ns2jb-modloader/lib/mods.cjs) and custom homebrew
// classifier. Go checks them before anything starts so an invalid folder never
// reaches the tool host or the console; the tool host checks them again.

const maxModFiles = 20000

var (
	titleIDPattern  = regexp.MustCompile(`^[0-9a-fA-F]{16}$`)
	patchPattern    = regexp.MustCompile(`^(?:[0-9a-fA-F]{2}){1,32}\.(?:ips|ips32)$`)
	modNotePattern  = regexp.MustCompile(`(?i)^(?:readme|license|copying|credits|changelog)|\.(?:md|txt|json)$`)
	customAppID     = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,47}$`)
	ignoredModFiles = map[string]bool{".gitkeep": true, ".DS_Store": true, "Thumbs.db": true}
	titleEntries    = map[string]bool{
		"romfs": true, "exefs": true, "romfs.bin": true, "exefs.nsp": true,
		"flags": true, "romfs_metadata.bin": true,
	}
)

// ValidateToolRequest validates tool-specific parameters, folder ownership,
// Atmosphere mod layout, custom homebrew app layout, and user-code consent
// before the session controller enters RunningTool.
func ValidateToolRequest(r session.ToolRequest) error {
	if len(r.Modules) > 0 {
		return errors.New("IZlauncher module selection is no longer supported")
	}
	if r.SDRomFolder != "" && r.Tool != "native-hblauncher" {
		return errors.New("an SD ROM folder belongs to the native launcher")
	}
	if r.AppFolder != "" && r.Tool != "native-hblauncher" && r.Tool != "mintbrew" {
		return errors.New("an app folder belongs to MintBrew or the native launcher")
	}
	if r.ModsFolder != "" && r.Tool != "modloader" {
		return errors.New("a mod folder belongs to the modloader")
	}
	if (r.CustomHomebrewFolder != "") != (r.Tool == "custom-homebrew") {
		return errors.New("custom homebrew needs exactly one built app folder")
	}
	if r.StremioQuality != "" {
		if r.Tool != "mintbrew" {
			return errors.New("Stremio quality belongs to MintBrew")
		}
		if r.StremioQuality != "480p" && r.StremioQuality != "720p" && r.StremioQuality != "1080p" {
			return errors.New("choose 480p, 720p, or 1080p for Stremio quality")
		}
	}
	if len(r.StremioAddonURLs) > 0 {
		if r.Tool != "mintbrew" {
			return errors.New("Stremio addon URLs belong to MintBrew")
		}
		if len(r.StremioAddonURLs) > 16 {
			return errors.New("choose up to 16 Stremio addon manifest URLs")
		}
		for _, raw := range r.StremioAddonURLs {
			u, err := url.Parse(raw)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || !strings.HasSuffix(u.Path, "/manifest.json") {
				return errors.New("Stremio addon URLs must be http or https manifest.json endpoints")
			}
		}
	}
	if r.ModsFolder != "" {
		if err := validateModsFolder(r.ModsFolder); err != nil {
			return err
		}
	}
	if r.CustomHomebrewFolder != "" {
		if err := validateCustomHomebrew(r.CustomHomebrewFolder); err != nil {
			return err
		}
	}
	if r.SDRomFolder != "" && !validSDRomFolder(r.SDRomFolder) {
		return errors.New("enter an absolute SD folder under /sd without a trailing slash")
	}
	if r.AppFolder != "" && !validAppFolder(r.AppFolder) {
		return errors.New("choose an existing absolute folder on this computer")
	}
	return r.ValidateCodeConsent()
}

func (m *Manager) Validate(r session.ToolRequest) error {
	return ValidateToolRequest(r)
}

func validSDRomFolder(value string) bool {
	if len(value) > 240 || !strings.HasPrefix(value, "/sd/") || strings.HasSuffix(value, "/") {
		return false
	}
	for _, part := range strings.Split(strings.TrimPrefix(value, "/"), "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return !strings.ContainsAny(value, "\x00\r\n")
}

func validAppFolder(value string) bool {
	if !filepath.IsAbs(value) || len(value) > 4096 || strings.ContainsAny(value, "\x00\r\n") {
		return false
	}
	info, err := os.Stat(value)
	return err == nil && info.IsDir()
}

func ownedFolder(value string) error {
	if !filepath.IsAbs(value) || len(value) > 4096 || strings.ContainsAny(value, "\x00\r\n") {
		return errors.New("choose an existing absolute folder on this Mac")
	}
	info, err := os.Lstat(value)
	if err != nil || !info.IsDir() {
		return errors.New("choose an existing absolute folder on this Mac")
	}
	return ownedByMe(info)
}

var errOwnership = errors.New("the folder and its files must be owned by you, without symlinks")

func regularTree(root string, count *int) error {
	return filepath.WalkDir(root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := os.Lstat(name)
		if err != nil {
			return err
		}
		if info.Mode()&fs.ModeSymlink != 0 || !(info.IsDir() || info.Mode().IsRegular()) {
			return fmt.Errorf("mod folders may contain only regular files and directories: %s", name)
		}
		if err := ownedByMe(info); err != nil {
			return fmt.Errorf("%w: %s", err, name)
		}
		if info.Mode().IsRegular() {
			if *count++; *count > maxModFiles {
				return errors.New("the selected folder has too many mod files")
			}
		}
		return nil
	})
}

func visible(dir string) ([]os.DirEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	kept := entries[:0]
	for _, entry := range entries {
		if !ignoredModFiles[entry.Name()] {
			kept = append(kept, entry)
		}
	}
	return kept, nil
}

func validateModsFolder(root string) error {
	if err := ownedFolder(root); err != nil {
		return err
	}
	count, found := 0, 0
	for _, name := range []string{"contents", "exefs_patches"} {
		info, err := os.Lstat(filepath.Join(root, name))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil || !info.IsDir() {
			return fmt.Errorf("%s must be a real directory", name)
		}
		if err := regularTree(filepath.Join(root, name), &count); err != nil {
			return err
		}
	}
	titles, err := visible(filepath.Join(root, "contents"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	seen := map[string]bool{}
	for _, title := range titles {
		id := strings.ToLower(title.Name())
		if !title.IsDir() || !titleIDPattern.MatchString(id) {
			return fmt.Errorf("expected a 16-digit hexadecimal Title ID directory under contents/: %s", title.Name())
		}
		if seen[id] {
			return fmt.Errorf("duplicate Title ID: %s", id)
		}
		seen[id] = true
		if err := validateTitle(filepath.Join(root, "contents", title.Name())); err != nil {
			return err
		}
		found++
	}
	sets, err := visible(filepath.Join(root, "exefs_patches"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	for _, set := range sets {
		if !set.IsDir() {
			return fmt.Errorf("expected a patchset directory under exefs_patches/: %s", set.Name())
		}
		patches, err := visible(filepath.Join(root, "exefs_patches", set.Name()))
		if err != nil {
			return err
		}
		for _, patch := range patches {
			if patch.IsDir() || !patchPattern.MatchString(patch.Name()) {
				return fmt.Errorf("ExeFS patches must be named for an NSO Build ID: %s/%s", set.Name(), patch.Name())
			}
			found++
		}
	}
	if found == 0 {
		return errors.New("no mods found; add contents/<TITLE_ID>/romfs/ or exefs/, or exefs_patches/")
	}
	return nil
}

func validateTitle(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if !titleEntries[name] && !modNotePattern.MatchString(name) {
			return fmt.Errorf("unsupported component %s in %s; runtime cheats and plugins need their own loader, so this mod is refused rather than partly installed", name, filepath.Base(dir))
		}
	}
	for _, section := range []string{"romfs", "exefs"} {
		names := map[string]bool{}
		err := filepath.WalkDir(filepath.Join(dir, section), func(name string, entry fs.DirEntry, err error) error {
			if errors.Is(err, fs.ErrNotExist) {
				return fs.SkipAll
			}
			if err != nil {
				return err
			}
			relative, _ := filepath.Rel(filepath.Join(dir, section), name)
			if relative == "." || ignoredModFiles[entry.Name()] {
				return nil
			}
			if section == "exefs" && entry.IsDir() {
				return errors.New("ExeFS files must be at the root of exefs/")
			}
			if lower := strings.ToLower(relative); names[lower] {
				return fmt.Errorf("case-conflicting %s entries: %s", section, relative)
			} else {
				names[lower] = true
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func validateCustomHomebrew(root string) error {
	if err := ownedFolder(root); err != nil {
		return err
	}
	exists := func(name string) bool {
		_, err := os.Lstat(filepath.Join(root, filepath.FromSlash(name)))
		return err == nil
	}
	switch {
	case exists("mac2jb-rpc-app.json") || exists("engine/oz-code/native.bin"):
		return errors.New("custom RPC packages are not supported in this build")
	case exists("module.json"):
		return errors.New("custom IZlauncher modules are not supported in this build")
	case exists("manifest.json"):
		return errors.New("a native ABI 3 app runs from the native HBLauncher's app folder, not as custom homebrew")
	}
	info, err := os.Lstat(filepath.Join(root, "app.json"))
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return errors.New("choose a built GPU homebrew folder that contains app.json")
	}
	data, err := os.ReadFile(filepath.Join(root, "app.json"))
	if err != nil {
		return err
	}
	var app struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if json.Unmarshal(data, &app) != nil || !customAppID.MatchString(app.ID) || app.Name == "" {
		return errors.New("app.json must name a lowercase app id and a display name")
	}
	count := 0
	return regularTree(root, &count)
}
