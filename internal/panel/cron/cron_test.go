package cron

import (
	"testing"
	"time"
)

func TestSchedules(t *testing.T) {
	at := func(s string) time.Time { v, _ := time.Parse("2006-01-02 15:04", s); return v }
	for expr, want := range map[string][2]string{
		"0 4 * * *":    {"2026-11-09 10:00", "2026-11-10 04:00"},
		"*/15 * * * *": {"2026-11-09 10:01", "2026-11-09 10:15"},
		"30 6 * * 1-5": {"2026-11-13 07:00", "2026-11-16 06:30"}, // Friday after 6:30 → Monday
		"0 0 1 * *":    {"2026-11-09 10:00", "2026-12-01 00:00"},
		"0 12 * * 7":   {"2026-11-09 10:00", "2026-11-15 12:00"}, // 7 is Sunday
		"0 3 1,15 * *": {"2026-11-02 00:00", "2026-11-15 03:00"},
	} {
		s, err := Parse(expr)
		if err != nil {
			t.Fatalf("%s: %v", expr, err)
		}
		if got := s.Next(at(want[0])); !got.Equal(at(want[1])) {
			t.Errorf("%s after %s: %s, want %s", expr, want[0], got.Format("2006-01-02 15:04"), want[1])
		}
	}
	for _, bad := range []string{"", "* * * *", "60 * * * *", "* 24 * * *", "*/0 * * * *", "a * * * *", "5-1 * * * *"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}
