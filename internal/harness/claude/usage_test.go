package claude

import (
	"encoding/json"
	"testing"
	"time"

	"agents/internal/harness"
)

func TestDecodeUsageLimitsUnifiedWindows(t *testing.T) {
	limits := decodeUsageLimits(json.RawMessage(`{
		"_claude/rateLimit": {
			"status": "allowed_warning",
			"resetsAt": 1789388400,
			"rateLimitType": "five_hour",
			"utilization": 0.9,
			"unifiedWindows": {
				"five_hour": {"utilization": 0.9, "resetsAt": 1789388400},
				"seven_day": {"utilization": 0.24, "resetsAt": 1789437600}
			}
		}
	}`))
	if len(limits) != 2 {
		t.Fatalf("expected two windows, got %+v", limits)
	}
	five, seven := limits[0], limits[1]
	if five.Kind != "five_hour" || five.Label != "5h" || five.Utilization != 0.9 || five.Status != harness.LimitWarning {
		t.Fatalf("unexpected five-hour window: %+v", five)
	}
	if seven.Kind != "seven_day" || seven.Label != "7d" || seven.Utilization != 0.24 || seven.Status != harness.LimitAllowed {
		t.Fatalf("unexpected weekly window: %+v", seven)
	}
	if !five.ResetsAt.Equal(time.Unix(1789388400, 0)) || !seven.ResetsAt.Equal(time.Unix(1789437600, 0)) {
		t.Fatalf("unexpected reset times: %+v", limits)
	}
}

func TestDecodeUsageLimitsFallsBackToPrimaryWindow(t *testing.T) {
	limits := decodeUsageLimits(json.RawMessage(`{
		"_claude/rateLimit": {
			"status": "rejected",
			"resetsAt": 1789388400,
			"rateLimitType": "seven_day",
			"utilization": 1.2
		}
	}`))
	if len(limits) != 1 {
		t.Fatalf("expected one fallback window, got %+v", limits)
	}
	limit := limits[0]
	if limit.Kind != "seven_day" || limit.Utilization != 1 || limit.Status != harness.LimitRejected {
		t.Fatalf("unexpected fallback window: %+v", limit)
	}
}

func TestDecodeUsageLimitsClampsAndIgnoresMalformedMetadata(t *testing.T) {
	limits := decodeUsageLimits(json.RawMessage(`{
		"_claude/rateLimit": {
			"rateLimitType": "five_hour",
			"utilization": -0.5,
			"unifiedWindows": {"seven_day": {"resetsAt": 1789437600}}
		}
	}`))
	if len(limits) != 1 || limits[0].Kind != "five_hour" || limits[0].Utilization != 0 || limits[0].Status != harness.LimitAllowed {
		t.Fatalf("unexpected clamped window: %+v", limits)
	}

	for _, meta := range []string{
		``,
		`not json`,
		`{}`,
		`{"_claude/rateLimit": null}`,
		`{"_claude/rateLimit": "wrong"}`,
		`{"_claude/rateLimit": {"rateLimitType": "overage", "utilization": 0.5}}`,
	} {
		if limits := decodeUsageLimits(json.RawMessage(meta)); len(limits) != 0 {
			t.Fatalf("expected no limits for %q, got %+v", meta, limits)
		}
	}
}

func TestResetTimeAcceptsMilliseconds(t *testing.T) {
	milliseconds := time.Now().UnixMilli()
	got := resetTime(&milliseconds)
	if !got.Equal(time.UnixMilli(milliseconds)) {
		t.Fatalf("unexpected millisecond reset time: %v", got)
	}
}
