package savekit

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Mutation source limits, mirroring the pinned upstream staging policy.
const (
	maxWriteFile  = 64 << 20
	maxWriteTotal = 256 << 20
	maxWriteFiles = 2048
)

// ExportFile is one file of a verified export or preview: console path with a
// leading slash, size and SHA-256 of its bytes.
type ExportFile struct {
	Path   string
	Size   int64
	SHA256 string
}

// Export is a verified save export's identity and file record.
type Export struct {
	Dir     string
	TitleID string
	UID     string
	Archive Archive
	Files   []ExportFile
}

// ReadExport verifies an export folder and returns its identity and files.
func ReadExport(dir string) (Export, error) {
	if err := VerifyExport(dir); err != nil {
		return Export{}, err
	}
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return Export{}, err
	}
	m, err := parseExportManifest(data)
	if err != nil {
		return Export{}, err
	}
	export := Export{
		Dir: dir, TitleID: m.ProgramID, UID: m.UID,
		Archive: Archive{
			Space: uint32(m.Space), Type: uint32(m.Type),
			Rank: uint32(m.Rank), Index: uint32(m.Index),
		},
	}
	if m.TitleID != "" {
		if m.ProgramID != "" && m.TitleID != m.ProgramID {
			return Export{}, errors.New("the save export has conflicting program IDs")
		}
		export.TitleID = m.TitleID
	}
	if !titlePattern.MatchString(export.TitleID) || !uidPattern.MatchString(export.UID) {
		return Export{}, errors.New("the save export has an invalid identity")
	}
	for _, entry := range m.Entries {
		name, err := safeExportPath(entry.Path)
		if err != nil {
			return Export{}, err
		}
		export.Files = append(export.Files, ExportFile{
			Path: "/" + name,
			Size: entry.Size, SHA256: entry.SHA256,
		})
	}
	return export, nil
}

// MatchesTarget verifies the export's identity against the confirmed
// operation target: title, account and archive rank and index. A device save
// records an all-zero account, which matches its title's target.
func (e Export) MatchesTarget(request Request) error {
	if e.TitleID != request.TitleID {
		return fmt.Errorf("the save belongs to title %s, not the confirmed %s", e.TitleID, request.TitleID)
	}
	if e.UID != request.UID && e.UID != strings.Repeat("0", 32) {
		return errors.New("the save belongs to a different account than confirmed")
	}
	want := Archive{Space: 1, Type: 1}
	if request.Archive != nil {
		want = *request.Archive
	}
	if e.Archive.Space != want.Space || e.Archive.Type != want.Type ||
		e.Archive.Rank != want.Rank || e.Archive.Index != want.Index {
		return fmt.Errorf("the save archive (space %d type %d rank %d index %d) differs from the confirmed target",
			e.Archive.Space, e.Archive.Type, e.Archive.Rank, e.Archive.Index)
	}
	return nil
}

// Preview is the confirmed record of what one operation will change, bound to
// the request by its digest.
type Preview struct {
	Files   []ExportFile
	Bytes   int64
	Digest  string
	TitleID string
	UID     string
	Archive Archive
}

