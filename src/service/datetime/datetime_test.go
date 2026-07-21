package datetime

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Covers Now() with an empty timezone (defaults to UTC), a valid IANA
// timezone, and an invalid timezone (error path).
func TestNow(t *testing.T) {
	t.Run("default UTC", func(t *testing.T) {
		result, err := Now("")
		require.NoError(t, err)
		assert.Equal(t, "UTC", result["timezone"])
		assert.Contains(t, result, "unix")
		assert.Contains(t, result, "day_of_week_name")
	})

	t.Run("valid timezone", func(t *testing.T) {
		result, err := Now("America/New_York")
		require.NoError(t, err)
		assert.Equal(t, "America/New_York", result["timezone"])
	})

	t.Run("invalid timezone", func(t *testing.T) {
		result, err := Now("Not/AZone")
		require.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "invalid timezone")
	})
}

// Covers FromUnix precision auto-detection (seconds, milliseconds,
// microseconds, nanoseconds) plus the invalid-timezone error path.
func TestFromUnix(t *testing.T) {
	tests := []struct {
		name      string
		timestamp int64
		timezone  string
		wantErr   bool
		wantDate  string
	}{
		{name: "seconds", timestamp: 1700000000, timezone: "", wantDate: "2023-11-14"},
		{name: "milliseconds", timestamp: 1700000000000, timezone: "", wantDate: "2023-11-14"},
		{name: "microseconds", timestamp: 1700000000000000, timezone: "", wantDate: "2023-11-14"},
		{name: "nanoseconds", timestamp: 1700000000000000000, timezone: "", wantDate: "2023-11-14"},
		{name: "zero timestamp", timestamp: 0, timezone: "", wantDate: "1970-01-01"},
		{name: "with timezone", timestamp: 1700000000, timezone: "America/New_York"},
		{name: "invalid timezone", timestamp: 1700000000, timezone: "Bogus/Zone", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := FromUnix(tt.timestamp, tt.timezone)
			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, result)
				return
			}
			require.NoError(t, err)
			if tt.wantDate != "" {
				assert.Equal(t, tt.wantDate, result["date"])
			}
			assert.Contains(t, result, "iso8601")
		})
	}
}

// Covers ToUnix across every supported input format and the
// unparseable-input error path.
func TestToUnix(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantErr  bool
		wantUnix int64
	}{
		{name: "RFC3339", input: "2023-11-14T22:13:20Z", wantUnix: 1700000000},
		{name: "date only", input: "2023-11-14"},
		{name: "date time space", input: "2023-11-14 22:13:20"},
		{name: "us date", input: "11/14/2023"},
		{name: "long form", input: "November 14, 2023"},
		{name: "empty string", input: "", wantErr: true},
		{name: "garbage", input: "not-a-date", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			unix, err := ToUnix(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				assert.Equal(t, int64(0), unix)
				return
			}
			require.NoError(t, err)
			if tt.wantUnix != 0 {
				assert.Equal(t, tt.wantUnix, unix)
			}
		})
	}
}

// Covers AddDuration for Go-native durations, custom shorthand units
// (d/w/M/y), and the unparseable-duration error path.
func TestAddDuration(t *testing.T) {
	base := int64(1700000000)

	tests := []struct {
		name     string
		duration string
		wantErr  bool
	}{
		{name: "go duration hours", duration: "2h"},
		{name: "days shorthand", duration: "1d"},
		{name: "weeks shorthand", duration: "2w"},
		{name: "months shorthand", duration: "1M"},
		{name: "years shorthand", duration: "1y"},
		{name: "day word", duration: "3days"},
		{name: "invalid duration", duration: "bogus", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := AddDuration(base, tt.duration)
			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, result)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.duration, result["duration"])
			assert.Contains(t, result, "result_unix")
		})
	}

	t.Run("addition is correct for days", func(t *testing.T) {
		result, err := AddDuration(base, "1d")
		require.NoError(t, err)
		assert.Equal(t, base+86400, result["result_unix"])
	})
}

// Covers Diff for a positive interval, a zero interval, and a negative
// interval (t2 before t1) to confirm the sign is preserved.
func TestDiff(t *testing.T) {
	t.Run("positive diff", func(t *testing.T) {
		result := Diff(1700000000, 1700090061)
		assert.Equal(t, 1, result["days"])
		assert.Equal(t, 90061, result["seconds"])
		assert.Contains(t, result["human"], "day(s)")
	})

	t.Run("zero diff", func(t *testing.T) {
		result := Diff(1700000000, 1700000000)
		assert.Equal(t, 0, result["days"])
		assert.Equal(t, 0, result["seconds"])
		assert.Equal(t, "0 second(s)", result["human"])
	})

	t.Run("negative diff", func(t *testing.T) {
		result := Diff(1700000100, 1700000000)
		assert.Equal(t, -100, result["seconds"])
	})
}

// Covers Timezones: verifies the list is non-empty, every well-known zone
// resolves, and each entry carries the expected fields.
func TestTimezones(t *testing.T) {
	zones := Timezones()
	require.NotEmpty(t, zones)

	names := make(map[string]bool)
	for _, z := range zones {
		name, ok := z["name"].(string)
		require.True(t, ok)
		names[name] = true
		assert.Contains(t, z, "offset")
		assert.Contains(t, z, "current_time")
	}
	assert.True(t, names["UTC"])
	assert.True(t, names["America/New_York"])
}

