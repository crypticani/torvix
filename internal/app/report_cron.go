package app

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type reportCron struct {
	minute   int
	hour     int
	weekdays map[time.Weekday]bool
	location *time.Location
}

func parseReportCron(expr string, location *time.Location) (reportCron, error) {
	if location == nil {
		location = time.UTC
	}
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return reportCron{}, fmt.Errorf("expected 5 cron fields")
	}
	minute, err := parseExactCronInt(fields[0], 0, 59, "minute")
	if err != nil {
		return reportCron{}, err
	}
	hour, err := parseExactCronInt(fields[1], 0, 23, "hour")
	if err != nil {
		return reportCron{}, err
	}
	weekdays, err := parseCronWeekdays(fields[4])
	if err != nil {
		return reportCron{}, err
	}
	if fields[2] != "*" || fields[3] != "*" {
		return reportCron{}, fmt.Errorf("day-of-month and month fields must be *")
	}
	return reportCron{minute: minute, hour: hour, weekdays: weekdays, location: location}, nil
}

func (c reportCron) Next(after time.Time) time.Time {
	local := after.In(c.location)
	candidate := time.Date(local.Year(), local.Month(), local.Day(), c.hour, c.minute, 0, 0, c.location)
	for i := 0; i < 8; i++ {
		if candidate.After(local) && c.matchesWeekday(candidate.Weekday()) {
			return candidate
		}
		candidate = candidate.AddDate(0, 0, 1)
	}
	return candidate
}

func (c reportCron) DueAtOrBefore(now time.Time) bool {
	local := now.In(c.location)
	candidate := time.Date(local.Year(), local.Month(), local.Day(), c.hour, c.minute, 0, 0, c.location)
	return !candidate.After(local) && c.matchesWeekday(candidate.Weekday())
}

func (c reportCron) matchesWeekday(day time.Weekday) bool {
	if len(c.weekdays) == 0 {
		return true
	}
	return c.weekdays[day]
}

func parseExactCronInt(value string, minValue, maxValue int, name string) (int, error) {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an exact integer: %w", name, err)
	}
	if parsed < minValue || parsed > maxValue {
		return 0, fmt.Errorf("%s must be between %d and %d", name, minValue, maxValue)
	}
	return parsed, nil
}

func parseCronWeekdays(value string) (map[time.Weekday]bool, error) {
	if value == "*" {
		return nil, nil
	}
	out := make(map[time.Weekday]bool)
	for _, part := range strings.Split(value, ",") {
		parsed, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			return nil, fmt.Errorf("weekday must be * or integers 0-7: %w", err)
		}
		if parsed < 0 || parsed > 7 {
			return nil, fmt.Errorf("weekday must be between 0 and 7")
		}
		if parsed == 7 {
			parsed = 0
		}
		out[time.Weekday(parsed)] = true
	}
	return out, nil
}
