package scheduler

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// schedule computes the next run time for a parsed schedule expression.
// Two kinds exist: cronSpec (clock-aligned 5-field cron) and everySpec
// (fixed interval relative to the last run, used by "@every X").
type schedule interface {
	// next returns the first matching time strictly after from, truncated
	// to whole minutes for cron schedules.
	next(from time.Time) time.Time
}

// everySpec implements the "@every X" schedule - a fixed duration relative
// to the previous run, not aligned to the clock.
type everySpec struct {
	interval time.Duration
}

func (e everySpec) next(from time.Time) time.Time {
	return from.Add(e.interval)
}

// cronSpec implements a standard 5-field cron expression
// (minute hour day-of-month month day-of-week).
type cronSpec struct {
	minute  fieldSet
	hour    fieldSet
	day     fieldSet
	month   fieldSet
	weekday fieldSet
	// dayStar / weekdayStar track whether the original field was "*", per
	// POSIX cron rules: when BOTH day-of-month and day-of-week are
	// restricted, a date matches if EITHER matches (OR), not AND.
	dayStar     bool
	weekdayStar bool
}

// fieldSet is the set of accepted values for one cron field.
type fieldSet map[int]bool

func (f fieldSet) has(v int) bool {
	return f[v]
}

// maxSearchMinutes bounds the next-run search so a schedule that can never
// match (e.g. Feb 30) does not loop forever.
const maxSearchMinutes = 4 * 366 * 24 * 60

func (c cronSpec) next(from time.Time) time.Time {
	t := from.Truncate(time.Minute).Add(time.Minute)

	for i := 0; i < maxSearchMinutes; i++ {
		if c.matches(t) {
			return t
		}
		t = t.Add(time.Minute)
	}

	// Unreachable for any valid cron expression - fall back to daily.
	return from.Add(24 * time.Hour)
}

func (c cronSpec) matches(t time.Time) bool {
	if !c.minute.has(t.Minute()) || !c.hour.has(t.Hour()) || !c.month.has(int(t.Month())) {
		return false
	}

	dayMatch := c.day.has(t.Day())
	weekdayMatch := c.weekday.has(int(t.Weekday()))

	switch {
	case c.dayStar && c.weekdayStar:
		return true
	case c.dayStar:
		return weekdayMatch
	case c.weekdayStar:
		return dayMatch
	default:
		return dayMatch || weekdayMatch
	}
}

// parseSchedule parses a schedule expression per AI.md PART 18 "Schedule
// Format": standard 5-field cron, @hourly, @daily, @weekly, @monthly, and
// @every X. No external cron library is used, per PART 18 Implementation
// Requirements ("Use Go's time/ticker - No external cron libraries
// required").
func parseSchedule(expr string) (schedule, error) {
	expr = strings.TrimSpace(expr)

	switch expr {
	case "@hourly":
		return parseCron("0 * * * *")
	case "@daily", "@midnight":
		return parseCron("0 0 * * *")
	case "@weekly":
		return parseCron("0 0 * * 0")
	case "@monthly":
		return parseCron("0 0 1 * *")
	}

	if strings.HasPrefix(expr, "@every ") {
		d, err := time.ParseDuration(strings.TrimPrefix(expr, "@every "))
		if err != nil {
			return nil, fmt.Errorf("invalid @every duration %q: %w", expr, err)
		}
		return everySpec{interval: d}, nil
	}

	return parseCron(expr)
}

// parseCron parses a standard 5-field cron expression.
func parseCron(expr string) (cronSpec, error) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return cronSpec{}, fmt.Errorf("cron expression %q must have 5 fields (minute hour day month weekday)", expr)
	}

	minute, err := parseField(fields[0], 0, 59)
	if err != nil {
		return cronSpec{}, fmt.Errorf("minute field: %w", err)
	}
	hour, err := parseField(fields[1], 0, 23)
	if err != nil {
		return cronSpec{}, fmt.Errorf("hour field: %w", err)
	}
	day, err := parseField(fields[2], 1, 31)
	if err != nil {
		return cronSpec{}, fmt.Errorf("day field: %w", err)
	}
	month, err := parseField(fields[3], 1, 12)
	if err != nil {
		return cronSpec{}, fmt.Errorf("month field: %w", err)
	}
	weekday, err := parseField(fields[4], 0, 7)
	if err != nil {
		return cronSpec{}, fmt.Errorf("weekday field: %w", err)
	}
	// Cron allows both 0 and 7 for Sunday - normalize 7 to 0.
	if weekday.has(7) {
		weekday[0] = true
		delete(weekday, 7)
	}

	return cronSpec{
		minute:      minute,
		hour:        hour,
		day:         day,
		month:       month,
		weekday:     weekday,
		dayStar:     fields[2] == "*",
		weekdayStar: fields[4] == "*",
	}, nil
}

// parseField parses one cron field: "*", "*/n", "a", "a-b", "a-b/n", or a
// comma-separated list of any of the above.
func parseField(field string, min, max int) (fieldSet, error) {
	set := fieldSet{}

	for _, part := range strings.Split(field, ",") {
		if err := parseFieldPart(part, min, max, set); err != nil {
			return nil, err
		}
	}

	if len(set) == 0 {
		return nil, fmt.Errorf("empty field %q", field)
	}

	return set, nil
}

func parseFieldPart(part string, min, max int, set fieldSet) error {
	step := 1

	rangePart := part
	if idx := strings.Index(part, "/"); idx != -1 {
		rangePart = part[:idx]
		n, err := strconv.Atoi(part[idx+1:])
		if err != nil || n <= 0 {
			return fmt.Errorf("invalid step in %q", part)
		}
		step = n
	}

	start, end := min, max
	switch {
	case rangePart == "*":
		// full range, already set above
	case strings.Contains(rangePart, "-"):
		bounds := strings.SplitN(rangePart, "-", 2)
		s, err := strconv.Atoi(bounds[0])
		if err != nil {
			return fmt.Errorf("invalid range start in %q", part)
		}
		e, err := strconv.Atoi(bounds[1])
		if err != nil {
			return fmt.Errorf("invalid range end in %q", part)
		}
		start, end = s, e
	default:
		v, err := strconv.Atoi(rangePart)
		if err != nil {
			return fmt.Errorf("invalid value %q", part)
		}
		start, end = v, v
	}

	if start < min || end > max || start > end {
		return fmt.Errorf("value %q out of range [%d-%d]", part, min, max)
	}

	for v := start; v <= end; v += step {
		set[v] = true
	}

	return nil
}
