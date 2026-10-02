# Application layer

## Responsibilities

- `controller.go` owns adapter connection lifetimes and I/O. Its generation
  guards prevent events from replaced connections reaching current state.
- `state.go` reduces harness events into conversations and transcript entries.
  State is owned exclusively by the UI event loop.
- `skills.go` discovers local skill documents; it does not register skills with
  a harness or execute their instructions.

## Change rules

- Keep protocol-specific types out of this package. Use `internal/harness`
  contracts and events.
- Preserve synchronization and cancellation around connection replacement,
  prompt completion, permissions, and shutdown. Do not launch goroutines that
  directly mutate `State`.
- Preserve optimistic user-message reconciliation with harness echoes; replay
  and streaming updates must not duplicate prompts or corrupt entry revisions.
- A cancelled prompt can already have affected agent context or files. Do not
  infer rollback from a cancelled turn or remove history as a substitute for
  native rewind.
- Skill discovery must remain bounded, cancellable, and read-only. Preserve
  source precedence, document/base paths, parsing warnings, and symlink safety.
  A malformed document should not hide valid skills from other entries.

## Validation

Run `go test -race ./internal/app/...`. Cover reducer events, stale connection
events, cancellation, replay, and discovery edge cases with fake adapters or
temporary files, not real harness/model calls.
