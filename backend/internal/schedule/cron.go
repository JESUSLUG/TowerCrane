// Package schedule parses the standard 5-field cron expressions Jobs use
// ("minute hour day-of-month month day-of-week") and computes their next
// times in a time zone.
//
// Fields accept "*", numbers, lists (1,15), ranges (1-5) and steps (*/15,
// 9-17/2). Day of week is 0-6 (Sunday = 0; 7 is also Sunday). As in classic
// cron, when both day of month and day of week are restricted, a day
// matching either one fires.
package schedule

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type field struct {
	min, max int
	any      bool
	allowed  map[int]bool
}

// Schedule is a parsed cron expression in a time zone.
type Schedule struct {
	Expr     string
	Location *time.Location
	minute   field
	hour     field
	dom      field
	month    field
	dow      field
}

// Parse reads a 5-field cron expression; tz is an IANA zone name ("" = UTC).
func Parse(expr, tz string) (*Schedule, error) {
	loc := time.UTC
	if tz != "" {
		l, err := time.LoadLocation(tz)
		if err != nil {
			return nil, fmt.Errorf("unknown time zone %q", tz)
		}
		loc = l
	}
	parts := strings.Fields(expr)
	if len(parts) != 5 {
		return nil, fmt.Errorf("cron %q must have 5 fields: minute hour day-of-month month day-of-week", expr)
	}
	s := &Schedule{Expr: expr, Location: loc}
	specs := []struct {
		dst      *field
		min, max int
		name     string
	}{
		{&s.minute, 0, 59, "minute"},
		{&s.hour, 0, 23, "hour"},
		{&s.dom, 1, 31, "day of month"},
		{&s.month, 1, 12, "month"},
		{&s.dow, 0, 7, "day of week"},
	}
	for i, sp := range specs {
		f, err := parseField(parts[i], sp.min, sp.max)
		if err != nil {
			return nil, fmt.Errorf("cron %q: %s: %w", expr, sp.name, err)
		}
		*sp.dst = f
	}
	if s.dow.allowed[7] {
		s.dow.allowed[0] = true // 7 is Sunday too
	}
	return s, nil
}

func parseField(text string, min, max int) (field, error) {
	f := field{min: min, max: max, allowed: map[int]bool{}}
	if text == "*" {
		f.any = true
	}
	for _, part := range strings.Split(text, ",") {
		rangePart, step := part, 1
		if i := strings.Index(part, "/"); i >= 0 {
			n, err := strconv.Atoi(part[i+1:])
			if err != nil || n <= 0 {
				return f, fmt.Errorf("invalid step in %q", part)
			}
			rangePart, step = part[:i], n
		}
		lo, hi := min, max
		switch {
		case rangePart == "*":
		case strings.Contains(rangePart, "-"):
			a, b, _ := strings.Cut(rangePart, "-")
			var err1, err2 error
			lo, err1 = strconv.Atoi(a)
			hi, err2 = strconv.Atoi(b)
			if err1 != nil || err2 != nil || lo > hi {
				return f, fmt.Errorf("invalid range %q", rangePart)
			}
		default:
			n, err := strconv.Atoi(rangePart)
			if err != nil {
				return f, fmt.Errorf("invalid value %q", rangePart)
			}
			lo, hi = n, n
			if step > 1 { // "5/15" means from 5 to the end, every 15
				hi = max
			}
		}
		if lo < min || hi > max {
			return f, fmt.Errorf("%q is out of range %d-%d", part, min, max)
		}
		for v := lo; v <= hi; v += step {
			f.allowed[v] = true
		}
	}
	return f, nil
}

func (s *Schedule) matchesDay(t time.Time) bool {
	dom := s.dom.allowed[t.Day()]
	dow := s.dow.allowed[int(t.Weekday())]
	switch {
	case s.dom.any && s.dow.any:
		return true
	case s.dom.any:
		return dow
	case s.dow.any:
		return dom
	default:
		return dom || dow
	}
}

// Next returns the first time strictly after `after` that matches, or the
// zero time if none within 5 years (e.g. "0 0 31 2 *").
func (s *Schedule) Next(after time.Time) time.Time {
	t := after.In(s.Location).Truncate(time.Minute).Add(time.Minute)
	limit := t.AddDate(5, 0, 0)
	for t.Before(limit) {
		if !s.month.allowed[int(t.Month())] {
			t = time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, s.Location)
			continue
		}
		if !s.matchesDay(t) {
			t = time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, s.Location)
			continue
		}
		if !s.hour.allowed[t.Hour()] {
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour()+1, 0, 0, 0, s.Location)
			continue
		}
		if !s.minute.allowed[t.Minute()] {
			t = t.Add(time.Minute)
			continue
		}
		return t
	}
	return time.Time{}
}