func previewDigest(files []ExportFile) string {
	hash := sha256.New()
	for _, file := range files {
		fmt.Fprintf(hash, "%s\t%d\t%s\n", file.Path, file.Size, file.SHA256)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

// PreviewWrite scans a local save folder the way the pinned upstream staging
// does: regular files only, safe names, per-file and total size limits.
func PreviewWrite(folder string) (Preview, error) {
	if !filepath.IsAbs(folder) {
		return Preview{}, errors.New("choose an absolute local save folder")
	}
	root, err := os.OpenRoot(folder)
	if err != nil {
		return Preview{}, err
	}
	defer root.Close()
	var files []ExportFile
	var total int64
	err = fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if name == "." {
			return nil
		}
		base := entry.Name()
		if base == ".DS_Store" || base == ".nx_save_meta.bin" || strings.HasPrefix(base, "._") ||
			(name == "manifest.json") {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		for _, part := range strings.Split(name, "/") {
			if !SafeRootName(part) {
				return fmt.Errorf("the source contains an unsafe name: %s", name)
			}
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("the source contains a link or special file: %s", name)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Size() > maxWriteFile || len(files) >= maxWriteFiles {
			return fmt.Errorf("the source contains an unsupported file: %s", name)
		}
		total += info.Size()
		if total > maxWriteTotal {
			return errors.New("the selected save folder is too large for a single write")
		}
		file, err := root.Open(name)
		if err != nil {
			return err
		}
		hash := sha256.New()
		_, copyErr := io.Copy(hash, file)
		closeErr := file.Close()
		if copyErr != nil || closeErr != nil {
			return errors.Join(copyErr, closeErr)
		}
		files = append(files, ExportFile{
			Path: "/" + filepath.ToSlash(name),
			Size: info.Size(), SHA256: hex.EncodeToString(hash.Sum(nil)),
		})
		return nil
	})
	if err != nil {
		return Preview{}, err
	}
	if len(files) == 0 {
		return Preview{}, errors.New("the selected folder contains no save files")
	}
	slices.SortFunc(files, func(a, b ExportFile) int { return strings.Compare(a.Path, b.Path) })
	return Preview{Files: files, Bytes: total, Digest: previewDigest(files)}, nil
}

// PreviewRestore verifies a backup folder and previews the files a restore
// would put back, with the backup's identity.
func PreviewRestore(folder string) (Preview, error) {
	data, err := os.ReadFile(filepath.Join(folder, "manifest.json"))
	if err != nil {
		return Preview{}, err
	}
	m, err := parseExportManifest(data)
	if err != nil {
		return Preview{}, err
	}
	if len(m.Entries) == 0 || len(m.Entries) > maxWriteFiles {
		return Preview{}, errors.New("the selected backup has too many files for one restore")
	}
	var declared int64
	for _, file := range m.Entries {
		if file.Size < 0 || file.Size > maxWriteFile {
			return Preview{}, fmt.Errorf("the selected backup has a file larger than 64 MiB: %s", file.Path)
		}
		declared += file.Size
		if declared > maxWriteTotal {
			return Preview{}, errors.New("the selected backup is too large for one restore")
		}
	}
	export, err := ReadExport(folder)
	if err != nil {
		return Preview{}, err
	}
	if len(export.Files) == 0 || len(export.Files) > maxWriteFiles {
		return Preview{}, errors.New("the selected backup has too many files for one restore")
	}
	var total int64
	for _, file := range export.Files {
		if file.Size > maxWriteFile {
			return Preview{}, fmt.Errorf("the selected backup has a file larger than 64 MiB: %s", file.Path)
		}
		total += file.Size
	}
	if total > maxWriteTotal {
		return Preview{}, errors.New("the selected backup is too large for one restore")
	}
	files := slices.Clone(export.Files)
	slices.SortFunc(files, func(a, b ExportFile) int { return strings.Compare(a.Path, b.Path) })
	return Preview{
		Files: files, Bytes: total, Digest: previewDigest(files),
		TitleID: export.TitleID, UID: export.UID, Archive: export.Archive,
	}, nil
}

// PreviewDelete previews the one save-root file a delete removes.
func PreviewDelete(fileName string) Preview {
	files := []ExportFile{{Path: "/" + fileName}}
	hash := sha256.Sum256([]byte("delete\n/" + fileName + "\n"))
	return Preview{Files: files, Digest: hex.EncodeToString(hash[:])}
}

// CompareWritten verifies a fresh read-back export holds every written file
// with its written hash. Unrelated live files are expected and ignored.
func CompareWritten(readback Export, preview Preview) error {
	live := map[string]ExportFile{}
	for _, file := range readback.Files {
		live[file.Path] = file
	}
	for _, written := range preview.Files {
		got, ok := live[written.Path]
		if !ok {
			return fmt.Errorf("the read-back save is missing the written file %s", written.Path)
		}
		if got.Size != written.Size || got.SHA256 != written.SHA256 {
			return fmt.Errorf("the read-back save differs from the written data: %s", written.Path)
		}
	}
	return nil
}

// CompareDeleted verifies a fresh read-back export no longer holds the deleted
// file and every other pre-operation file is intact.
func CompareDeleted(readback, backup Export, fileName string) error {
	target := "/" + fileName
	live := map[string]ExportFile{}
	for _, file := range readback.Files {
		if file.Path == target {
			return errors.New("the deleted file is still present after commit: " + target)
		}
		live[file.Path] = file
	}
	if len(live) != len(backup.Files)-1 {
		return errors.New("the read-back save's file list changed beyond the deleted file")
	}
	for _, before := range backup.Files {
		if before.Path == target {
			continue
		}
		got, ok := live[before.Path]
		if !ok || got.Size != before.Size || got.SHA256 != before.SHA256 {
			return fmt.Errorf("another save file changed after deletion: %s", before.Path)
		}
	}
	return nil
}

// CompareExports verifies the live export matches the expected export exactly.
func CompareExports(live, expected Export) error {
	if len(live.Files) != len(expected.Files) {
		return errors.New("the live save's file list differs from the export")
	}
	byHash := map[string]ExportFile{}
	for _, file := range live.Files {
		byHash[file.Path] = file
	}
	for _, want := range expected.Files {
		got, ok := byHash[want.Path]
		if !ok || got.Size != want.Size || got.SHA256 != want.SHA256 {
			return fmt.Errorf("the live save differs from the export: %s", want.Path)
		}
	}
	return nil
}
