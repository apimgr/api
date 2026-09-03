package datetime

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Covers FormatDatetime across every named format, a literal Go layout
// fallback, case-insensitive name matching, and the empty-format error.
func TestFormatDatetime(t *testing.T) {
	tests := []struct {
		name      string
		timestamp int64
		format    string
		wantErr   bool
		want      string
	}{
		{name: "iso8601", timestamp: 1700000000, format: "iso8601", want: "2023-11-14T22:13:20Z"},
		{name: "rfc3339", timestamp: 1700000000, format: "rfc3339", want: "2023-11-14T22:13:20Z"},
		{name: "rfc1123", timestamp: 1700000000, format: "rfc1123"},
		{name: "rfc822", timestamp: 1700000000, format: "rfc822"},
		{name: "kitchen", timestamp: 1700000000, format: "kitchen"},
		{name: "date", timestamp: 1700000000, format: "date", want: "2023-11-14"},
		{name: "time", timestamp: 1700000000, format: "time", want: "22:13:20"},
		{name: "datetime", timestamp: 1700000000, format: "datetime", want: "2023-11-14 22:13:20"},
		{name: "case insensitive", timestamp: 1700000000, format: "ISO8601", want: "2023-11-14T22:13:20Z"},
		{name: "literal layout fallback", timestamp: 1700000000, format: "2006", want: "2023"},
		{name: "empty format", timestamp: 1700000000, format: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := FormatDatetime(tt.timestamp, tt.format)
			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, result)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.format, result["format"])
			if tt.want != "" {
				assert.Equal(t, tt.want, result["result"])
			}
		})
	}
}

// Covers ParseDateString across every supported layout and the
// unparseable-input error path.
func TestParseDateString(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "RFC3339", input: "2023-11-14T22:13:20Z"},
		{name: "RFC3339Nano", input: "2023-11-14T22:13:20.5Z"},
		{name: "RFC1123Z", input: "Tue, 14 Nov 2023 22:13:20 +0000"},
		{name: "RFC1123", input: "Tue, 14 Nov 2023 22:13:20 UTC"},
		{name: "RFC822", input: "14 Nov 23 22:13 UTC"},
		{name: "date time T", input: "2023-11-14T22:13:20"},
		{name: "date time space", input: "2023-11-14 22:13:20"},
		{name: "date only", input: "2023-11-14"},
		{name: "us date", input: "11/14/2023"},
		{name: "long form", input: "January 2, 2006"},
		{name: "short month", input: "Nov 14, 2023"},
		{name: "time only", input: "22:13:20"},
		{name: "garbage", input: "not-a-date", wantErr: true},
		{name: "empty", input: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseDateString(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, result)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.input, result["input"])
			assert.Contains(t, result, "matched_layout")
			assert.Contains(t, result, "day_of_week")
		})
	}
}

// Covers GenerateCalendar for a month starting mid-week (needs leading
// padding), a month ending mid-week (needs trailing padding), a leap
// February, and the out-of-range month error.
func TestGenerateCalendar(t *testing.T) {
	t.Run("november 2023", func(t *testing.T) {
		result, err := GenerateCalendar(2023, 11)
		require.NoError(t, err)
		assert.Equal(t, 30, result["days_in_month"])
		assert.Equal(t, "November", result["month_name"])
		weeks, ok := result["weeks"].([][]int)
		require.True(t, ok)
		require.NotEmpty(t, weeks)
		for _, w := range weeks {
			assert.Len(t, w, 7)
		}
	})

	t.Run("leap february", func(t *testing.T) {
		result, err := GenerateCalendar(2024, 2)
		require.NoError(t, err)
		assert.Equal(t, 29, result["days_in_month"])
		assert.Equal(t, true, result["is_leap_year"])
	})

	t.Run("month too low", func(t *testing.T) {
		result, err := GenerateCalendar(2023, 0)
		require.Error(t, err)
		assert.Nil(t, result)
	})

	t.Run("month too high", func(t *testing.T) {
		result, err := GenerateCalendar(2023, 13)
		require.Error(t, err)
		assert.Nil(t, result)
	})
}

