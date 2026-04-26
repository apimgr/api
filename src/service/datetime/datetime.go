package datetime

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Now returns current time information
func Now(timezone string) (map[string]interface{}, error) {
	loc := time.UTC
	if timezone != "" {
		var err error
		loc, err = time.LoadLocation(timezone)
		if err != nil {
			return nil, fmt.Errorf("invalid timezone: %s", timezone)
		}
	}

	now := time.Now().In(loc)
	_, offset := now.Zone()

	return map[string]interface{}{
		"unix":              now.Unix(),
		"unix_ms":           now.UnixMilli(),
		"unix_ns":           now.UnixNano(),
		"iso8601":           now.Format(time.RFC3339),
		"rfc2822":           now.Format(time.RFC1123Z),
		"rfc3339":           now.Format(time.RFC3339),
		"rfc3339_nano":      now.Format(time.RFC3339Nano),
		"human":             now.Format("Monday, January 2, 2006 at 3:04:05 PM MST"),
		"date":              now.Format("2006-01-02"),
		"time":              now.Format("15:04:05"),
		"timezone":          loc.String(),
		"offset":            formatOffset(offset),
		"offset_seconds":    offset,
		"day_of_week":       int(now.Weekday()),
		"day_of_week_name":  now.Weekday().String(),
		"day_of_year":       now.YearDay(),
		"week_number":       getWeekNumber(now),
		"quarter":           (int(now.Month())-1)/3 + 1,
		"is_leap_year":      isLeapYear(now.Year()),
		"days_in_month":     daysInMonth(now.Year(), int(now.Month())),
	}, nil
}

// FromUnix converts unix timestamp to various formats
func FromUnix(timestamp int64, timezone string) (map[string]interface{}, error) {
	loc := time.UTC
	if timezone != "" {
		var err error
		loc, err = time.LoadLocation(timezone)
		if err != nil {
			return nil, fmt.Errorf("invalid timezone: %s", timezone)
		}
	}

	// Detect timestamp precision
	var t time.Time
	switch {
	case timestamp > 1e18: // nanoseconds
		t = time.Unix(0, timestamp).In(loc)
	case timestamp > 1e15: // microseconds
		t = time.UnixMicro(timestamp).In(loc)
	case timestamp > 1e12: // milliseconds
		t = time.UnixMilli(timestamp).In(loc)
	default: // seconds
		t = time.Unix(timestamp, 0).In(loc)
	}

	_, offset := t.Zone()

	return map[string]interface{}{
		"unix":           t.Unix(),
		"unix_ms":        t.UnixMilli(),
		"iso8601":        t.Format(time.RFC3339),
		"rfc2822":        t.Format(time.RFC1123Z),
		"human":          t.Format("Monday, January 2, 2006 at 3:04:05 PM MST"),
		"date":           t.Format("2006-01-02"),
		"time":           t.Format("15:04:05"),
		"timezone":       loc.String(),
		"offset":         formatOffset(offset),
		"day_of_week":    t.Weekday().String(),
		"day_of_year":    t.YearDay(),
		"is_leap_year":   isLeapYear(t.Year()),
	}, nil
}

// ToUnix converts various date formats to unix timestamp
func ToUnix(datetime string) (int64, error) {
	// Try common formats
	formats := []string{
		time.RFC3339,
		time.RFC3339Nano,
		time.RFC1123Z,
		time.RFC1123,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
		"01/02/2006",
		"January 2, 2006",
	}

	for _, format := range formats {
		if t, err := time.Parse(format, datetime); err == nil {
			return t.Unix(), nil
		}
	}

	return 0, fmt.Errorf("unable to parse datetime: %s", datetime)
}

// AddDuration adds a duration to a timestamp
func AddDuration(timestamp int64, duration string) (map[string]interface{}, error) {
	t := time.Unix(timestamp, 0)

	d, err := parseDuration(duration)
	if err != nil {
		return nil, err
	}

	result := t.Add(d)

	return map[string]interface{}{
		"original":     t.Format(time.RFC3339),
		"duration":     duration,
		"result":       result.Format(time.RFC3339),
		"result_unix":  result.Unix(),
	}, nil
}

// Diff calculates the difference between two timestamps
func Diff(timestamp1, timestamp2 int64) map[string]interface{} {
	t1 := time.Unix(timestamp1, 0)
	t2 := time.Unix(timestamp2, 0)

	diff := t2.Sub(t1)

	days := int(diff.Hours() / 24)
	hours := int(diff.Hours()) % 24
	minutes := int(diff.Minutes()) % 60
	seconds := int(diff.Seconds()) % 60

	return map[string]interface{}{
		"from":         t1.Format(time.RFC3339),
		"to":           t2.Format(time.RFC3339),
		"days":         days,
		"hours":        int(diff.Hours()),
		"minutes":      int(diff.Minutes()),
		"seconds":      int(diff.Seconds()),
		"milliseconds": diff.Milliseconds(),
		"human":        formatDurationHuman(days, hours, minutes, seconds),
	}
}

