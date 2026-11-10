package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
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
	titleEntries    = map[string]bool{"romfs": true, "exefs": true, "romfs.bin": true, "exefs.nsp": true,
		"flags": true, "romfs_metadata.bin": true}
)

// ownedFolder requires an absolute directory owned by this user that is not
// itself a symlink.
func ownedFolder(value string) error {
	if !filepath.IsAbs(value) || len(value) > 4096 || strings.ContainsAny(value, "\x00\r\n") {
		return errors.New("choose an existing absolute folder on this Mac")
	}
	info, err := os.Lstat(value)
	if err != nil || !info.IsDir() {
		return errors.New("choose an existing absolute folder on this Mac")
	}
	if !ownedByMe(info) {
		return errors.New("the folder must be an owned directory without symlinks")
	}
	return nil
}

func ownedByMe(info fs.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(stat.Uid) == os.Getuid()
}

// regularTree walks a subtree and refuses symlinks, special files, foreign
// owners and more than the recorded file limit.
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
		if !ownedByMe(info) {
			return fmt.Errorf("a mod file is owned by another user: %s", name)
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

// validateModsFolder accepts an Atmosphere-style tree: contents/<TITLE_ID>
// with romfs/exefs replacements, and exefs_patches/<set>/<BUILD_ID>.ips.
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

// validateCustomHomebrew classifies a built homebrew folder the way the mac2jb
// preflight does. This build launches only a built GPU app (app.json) inside
// MintBrew; every other recognized format is refused with its reason.
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