// Covers WorkdaysBetween for a normal range, a range given in reverse
// order (must swap), a same-day range, and both invalid-format error
// paths.
func TestWorkdaysBetween(t *testing.T) {
	t.Run("normal week", func(t *testing.T) {
		// 2023-11-13 is a Monday, 2023-11-19 is a Sunday.
		result, err := WorkdaysBetween("2023-11-13", "2023-11-19")
		require.NoError(t, err)
		assert.Equal(t, 7, result["total_days"])
		assert.Equal(t, 5, result["workdays"])
		assert.Equal(t, 2, result["weekend_days"])
	})

	t.Run("reversed order swaps", func(t *testing.T) {
		result, err := WorkdaysBetween("2023-11-19", "2023-11-13")
		require.NoError(t, err)
		assert.Equal(t, "2023-11-13", result["start"])
		assert.Equal(t, "2023-11-19", result["end"])
	})

	t.Run("same day", func(t *testing.T) {
		result, err := WorkdaysBetween("2023-11-13", "2023-11-13")
		require.NoError(t, err)
		assert.Equal(t, 1, result["total_days"])
		assert.Equal(t, 1, result["workdays"])
	})

	t.Run("invalid start", func(t *testing.T) {
		result, err := WorkdaysBetween("bogus", "2023-11-13")
		require.Error(t, err)
		assert.Nil(t, result)
	})

	t.Run("invalid end", func(t *testing.T) {
		result, err := WorkdaysBetween("2023-11-13", "bogus")
		require.Error(t, err)
		assert.Nil(t, result)
	})
}

// Covers SunriseSunset for a typical mid-latitude location, the
// default-to-today path (empty dateStr), invalid latitude/longitude, an
// invalid date string, and the polar-event branch (no sunrise/sunset).
func TestSunriseSunset(t *testing.T) {
	t.Run("new york", func(t *testing.T) {
		result, err := SunriseSunset(40.7128, -74.0060, "2023-06-21")
		require.NoError(t, err)
		assert.Equal(t, "2023-06-21", result["date"])
		assert.Contains(t, result, "sunrise_utc")
		assert.Contains(t, result, "sunset_utc")
		assert.NotContains(t, result, "polar_event")
	})

	t.Run("default date", func(t *testing.T) {
		result, err := SunriseSunset(0, 0, "")
		require.NoError(t, err)
		assert.Contains(t, result, "date")
	})

	t.Run("latitude too low", func(t *testing.T) {
		result, err := SunriseSunset(-91, 0, "2023-06-21")
		require.Error(t, err)
		assert.Nil(t, result)
	})

	t.Run("latitude too high", func(t *testing.T) {
		result, err := SunriseSunset(91, 0, "2023-06-21")
		require.Error(t, err)
		assert.Nil(t, result)
	})

	t.Run("longitude too low", func(t *testing.T) {
		result, err := SunriseSunset(0, -181, "2023-06-21")
		require.Error(t, err)
		assert.Nil(t, result)
	})

	t.Run("longitude too high", func(t *testing.T) {
		result, err := SunriseSunset(0, 181, "2023-06-21")
		require.Error(t, err)
		assert.Nil(t, result)
	})

	t.Run("invalid date", func(t *testing.T) {
		result, err := SunriseSunset(0, 0, "bogus")
		require.Error(t, err)
		assert.Nil(t, result)
	})

	t.Run("polar night at north pole", func(t *testing.T) {
		// At 89.9N in December, the sun does not rise: the algorithm
		// hits the |cosH| > 1 branch and reports a polar event.
		result, err := SunriseSunset(89.9, 0, "2023-12-21")
		require.NoError(t, err)
		assert.Equal(t, true, result["polar_event"])
		assert.Contains(t, result, "description")
	})
}

// Covers MoonPhase for the known reference new moon date, the
// default-to-today path, and the invalid-date error path. Also checks
// every phase-name bucket is reachable by scanning a full synodic month.
func TestMoonPhase(t *testing.T) {
	t.Run("reference new moon", func(t *testing.T) {
		result, err := MoonPhase("2000-01-06")
		require.NoError(t, err)
		assert.Equal(t, "New Moon", result["phase"])
	})

	t.Run("default date", func(t *testing.T) {
		result, err := MoonPhase("")
		require.NoError(t, err)
		assert.Contains(t, result, "phase")
	})

	t.Run("invalid date", func(t *testing.T) {
		result, err := MoonPhase("bogus")
		require.Error(t, err)
		assert.Nil(t, result)
	})

	t.Run("every phase bucket is reachable", func(t *testing.T) {
		seen := map[string]bool{}
		for day := 0; day < 30; day++ {
			date := addDaysToDateString("2000-01-06", day)
			result, err := MoonPhase(date)
			require.NoError(t, err)
			phase, ok := result["phase"].(string)
			require.True(t, ok)
			seen[phase] = true
		}
		expected := []string{
			"New Moon", "Waxing Crescent", "First Quarter", "Waxing Gibbous",
			"Full Moon", "Waning Gibbous", "Last Quarter", "Waning Crescent",
		}
		for _, phase := range expected {
			assert.True(t, seen[phase], "phase %q was never produced", phase)
		}
	})
}

