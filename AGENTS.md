# Working on Agents

Agents is a keyboard-first Go terminal client for coding agents over ACP. Read
`README.md` for user-facing behavior and the nearest scoped `AGENTS.md` before
changing a package.

## Repository map

- `main.go`, `internal/cli/`: entry point and composition root; assemble the
  registry, configuration, controller, and Bubble Tea program here.
- `internal/harness/`: protocol-independent contracts, registry, and harness
  descriptors. `demo/` provides an in-memory agent over the production ACP path.
- `internal/acpclient/`: ACP transport, session operations, callbacks, and event
  normalization. Keep ACP SDK types behind this boundary.
- `internal/clientservices/`: session-scoped filesystem and terminal services.
- `internal/app/`: controller I/O, event-reduced application state, local skill
  discovery, and application helpers.
- `internal/storage/`: preferences and workspace drafts, not authoritative agent
  conversation history.
- `internal/tui/`: input routing, commands, navigation, views, and editor handoffs;
  `overlays/`, `transcript/`, and `theme/` hold reusable UI components.

## Development

Use the Go version declared in `go.mod` and the existing v2 Charm packages.

```sh
make build                 # builds bin/agents (bin/agents.exe on Windows)
go test ./internal/tui/... # example focused test run
make check                 # go vet ./... and go test -race ./...
make demo                  # interactive, no credentials or model calls
```

- Run `gofmt` on changed Go files. Add focused regression tests for behavior
  changes, then run `make check` before handing off when possible.
- Tests should use fake adapters, temporary directories, or the demo agent.
  Live tests are opt-in via `OPENCODE_INTEGRATION=1` or
  `CLAUDE_INTEGRATION=1`; do not enable them without explicit authorization.
- `make install` updates the user's installed executable; do not run it merely
  to validate a source change.
- Keep changes small and consistent with neighboring code. Update `README.md`
  when changing commands, shortcuts, configuration, or visible behavior.

## Architectural rules

- Wire concrete dependencies in `internal/cli`, not in lower-layer packages.
- The UI event loop owns application state. Background operations return
  messages/events; they must not mutate UI-owned state.
- Respect advertised capabilities and preserve `harness.ErrUnsupported` and
  authentication error semantics. A native CLI feature is not necessarily
  available over ACP.
- Never describe a local transcript edit as agent-context undo. Rewind, message
  editing, and file rollback are separate capabilities and are not implemented
  by merely deleting rendered entries.
- Protect drafts, attachments, user edits, and reading position. Ask before
  destructive or externally visible operations outside the requested scope.
- Do not log credentials or add them to persisted client state. Filesystem
  service boundaries are not a sandbox for the agent process.

## Working-tree care

Check `git status` before editing. This repository may contain untracked source
and user work; untracked does not mean disposable. Do not reset, clean, delete,
commit, push, or overwrite unrelated work to obtain a tidy diff. In handoffs,
state what changed, which checks ran, and any limitations honestly.
