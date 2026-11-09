// Package cron reads five-field cron expressions ("0 4 * * *": 4am every day) for game server schedules:
// minute, hour, day of the month, month, and day of the week (0 or 7 is Sunday). Each field takes *,
// a number, a range (1-5), a list (1,15) and a step (*/15).
package cron

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Schedule is which minutes, hours, days, months and weekdays match.
type Schedule struct {
	minute, hour, day, month, weekday uint64 // bit n set: n matches
	anyDay, anyWeekday                bool
}

var fields = []struct {
	name     string
	min, max int
}{{"minute", 0, 59}, {"hour", 0, 23}, {"day", 1, 31}, {"month", 1, 12}, {"weekday", 0, 7}}

// Parse reads an expression.
func Parse(expr string) (*Schedule, error) {
	parts := strings.Fields(expr)
	if len(parts) != 5 {
		return nil, errors.New("a schedule has five parts: minute, hour, day, month and weekday, like 0 4 * * *")
	}
	var bits [5]uint64
	for i, part := range parts {
		f := fields[i]
		for _, item := range strings.Split(part, ",") {
			rangePart, stepPart, hasStep := strings.Cut(item, "/")
			step := 1
			if hasStep {
				n, err := strconv.Atoi(stepPart)
				if err != nil || n < 1 {
					return nil, fmt.Errorf("the %s's step %q isn't a number", f.name, stepPart)
				}
				step = n
			}
			lo, hi := f.min, f.max
			if rangePart != "*" {
				a, b, isRange := strings.Cut(rangePart, "-")
				var err error
				if lo, err = strconv.Atoi(a); err != nil {
					return nil, fmt.Errorf("the %s %q isn't a number", f.name, a)
				}
				hi = lo
				if isRange {
					if hi, err = strconv.Atoi(b); err != nil {
						return nil, fmt.Errorf("the %s %q isn't a number", f.name, b)
					}
				} else if hasStep {
					hi = f.max
				}
			}
			if lo < f.min || hi > f.max || lo > hi {
				return nil, fmt.Errorf("the %s should be from %d to %d", f.name, f.min, f.max)
			}
			for n := lo; n <= hi; n += step {
				bits[i] |= 1 << n
			}
		}
	}
	if bits[4]&(1<<7) != 0 {
		bits[4] |= 1 // 7 is Sunday too
	}
	return &Schedule{minute: bits[0], hour: bits[1], day: bits[2], month: bits[3], weekday: bits[4],
		anyDay: parts[2] == "*", anyWeekday: parts[4] == "*"}, nil
}

// Matches says whether the schedule runs in t's minute. As in cron, when both the day and the weekday are
// given, either matching is enough.
func (s *Schedule) Matches(t time.Time) bool {
	has := func(bits uint64, n int) bool { return bits&(1<<n) != 0 }
	if !has(s.minute, t.Minute()) || !has(s.hour, t.Hour()) || !has(s.month, int(t.Month())) {
		return false
	}
	day, weekday := has(s.day, t.Day()), has(s.weekday, int(t.Weekday()))
	switch {
	case s.anyDay && s.anyWeekday:
		return true
	case s.anyDay:
		return weekday
	case s.anyWeekday:
		return day
	}
	return day || weekday
}

// Next is the first minute after t that the schedule runs in, within a year (zero if never).
func (s *Schedule) Next(t time.Time) time.Time {
	t = t.Truncate(time.Minute).Add(time.Minute)
	for end := t.AddDate(1, 0, 1); t.Before(end); t = t.Add(time.Minute) {
		if s.Matches(t) {
			return t
		}
	}
	return time.Time{}
}
