package backup

import (
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v3/disk"

	"github.com/apimgr/api/src/config"
)

// Retention marking categories, ordered by the PART 21 priority list:
// yearly > monthly > weekly > daily.
const (
	CategoryYearly  = "yearly"
	CategoryMonthly = "monthly"
	CategoryWeekly  = "weekly"
	CategoryDaily   = "daily"
)

// Deletion reasons recorded in the backup.retention_cleanup audit event.
const (
	ReasonCountLimit = "count_limit"
	ReasonSizeCap    = "size_cap"
)

// sizeUnits maps the suffixes accepted by max_total_size onto byte multipliers.
var sizeUnits = map[string]int64{
	"":  1,
	"B": 1,
	"K": 1 << 10,
	"M": 1 << 20,
	"G": 1 << 30,
	"T": 1 << 40,
	"P": 1 << 50,
}

// Deletion records one archive removed by a retention sweep.
type Deletion struct {
	// Name is the deleted archive's base filename.
	Name string
	// Size is the number of bytes reclaimed.
	Size int64
	// Reason is why it was removed: ReasonCountLimit or ReasonSizeCap.
	Reason string
}

// RetentionResult reports what a retention sweep did, and feeds the
// backup.retention_cleanup audit event.
type RetentionResult struct {
	// Deleted lists every archive removed, oldest first.
	Deleted []Deletion
	// Remaining is the number of archives left in the directory.
	Remaining int
	// RemainingSize is the total size of the archives left, in bytes.
	RemainingSize int64
	// Warnings carries non-fatal problems, such as an unparseable size cap.
	Warnings []string
}

// ParseSizeCap resolves a max_total_size value to a byte count for the volume
// holding dir. It accepts a percentage of that volume ("10%") or an absolute
// size ("50G"). A disabled or unparseable cap returns 0.
func ParseSizeCap(value, dir string) (int64, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, nil
	}

	if strings.HasSuffix(trimmed, "%") {
		percent, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(trimmed, "%")), 64)
		if err != nil {
			return 0, fmt.Errorf("invalid percentage size cap %q: %w", value, err)
		}
		if percent <= 0 {
			return 0, nil
		}
		usage, err := disk.Usage(dir)
		if err != nil {
			return 0, fmt.Errorf("failed to size the backup volume: %w", err)
		}
		return int64(math.Round(float64(usage.Total) * percent / 100)), nil
	}

	upper := strings.ToUpper(trimmed)
	upper = strings.TrimSuffix(upper, "IB")
	upper = strings.TrimSuffix(upper, "B")

	unit := ""
	if len(upper) > 0 {
		last := upper[len(upper)-1:]
		if _, ok := sizeUnits[last]; ok && last != "B" {
			unit = last
			upper = upper[:len(upper)-1]
		}
	}

	amount, err := strconv.ParseFloat(strings.TrimSpace(upper), 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size cap %q: %w", value, err)
	}
	if amount <= 0 {
		return 0, nil
	}
	return int64(math.Round(amount * float64(sizeUnits[unit]))), nil
}

// ApplyRetention prunes the backup directory per PART 21's cleanup algorithm:
// mark yearly, monthly, weekly then daily archives, delete everything left
// unmarked oldest first, and finally enforce the hard size cap. dir must be
// the path cached at startup — PART 21 forbids re-resolving it here.
func ApplyRetention(dir string, policy config.BackupRetentionConfig) (RetentionResult, error) {
	normalized, warnings := policy.Normalized()
	result := RetentionResult{Warnings: warnings}

	files, err := ListBackups(dir)
	if err != nil {
		return result, err
	}

	keep, prune := markForRetention(files, normalized)

	for _, file := range prune {
		if err := os.Remove(file.Path); err != nil && !os.IsNotExist(err) {
			return result, fmt.Errorf("failed to delete %s: %w", file.Name, err)
		}
		result.Deleted = append(result.Deleted, Deletion{Name: file.Name, Size: file.Size, Reason: ReasonCountLimit})
	}

	if !normalized.SizeCapDisabled() {
		capBytes, err := ParseSizeCap(normalized.MaxTotalSize, dir)
		if err != nil {
			result.Warnings = append(result.Warnings, err.Error())
		} else if capBytes > 0 {
			kept, deleted, err := enforceSizeCap(keep, capBytes)
			if err != nil {
				return result, err
			}
			keep = kept
			result.Deleted = append(result.Deleted, deleted...)
		}
	}

	result.Remaining = len(keep)
	for _, file := range keep {
		result.RemainingSize += file.Size
	}

	return result, nil
}

