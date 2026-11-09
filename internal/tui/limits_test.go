package tui

import (
	"strings"
	"testing"
	"time"

	"agents/internal/harness"
	"agents/internal/tui/theme"
)

func TestActivityShowsLimitBarAndPlacesTooltipBeforeIt(t *testing.T) {
	m := testModel(t)
	m.state.UsageLimits = []harness.UsageLimit{
		{Kind: "five_hour", Label: "5h", Utilization: 0.27, ResetsAt: time.Now().Add(2 * time.Hour), Status: harness.LimitAllowed},
		{Kind: "seven_day", Label: "7d", Utilization: 0.25, ResetsAt: time.Now().Add(48 * time.Hour), Status: harness.LimitAllowed},
	}
	m.state.Active().Busy = true
	m.state.Active().StartedAt = time.Now()
	line := theme.Clean(m.activity(200))
	tooltip := strings.Index(line, "Ctrl+C stop")
	bar := strings.IndexAny(line, "━─")
	if tooltip < 0 || bar < 0 {
		t.Fatalf("activity line missing tooltip or bar: %q", line)
	}
	if tooltip >= bar {
		t.Fatalf("tooltip is not left of the bar: %q", line)
	}
	if !strings.Contains(line, "↻") {
		t.Fatalf("activity line missing reset countdown: %q", line)
	}
	if strings.Contains(line, "5h") == strings.Contains(line, "7d") {
		t.Fatalf("expected exactly one window label: %q", line)
	}
}

func TestActivityShowsLimitBarAtNarrowWidth(t *testing.T) {
	m := testModel(t)
	m.state.UsageLimits = []harness.UsageLimit{
		{Kind: "five_hour", Label: "5h", Utilization: 0.27, ResetsAt: time.Now().Add(2 * time.Hour), Status: harness.LimitAllowed},
	}
	m.state.Active().Busy = true
	m.state.Active().StartedAt = time.Now()
	line := theme.Clean(m.activity(40))
	if !strings.Contains(line, "Ctrl+C stop") || !strings.ContainsAny(line, "━─") {
		t.Fatalf("narrow activity line lost the tooltip or bar: %q", line)
	}
	if strings.Contains(line, "↻") {
		t.Fatalf("narrow activity line should drop the reset before the bar: %q", line)
	}
}

func TestActivityKeepsMeterAtMinimumWidth(t *testing.T) {
	m := testModel(t)
	m.state.UsageLimits = []harness.UsageLimit{
		{Kind: "five_hour", Label: "5h", Utilization: 0.27, ResetsAt: time.Now().Add(2 * time.Hour), Status: harness.LimitAllowed},
	}
	m.state.Active().Busy = true
	m.state.Active().StartedAt = time.Now()
	line := theme.Clean(m.activity(28))
	if !strings.Contains(line, "Ctrl+C stop") || !strings.Contains(line, "5h 27%") {
		t.Fatalf("minimum-width activity line lost the tooltip or meter: %q", line)
	}
	if strings.ContainsAny(line, "━─") {
		t.Fatalf("minimum-width activity line should keep only the compact meter: %q", line)
	}
}

func TestLimitBarAlternatesEvery15Seconds(t *testing.T) {
	m := testModel(t)
	m.state.UsageLimits = []harness.UsageLimit{
		{Kind: "five_hour", Label: "5h", Utilization: 0.27, ResetsAt: time.Now().Add(2 * time.Hour), Status: harness.LimitAllowed},
		{Kind: "seven_day", Label: "7d", Utilization: 0.25, ResetsAt: time.Now().Add(48 * time.Hour), Status: harness.LimitAllowed},
	}
	at := time.Unix(0, 0)
	if first := theme.Clean(m.limitBarAt(at, -1)); !strings.HasPrefix(first, "5h ") {
		t.Fatalf("expected five-hour window at the start of a cycle: %q", first)
	}
	if next := theme.Clean(m.limitBarAt(at.Add(14*time.Second), -1)); !strings.HasPrefix(next, "5h ") {
		t.Fatalf("bar switched before 15 seconds: %q", next)
	}
	if next := theme.Clean(m.limitBarAt(at.Add(15*time.Second), -1)); !strings.HasPrefix(next, "7d ") {
		t.Fatalf("expected weekly window after 15 seconds: %q", next)
	}
	if next := theme.Clean(m.limitBarAt(at.Add(30*time.Second), -1)); !strings.HasPrefix(next, "5h ") {
		t.Fatalf("expected the cycle to return to the five-hour window: %q", next)
	}
}

