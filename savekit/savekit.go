// Package savekit describes save operations and their safety gate. Read-only
// operations observe and export; write, restore and delete additionally
// require a preview bound to the request, explicit confirmation and a verified
// backup, which the retained manager orchestrates before any console change.
// Comparing a live save against an export is read-only.
package savekit

import (
	"errors"
	"path/filepath"
	"regexp"
)

var titlePattern = regexp.MustCompile(`^[0-9a-f]{16}$`)
var uidPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type Archive struct {
	Space    uint32 `json:"space"`
	Type     uint32 `json:"type"`
	Rank     uint32 `json:"rank"`
	Index    uint32 `json:"index"`
	StaticID uint64 `json:"staticId"`
}

type Entry struct {
	Archive
	ID      string `json:"id"`
	TitleID string `json:"titleId"`
	UID     string `json:"uid"`
	Size    string `json:"size"`
}

type Request struct {
	Action  string   `json:"action"`
	TitleID string   `json:"titleId,omitempty"`
	UID     string   `json:"uid,omitempty"`
	Archive *Archive `json:"archive,omitempty"`
	All     bool     `json:"all,omitempty"`
	// The mutation gate: SourceFolder is the absolute local folder a write or
	// restore takes its files from, FileName the one save-root file a delete
	// removes, ExpectedFolder the verified export a read-only compare checks
	// the live save against. Confirm and PreviewDigest bind one individual
	// confirmation to one preview of exactly these affected files; they are
	// required for every console-changing operation.
	SourceFolder   string `json:"sourceFolder,omitempty"`
	FileName       string `json:"fileName,omitempty"`
	AllowReplace   bool   `json:"allowReplace,omitempty"`
	ExpectedFolder string `json:"expectedFolder,omitempty"`
	Confirm        bool   `json:"confirm,omitempty"`
	PreviewDigest  string `json:"previewDigest,omitempty"`
}

func (r Request) Validate() error {
	switch r.Action {
	case "accounts", "titles", "inventory":
		if r.TitleID != "" || r.UID != "" || r.Archive != nil || r.All {
			return errors.New("listing accepts no archive target")
		}
		return r.readOnly()
	case "inspect", "backup", "filetime":
		if err := r.readOnly(); err != nil {
			return err
		}
	case "verify":
		if err := r.readOnly(); err != nil {
			return err
		}
		if !filepath.IsAbs(r.ExpectedFolder) {
			return errors.New("choose an absolute verified export folder to compare against")
		}
	case "write", "restore", "delete":
		return r.gated()
	default:
		return errors.New("unsupported SaveKit action")
	}
	if !titlePattern.MatchString(r.TitleID) || !uidPattern.MatchString(r.UID) {
		return errors.New("use a 16-digit title ID and 32-digit account ID")
	}
	if r.All && r.Action == "filetime" {
		return errors.New("timestamp reads need one archive")
	}
	if r.All && r.Action == "verify" {
		return errors.New("a live comparison needs one archive")
	}
	return r.archiveBounds()
}

// readOnly refuses gate fields on operations that never change the console.
func (r Request) readOnly() error {
	if r.SourceFolder != "" || r.FileName != "" || r.AllowReplace || r.Confirm ||
		r.PreviewDigest != "" || (r.ExpectedFolder != "" && r.Action != "verify") {
		return errors.New("a read-only SaveKit action carries no write fields")
	}
	return nil
}

// gated validates one console-changing operation: an explicit target, the
// affected files pinned by a preview digest and an individual confirmation.
func (r Request) gated() error {
	if r.ExpectedFolder != "" || r.All {
		return errors.New("a SaveKit mutation carries no compare fields")
	}
	if !titlePattern.MatchString(r.TitleID) || !uidPattern.MatchString(r.UID) {
		return errors.New("use a 16-digit title ID and 32-digit account ID")
	}
	if !r.Confirm {
		return errors.New("confirm this exact save operation before running it")
	}
	if !digestPattern.MatchString(r.PreviewDigest) {
		return errors.New("preview the affected files before confirming this save operation")
	}
	switch r.Action {
	case "write":
		if r.Archive != nil || r.FileName != "" {
			return errors.New("a save write targets the default account archive")
		}
		if !filepath.IsAbs(r.SourceFolder) {
			return errors.New("choose an absolute local save folder")
		}
	case "restore":
		if r.FileName != "" || r.AllowReplace {
			return errors.New("a save restore takes only a verified backup")
		}
		if !filepath.IsAbs(r.SourceFolder) {
			return errors.New("choose an absolute verified backup folder")
		}
		if err := r.archiveBounds(); err != nil {
			return err
		}
		archive := Archive{Space: 1, Type: 1}
		if r.Archive != nil {
			archive = *r.Archive
		}
		if (archive.Type != 1 && archive.Type != 3) || archive.Rank != 0 ||
			archive.Index != 0 || archive.StaticID != 0 {
			return errors.New("restore supports Account or Device saves at rank 0 and index 0 only")
		}
	case "delete":
		if r.Archive != nil || r.SourceFolder != "" || r.AllowReplace {
			return errors.New("a save delete removes one file from the default account archive")
		}
		if !SafeRootName(r.FileName) {
			return errors.New("choose one file at the save root to delete")
		}
	}
	return nil
}

func (r Request) archiveBounds() error {
	if a := r.Archive; a != nil && (a.Space > 255 || a.Type > 7 || a.Rank > 1 ||
		a.Index > 65535 || a.StaticID > 9007199254740991) {
		return errors.New("invalid save archive")
	}
	return nil
}

// SafeRootName reports whether name selects exactly one file at the save root,
// matching the pinned upstream delete policy.
func SafeRootName(name string) bool {
	if name == "" || name == "." || name == ".." || len(name) >= 0x300 {
		return false
	}
	for _, r := range name {
		if r == '/' || r == '\\' || r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

type Result struct {
	Action    string   `json:"action"`
	Users     []string `json:"users,omitempty"`
	Titles    []string `json:"titles,omitempty"`
	Inventory []Entry  `json:"inventory,omitempty"`
	Output    string   `json:"output,omitempty"`
	Path      string   `json:"path,omitempty"`
	Files     int      `json:"files,omitempty"`
	Replaced  int      `json:"replaced,omitempty"`
}
