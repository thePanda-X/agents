package app

import (
	"context"
	"errors"
	"sync"
	"time"

	"agents/internal/harness"
)

// Controller owns connection lifetimes and serializes connection replacement.
// It performs I/O only; State remains exclusively owned by the UI event loop.
type Controller struct {
	ctx              context.Context
	cancel           context.CancelFunc
	connectMu        sync.Mutex
	mu               sync.RWMutex
	adapter          harness.Adapter
	generation       uint64
	connectionCancel context.CancelFunc
	Events           chan harness.Event
}

func NewController(ctx context.Context) *Controller {
	owned, cancel := context.WithCancel(ctx)
	return &Controller{ctx: owned, cancel: cancel, Events: make(chan harness.Event, 256)}
}

func (c *Controller) publish(e harness.Event) {
	// Message/tool updates may be historical. The reducer knows whether the
	// session is replaying and assigns receipt times only to live entries.
	if e.At.IsZero() && e.Kind != harness.Message && e.Kind != harness.Tool {
		e.At = time.Now()
	}
	select {
	case c.Events <- e:
	case <-c.ctx.Done():
	}
}

func (c *Controller) Connect(descriptor harness.Descriptor, options harness.Options, restore *harness.SessionInfo) {
	c.connectMu.Lock()
	defer c.connectMu.Unlock()
	if c.ctx.Err() != nil {
		return
	}
	c.mu.Lock()
	old := c.adapter
	c.adapter = nil
	if c.connectionCancel != nil {
		c.connectionCancel()
	}
	c.generation++
	generation := c.generation
	ctx, cancel := context.WithCancel(c.ctx)
	c.connectionCancel = cancel
	c.mu.Unlock()
	sink := func(e harness.Event) {
		if ctx.Err() != nil {
			return
		}
		e.Generation = generation
		c.publish(e)
	}
	sink(harness.Event{Kind: harness.Connecting})
	if old != nil {
		_ = old.Close()
	}
	if options.Launch.Command == "" {
		options.Launch = descriptor.Launch
	}
	adapter, err := descriptor.Factory(ctx, options, sink)
	if err != nil {
		sink(harness.Event{Kind: harness.Disconnected, Err: err})
		return
	}
	c.mu.Lock()
	c.adapter = adapter
	c.mu.Unlock()
	sink(harness.Event{Kind: harness.Connected, Peer: adapter.Peer()})
	// Connecting initializes the agent, not a conversation. Fresh sessions are
	// created explicitly when the client sends its first prompt.
	if restore == nil {
		return
	}
	if adapter.Peer().Capabilities.Load {
		sink(harness.Event{Kind: harness.SessionLoading, SessionID: restore.ID})
	}
	session, err := adapter.LoadSession(ctx, *restore, adapter.Peer().Capabilities.Load)
	if err != nil {
		sink(harness.Event{Kind: harness.Failure, SessionID: restore.ID, Err: err})
		return
	}
	sink(harness.Event{Kind: harness.SessionReady, Session: session})
}

func (c *Controller) current() (harness.Adapter, uint64, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.adapter == nil {
		return nil, c.generation, errors.New("no agent is connected")
	}
	return c.adapter, c.generation, nil
}

func (c *Controller) operation(sessionID string, fn func(context.Context, harness.Adapter) (harness.Event, error)) {
	adapter, generation, err := c.current()
	if err != nil {
		c.publish(harness.Event{Kind: harness.Failure, Generation: generation, Err: err})
		return
	}
	ctx, cancel := context.WithTimeout(c.ctx, 45*time.Second)
	defer cancel()
	e, err := fn(ctx, adapter)
	e.Operation = e.Kind
	e.Generation, e.SessionID = generation, sessionID
	if err != nil {
		e.Kind, e.Err = harness.Failure, err
	}
	c.publish(e)
}

func (c *Controller) NewSession() {
	c.operation("", func(ctx context.Context, a harness.Adapter) (harness.Event, error) {
		s, err := a.NewSession(ctx)
		return harness.Event{Kind: harness.SessionReady, Session: s}, err
	})
}

func (c *Controller) ListSessions() {
	c.operation("", func(ctx context.Context, a harness.Adapter) (harness.Event, error) {
		sessions, err := a.ListSessions(ctx)
		return harness.Event{Kind: harness.SessionsListed, Sessions: sessions}, err
	})
}

func (c *Controller) LoadSession(info harness.SessionInfo) {
	adapter, generation, err := c.current()
	if err != nil {
		return
	}
	if adapter.Peer().Capabilities.Load {
		c.publish(harness.Event{Kind: harness.SessionLoading, Generation: generation, SessionID: info.ID})
	}
	c.operation(info.ID, func(ctx context.Context, a harness.Adapter) (harness.Event, error) {
		s, err := a.LoadSession(ctx, info, a.Peer().Capabilities.Load)
		return harness.Event{Kind: harness.SessionReady, Session: s}, err
	})
}

func (c *Controller) DeleteSession(id string) {
	c.operation(id, func(ctx context.Context, a harness.Adapter) (harness.Event, error) {
		return harness.Event{Kind: harness.SessionDeleted}, a.DeleteSession(ctx, id)
	})
}

func (c *Controller) CloseSession(id string) {
	c.operation(id, func(ctx context.Context, a harness.Adapter) (harness.Event, error) {
		return harness.Event{Kind: harness.SessionClosed}, a.CloseSession(ctx, id)
	})
}

func (c *Controller) SetOption(id string, option harness.ConfigOption, newValue any) {
	c.operation(id, func(ctx context.Context, a harness.Adapter) (harness.Event, error) {
		options, err := a.SetOption(ctx, id, option, newValue)
		return harness.Event{Kind: harness.Config, Options: options}, err
	})
}

func (c *Controller) Cancel(id string) {
	c.operation(id, func(ctx context.Context, a harness.Adapter) (harness.Event, error) {
		return harness.Event{Kind: harness.Notice, Text: "Cancellation requested; waiting for the agent."}, a.Cancel(ctx, id)
	})
}

func (c *Controller) Prompt(id string, prompt harness.Prompt) {
	a, generation, err := c.current()
	if err != nil {
		c.publish(harness.Event{Kind: harness.Failure, Generation: generation, Err: err})
		return
	}
	c.publish(harness.Event{Kind: harness.TurnStarted, Generation: generation, SessionID: id, Text: prompt.Text})
	// No arbitrary prompt timeout: cancelling this context is not ACP turn
	// cancellation. A user cancels with Cancel, which sends session/cancel.
	reason, err := a.Prompt(c.ctx, id, prompt)
	c.publish(harness.Event{Kind: harness.TurnFinished, Generation: generation, SessionID: id, Reason: reason, Err: err})
}

func (c *Controller) Authenticate(id string) {
	c.operation("", func(ctx context.Context, a harness.Adapter) (harness.Event, error) {
		return harness.Event{Kind: harness.Notice, Text: "Authentication finished. Create or reload a session."}, a.Authenticate(ctx, id)
	})
}

func (c *Controller) Close() error {
	c.cancel()
	c.connectMu.Lock()
	defer c.connectMu.Unlock()
	c.mu.Lock()
	if c.connectionCancel != nil {
		c.connectionCancel()
	}
	a := c.adapter
	c.adapter = nil
	c.mu.Unlock()
	if a != nil {
		return a.Close()
	}
	return nil
}
