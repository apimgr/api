package datetime

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// namedFormats maps human-friendly format names to Go reference-time
// layouts. FormatDatetime falls back to treating the format string
// itself as a literal Go layout when it does not match a named entry.
var namedFormats = map[string]string{
	"iso8601":  time.RFC3339,
	"rfc3339":  time.RFC3339,
	"rfc1123":  time.RFC1123,
	"rfc822":   time.RFC822,
	"kitchen":  time.Kitchen,
	"date":     "2006-01-02",
	"time":     "15:04:05",
	"datetime": "2006-01-02 15:04:05",
}

// FormatDatetime formats a unix timestamp using either a named format
// (iso8601, rfc3339, rfc1123, rfc822, kitchen, date, time, datetime) or a
// literal Go reference-time layout string.
func FormatDatetime(timestamp int64, format string) (map[string]interface{}, error) {
	if format == "" {
		return nil, fmt.Errorf("format is required")
	}

	t := time.Unix(timestamp, 0).UTC()

	layout, ok := namedFormats[strings.ToLower(format)]
	if !ok {
		layout = format
	}

	return map[string]interface{}{
		"unix":   timestamp,
		"format": format,
		"result": t.Format(layout),
	}, nil
}

// ParseDateString parses a free-form date/time string against a list of
// common layouts and returns a richer breakdown than ToUnix's bare
// timestamp (matched layout, date components, weekday).
func ParseDateString(value string) (map[string]interface{}, error) {
	formats := []string{
		time.RFC3339,
		time.RFC3339Nano,
		time.RFC1123Z,
		time.RFC1123,
		time.RFC822,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
		"01/02/2006",
		"January 2, 2006",
		"Jan 2, 2006",
		"15:04:05",
	}

	for _, layout := range formats {
		if t, err := time.Parse(layout, value); err == nil {
			return map[string]interface{}{
				"input":          value,
				"matched_layout": layout,
				"unix":           t.Unix(),
				"iso8601":        t.Format(time.RFC3339),
				"date":           t.Format("2006-01-02"),
				"time":           t.Format("15:04:05"),
				"year":           t.Year(),
				"month":          int(t.Month()),
				"day":            t.Day(),
				"day_of_week":    t.Weekday().String(),
			}, nil
		}
	}

	return nil, fmt.Errorf("unable to parse date string: %s", value)
}

// GenerateCalendar builds a week-grid calendar for the given year/month
// (weeks start on Sunday), padding leading/trailing days with 0.
func GenerateCalendar(year, month int) (map[string]interface{}, error) {
	if month < 1 || month > 12 {
		return nil, fmt.Errorf("month must be between 1 and 12")
	}

	first := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	days := daysInMonth(year, month)
	startWeekday := int(first.Weekday())

	var weeks [][]int
	week := make([]int, startWeekday)
	for d := 1; d <= days; d++ {
		week = append(week, d)
		if len(week) == 7 {
			weeks = append(weeks, week)
			week = []int{}
		}
	}
	if len(week) > 0 {
		for len(week) < 7 {
			week = append(week, 0)
		}
		weeks = append(weeks, week)
	}

	return map[string]interface{}{
		"year":          year,
		"month":         month,
		"month_name":    first.Month().String(),
		"days_in_month": days,
		"starts_on":     first.Weekday().String(),
		"weeks":         weeks,
		"is_leap_year":  isLeapYear(year),
	}, nil
}

// WorkdaysBetween counts weekdays (Mon-Fri) between two dates inclusive
// of both endpoints, skipping Saturdays and Sundays. No holiday calendar
// is applied -- only weekends are excluded.
func WorkdaysBetween(startStr, endStr string) (map[string]interface{}, error) {
	start, err := time.Parse("2006-01-02", startStr)
	if err != nil {
		return nil, fmt.Errorf("start must be YYYY-MM-DD: %w", err)
	}
	end, err := time.Parse("2006-01-02", endStr)
	if err != nil {
		return nil, fmt.Errorf("end must be YYYY-MM-DD: %w", err)
	}
	if end.Before(start) {
		start, end = end, start
	}

	workdays := 0
	totalDays := 0
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		totalDays++
		if wd := d.Weekday(); wd != time.Saturday && wd != time.Sunday {
			workdays++
		}
	}

	return map[string]interface{}{
		"start":        start.Format("2006-01-02"),
		"end":          end.Format("2006-01-02"),
		"total_days":   totalDays,
		"workdays":     workdays,
		"weekend_days": totalDays - workdays,
	}, nil
}

