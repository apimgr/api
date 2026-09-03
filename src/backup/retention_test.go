package backup

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/apimgr/api/src/config"
)

// writeBackupFile creates a placeholder archive of the requested size so
// retention tests can exercise both count and size limits.
func writeBackupFile(t *testing.T, dir, name string, size int) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, make([]byte, size), 0600))
	return path
}

// backupNames lists what survived a retention sweep.
func backupNames(t *testing.T, dir string) []string {
	t.Helper()
	files, err := ListBackups(dir)
	require.NoError(t, err)

	names := make([]string, 0, len(files))
	for _, file := range files {
		names = append(names, file.Name)
	}
	return names
}

// Every PART 21 naming pattern must classify to its own kind, and files the
// app never creates must be left alone.
func TestClassifyNamingPatterns(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]Kind{
		"api_backup_2025-01-15.tar.gz":            KindFull,
		"api_backup_2025-01-15.tar.gz.enc":        KindFull,
		"api_backup_2025-01-15_103000.tar.gz":     KindManual,
		"api_backup_2025-01-15_103000.tar.gz.enc": KindManual,
		"api-daily.tar.gz":                        KindDailyIncremental,
		"api-hourly.tar.gz.enc":                   KindHourlyIncremental,
		"api_backup_legacy.tar.gz":                KindUnclassified,
	}

	for name, expected := range cases {
		writeBackupFile(t, dir, name, 1)
		info, err := os.Stat(filepath.Join(dir, name))
		require.NoError(t, err)

		file, ok := Classify(dir, info)
		require.True(t, ok, "expected %s to be classified", name)
		assert.Equal(t, expected, file.Kind, "kind for %s", name)
	}

	writeBackupFile(t, dir, "unrelated.tar.gz", 1)
	info, err := os.Stat(filepath.Join(dir, "unrelated.tar.gz"))
	require.NoError(t, err)
	_, ok := Classify(dir, info)
	assert.False(t, ok, "files outside the app's naming must be left untouched")
}

// The default policy keeps a single full backup plus the incrementals, which
// PART 21 excludes from count-based retention.
func TestApplyRetentionDefaultKeepsOneFullPlusIncrementals(t *testing.T) {
	dir := t.TempDir()
	writeBackupFile(t, dir, "api_backup_2025-01-13.tar.gz", 10)
	writeBackupFile(t, dir, "api_backup_2025-01-14.tar.gz", 10)
	writeBackupFile(t, dir, "api_backup_2025-01-15.tar.gz", 10)
	writeBackupFile(t, dir, "api-daily.tar.gz", 5)
	writeBackupFile(t, dir, "api-hourly.tar.gz", 5)

	result, err := ApplyRetention(dir, config.BackupRetentionConfig{MaxBackups: 1, MaxTotalSize: "0"})
	require.NoError(t, err)

	assert.Len(t, result.Deleted, 2)
	assert.Equal(t, "api_backup_2025-01-13.tar.gz", result.Deleted[0].Name)
	assert.Equal(t, "api_backup_2025-01-14.tar.gz", result.Deleted[1].Name)
	assert.ElementsMatch(t,
		[]string{"api_backup_2025-01-15.tar.gz", "api-daily.tar.gz", "api-hourly.tar.gz"},
		backupNames(t, dir))
}

// Yearly beats monthly beats weekly beats daily, and one archive may satisfy
// several categories at once.
func TestApplyRetentionPriorityOrder(t *testing.T) {
	dir := t.TempDir()
	// 2026-01-01 is a Thursday: yearly and monthly, not weekly.
	writeBackupFile(t, dir, "api_backup_2026-01-01.tar.gz", 10)
	// 2025-12-01 is the first of the month.
	writeBackupFile(t, dir, "api_backup_2025-12-01.tar.gz", 10)
	// 2026-01-11 is a Sunday.
	writeBackupFile(t, dir, "api_backup_2026-01-11.tar.gz", 10)
	writeBackupFile(t, dir, "api_backup_2026-01-15.tar.gz", 10)
	// Unmarked filler that must be pruned.
	writeBackupFile(t, dir, "api_backup_2026-01-14.tar.gz", 10)

	result, err := ApplyRetention(dir, config.BackupRetentionConfig{
		MaxBackups:   1,
		KeepWeekly:   1,
		KeepMonthly:  1,
		KeepYearly:   1,
		MaxTotalSize: "0",
	})
	require.NoError(t, err)

	assert.Equal(t, []string{"api_backup_2026-01-14.tar.gz"}, deletionNames(result))
	assert.ElementsMatch(t, []string{
		"api_backup_2025-12-01.tar.gz",
		"api_backup_2026-01-01.tar.gz",
		"api_backup_2026-01-11.tar.gz",
		"api_backup_2026-01-15.tar.gz",
	}, backupNames(t, dir))
}

