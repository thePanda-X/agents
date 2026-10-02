# Client persistence

`store.go` persists preferences and per-workspace session references/drafts. It
does not hold credentials or an authoritative agent conversation transcript.

- Preserve existing JSON field names and compatibility with omitted fields.
  Keep new preferences optional or give them deliberate defaults.
- Preserve restrictive permissions, synchronized access, and temporary-file
  replacement. Never write partially serialized configuration in place.
- Keep workspace state scoped using the existing workspace-key mechanism.
- Do not persist authentication tokens, transient permission requests, or claim
  local session metadata can reconstruct native agent context.
- Coordinate settings changes with TUI defaults, remapping, saving, tests, and
  user-facing documentation. Explicit user keybindings override defaults.

Run `go test -race ./internal/storage/...`. Test round trips, missing/older fields,
workspace separation, and failure paths with temporary config directories; never
modify the user's real configuration in tests.