// addDaysToDateString is a small test helper that adds n days to a
// YYYY-MM-DD date string, used to sweep MoonPhase across a full synodic
// month.
func addDaysToDateString(dateStr string, n int) string {
	t, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return dateStr
	}
	return t.AddDate(0, 0, n).Format("2006-01-02")
}

// Covers ParseCron for a valid expression, every field-validation error
// path, the wrong-field-count error, and the day-of-month/day-of-week
// OR-match behavior when both are restricted.
func TestParseCron(t *testing.T) {
	t.Run("every 5 minutes", func(t *testing.T) {
		result, err := ParseCron("*/5 * * * *")
		require.NoError(t, err)
		fields, ok := result["fields"].(map[string]string)
		require.True(t, ok)
		assert.Equal(t, "*/5", fields["minute"])
		nextRuns, ok := result["next_runs"].([]string)
		require.True(t, ok)
		assert.Len(t, nextRuns, 5)
	})

	t.Run("wrong field count", func(t *testing.T) {
		result, err := ParseCron("* * * *")
		require.Error(t, err)
		assert.Nil(t, result)
	})

	t.Run("invalid minute", func(t *testing.T) {
		result, err := ParseCron("60 * * * *")
		require.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "minute field")
	})

	t.Run("invalid hour", func(t *testing.T) {
		result, err := ParseCron("0 24 * * *")
		require.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "hour field")
	})

	t.Run("invalid day of month", func(t *testing.T) {
		result, err := ParseCron("0 0 32 * *")
		require.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "day-of-month field")
	})

	t.Run("invalid month", func(t *testing.T) {
		result, err := ParseCron("0 0 1 13 *")
		require.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "month field")
	})

	t.Run("invalid day of week", func(t *testing.T) {
		result, err := ParseCron("0 0 * * 7")
		require.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "day-of-week field")
	})

	t.Run("dom and dow both restricted are OR matched", func(t *testing.T) {
		// Runs on the 1st of the month OR any Monday.
		result, err := ParseCron("0 0 1 * 1")
		require.NoError(t, err)
		nextRuns, ok := result["next_runs"].([]string)
		require.True(t, ok)
		assert.Len(t, nextRuns, 5)
	})
}

// Covers parseCronField across comma lists, ranges, steps, the wildcard,
// and every documented error condition.
func TestParseCronField(t *testing.T) {
	tests := []struct {
		name    string
		field   string
		min     int
		max     int
		wantErr bool
		want    map[int]bool
	}{
		{name: "wildcard", field: "*", min: 0, max: 3, want: map[int]bool{0: true, 1: true, 2: true, 3: true}},
		{name: "single value", field: "5", min: 0, max: 59, want: map[int]bool{5: true}},
		{name: "comma list", field: "1,3,5", min: 0, max: 59, want: map[int]bool{1: true, 3: true, 5: true}},
		{name: "range", field: "1-3", min: 0, max: 59, want: map[int]bool{1: true, 2: true, 3: true}},
		{name: "step", field: "*/15", min: 0, max: 59, want: map[int]bool{0: true, 15: true, 30: true, 45: true}},
		{name: "range with step", field: "0-10/5", min: 0, max: 59, want: map[int]bool{0: true, 5: true, 10: true}},
		{name: "invalid step", field: "*/0", min: 0, max: 59, wantErr: true},
		{name: "invalid step non-numeric", field: "*/x", min: 0, max: 59, wantErr: true},
		{name: "invalid range bounds", field: "1-abc", min: 0, max: 59, wantErr: true},
		{name: "range out of bounds", field: "1-100", min: 0, max: 59, wantErr: true},
		{name: "range reversed", field: "10-1", min: 0, max: 59, wantErr: true},
		{name: "invalid single value", field: "abc", min: 0, max: 59, wantErr: true},
		{name: "single value out of range", field: "100", min: 0, max: 59, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseCronField(tt.field, tt.min, tt.max)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// Covers ParseCron's error-message wrapping for the day-of-week field
// specifically (parseCronField returning an error via the last field).
func TestParseCron_FieldErrorMessages(t *testing.T) {
	_, err := ParseCron("* * * * 99")
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "day-of-week field"))
}