// Manual/timestamped backups compete for max_backups alongside daily fulls.
func TestApplyRetentionCountsManualBackups(t *testing.T) {
	dir := t.TempDir()
	writeBackupFile(t, dir, "api_backup_2026-02-10_010000.tar.gz", 10)
	writeBackupFile(t, dir, "api_backup_2026-02-11_010000.tar.gz", 10)
	writeBackupFile(t, dir, "api_backup_2026-02-12.tar.gz", 10)

	result, err := ApplyRetention(dir, config.BackupRetentionConfig{MaxBackups: 2, MaxTotalSize: "0"})
	require.NoError(t, err)

	assert.Equal(t, []string{"api_backup_2026-02-10_010000.tar.gz"}, deletionNames(result))
}

// The hard size cap overrides the count limits and deletes oldest first.
func TestApplyRetentionSizeCapOverridesCounts(t *testing.T) {
	dir := t.TempDir()
	writeBackupFile(t, dir, "api_backup_2026-03-01.tar.gz", 1000)
	writeBackupFile(t, dir, "api_backup_2026-03-02.tar.gz", 1000)
	writeBackupFile(t, dir, "api_backup_2026-03-03.tar.gz", 1000)

	result, err := ApplyRetention(dir, config.BackupRetentionConfig{MaxBackups: 5, MaxTotalSize: "2000"})
	require.NoError(t, err)

	assert.Equal(t, []string{"api_backup_2026-03-01.tar.gz"}, deletionNames(result))
	assert.Equal(t, ReasonSizeCap, result.Deleted[0].Reason)
	assert.Equal(t, int64(2000), result.RemainingSize)
}

// An invalid max_backups warns and falls back to the default rather than
// failing the sweep.
func TestApplyRetentionInvalidPolicyWarns(t *testing.T) {
	dir := t.TempDir()
	writeBackupFile(t, dir, "api_backup_2026-04-01.tar.gz", 10)

	result, err := ApplyRetention(dir, config.BackupRetentionConfig{MaxBackups: 0, MaxTotalSize: "0"})
	require.NoError(t, err)

	assert.NotEmpty(t, result.Warnings)
	assert.Empty(t, result.Deleted)
}

// Size caps accept both absolute sizes and percentages, and every falsey form
// disables the cap.
func TestParseSizeCap(t *testing.T) {
	dir := t.TempDir()

	absolute, err := ParseSizeCap("50G", dir)
	require.NoError(t, err)
	assert.Equal(t, int64(50)*(1<<30), absolute)

	bytesOnly, err := ParseSizeCap("2048B", dir)
	require.NoError(t, err)
	assert.Equal(t, int64(2048), bytesOnly)

	percent, err := ParseSizeCap("10%", dir)
	require.NoError(t, err)
	assert.Greater(t, percent, int64(0))

	zero, err := ParseSizeCap("0", dir)
	require.NoError(t, err)
	assert.Equal(t, int64(0), zero)

	_, err = ParseSizeCap("banana", dir)
	assert.Error(t, err)
}

// Filenames must match the exact PART 21 patterns.
func TestBackupNameFormats(t *testing.T) {
	when := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)

	assert.Equal(t, "api_backup_2025-01-15.tar.gz", FullBackupName(when, false))
	assert.Equal(t, "api_backup_2025-01-15.tar.gz.enc", FullBackupName(when, true))
	assert.Equal(t, "api_backup_2025-01-15_103000.tar.gz", ManualBackupName(when, false))
	assert.Equal(t, "api-daily.tar.gz", DailyIncrementalName(false))
	assert.Equal(t, "api-hourly.tar.gz.enc", HourlyIncrementalName(true))
}

// deletionNames extracts the deleted filenames from a sweep result.
func deletionNames(result RetentionResult) []string {
	names := make([]string, 0, len(result.Deleted))
	for _, deletion := range result.Deleted {
		names = append(names, deletion.Name)
	}
	return names
}
