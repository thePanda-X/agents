package app

import (
	"slices"
	"time"

	"agents/internal/harness"
)

// mergeUsageLimits updates account-wide windows by kind and drops expired ones.
// A usage update without limits leaves existing windows in place because
// harnesses often send token/cost updates separately from subscription data.
func mergeUsageLimits(current, updates []harness.UsageLimit, now time.Time) []harness.UsageLimit {
	result := make([]harness.UsageLimit, 0, len(current)+len(updates))
	for _, limit := range current {
		if !expired(limit, now) {
			result = append(result, limit)
		}
	}
	for _, update := range updates {
		if update.Kind == "" || expired(update, now) {
			continue
		}
		if index := slices.IndexFunc(result, func(limit harness.UsageLimit) bool {
			return limit.Kind == update.Kind
		}); index >= 0 {
			result[index] = update
			continue
		}
		result = append(result, update)
	}
	return result
}

func expired(limit harness.UsageLimit, now time.Time) bool {
	return !limit.ResetsAt.IsZero() && !limit.ResetsAt.After(now)
}
