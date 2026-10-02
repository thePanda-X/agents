package app

import (
	"context"
	"testing"

	"agents/internal/harness"
)

type lazySessionAdapter struct {
	harness.Adapter
	newCalls, loadCalls int
}

func (a *lazySessionAdapter) Peer() harness.Peer {
	return harness.Peer{Capabilities: harness.Capabilities{Load: true}}
}
func (a *lazySessionAdapter) Close() error { return nil }
func (a *lazySessionAdapter) NewSession(context.Context) (harness.Session, error) {
	a.newCalls++
	return harness.Session{SessionInfo: harness.SessionInfo{ID: "new"}}, nil
}
func (a *lazySessionAdapter) LoadSession(_ context.Context, info harness.SessionInfo, _ bool) (harness.Session, error) {
	a.loadCalls++
	return harness.Session{SessionInfo: info}, nil
}

func TestConnectDoesNotCreateSessionButResumeStillLoads(t *testing.T) {
	controller := NewController(context.Background())
	t.Cleanup(func() { _ = controller.Close() })
	adapter := &lazySessionAdapter{}
	descriptor := harness.Descriptor{Factory: func(context.Context, harness.Options, harness.Sink) (harness.Adapter, error) { return adapter, nil }}
	controller.Connect(descriptor, harness.Options{}, nil)
	if adapter.newCalls != 0 || adapter.loadCalls != 0 {
		t.Fatal("startup created or loaded a session")
	}
	for len(controller.Events) > 0 {
		if e := <-controller.Events; e.Kind == harness.SessionReady {
			t.Fatal("startup claimed a session was ready")
		}
	}
	controller.NewSession()
	if adapter.newCalls != 1 {
		t.Fatal("explicit lazy creation did not create a session")
	}
	<-controller.Events
	controller.Connect(descriptor, harness.Options{}, &harness.SessionInfo{ID: "existing"})
	if adapter.newCalls != 1 || adapter.loadCalls != 1 {
		t.Fatal("resume created a new session instead of loading")
	}
	var ready bool
	for len(controller.Events) > 0 {
		if e := <-controller.Events; e.Kind == harness.SessionReady && e.Session.ID == "existing" {
			ready = true
		}
	}
	if !ready {
		t.Fatal("resume did not publish loaded session")
	}
}