func TestLimitBarFillsByUtilization(t *testing.T) {
	now := time.Unix(0, 0)
	bar := theme.Clean(renderLimitBar(harness.UsageLimit{
		Kind:        "five_hour",
		Label:       "5h",
		Utilization: 0.5,
		ResetsAt:    now.Add(2*time.Hour + 10*time.Minute),
		Status:      harness.LimitAllowed,
	}, now, -1))
	if !strings.Contains(bar, "━━━━━─────") || !strings.Contains(bar, "50%") || !strings.Contains(bar, "↻ 2h 10m") {
		t.Fatalf("unexpected bar rendering: %q", bar)
	}
}

func TestUntilResetFormat(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{42 * time.Minute, "42m"},
		{3*time.Hour + 5*time.Minute, "3h 5m"},
		{2*24*time.Hour + 4*time.Hour, "2d 4h"},
		{30 * time.Second, "1m"},
	}
	for _, tc := range cases {
		if got := untilReset(tc.in); got != tc.want {
			t.Errorf("untilReset(%s) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestLimitBarAdaptsToAvailableWidth(t *testing.T) {
	now := time.Unix(0, 0)
	limit := harness.UsageLimit{
		Kind:        "five_hour",
		Label:       "5h",
		Utilization: 0.27,
		ResetsAt:    now.Add(2*time.Hour + 10*time.Minute),
		Status:      harness.LimitAllowed,
	}
	full := theme.Clean(renderLimitBar(limit, now, -1))
	if !strings.Contains(full, "━") || !strings.Contains(full, "↻ 2h 10m") {
		t.Fatalf("full form missing bar or reset: %q", full)
	}
	noReset := theme.Clean(renderLimitBar(limit, now, 17))
	if !strings.Contains(noReset, "━") || strings.Contains(noReset, "↻") {
		t.Fatalf("bar form should drop the reset first: %q", noReset)
	}
	noBar := theme.Clean(renderLimitBar(limit, now, 15))
	if strings.Contains(noBar, "━") || !strings.Contains(noBar, "↻ 2h 10m") {
		t.Fatalf("reset form should keep the countdown without the bar: %q", noBar)
	}
	compact := theme.Clean(renderLimitBar(limit, now, 6))
	if compact != "5h 27%" {
		t.Fatalf("compact form should keep only label and percentage: %q", compact)
	}
	if empty := renderLimitBar(limit, now, 5); empty != "" {
		t.Fatalf("meter should disappear when it cannot fit: %q", empty)
	}
}

func TestLimitsDialogShowsWindowsAndResetTimes(t *testing.T) {
	m := testModel(t)
	m.state.UsageLimits = []harness.UsageLimit{
		{Kind: "five_hour", Label: "5h", Utilization: 0.9, ResetsAt: time.Now().Add(2 * time.Hour), Status: harness.LimitWarning},
		{Kind: "seven_day", Label: "7d", Utilization: 0.24, ResetsAt: time.Now().Add(48 * time.Hour), Status: harness.LimitAllowed},
	}
	m.openLimits()
	content := theme.Clean(m.infoContent)
	for _, want := range []string{"5h", "90%", "warning", "7d", "24%", "resets in", "/agent usage"} {
		if !strings.Contains(content, want) {
			t.Fatalf("limits dialog missing %q: %s", want, content)
		}
	}
}

func TestLimitsDialogExplainsMissingData(t *testing.T) {
	m := testModel(t)
	m.state.UsageLimits = nil
	m.openLimits()
	content := theme.Clean(m.infoContent)
	if !strings.Contains(content, "has not reported subscription windows") || !strings.Contains(content, "/agent usage") {
		t.Fatalf("missing-data explanation omitted: %s", content)
	}
}

func TestExpiredLimitsAreHidden(t *testing.T) {
	m := testModel(t)
	m.state.UsageLimits = []harness.UsageLimit{{Kind: "five_hour", Label: "5h", Utilization: 0.5, ResetsAt: time.Now().Add(-time.Minute)}}
	if limits := m.visibleLimits(); len(limits) != 0 {
		t.Fatalf("expired window remained visible: %+v", limits)
	}
}

func TestLimitsCommandDoesNotShadowNativeUsage(t *testing.T) {
	command, ok := findLocalCommand("limits")
	if !ok || command.Action != "limits" {
		t.Fatalf("limits command not registered: %+v", command)
	}
	if _, ok := findLocalCommand("usage"); ok {
		t.Fatal("local usage command would shadow the native agent command")
	}
}
