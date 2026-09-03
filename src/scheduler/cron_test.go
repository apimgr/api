package scheduler

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseField covers the single-value, wildcard, range, step, and
// comma-list forms accepted by one cron field, plus the error paths for
// out-of-range and malformed input.
func TestParseField(t *testing.T) {
	fullRange := func(min, max int) fieldSet {
		s := fieldSet{}
		for v := min; v <= max; v++ {
			s[v] = true
		}
		return s
	}

	tests := []struct {
		name    string
		field   string
		min     int
		max     int
		want    fieldSet
		wantErr bool
	}{
		{"wildcard", "*", 0, 59, fullRange(0, 59), false},
		{"single value", "5", 0, 59, fieldSet{5: true}, false},
		{"range", "1-3", 0, 59, fieldSet{1: true, 2: true, 3: true}, false},
		{"step wildcard", "*/15", 0, 59, fieldSet{0: true, 15: true, 30: true, 45: true}, false},
		{"step range", "0-10/5", 0, 59, fieldSet{0: true, 5: true, 10: true}, false},
		{"comma list", "1,3,5", 0, 59, fieldSet{1: true, 3: true, 5: true}, false},
		{"mixed list", "1,5-7,*/20", 0, 59, fieldSet{1: true, 5: true, 6: true, 7: true, 0: true, 20: true, 40: true}, false},
		{"empty field", "", 0, 59, nil, true},
		{"out of range low", "-1", 0, 59, nil, true},
		{"out of range high", "60", 0, 59, nil, true},
		{"reversed range", "10-5", 0, 59, nil, true},
		{"invalid step", "*/0", 0, 59, nil, true},
		{"invalid step negative", "*/-5", 0, 59, nil, true},
		{"non-numeric", "abc", 0, 59, nil, true},
		{"non-numeric range start", "a-5", 0, 59, nil, true},
		{"non-numeric range end", "5-a", 0, 59, nil, true},
		{"boundary min", "0", 0, 59, fieldSet{0: true}, false},
		{"boundary max", "59", 0, 59, fieldSet{59: true}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseField(tc.field, tc.min, tc.max)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestParseCron covers structural validation (field count), per-field
// range enforcement, and the "7 == Sunday == 0" cron weekday alias.
func TestParseCron(t *testing.T) {
	t.Run("valid expression", func(t *testing.T) {
		spec, err := parseCron("30 4 1 6 2")
		require.NoError(t, err)
		assert.True(t, spec.minute.has(30))
		assert.True(t, spec.hour.has(4))
		assert.True(t, spec.day.has(1))
		assert.True(t, spec.month.has(6))
		assert.True(t, spec.weekday.has(2))
		assert.False(t, spec.dayStar)
		assert.False(t, spec.weekdayStar)
	})

	t.Run("star flags recorded", func(t *testing.T) {
		spec, err := parseCron("0 0 * * *")
		require.NoError(t, err)
		assert.True(t, spec.dayStar)
		assert.True(t, spec.weekdayStar)
	})

	t.Run("weekday 7 normalizes to 0 (Sunday)", func(t *testing.T) {
		spec, err := parseCron("0 0 * * 7")
		require.NoError(t, err)
		assert.True(t, spec.weekday.has(0))
		assert.False(t, spec.weekday.has(7))
	})

	t.Run("wrong field count too few", func(t *testing.T) {
		_, err := parseCron("0 0 * *")
		assert.Error(t, err)
	})

	t.Run("wrong field count too many", func(t *testing.T) {
		_, err := parseCron("0 0 * * * *")
		assert.Error(t, err)
	})

	t.Run("bad minute field", func(t *testing.T) {
		_, err := parseCron("60 0 * * *")
		assert.ErrorContains(t, err, "minute field")
	})

	t.Run("bad hour field", func(t *testing.T) {
		_, err := parseCron("0 24 * * *")
		assert.ErrorContains(t, err, "hour field")
	})

	t.Run("bad day field", func(t *testing.T) {
		_, err := parseCron("0 0 32 * *")
		assert.ErrorContains(t, err, "day field")
	})

	t.Run("bad month field", func(t *testing.T) {
		_, err := parseCron("0 0 * 13 *")
		assert.ErrorContains(t, err, "month field")
	})

	t.Run("bad weekday field", func(t *testing.T) {
		_, err := parseCron("0 0 * * 8")
		assert.ErrorContains(t, err, "weekday field")
	})

	t.Run("day 0 out of range", func(t *testing.T) {
		_, err := parseCron("0 0 0 * *")
		assert.Error(t, err)
	})
}

// TestParseSchedule covers the named aliases (@hourly/@daily/@midnight/
// @weekly/@monthly), the "@every X" duration form (including its error
// path), plain cron fallthrough, and surrounding-whitespace tolerance.
func TestParseSchedule(t *testing.T) {
	t.Run("hourly alias", func(t *testing.T) {
		sched, err := parseSchedule("@hourly")
		require.NoError(t, err)
		cs, ok := sched.(cronSpec)
		require.True(t, ok)
		assert.True(t, cs.minute.has(0))
		assert.True(t, cs.dayStar)
	})

	t.Run("daily and midnight are equivalent", func(t *testing.T) {
		daily, err := parseSchedule("@daily")
		require.NoError(t, err)
		midnight, err := parseSchedule("@midnight")
		require.NoError(t, err)
		assert.Equal(t, daily, midnight)
	})

	t.Run("weekly alias", func(t *testing.T) {
		sched, err := parseSchedule("@weekly")
		require.NoError(t, err)
		cs := sched.(cronSpec)
		assert.True(t, cs.weekday.has(0))
		assert.False(t, cs.weekdayStar)
	})

	t.Run("monthly alias", func(t *testing.T) {
		sched, err := parseSchedule("@monthly")
		require.NoError(t, err)
		cs := sched.(cronSpec)
		assert.True(t, cs.day.has(1))
	})

	t.Run("every valid duration", func(t *testing.T) {
		sched, err := parseSchedule("@every 15m")
		require.NoError(t, err)
		es, ok := sched.(everySpec)
		require.True(t, ok)
		assert.Equal(t, 15*time.Minute, es.interval)
	})

	t.Run("every invalid duration", func(t *testing.T) {
		_, err := parseSchedule("@every notaduration")
		assert.ErrorContains(t, err, "invalid @every duration")
	})

	t.Run("plain cron falls through", func(t *testing.T) {
		sched, err := parseSchedule("*/5 * * * *")
		require.NoError(t, err)
		_, ok := sched.(cronSpec)
		assert.True(t, ok)
	})

	t.Run("surrounding whitespace trimmed", func(t *testing.T) {
		_, err := parseSchedule("  @hourly  ")
		assert.NoError(t, err)
	})

	t.Run("unknown at-alias falls through to cron parser and errors", func(t *testing.T) {
		_, err := parseSchedule("@unknown")
		assert.Error(t, err)
	})

	t.Run("empty expression errors", func(t *testing.T) {
		_, err := parseSchedule("")
		assert.Error(t, err)
	})
}

// TestEverySpecNext confirms @every is a fixed offset from "from", not
// clock-aligned.
func TestEverySpecNext(t *testing.T) {
	from := time.Date(2026, 7, 20, 10, 17, 33, 0, time.UTC)
	e := everySpec{interval: 15 * time.Minute}
	assert.Equal(t, from.Add(15*time.Minute), e.next(from))

	zero := everySpec{interval: 0}
	assert.Equal(t, from, zero.next(from))
}

// TestCronSpecMatches drives the POSIX "day-of-month OR day-of-week when
// both are restricted" rule directly, since it is easy to get backwards.
func TestCronSpecMatches(t *testing.T) {
	// "0 0 1 * 1" -> midnight, either the 1st of the month OR a Monday.
	spec, err := parseCron("0 0 1 * 1")
	require.NoError(t, err)

	// 2026-07-01 is a Wednesday: matches via day-of-month.
	assert.True(t, spec.matches(time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)))
	// 2026-07-06 is a Monday: matches via day-of-week.
	assert.True(t, spec.matches(time.Date(2026, 7, 6, 0, 0, 0, 0, time.UTC)))
	// 2026-07-07 is a Tuesday, and not the 1st: matches neither.
	assert.False(t, spec.matches(time.Date(2026, 7, 7, 0, 0, 0, 0, time.UTC)))

	// Both "*": always matches on minute/hour/month regardless of date.
	starBoth, err := parseCron("0 0 * * *")
	require.NoError(t, err)
	assert.True(t, starBoth.matches(time.Date(2026, 7, 7, 0, 0, 0, 0, time.UTC)))

	// dayStar only: weekday restriction applies (AND with minute/hour/month).
	weekdayOnly, err := parseCron("0 0 * * 1")
	require.NoError(t, err)
	assert.True(t, weekdayOnly.matches(time.Date(2026, 7, 6, 0, 0, 0, 0, time.UTC)))
	assert.False(t, weekdayOnly.matches(time.Date(2026, 7, 7, 0, 0, 0, 0, time.UTC)))

	// weekdayStar only: day-of-month restriction applies.
	dayOnly, err := parseCron("0 0 15 * *")
	require.NoError(t, err)
	assert.True(t, dayOnly.matches(time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)))
	assert.False(t, dayOnly.matches(time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)))

	// The month field is "*", so the 1st of any month still matches via
	// day-of-month regardless of month (2026-08-01 is a Saturday, so this
	// only matches through the day-of-month branch, not weekday).
	assert.True(t, spec.matches(time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)))
	// Hour/minute mismatches fail regardless of day/weekday.
	assert.False(t, spec.matches(time.Date(2026, 7, 1, 1, 0, 0, 0, time.UTC)))
	assert.False(t, spec.matches(time.Date(2026, 7, 1, 0, 1, 0, 0, time.UTC)))
}

