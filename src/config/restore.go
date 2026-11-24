package config

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// RestoreConfig bounds backup archive restoration and import operations.
type RestoreConfig struct {
	MaxArchiveSize  string `yaml:"max_archive_size"`
	MaxExpandedSize string `yaml:"max_expanded_size"`
	MaxFiles        int    `yaml:"max_files"`
	MaxFileSize     string `yaml:"max_file_size"`
	MaxRatio        int64  `yaml:"max_ratio"`
}

// DefaultRestoreConfig returns the archive safety defaults from AI.md PART 16.
func DefaultRestoreConfig() RestoreConfig {
	return RestoreConfig{
		MaxArchiveSize:  "1G",
		MaxExpandedSize: "4G",
		MaxFiles:        10000,
		MaxFileSize:     "512M",
		MaxRatio:        100,
	}
}

// Normalized replaces invalid restore limits with their documented defaults.
func (c RestoreConfig) Normalized() (RestoreConfig, []string) {
	defaults := DefaultRestoreConfig()
	var warnings []string
	for _, item := range []struct {
		name  string
		value *string
		def   string
	}{
		{"server.restore.max_archive_size", &c.MaxArchiveSize, defaults.MaxArchiveSize},
		{"server.restore.max_expanded_size", &c.MaxExpandedSize, defaults.MaxExpandedSize},
		{"server.restore.max_file_size", &c.MaxFileSize, defaults.MaxFileSize},
	} {
		if _, err := ParseByteSize(*item.value); err != nil {
			warnings = append(warnings, item.name+" is invalid; using "+item.def)
			*item.value = item.def
		}
	}
	if c.MaxFiles < 1 {
		warnings = append(warnings, "server.restore.max_files must be at least 1; using 10000")
		c.MaxFiles = defaults.MaxFiles
	}
	if c.MaxRatio < 1 {
		warnings = append(warnings, "server.restore.max_ratio must be at least 1; using 100")
		c.MaxRatio = defaults.MaxRatio
	}
	return c, warnings
}

// ParseByteSize converts a positive byte count with an optional binary unit.
func ParseByteSize(value string) (int64, error) {
	value = strings.TrimSpace(strings.ToUpper(value))
	if value == "" {
		return 0, fmt.Errorf("size is empty")
	}
	units := []struct {
		suffix string
		factor int64
	}{
		{"GIB", 1 << 30}, {"GB", 1 << 30}, {"G", 1 << 30},
		{"MIB", 1 << 20}, {"MB", 1 << 20}, {"M", 1 << 20},
		{"KIB", 1 << 10}, {"KB", 1 << 10}, {"K", 1 << 10},
		{"B", 1},
	}
	factor := int64(1)
	for _, unit := range units {
		if strings.HasSuffix(value, unit.suffix) {
			factor = unit.factor
			value = strings.TrimSpace(strings.TrimSuffix(value, unit.suffix))
			break
		}
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n < 1 || n > math.MaxInt64/factor {
		return 0, fmt.Errorf("invalid or overflowing size")
	}
	return n * factor, nil
}
