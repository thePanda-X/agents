# Harness contracts and registrations

- `types.go` defines the protocol-independent adapter boundary. ACP SDK types
  must not cross it into application state or the TUI.
- `registry.go` holds descriptors assembled at startup and then treated as
  immutable. Register concrete harnesses through the CLI composition root.
- `opencode/` and `claude/` configure launch behavior and actionable setup hints;
  shared stable ACP behavior belongs in `internal/acpclient`.
- `demo/` supplies a deterministic in-memory ACP agent through the real transport
  path, without credentials, model calls, or workspace edits.

Expose capabilities explicitly rather than assuming every harness supports a
native UI feature. Preserve unsupported/authentication error semantics. New
optional operations need adapter behavior, capability handling, and tests;
never claim support based solely on a slash-command name.

Do not silently substitute another executable, download an agent, change command
overrides, or contact a different server instance when launch fails. Keep native
extensions and instance-specific behavior isolated from portable ACP contracts.

Run `go test -race ./internal/harness/...` and affected adapter tests. Extend the
demo/fakes when adding protocol-independent behavior that needs offline coverage.