// TestCronSpecNext exercises next-run computation: strictly-after
// semantics, truncation to whole minutes, and multi-field alignment.
func TestCronSpecNext(t *testing.T) {
	t.Run("next minute wildcard", func(t *testing.T) {
		spec, err := parseCron("* * * * *")
		require.NoError(t, err)
		from := time.Date(2026, 7, 20, 10, 17, 33, 500, time.UTC)
		got := spec.next(from)
		want := time.Date(2026, 7, 20, 10, 18, 0, 0, time.UTC)
		assert.Equal(t, want, got)
	})

	t.Run("next is strictly after from, even on exact match", func(t *testing.T) {
		spec, err := parseCron("0 * * * *")
		require.NoError(t, err)
		from := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)
		got := spec.next(from)
		want := time.Date(2026, 7, 20, 11, 0, 0, 0, time.UTC)
		assert.Equal(t, want, got)
	})

	t.Run("daily rolls to next day when hour has passed", func(t *testing.T) {
		spec, err := parseCron("0 0 * * *")
		require.NoError(t, err)
		from := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)
		got := spec.next(from)
		want := time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC)
		assert.Equal(t, want, got)
	})

	t.Run("monthly rolls across month/year boundary", func(t *testing.T) {
		spec, err := parseCron("0 0 1 * *")
		require.NoError(t, err)
		from := time.Date(2026, 12, 15, 0, 0, 0, 0, time.UTC)
		got := spec.next(from)
		want := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
		assert.Equal(t, want, got)
	})

	t.Run("specific weekday and hour", func(t *testing.T) {
		// Every Sunday at 03:00.
		spec, err := parseCron("0 3 * * 0")
		require.NoError(t, err)
		// 2026-07-20 is a Monday.
		from := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
		got := spec.next(from)
		// Next Sunday is 2026-07-26.
		want := time.Date(2026, 7, 26, 3, 0, 0, 0, time.UTC)
		assert.Equal(t, want, got)
		assert.Equal(t, time.Sunday, got.Weekday())
	})

	t.Run("unsatisfiable expression falls back rather than hanging", func(t *testing.T) {
		// Feb 30 never exists; next() must not loop forever.
		spec, err := parseCron("0 0 30 2 *")
		require.NoError(t, err)
		from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		got := spec.next(from)
		assert.Equal(t, from.Add(24*time.Hour), got)
	})
}
