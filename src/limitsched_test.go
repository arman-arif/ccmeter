package ccmeter

import (
	"testing"
	"time"
)

func TestLimitsDue(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	iv := 5 * time.Minute
	active := now.Add(-time.Hour)
	idle := now.Add(-10 * time.Hour)
	cases := []struct {
		name string
		st   fetchState
		last time.Time
		want bool
	}{
		{"never checked", fetchState{}, idle, true},
		{"active, checked 6m ago", fetchState{LastAttempt: now.Add(-6 * time.Minute)}, active, true},
		{"active, checked 2m ago", fetchState{LastAttempt: now.Add(-2 * time.Minute)}, active, false},
		{"idle, checked 10m ago", fetchState{LastAttempt: now.Add(-10 * time.Minute)}, idle, false},
		{"idle, checked 31m ago", fetchState{LastAttempt: now.Add(-31 * time.Minute)}, idle, true},
		{"backing off", fetchState{NextAllowed: now.Add(time.Minute)}, active, false},
	}
	for _, c := range cases {
		if got := limitsDue(c.st, c.last, iv, now); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
	// A long custom interval also stretches the idle interval.
	if limitsDue(fetchState{LastAttempt: now.Add(-40 * time.Minute)}, idle, time.Hour, now) {
		t.Error("idle interval should never be shorter than the configured interval")
	}
}
