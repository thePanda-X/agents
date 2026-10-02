package app

import (
	"context"
	"testing"

	"agents/internal/harness"
	"agents/internal/harness/demo"
)

func TestControllerDiscardsEventsFromReplacedConnection(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	controller := NewController(ctx)
	t.Cleanup(func() { cancel(); _ = controller.Close() })
	descriptor := demo.Descriptor()
	var sinks []harness.Sink
	descriptor.Factory = func(ctx context.Context, options harness.Options, sink harness.Sink) (harness.Adapter, error) {
		sinks = append(sinks, sink)
		return demo.New(ctx, options, sink)
	}
	controller.Connect(descriptor, harness.Options{Workspace: t.TempDir()}, nil)
	controller.Connect(descriptor, harness.Options{Workspace: t.TempDir()}, nil)
	for len(controller.Events) > 0 {
		<-controller.Events
	}
	sinks[0](harness.Event{Kind: harness.Notice, Text: "stale"})
	sinks[1](harness.Event{Kind: harness.Notice, Text: "current"})
	e := <-controller.Events
	if e.Text != "current" || e.Generation != 2 {
		t.Fatalf("stale connection event survived: %+v", e)
	}
}