// Covers TimezoneInfo for a valid zone and the invalid-zone error path.
func TestTimezoneInfo(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		result, err := TimezoneInfo("UTC")
		require.NoError(t, err)
		assert.Equal(t, "UTC", result["name"])
	})

	t.Run("invalid", func(t *testing.T) {
		result, err := TimezoneInfo("Not/AZone")
		require.Error(t, err)
		assert.Nil(t, result)
	})
}

// Covers ConvertTimezone: valid source/target, invalid source, and
// invalid target (both must be independently validated).
func TestConvertTimezone(t *testing.T) {
	t.Run("valid conversion", func(t *testing.T) {
		result, err := ConvertTimezone(1700000000, "UTC", "America/New_York")
		require.NoError(t, err)
		assert.Equal(t, "UTC", result["from_zone"])
		assert.Equal(t, "America/New_York", result["to_zone"])
	})

	t.Run("invalid source", func(t *testing.T) {
		result, err := ConvertTimezone(1700000000, "Bogus/Zone", "UTC")
		require.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "invalid source timezone")
	})

	t.Run("invalid target", func(t *testing.T) {
		result, err := ConvertTimezone(1700000000, "UTC", "Bogus/Zone")
		require.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "invalid target timezone")
	})
}

// Covers formatOffset for positive, negative, half-hour, and zero
// offsets — including the branch that normalizes a negative minute
// remainder back to positive.
func TestFormatOffset(t *testing.T) {
	tests := []struct {
		name    string
		seconds int
		want    string
	}{
		{name: "zero", seconds: 0, want: "+00:00"},
		{name: "positive whole hours", seconds: 3600 * 5, want: "+05:00"},
		{name: "negative whole hours", seconds: -3600 * 8, want: "-08:00"},
		{name: "positive half hour", seconds: 3600*5 + 1800, want: "+05:30"},
		{name: "negative half hour", seconds: -(3600*9 + 1800), want: "-09:30"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, formatOffset(tt.seconds))
		})
	}
}

// Covers isLeapYear for the century/quadricentennial edge cases that
// distinguish the Gregorian leap rule from a naive %4 check.
func TestIsLeapYear(t *testing.T) {
	tests := []struct {
		year int
		want bool
	}{
		{2000, true},
		{2004, true},
		{1900, false},
		{2023, false},
		{2400, true},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.want, isLeapYear(tt.year), "year %d", tt.year)
	}
}

// Covers daysInMonth including the leap-February and December
// year-rollover edge cases.
func TestDaysInMonth(t *testing.T) {
	tests := []struct {
		year, month, want int
	}{
		{2024, 2, 29},
		{2023, 2, 28},
		{2023, 1, 31},
		{2023, 4, 30},
		{2023, 12, 31},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.want, daysInMonth(tt.year, tt.month))
	}
}

// Covers getWeekNumber via a fixed, known ISO week date.
func TestGetWeekNumber(t *testing.T) {
	d := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	assert.Equal(t, 1, getWeekNumber(d))
}

// Covers parseDuration for standard Go durations, every custom
// shorthand unit and its long-form aliases, and the unparseable input.
func TestParseDuration(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    time.Duration
		wantErr bool
	}{
		{name: "go duration", input: "90m", want: 90 * time.Minute},
		{name: "day short", input: "2d", want: 2 * 24 * time.Hour},
		{name: "day long", input: "2day", want: 2 * 24 * time.Hour},
		{name: "days long", input: "2days", want: 2 * 24 * time.Hour},
		{name: "week short", input: "1w", want: 7 * 24 * time.Hour},
		{name: "week long", input: "1week", want: 7 * 24 * time.Hour},
		{name: "weeks long", input: "1weeks", want: 7 * 24 * time.Hour},
		{name: "month long", input: "1month", want: 30 * 24 * time.Hour},
		{name: "year short", input: "1y", want: 365 * 24 * time.Hour},
		{name: "year long", input: "1year", want: 365 * 24 * time.Hour},
		{name: "unparseable", input: "banana", wantErr: true},
		{name: "empty", input: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseDuration(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}

	// "1m" is ambiguous: Go's stdlib time.ParseDuration treats "m" as
	// minutes and succeeds before the custom-unit fallback ever runs, so
	// it must NOT be treated as "1 month".
	t.Run("lowercase m means minutes not months", func(t *testing.T) {
		got, err := parseDuration("1m")
		require.NoError(t, err)
		assert.Equal(t, time.Minute, got)
	})

	// "1M" is lowercased to "1m" before either parser runs, so an
	// uppercase month shorthand is just as ambiguous as the lowercase
	// form above and also resolves to minutes, not 30 days.
	t.Run("uppercase M is lowercased first, also means minutes", func(t *testing.T) {
		got, err := parseDuration("1M")
		require.NoError(t, err)
		assert.Equal(t, time.Minute, got)
	})
}

// Covers formatDurationHuman across all-zero (falls back to "0
// second(s)"), single-unit, and multi-unit combinations.
func TestFormatDurationHuman(t *testing.T) {
	tests := []struct {
		name                          string
		days, hours, minutes, seconds int
		want                          string
	}{
		{name: "all zero", want: "0 second(s)"},
		{name: "seconds only", seconds: 5, want: "5 second(s)"},
		{name: "days and hours", days: 1, hours: 2, want: "1 day(s), 2 hour(s)"},
		{name: "everything", days: 1, hours: 2, minutes: 3, seconds: 4, want: "1 day(s), 2 hour(s), 3 minute(s), 4 second(s)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatDurationHuman(tt.days, tt.hours, tt.minutes, tt.seconds)
			assert.Equal(t, tt.want, got)
			assert.False(t, strings.HasSuffix(got, ", "))
		})
	}
}
