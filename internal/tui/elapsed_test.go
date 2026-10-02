package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func TestElapsedTimeOmitsZeroUnits(t *testing.T) {
	for _, tc := range []struct {
		elapsed time.Duration
		want    string
	}{
		{-time.Second, "0s"},
		{0, "0s"},
		{999 * time.Millisecond, "0s"},
		{41 * time.Second, "41s"},
		{time.Minute, "1m"},
		{time.Minute + time.Second, "1m 1s"},
		{59*time.Minute + 59*time.Second, "59m 59s"},
		{time.Hour, "1h"},
		{time.Hour + 41*time.Second, "1h 41s"},
		{time.Hour + 2*time.Minute, "1h 2m"},
		{2*time.Hour + 3*time.Minute + 4*time.Second, "2h 3m 4s"},
		{25*time.Hour + time.Second, "25h 1s"},
	} {
		if got := elapsedTime(tc.elapsed); got != tc.want {
			t.Errorf("elapsedTime(%s) = %q; want %q", tc.elapsed, got, tc.want)
		}
	}
}

func TestWorkingActivityUsesHourMinuteSecondElapsedTime(t *testing.T) {
	m := testModel(t)
	c := m.state.Active()
	c.Busy = true
	c.StartedAt = time.Now().Add(-time.Hour - 2*time.Minute - 41*time.Second)
	if got := ansi.Strip(m.activity(100)); !strings.Contains(got, "Working · 1h 2m 41s") {
		t.Fatalf("activity did not use formatted elapsed time: %q", got)
	}
}
