package job

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type cronSchedule struct {
	minutes     map[int]struct{}
	hours       map[int]struct{}
	monthDays   map[int]struct{}
	months      map[int]struct{}
	weekdays    map[int]struct{}
	anyMonthDay bool
	anyWeekday  bool
}

func ValidateCronExpression(expression string) error {
	_, err := parseCronSchedule(expression)
	return err
}

func parseCronSchedule(expression string) (*cronSchedule, error) {
	fields := strings.Fields(strings.TrimSpace(expression))
	if len(fields) != 5 {
		return nil, fmt.Errorf("cron expression must use 5 fields")
	}
	minutes, _, err := parseCronField(fields[0], 0, 59)
	if err != nil {
		return nil, fmt.Errorf("minute field: %w", err)
	}
	hours, _, err := parseCronField(fields[1], 0, 23)
	if err != nil {
		return nil, fmt.Errorf("hour field: %w", err)
	}
	monthDays, anyMonthDay, err := parseCronField(fields[2], 1, 31)
	if err != nil {
		return nil, fmt.Errorf("day-of-month field: %w", err)
	}
	months, _, err := parseCronField(fields[3], 1, 12)
	if err != nil {
		return nil, fmt.Errorf("month field: %w", err)
	}
	weekdays, anyWeekday, err := parseCronField(fields[4], 0, 7)
	if err != nil {
		return nil, fmt.Errorf("day-of-week field: %w", err)
	}
	if _, ok := weekdays[7]; ok {
		delete(weekdays, 7)
		weekdays[0] = struct{}{}
	}
	return &cronSchedule{
		minutes:     minutes,
		hours:       hours,
		monthDays:   monthDays,
		months:      months,
		weekdays:    weekdays,
		anyMonthDay: anyMonthDay,
		anyWeekday:  anyWeekday,
	}, nil
}

func parseCronField(field string, minValue, maxValue int) (map[int]struct{}, bool, error) {
	values := make(map[int]struct{})
	any := false
	for _, part := range strings.Split(field, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, false, fmt.Errorf("empty list item")
		}
		step := 1
		base := part
		if strings.Contains(part, "/") {
			pieces := strings.Split(part, "/")
			if len(pieces) != 2 {
				return nil, false, fmt.Errorf("invalid step %q", part)
			}
			base = pieces[0]
			parsedStep, err := strconv.Atoi(pieces[1])
			if err != nil || parsedStep <= 0 {
				return nil, false, fmt.Errorf("invalid step %q", pieces[1])
			}
			step = parsedStep
		}

		start := minValue
		end := maxValue
		if base == "*" {
			any = true
		} else if strings.Contains(base, "-") {
			pieces := strings.Split(base, "-")
			if len(pieces) != 2 {
				return nil, false, fmt.Errorf("invalid range %q", base)
			}
			var err error
			start, err = strconv.Atoi(pieces[0])
			if err != nil {
				return nil, false, fmt.Errorf("invalid range start %q", pieces[0])
			}
			end, err = strconv.Atoi(pieces[1])
			if err != nil {
				return nil, false, fmt.Errorf("invalid range end %q", pieces[1])
			}
		} else {
			value, err := strconv.Atoi(base)
			if err != nil {
				return nil, false, fmt.Errorf("invalid value %q", base)
			}
			start = value
			end = value
		}

		if start < minValue || end > maxValue || start > end {
			return nil, false, fmt.Errorf("value out of range %d-%d", minValue, maxValue)
		}
		for value := start; value <= end; value += step {
			values[value] = struct{}{}
		}
	}
	return values, any, nil
}

func (s *cronSchedule) matches(t time.Time) bool {
	if _, ok := s.minutes[t.Minute()]; !ok {
		return false
	}
	if _, ok := s.hours[t.Hour()]; !ok {
		return false
	}
	if _, ok := s.months[int(t.Month())]; !ok {
		return false
	}
	_, monthDayMatches := s.monthDays[t.Day()]
	_, weekdayMatches := s.weekdays[int(t.Weekday())]
	if !s.anyMonthDay && !s.anyWeekday {
		return monthDayMatches || weekdayMatches
	}
	return monthDayMatches && weekdayMatches
}

func scheduledMinute(t time.Time) time.Time {
	return t.Truncate(time.Minute)
}