// SunriseSunset computes sunrise/sunset UTC times for a given latitude,
// longitude, and date using the standard Sunrise/Sunset Algorithm from
// the Almanac for Computers, 1990 (U.S. Naval Observatory) -- the same
// widely-implemented formula used by most open-source sunrise
// calculators.
func SunriseSunset(lat, lon float64, dateStr string) (map[string]interface{}, error) {
	if lat < -90 || lat > 90 {
		return nil, fmt.Errorf("latitude must be between -90 and 90")
	}
	if lon < -180 || lon > 180 {
		return nil, fmt.Errorf("longitude must be between -180 and 180")
	}

	var date time.Time
	var err error
	if dateStr == "" {
		date = time.Now().UTC()
	} else {
		date, err = time.Parse("2006-01-02", dateStr)
		if err != nil {
			return nil, fmt.Errorf("date must be YYYY-MM-DD: %w", err)
		}
	}

	n := date.YearDay()
	rise, riseOk := sunEvent(n, lat, lon, true)
	set, setOk := sunEvent(n, lat, lon, false)

	result := map[string]interface{}{
		"date":      date.Format("2006-01-02"),
		"latitude":  lat,
		"longitude": lon,
	}

	if !riseOk || !setOk {
		result["polar_event"] = true
		result["description"] = "sun does not rise or set on this date at this latitude"
		return result, nil
	}

	riseTime := date.Add(time.Duration(rise * float64(time.Hour))).Truncate(time.Second)
	setTime := date.Add(time.Duration(set * float64(time.Hour))).Truncate(time.Second)

	result["sunrise_utc"] = riseTime.Format("15:04:05")
	result["sunset_utc"] = setTime.Format("15:04:05")
	result["sunrise_iso8601"] = riseTime.Format(time.RFC3339)
	result["sunset_iso8601"] = setTime.Format(time.RFC3339)

	return result, nil
}

// sunEvent implements the core Almanac-for-Computers sunrise/sunset
// formula for a given day-of-year, latitude, and longitude. rising
// selects the sunrise (true) or sunset (false) branch. ok is false if
// the sun never rises/sets that day at that latitude (polar day/night).
func sunEvent(dayOfYear int, lat, lon float64, rising bool) (hoursUTC float64, ok bool) {
	const zenith = 90.833

	lngHour := lon / 15.0
	var t float64
	if rising {
		t = float64(dayOfYear) + ((6 - lngHour) / 24)
	} else {
		t = float64(dayOfYear) + ((18 - lngHour) / 24)
	}

	m := (0.9856 * t) - 3.289

	l := m + (1.916 * sinDeg(m)) + (0.020 * sinDeg(2*m)) + 282.634
	l = normalizeDegrees(l)

	ra := atanDeg(0.91764 * tanDeg(l))
	ra = normalizeDegrees(ra)

	lQuadrant := math.Floor(l/90) * 90
	raQuadrant := math.Floor(ra/90) * 90
	ra += lQuadrant - raQuadrant
	ra /= 15

	sinDec := 0.39782 * sinDeg(l)
	cosDec := cosDeg(asinDeg(sinDec))

	cosH := (cosDeg(zenith) - (sinDec * sinDeg(lat))) / (cosDec * cosDeg(lat))
	if cosH > 1 || cosH < -1 {
		return 0, false
	}

	var h float64
	if rising {
		h = 360 - acosDeg(cosH)
	} else {
		h = acosDeg(cosH)
	}
	h /= 15

	tLocal := h + ra - (0.06571 * t) - 6.622

	ut := tLocal - lngHour
	for ut < 0 {
		ut += 24
	}
	for ut >= 24 {
		ut -= 24
	}

	return ut, true
}

func sinDeg(deg float64) float64 { return math.Sin(deg * math.Pi / 180) }
func cosDeg(deg float64) float64 { return math.Cos(deg * math.Pi / 180) }
func tanDeg(deg float64) float64 { return math.Tan(deg * math.Pi / 180) }
func asinDeg(x float64) float64  { return math.Asin(x) * 180 / math.Pi }
func acosDeg(x float64) float64  { return math.Acos(x) * 180 / math.Pi }
func atanDeg(x float64) float64  { return math.Atan(x) * 180 / math.Pi }

func normalizeDegrees(deg float64) float64 {
	for deg < 0 {
		deg += 360
	}
	for deg >= 360 {
		deg -= 360
	}
	return deg
}

// MoonPhase computes the current lunar phase using the synodic-month
// method: days elapsed since a known reference new moon (2000-01-06
// 18:14 UTC) modulo the mean synodic month length (29.53058867 days).
// This is the standard textbook lunar-phase approximation used by most
// open-source moon-phase calculators (accurate to roughly half a day).
func MoonPhase(dateStr string) (map[string]interface{}, error) {
	var date time.Time
	var err error
	if dateStr == "" {
		date = time.Now().UTC()
	} else {
		date, err = time.Parse("2006-01-02", dateStr)
		if err != nil {
			return nil, fmt.Errorf("date must be YYYY-MM-DD: %w", err)
		}
	}

	const synodicMonth = 29.53058867
	referenceNewMoon := time.Date(2000, 1, 6, 18, 14, 0, 0, time.UTC)

	daysSince := date.Sub(referenceNewMoon).Hours() / 24
	age := math.Mod(daysSince, synodicMonth)
	if age < 0 {
		age += synodicMonth
	}

	fraction := age / synodicMonth
	illumination := (1 - math.Cos(2*math.Pi*fraction)) / 2

	var phaseName string
	switch {
	case age < 1.84566:
		phaseName = "New Moon"
	case age < 5.53699:
		phaseName = "Waxing Crescent"
	case age < 9.22831:
		phaseName = "First Quarter"
	case age < 12.91963:
		phaseName = "Waxing Gibbous"
	case age < 16.61096:
		phaseName = "Full Moon"
	case age < 20.30228:
		phaseName = "Waning Gibbous"
	case age < 23.99361:
		phaseName = "Last Quarter"
	case age < 27.68493:
		phaseName = "Waning Crescent"
	default:
		phaseName = "New Moon"
	}

	return map[string]interface{}{
		"date":               date.Format("2006-01-02"),
		"age_days":           math.Round(age*100) / 100,
		"phase":              phaseName,
		"illumination":       math.Round(illumination*10000) / 10000,
		"illumination_pct":   math.Round(illumination*10000) / 100,
		"synodic_month_days": synodicMonth,
	}, nil
}

