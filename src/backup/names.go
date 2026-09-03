package backup

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// AppName is the frozen internal project name that prefixes every backup
// file (AI.md PART 3: internal_name never changes, even when the binary is
// renamed). PART 21 names every archive after it.
const AppName = "api"

// Kind classifies a backup file by the naming pattern that produced it
// (AI.md PART 21 "Backup Cleanup Logic").
type Kind string

const (
	// KindFull is the scheduled daily full: api_backup_YYYY-MM-DD.tar.gz[.enc]
	KindFull Kind = "full"
	// KindManual is a manual/timestamped full: api_backup_YYYY-MM-DD_HHMMSS.tar.gz[.enc]
	KindManual Kind = "manual"
	// KindDailyIncremental is api-daily.tar.gz[.enc], always exactly one file.
	KindDailyIncremental Kind = "daily_incremental"
	// KindHourlyIncremental is api-hourly.tar.gz[.enc], always exactly one file.
	KindHourlyIncremental Kind = "hourly_incremental"
	// KindUnclassified covers any other file matching the app's naming
	// prefixes. PART 21 treats these as daily backups for retention so that
	// nothing matching the app's naming is exempt from pruning.
	KindUnclassified Kind = "unclassified"
)

// dateLayout is the date component of the full/manual backup filenames.
const dateLayout = "2006-01-02"

// timeLayout is the time component of manual/timestamped backup filenames.
const timeLayout = "150405"

var (
	fullPattern      = regexp.MustCompile(`^` + AppName + `_backup_(\d{4}-\d{2}-\d{2})\.tar\.gz(\.enc)?$`)
	manualPattern    = regexp.MustCompile(`^` + AppName + `_backup_(\d{4}-\d{2}-\d{2})_(\d{6})\.tar\.gz(\.enc)?$`)
	incrementPattern = regexp.MustCompile(`^` + AppName + `-(daily|hourly)\.tar\.gz(\.enc)?$`)
	catchAllPattern  = regexp.MustCompile(`^` + AppName + `[_-].*\.tar\.gz(\.enc)?$`)
)

// File is one backup archive found in the backup directory.
type File struct {
	// Path is the absolute path to the archive.
	Path string
	// Name is the base filename.
	Name string
	// Kind is the naming pattern the file matched.
	Kind Kind
	// Date is the backup date parsed from the filename, falling back to the
	// file's modification time when the name carries no date.
	Date time.Time
	// Encrypted reports whether the name carries the .enc suffix.
	Encrypted bool
	// Size is the archive size in bytes.
	Size int64
}

// Incremental reports whether the file is one of the always-replaced
// incremental archives, which PART 21 excludes from count-based retention.
func (f File) Incremental() bool {
	return f.Kind == KindDailyIncremental || f.Kind == KindHourlyIncremental
}

// IsEncryptedName reports whether a backup path carries the encrypted
// .tar.gz.enc suffix.
func IsEncryptedName(path string) bool {
	return strings.HasSuffix(path, ".enc")
}

// archiveSuffix returns the extension for an archive, per PART 21's
// ".tar.gz (unencrypted) or .tar.gz.enc (encrypted)" rule.
func archiveSuffix(encrypted bool) string {
	if encrypted {
		return ".tar.gz.enc"
	}
	return ".tar.gz"
}

// FullBackupName returns the scheduled daily full filename for a date:
// api_backup_YYYY-MM-DD.tar.gz[.enc].
func FullBackupName(when time.Time, encrypted bool) string {
	return fmt.Sprintf("%s_backup_%s%s", AppName, when.Format(dateLayout), archiveSuffix(encrypted))
}

// ManualBackupName returns the manual/timestamped filename for a moment:
// api_backup_YYYY-MM-DD_HHMMSS.tar.gz[.enc].
func ManualBackupName(when time.Time, encrypted bool) string {
	return fmt.Sprintf("%s_backup_%s_%s%s", AppName, when.Format(dateLayout), when.Format(timeLayout), archiveSuffix(encrypted))
}

// DailyIncrementalName returns api-daily.tar.gz[.enc], the single daily
// incremental that is replaced on every run.
func DailyIncrementalName(encrypted bool) string {
	return fmt.Sprintf("%s-daily%s", AppName, archiveSuffix(encrypted))
}

// HourlyIncrementalName returns api-hourly.tar.gz[.enc], the single hourly
// incremental that is replaced on every run.
func HourlyIncrementalName(encrypted bool) string {
	return fmt.Sprintf("%s-hourly%s", AppName, archiveSuffix(encrypted))
}

// Classify maps a directory entry onto the PART 21 naming patterns. It
// reports false for files the app never creates, which retention leaves
// untouched.
func Classify(dir string, info os.FileInfo) (File, bool) {
	if info.IsDir() {
		return File{}, false
	}

	name := info.Name()
	file := File{
		Path:      filepath.Join(dir, name),
		Name:      name,
		Encrypted: IsEncryptedName(name),
		Size:      info.Size(),
		Date:      info.ModTime(),
	}

	if match := manualPattern.FindStringSubmatch(name); match != nil {
		file.Kind = KindManual
		if parsed, err := time.Parse(dateLayout+timeLayout, match[1]+match[2]); err == nil {
			file.Date = parsed
		}
		return file, true
	}

	if match := fullPattern.FindStringSubmatch(name); match != nil {
		file.Kind = KindFull
		if parsed, err := time.Parse(dateLayout, match[1]); err == nil {
			file.Date = parsed
		}
		return file, true
	}

	if match := incrementPattern.FindStringSubmatch(name); match != nil {
		if match[1] == "hourly" {
			file.Kind = KindHourlyIncremental
		} else {
			file.Kind = KindDailyIncremental
		}
		return file, true
	}

	// Anything else matching the app's naming prefix is pruned as a daily
	// backup: PART 21 exempts nothing that matches the app's names.
	if catchAllPattern.MatchString(name) {
		file.Kind = KindUnclassified
		return file, true
	}

	return File{}, false
}

// ListBackups returns every app-created archive in dir, oldest first. The
// directory must be the path cached at startup — PART 21 forbids resolving
// it again at cleanup time.
func ListBackups(dir string) ([]File, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read backup directory: %w", err)
	}

	files := make([]File, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if file, ok := Classify(dir, info); ok {
			files = append(files, file)
		}
	}

	sort.Slice(files, func(i, j int) bool {
		if files[i].Date.Equal(files[j].Date) {
			return files[i].Name < files[j].Name
		}
		return files[i].Date.Before(files[j].Date)
	})

	return files, nil
}
