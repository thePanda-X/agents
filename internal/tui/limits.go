package tui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"agents/internal/harness"
	"agents/internal/tui/theme"

	"charm.land/lipgloss/v2"
)

// visibleLimits hides windows whose reset time has passed. A later usage
// update normally replaces them, but the app may stay open across a reset
// without another agent turn.
func (m *Model) visibleLimits() []harness.UsageLimit {
	return m.visibleLimitsAt(time.Now())
}

func (m *Model) visibleLimitsAt(now time.Time) []harness.UsageLimit {
	limits := make([]harness.UsageLimit, 0, len(m.state.UsageLimits))
	for _, limit := range m.state.UsageLimits {
		if !limit.ResetsAt.IsZero() && !limit.ResetsAt.After(now) {
			continue
		}
		limits = append(limits, limit)
	}
	return limits
}

// limitBarAt shows one window at a time. Switching on wall-clock 15-second
// boundaries keeps every render consistent without another timer. A negative
// width means no limit.
func (m *Model) limitBarAt(now time.Time, width int) string {
	limits := m.visibleLimitsAt(now)
	if len(limits) == 0 {
		return ""
	}
	return renderLimitBar(limits[int(now.Unix()/15)%len(limits)], now, width)
}

const limitBarWidth = 10

// renderLimitBar draws the widest form that fits width, preferring the bar over
// the reset countdown when both cannot fit.
func renderLimitBar(limit harness.UsageLimit, now time.Time, width int) string {
	utilization := limitUtilization(limit)
	filled := int(math.Round(utilization * limitBarWidth))
	labelText := theme.Clean(limitLabel(limit))
	percentText := limitPercent(limit)
	label := theme.Quiet.Render(labelText)
	bar := limitFillStyle(limit).Render(strings.Repeat("━", filled)) +
		theme.Quiet.Render(strings.Repeat("─", limitBarWidth-filled))
	percent := theme.Body.Render(percentText)

	resetText := ""
	if !limit.ResetsAt.IsZero() {
		if remaining := limit.ResetsAt.Sub(now); remaining > 0 {
			resetText = " ↻ " + untilReset(remaining)
		}
	}
	reset := theme.Quiet.Render(resetText)

	labelWidth := lipgloss.Width(labelText)
	percentWidth := lipgloss.Width(percentText)
	resetWidth := lipgloss.Width(resetText)
	fullWidth := labelWidth + 1 + limitBarWidth + 1 + percentWidth + resetWidth
	barWidth := labelWidth + 1 + limitBarWidth + 1 + percentWidth
	resetOnlyWidth := labelWidth + 1 + percentWidth + resetWidth
	compactWidth := labelWidth + 1 + percentWidth

	full := label + " " + bar + " " + percent + reset
	barOnly := label + " " + bar + " " + percent
	resetOnly := label + " " + percent + reset
	compact := label + " " + percent

	if width < 0 {
		return full
	}
	switch {
	case fullWidth <= width:
		return full
	case barWidth <= width:
		return barOnly
	case resetOnlyWidth <= width:
		return resetOnly
	case compactWidth <= width:
		return compact
	default:
		return ""
	}
}

func (m *Model) openLimits() {
	limits := m.visibleLimits()
	name := theme.Clean(m.opts.Descriptor.Name)
	if len(limits) == 0 {
		m.openInfo("Subscription usage", name+" has not reported subscription windows.\n\nClaude Code reports them during a turn when signed in to a claude.ai subscription. /agent usage opens the agent's full native usage report.")
		return
	}
	lines := make([]string, 0, len(limits)+2)
	for _, limit := range limits {
		line := theme.Body.Render(fmt.Sprintf("%-3s  %s", theme.Clean(limitLabel(limit)), limitPercent(limit)))
		switch limit.Status {
		case harness.LimitRejected:
			line += theme.Error.Render("  rejected")
		case harness.LimitWarning:
			line += theme.Warning.Render("  warning")
		}
		if !limit.ResetsAt.IsZero() {
			line += theme.Quiet.Render("  " + resetLabel(limit.ResetsAt))
		}
		lines = append(lines, line)
	}
	lines = append(lines, "", theme.Quiet.Render("Reported by "+name+". /agent usage opens its full usage report."))
	m.openInfo("Subscription usage", strings.Join(lines, "\n"))
}

func limitLabel(limit harness.UsageLimit) string {
	if limit.Label != "" {
		return limit.Label
	}
	if limit.Kind != "" {
		return limit.Kind
	}
	return "limit"
}

func limitPercent(limit harness.UsageLimit) string {
	return fmt.Sprintf("%d%%", int(math.Round(limitUtilization(limit)*100)))
}

func limitUtilization(limit harness.UsageLimit) float64 {
	if math.IsNaN(limit.Utilization) {
		return 0
	}
	return min(1, max(0, limit.Utilization))
}

func limitFillStyle(limit harness.UsageLimit) lipgloss.Style {
	switch {
	case limit.Status == harness.LimitRejected || limit.Utilization >= 0.9:
		return theme.Error
	case limit.Status == harness.LimitWarning || limit.Utilization >= 0.75:
		return theme.Warning
	default:
		return theme.Accent
	}
}

// untilReset formats a positive duration as "42m", "3h 5m", or "2d 4h".
func untilReset(duration time.Duration) string {
	minutes := max(1, int(math.Round(duration.Minutes())))
	if minutes < 60 {
		return fmt.Sprintf("%dm", minutes)
	}
	hours := minutes / 60
	if hours < 24 {
		return fmt.Sprintf("%dh %dm", hours, minutes%60)
	}
	return fmt.Sprintf("%dd %dh", hours/24, hours%24)
}

func resetLabel(reset time.Time) string {
	remaining := time.Until(reset)
	if remaining <= 0 {
		return "resetting now"
	}
	if remaining < 24*time.Hour {
		return "resets in " + untilReset(remaining)
	}
	return "resets " + reset.Local().Format("Jan 2, 3:04 PM")
}