// ParseCron parses a standard 5-field cron expression (minute hour
// day-of-month month day-of-week) and returns a human-readable
// breakdown of each field plus the next scheduled run times, computed
// by brute-force minute-stepping -- the same approach most widely-used
// cron-description libraries take. POSIX cron semantics: when both
// day-of-month and day-of-week are restricted (not "*"), a match on
// either field is sufficient.
func ParseCron(expr string) (map[string]interface{}, error) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return nil, fmt.Errorf("cron expression must have exactly 5 fields (minute hour day-of-month month day-of-week), got %d", len(fields))
	}

	minutes, err := parseCronField(fields[0], 0, 59)
	if err != nil {
		return nil, fmt.Errorf("minute field: %w", err)
	}
	hours, err := parseCronField(fields[1], 0, 23)
	if err != nil {
		return nil, fmt.Errorf("hour field: %w", err)
	}
	doms, err := parseCronField(fields[2], 1, 31)
	if err != nil {
		return nil, fmt.Errorf("day-of-month field: %w", err)
	}
	months, err := parseCronField(fields[3], 1, 12)
	if err != nil {
		return nil, fmt.Errorf("month field: %w", err)
	}
	dows, err := parseCronField(fields[4], 0, 6)
	if err != nil {
		return nil, fmt.Errorf("day-of-week field: %w", err)
	}

	domRestricted := fields[2] != "*"
	dowRestricted := fields[4] != "*"

	next := nextCronRuns(minutes, hours, doms, months, dows, domRestricted, dowRestricted, 5)

	nextStrs := make([]string, len(next))
	for i, t := range next {
		nextStrs[i] = t.Format(time.RFC3339)
	}

	return map[string]interface{}{
		"expression": expr,
		"fields": map[string]string{
			"minute":       fields[0],
			"hour":         fields[1],
			"day_of_month": fields[2],
			"month":        fields[3],
			"day_of_week":  fields[4],
		},
		"next_runs": nextStrs,
	}, nil
}

func parseCronField(field string, min, max int) (map[int]bool, error) {
	values := map[int]bool{}

	for _, part := range strings.Split(field, ",") {
		step := 1
		rangePart := part
		if idx := strings.Index(part, "/"); idx != -1 {
			rangePart = part[:idx]
			s, err := strconv.Atoi(part[idx+1:])
			if err != nil || s <= 0 {
				return nil, fmt.Errorf("invalid step in %q", part)
			}
			step = s
		}

		var lo, hi int
		switch {
		case rangePart == "*":
			lo, hi = min, max
		case strings.Contains(rangePart, "-"):
			bounds := strings.SplitN(rangePart, "-", 2)
			l, err1 := strconv.Atoi(bounds[0])
			h, err2 := strconv.Atoi(bounds[1])
			if err1 != nil || err2 != nil || l < min || h > max || l > h {
				return nil, fmt.Errorf("invalid range %q", rangePart)
			}
			lo, hi = l, h
		default:
			v, err := strconv.Atoi(rangePart)
			if err != nil || v < min || v > max {
				return nil, fmt.Errorf("invalid value %q", rangePart)
			}
			lo, hi = v, v
		}

		for v := lo; v <= hi; v += step {
			values[v] = true
		}
	}

	return values, nil
}

func nextCronRuns(minutes, hours, doms, months, dows map[int]bool, domRestricted, dowRestricted bool, count int) []time.Time {
	var results []time.Time
	t := time.Now().UTC().Truncate(time.Minute).Add(time.Minute)
	limit := t.AddDate(2, 0, 0)

	for t.Before(limit) && len(results) < count {
		if !months[int(t.Month())] {
			t = time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 1, 0)
			continue
		}

		domMatch := doms[t.Day()]
		dowMatch := dows[int(t.Weekday())]
		var dayMatches bool
		switch {
		case domRestricted && dowRestricted:
			dayMatches = domMatch || dowMatch
		case domRestricted:
			dayMatches = domMatch
		case dowRestricted:
			dayMatches = dowMatch
		default:
			dayMatches = true
		}

		if !dayMatches {
			t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, 1)
			continue
		}

		if !hours[t.Hour()] {
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, time.UTC).Add(time.Hour)
			continue
		}

		if !minutes[t.Minute()] {
			t = t.Add(time.Minute)
			continue
		}

		results = append(results, t)
		t = t.Add(time.Minute)
	}

	return results
}
