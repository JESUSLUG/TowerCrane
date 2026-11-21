package api

import (
	"testing"
	"time"
)

func TestPollInterval(t *testing.T) {
	cases := map[string]time.Duration{
		"":     DefaultPollInterval,
		"off":  0,
		"0":    0,
		"90s":  90 * time.Second,
		"5m":   5 * time.Minute,
		"5s":   30 * time.Second, // clamped
		"junk": DefaultPollInterval,
	}
	for raw, want := range cases {
		t.Setenv("TOWERCRANE_POLL_INTERVAL", raw)
		if got := PollInterval(); got != want {
			t.Errorf("PollInterval(%q) = %s, want %s", raw, got, want)
		}
	}
}