// markForRetention splits the archive set into the files retention keeps and
// the files it prunes, applying the yearly > monthly > weekly > daily
// priority. Incrementals are never counted: exactly one of each exists and it
// is replaced on every run.
func markForRetention(files []File, policy config.BackupRetentionConfig) (keep, prune []File) {
	// Newest first, so each category keeps its most recent members.
	candidates := make([]File, 0, len(files))
	for _, file := range files {
		if file.Incremental() {
			keep = append(keep, file)
			continue
		}
		candidates = append(candidates, file)
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].Date.After(candidates[j].Date)
	})

	marked := make(map[string]bool, len(candidates))
	counts := map[string]int{}

	// A single archive can satisfy several categories; the highest-priority
	// one that still has room claims it.
	for _, category := range []string{CategoryYearly, CategoryMonthly, CategoryWeekly, CategoryDaily} {
		limit := categoryLimit(category, policy)
		for _, file := range candidates {
			if counts[category] >= limit {
				break
			}
			if marked[file.Path] || !matchesCategory(file, category) {
				continue
			}
			marked[file.Path] = true
			counts[category]++
		}
	}

	// Walk oldest first so deletions are reported in the order PART 21
	// prescribes.
	for i := len(candidates) - 1; i >= 0; i-- {
		file := candidates[i]
		if marked[file.Path] {
			keep = append(keep, file)
			continue
		}
		prune = append(prune, file)
	}

	sort.SliceStable(prune, func(i, j int) bool {
		return prune[i].Date.Before(prune[j].Date)
	})

	return keep, prune
}

// categoryLimit returns how many archives a retention category may keep.
func categoryLimit(category string, policy config.BackupRetentionConfig) int {
	switch category {
	case CategoryYearly:
		return policy.KeepYearly
	case CategoryMonthly:
		return policy.KeepMonthly
	case CategoryWeekly:
		return policy.KeepWeekly
	default:
		return policy.MaxBackups
	}
}

// matchesCategory reports whether an archive's date qualifies it for a
// retention category. Every non-incremental archive qualifies as daily,
// including manual/timestamped and unclassified files.
func matchesCategory(file File, category string) bool {
	switch category {
	case CategoryYearly:
		return file.Date.Month() == time.January && file.Date.Day() == 1
	case CategoryMonthly:
		return file.Date.Day() == 1
	case CategoryWeekly:
		return file.Date.Weekday() == time.Sunday
	default:
		return true
	}
}

// enforceSizeCap deletes the oldest archives until the retained set fits
// under the hard cap. PART 21 lets the cap override every count limit.
func enforceSizeCap(keep []File, capBytes int64) ([]File, []Deletion, error) {
	ordered := make([]File, len(keep))
	copy(ordered, keep)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].Date.Before(ordered[j].Date)
	})

	var total int64
	for _, file := range ordered {
		total += file.Size
	}

	var deleted []Deletion
	remaining := make([]File, 0, len(ordered))
	for index, file := range ordered {
		if total <= capBytes {
			remaining = append(remaining, ordered[index:]...)
			break
		}
		if err := os.Remove(file.Path); err != nil && !os.IsNotExist(err) {
			return keep, deleted, fmt.Errorf("failed to delete %s: %w", file.Name, err)
		}
		total -= file.Size
		deleted = append(deleted, Deletion{Name: file.Name, Size: file.Size, Reason: ReasonSizeCap})
	}

	return remaining, deleted, nil
}