// Timezones returns list of timezones
func Timezones() []map[string]interface{} {
	zones := []string{
		"UTC",
		"America/New_York",
		"America/Chicago",
		"America/Denver",
		"America/Los_Angeles",
		"America/Toronto",
		"America/Vancouver",
		"America/Mexico_City",
		"America/Sao_Paulo",
		"Europe/London",
		"Europe/Paris",
		"Europe/Berlin",
		"Europe/Moscow",
		"Asia/Dubai",
		"Asia/Kolkata",
		"Asia/Singapore",
		"Asia/Shanghai",
		"Asia/Tokyo",
		"Asia/Seoul",
		"Australia/Sydney",
		"Australia/Melbourne",
		"Pacific/Auckland",
		"Pacific/Honolulu",
	}

	result := make([]map[string]interface{}, 0, len(zones))
	now := time.Now()

	for _, zone := range zones {
		loc, err := time.LoadLocation(zone)
		if err != nil {
			continue
		}

		t := now.In(loc)
		_, offset := t.Zone()

		result = append(result, map[string]interface{}{
			"name":         zone,
			"abbreviation": t.Format("MST"),
			"offset":       formatOffset(offset),
			"offset_hours": float64(offset) / 3600,
			"current_time": t.Format("15:04:05"),
		})
	}

	return result
}

// TimezoneInfo returns info about a specific timezone
func TimezoneInfo(timezone string) (map[string]interface{}, error) {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, fmt.Errorf("invalid timezone: %s", timezone)
	}

	now := time.Now().In(loc)
	_, offset := now.Zone()

	return map[string]interface{}{
		"name":           loc.String(),
		"abbreviation":   now.Format("MST"),
		"offset":         formatOffset(offset),
		"offset_seconds": offset,
		"current_time":   now.Format(time.RFC3339),
		"date":           now.Format("2006-01-02"),
		"time":           now.Format("15:04:05"),
	}, nil
}

// ConvertTimezone converts time between timezones
func ConvertTimezone(timestamp int64, from, to string) (map[string]interface{}, error) {
	fromLoc, err := time.LoadLocation(from)
	if err != nil {
		return nil, fmt.Errorf("invalid source timezone: %s", from)
	}

	toLoc, err := time.LoadLocation(to)
	if err != nil {
		return nil, fmt.Errorf("invalid target timezone: %s", to)
	}

	t := time.Unix(timestamp, 0)
	fromTime := t.In(fromLoc)
	toTime := t.In(toLoc)

	return map[string]interface{}{
		"unix":      timestamp,
		"from":      fromTime.Format(time.RFC3339),
		"from_zone": from,
		"to":        toTime.Format(time.RFC3339),
		"to_zone":   to,
	}, nil
}

// Helper functions

func formatOffset(seconds int) string {
	hours := seconds / 3600
	minutes := (seconds % 3600) / 60
	if minutes < 0 {
		minutes = -minutes
	}
	sign := "+"
	if hours < 0 {
		sign = "-"
		hours = -hours
	}
	return fmt.Sprintf("%s%02d:%02d", sign, hours, minutes)
}

func getWeekNumber(t time.Time) int {
	_, week := t.ISOWeek()
	return week
}

func isLeapYear(year int) bool {
	return year%4 == 0 && (year%100 != 0 || year%400 == 0)
}

func daysInMonth(year, month int) int {
	return time.Date(year, time.Month(month+1), 0, 0, 0, 0, 0, time.UTC).Day()
}

func parseDuration(s string) (time.Duration, error) {
	// Handle common formats
	s = strings.ToLower(strings.TrimSpace(s))

	// Try standard Go duration
	if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}

	// Handle custom formats like "1d", "2w", "1M", "1y"
	var value int
	var unit string

	for i, c := range s {
		if c >= '0' && c <= '9' {
			continue
		}
		value, _ = strconv.Atoi(s[:i])
		unit = s[i:]
		break
	}

	switch unit {
	case "d", "day", "days":
		return time.Duration(value) * 24 * time.Hour, nil
	case "w", "week", "weeks":
		return time.Duration(value) * 7 * 24 * time.Hour, nil
	case "m", "month", "months":
		return time.Duration(value) * 30 * 24 * time.Hour, nil
	case "y", "year", "years":
		return time.Duration(value) * 365 * 24 * time.Hour, nil
	}

	return 0, fmt.Errorf("unable to parse duration: %s", s)
}

func formatDurationHuman(days, hours, minutes, seconds int) string {
	parts := []string{}

	if days > 0 {
		parts = append(parts, fmt.Sprintf("%d day(s)", days))
	}
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%d hour(s)", hours))
	}
	if minutes > 0 {
		parts = append(parts, fmt.Sprintf("%d minute(s)", minutes))
	}
	if seconds > 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%d second(s)", seconds))
	}

	return strings.Join(parts, ", ")
}
