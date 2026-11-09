package app

import (
	"testing"
	"time"

	"agents/internal/harness"
)

func TestUsageLimitsMergeAcrossUpdates(t *testing.T) {
	s := stateWithSession()
	reset := time.Now().Add(2 * time.Hour)
	s.Apply(harness.Event{Kind: harness.UsageChanged, Generation: 1, SessionID: "s", Usage: harness.Usage{
		Used: 10, Size: 100,
		Limits: []harness.UsageLimit{{Kind: "five_hour", Label: "5h", Utilization: 0.27, ResetsAt: reset, Status: harness.LimitAllowed}},
	}})
	if len(s.UsageLimits) != 1 || s.Active().Usage.Used != 10 {
		t.Fatalf("first usage update not applied: limits=%+v usage=%+v", s.UsageLimits, s.Active().Usage)
	}

	s.Apply(harness.Event{Kind: harness.UsageChanged, Generation: 1, SessionID: "s", Usage: harness.Usage{
		Used: 20, Size: 200,
		Limits: []harness.UsageLimit{{Kind: "seven_day", Label: "7d", Utilization: 0.41, ResetsAt: reset, Status: harness.LimitAllowed}},
	}})
	if len(s.UsageLimits) != 2 || s.Active().Usage.Used != 20 {
		t.Fatalf("second window not merged: limits=%+v usage=%+v", s.UsageLimits, s.Active().Usage)
	}

	s.Apply(harness.Event{Kind: harness.UsageChanged, Generation: 1, SessionID: "s", Usage: harness.Usage{
		Used: 25, Size: 200,
		Limits: []harness.UsageLimit{{Kind: "five_hour", Label: "5h", Utilization: 0.35, ResetsAt: reset, Status: harness.LimitAllowed}},
	}})
	if len(s.UsageLimits) != 2 || s.UsageLimits[0].Utilization != 0.35 {
		t.Fatalf("existing window was not replaced: %+v", s.UsageLimits)
	}

	// Token/cost updates commonly arrive without subscription metadata. They
	// must not erase windows learned earlier.
	s.Apply(harness.Event{Kind: harness.UsageChanged, Generation: 1, SessionID: "s", Usage: harness.Usage{Used: 30, Size: 300}})
	if len(s.UsageLimits) != 2 || s.Active().Usage.Used != 30 {
		t.Fatalf("limits were discarded with a plain usage update: limits=%+v usage=%+v", s.UsageLimits, s.Active().Usage)
	}
}

func TestUsageCostSurvivesUpdatesWithoutCost(t *testing.T) {
	s := stateWithSession()
	s.Apply(harness.Event{Kind: harness.UsageChanged, Generation: 1, SessionID: "s", Usage: harness.Usage{
		Used: 10, Size: 100, Cost: &harness.Cost{Amount: 1.25, Currency: "USD"},
	}})
	s.Apply(harness.Event{Kind: harness.UsageChanged, Generation: 1, SessionID: "s", Usage: harness.Usage{Used: 20, Size: 200}})
	usage := s.Active().Usage
	if usage == nil || usage.Cost == nil || usage.Cost.Amount != 1.25 || usage.Cost.Currency != "USD" {
		t.Fatalf("cost was discarded by a partial usage update: %+v", usage)
	}
	if usage.Used != 20 || usage.Size != 200 {
		t.Fatalf("token counts were not updated: %+v", usage)
	}
}

func TestExpiredUsageLimitsAreDropped(t *testing.T) {
	s := stateWithSession()
	s.UsageLimits = []harness.UsageLimit{{Kind: "five_hour", ResetsAt: time.Now().Add(-time.Minute)}}
	s.Apply(harness.Event{Kind: harness.UsageChanged, Generation: 1, SessionID: "s", Usage: harness.Usage{Used: 1, Size: 10}})
	if len(s.UsageLimits) != 0 {
		t.Fatalf("expired limits were retained: %+v", s.UsageLimits)
	}
}
