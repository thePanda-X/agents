# Terminal UI

Design for keyboard-first terminal users: predictable focus, restrained visual
hierarchy, discoverable commands, and preservation of drafts and reading position.

## Navigation map

- `model.go`: Bubble Tea messages, application events, focus, composer sizing,
  and viewport refresh.
- `sessions.go`: connection, reconnection, and new-conversation transitions.
- `persistence.go`: snapshots, draft persistence, and acknowledged autosave retries.
- `pastes.go`: bounded text paste folding, atomic markers, and full-text expansion.
- `keys.go`: action registry, defaults/remaps, key precedence, and routing.
- `commands.go`: local slash commands, aliases, completion, native bypass.
- `actions.go`: palettes, settings, selection dispatch, and help.
- `navigation.go`, `layout.go`: browse/search/inspection and density anchors.
- `view.go`: layout and overlay/footer budgets.
- `skills.go`: asynchronous discovery results and staged skill-loading requests.
- `editor.go`: external editor handoffs and scoped reported-file targets.
- `overlays/`: fuzzy pickers, previews, multi-selection, and scrolling.
- `transcript/`: rendering, folding, grouping, highlighting, and row positions.
- `theme/`: shared styling and terminal-safe text helpers.
- `ux_test.go`, `commands_test.go`, `skills_test.go`: interaction regressions.

## State and I/O

- Mutate model/application state only in the event loop. Capture inputs for
  asynchronous `tea.Cmd` work and return messages; guard stale results with the
  existing generation/session patterns.
- Avoid blocking I/O in input routing or rendering. Respect cancellation and
  keep focus restoration intentional after overlays/editor handoffs.
- Preserve the draft and attachments until submission succeeds or the user
  explicitly discards them. Picker cancellation must not unexpectedly send,
  clear, or replace a draft.
- Paste markers are presentation only. Use full draft text for submission,
  persistence, recovery, history, and external editors; keep their block data
  scoped to its draft buffer. Reject oversized pastes rather than truncating them.

## Keyboard and command semantics

- Use the action registry and generated help instead of adding disconnected
  shortcuts. Explicit remaps override default bindings.
- Vim navigation and compact density remain opt-in. In browse mode, `j/k`
  scroll visual rows; arrows select entries; Enter/Space toggle foldable entries.
- Ctrl+R searches prompt history; Alt+R reconnects. Ctrl+X toggles density in the
  main interface but retains session deletion inside the sessions picker.
- Preserve Ctrl+C precedence for cancellation, draft clearing, and quitting.
- Aliases must invoke semantically equivalent client actions. `/agent <command>`
  bypasses collisions to reach native commands. Do not map native undo,
  permissions, or context compaction to unrelated local behavior.
- `/skills` stages a loading request; it does not itself load a skill or send a
  model prompt. Preserve native permission boundaries and draft-size checks.

## Rendering invariants

- Measure composer height using visual rows, including soft wraps and explicit
  newlines. Bound visible height without preventing multiline entry.
- Entry row positions must exactly match rendered output. Account for separators,
  grouping, hidden entries, and padding in both rendering and navigation.
- Compact activity blocks stay dense, with one blank row at activity/response
  boundaries. Transcript text uses the available width rather than a fixed prose
  cap; account for gutters and message chrome without narrowing code/diffs.
- Preserve reading anchors across density changes, resizing, folding, and updates.
- Explicit `Expanded` map values override global thought visibility, including
  `false`. Absence means use the global preference.
- Budget all picker chrome, search, previews, and rows for small terminals.
  Preserve scrolling and selection when filtering or deleting entries.
- Use terminal-safe text helpers for untrusted labels/content; keep ANSI width
  accounting consistent with the renderer.

## Validation

Run `go test -race ./internal/tui/...` and `make check`. Add model-level key/message
tests and renderer row-position tests for navigation/layout changes. Cover narrow
and short terminals, wrapped drafts, stale async results, cancellation, and native
command collisions where relevant.

Use `make demo` for an optional interactive walkthrough. Automated tests are not
a live terminal walkthrough; report which form of validation actually occurred.
