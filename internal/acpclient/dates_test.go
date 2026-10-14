package acpclient

import (
	"testing"

	"agents/internal/harness"

	"github.com/Tangerg/acp"
)

func TestNormalizeSessionDateUpdatesPreservePartialAndNullFields(t *testing.T) {
	const date = "2026-10-13T00:15:32.123456789+02:00"
	for _, test := range []struct {
		name string
		date acp.Opt[string]
		want *string
	}{
		{name: "omitted"},
		{name: "reported", date: acp.OptValue(date), want: new(date)},
		{name: "cleared", date: acp.OptNull[string](), want: new("")},
	} {
		t.Run(test.name, func(t *testing.T) {
			e, err := normalizeUpdate(&acp.SessionNotification{SessionID: "s", Update: &acp.SessionInfoUpdate{UpdatedAt: test.date}})
			if err != nil || e.Kind != harness.Metadata || e.Metadata.Title != nil {
				t.Fatalf("bad metadata update: %+v, %v", e, err)
			}
			got := e.Metadata.UpdatedAt
			if (got == nil) != (test.want == nil) || got != nil && *got != *test.want {
				t.Fatalf("timestamp changed: got %v, want %v", got, test.want)
			}
		})
	}
	info := sessionInfo(acp.SessionInfo{SessionID: "s", Cwd: "/workspace", UpdatedAt: acp.OptValue(date)})
	if info.UpdatedAt != date {
		t.Fatal("listing changed the timestamp's offset or precision")
	}
}
