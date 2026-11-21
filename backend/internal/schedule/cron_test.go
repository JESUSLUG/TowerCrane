package schedule

import (
	"testing"
	"time"
)

func TestNext(t *testing.T) {
	mx, _ := time.LoadLocation("America/Mexico_City")
	base := time.Date(2026, 11, 19, 10, 30, 0, 0, mx) // Thursday 10:30
	cases := []struct {
		expr, want string
	}{
		{"0 9 * * 1-5", "2026-11-20 09:00"},  // next weekday 9:00 (Friday)
		{"0 22 * * 1-5", "2026-11-19 22:00"}, // today 22:00
		{"*/15 * * * *", "2026-11-19 10:45"},
		{"0 9 * * 1", "2026-11-23 09:00"},    // next Monday
		{"0 0 1 * *", "2026-12-01 00:00"},
		{"30 10 19 11 *", "2027-11-19 10:30"}, // strictly after now
		{"0 9 * * 0", "2026-11-22 09:00"},     // Sunday = 0
		{"0 9 * * 7", "2026-11-22 09:00"},     // Sunday = 7
		{"0 12 1 * 1", "2026-11-23 12:00"},    // dom OR dow
	}
	for _, c := range cases {
		s, err := Parse(c.expr, "America/Mexico_City")
		if err != nil {
			t.Fatalf("%s: %v", c.expr, err)
		}
		if got := s.Next(base).Format("2006-01-02 15:04"); got != c.want {
			t.Errorf("%s: got %s, want %s", c.expr, got, c.want)
		}
	}
}

func TestParseErrors(t *testing.T) {
	for _, bad := range []string{"", "* * * *", "60 * * * *", "* 24 * * *", "*/0 * * * *", "5-1 * * * *", "a * * * *"} {
		if _, err := Parse(bad, ""); err == nil {
			t.Errorf("%q must be rejected", bad)
		}
	}
	if _, err := Parse("* * * * *", "Mars/Olympus"); err == nil {
		t.Error("unknown time zone must be rejected")
	}
	if s, _ := Parse("0 0 31 2 *", ""); !s.Next(time.Now()).IsZero() {
		t.Error("an impossible date never fires")
	}
}
