package claude

import (
	"encoding/json"
	"math"
	"time"

	"agents/internal/harness"
)

// rateLimitPayload is the subset of Claude Code's rate_limit_event metadata
// that describes claude.ai subscription windows. Values are best-effort: older
// bridges may omit unifiedWindows, so the primary window fields are the
// fallback. utilization is a fraction in [0, 1], unlike the /usage endpoint's
// percentages.
type rateLimitPayload struct {
	Status         string                     `json:"status"`
	ResetsAt       *int64                     `json:"resetsAt"`
	RateLimitType  string                     `json:"rateLimitType"`
	Utilization    *float64                   `json:"utilization"`
	UnifiedWindows map[string]rateLimitWindow `json:"unifiedWindows"`
}

type rateLimitWindow struct {
	Utilization *float64 `json:"utilization"`
	ResetsAt    *int64   `json:"resetsAt"`
}

var subscriptionWindows = []struct {
	kind, label string
}{
	{kind: "five_hour", label: "5h"},
	{kind: "seven_day", label: "7d"},
}

// decodeUsageLimits extracts subscription windows from ACP usage metadata. It
// deliberately ignores unknown keys and malformed payloads so an incompatible
// bridge cannot break usage updates.
func decodeUsageLimits(meta json.RawMessage) []harness.UsageLimit {
	if len(meta) == 0 {
		return nil
	}
	var envelope struct {
		RateLimit json.RawMessage `json:"_claude/rateLimit"`
	}
	if err := json.Unmarshal(meta, &envelope); err != nil || len(envelope.RateLimit) == 0 || string(envelope.RateLimit) == "null" {
		return nil
	}
	var payload rateLimitPayload
	if err := json.Unmarshal(envelope.RateLimit, &payload); err != nil {
		return nil
	}

	limits := make([]harness.UsageLimit, 0, len(subscriptionWindows))
	for _, window := range subscriptionWindows {
		entry := payload.UnifiedWindows[window.kind]
		utilization, hasUtilization := fraction(entry.Utilization)
		resets := resetTime(entry.ResetsAt)
		status := harness.LimitAllowed

		if window.kind == payload.RateLimitType {
			status = limitStatus(payload.Status)
			if !hasUtilization {
				utilization, hasUtilization = fraction(payload.Utilization)
			}
			if resets.IsZero() {
				resets = resetTime(payload.ResetsAt)
			}
		}
		if !hasUtilization {
			continue
		}
		limits = append(limits, harness.UsageLimit{
			Kind:        window.kind,
			Label:       window.label,
			Utilization: utilization,
			ResetsAt:    resets,
			Status:      status,
		})
	}
	return limits
}

func fraction(value *float64) (float64, bool) {
	if value == nil || math.IsNaN(*value) || math.IsInf(*value, 0) {
		return 0, false
	}
	return min(1, max(0, *value)), true
}

func resetTime(seconds *int64) time.Time {
	if seconds == nil || *seconds <= 0 {
		return time.Time{}
	}
	// The SDK documents Unix seconds. Accept milliseconds defensively in case a
	// future bridge changes units.
	if *seconds > 1e12 {
		return time.UnixMilli(*seconds)
	}
	return time.Unix(*seconds, 0)
}

func limitStatus(status string) string {
	switch status {
	case harness.LimitWarning, harness.LimitRejected:
		return status
	default:
		return harness.LimitAllowed
	}
}
