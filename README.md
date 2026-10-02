# Agents

A keyboard-first Go TUI for ACP coding agents. Graphite surfaces, soft pink
accents, a quiet conversation layout, and a pinned composer. OpenCode V2,
Claude Code, and Pi are built in; other ACP agents reuse the same adapter.

## Run

Requires Go 1.27.1+ (the repository's toolchain) and an interactive terminal.
macOS and Linux are the primary targets.

Install once from this repository:

```sh
go install .                   # or make install
```

Then run `agents` from any project directory. It automatically uses your shell's
current working directory as the workspace, not the binary's location or a
parent Git repository root. No `--cwd` argument is required.

Go installs the command into `$GOBIN`, or `$(go env GOPATH)/bin` when GOBIN is
unset. Make sure that directory is on your `PATH`.

```sh
cd /path/to/project
agents                         # OpenCode in this directory
agents --harness claude         # Claude Code through its ACP bridge
agents --harness pi             # Pi through the pi-acp bridge
agents --demo                  # local demo, no model calls or file edits
agents --resume                # replay the last session for this directory
agents --cwd /another/project  # optional workspace override
```

For development without installing:

```sh
go run . --demo                 # local ACP demo: no credentials, edits, or model calls
go run .                        # OpenCode in the current directory
go run . --cwd /path/to/project # optional workspace override
go run . --resume               # replay the last session for this workspace
go run . --session SESSION_ID   # replay a specific saved session
make build                     # bin/agents (bin/agents.exe on Windows)
```

`make build` updates the executable in this repository's `bin` directory. To
update the installed `agents` command on your `PATH` after source changes, run
`make install` (or `go install .`) again. On Windows, you can run the local build
directly with `.\bin\agents.exe --demo`.

### Conversation startup

Agents connects to the harness on startup and normally creates a fresh session when
you send the first nonempty prompt (text or attachments). Opening and closing the
client, browsing commands, or choosing `/new` does not create an empty session.
Unsent text drafts are saved locally without a native session ID. `--resume`,
`--session`, and selecting a saved session still load that session immediately.
To configure before your first task, use `/model`, `/effort`, `/mode`, `/options`,
or Ctrl+T. These explicitly prepare an empty session, then open the advertised
controls without sending a prompt. `/model <value>` and `/effort <value>` also
work before the first task. ACP supplies model and thinking catalogs only after
session creation, so this cannot select them before a native session exists.
Drafts and attachments are preserved, and sending is blocked while an option
change is pending. Ctrl+C cancels pending setup without clearing the draft;
already-started creation may still leave an empty session. Native command
catalogs become available after creation or resume. The demo normally submits
its sample prompt automatically.

The first send waits for session creation and submits the original prompt once.
Creation errors preserve your text and attachments for retry. Text edited during
creation stays in the composer as a separate draft. Ctrl+C (or `/cancel`) cancels
the queued first send; an already-started native creation may still leave an
empty session, but its prompt will not be sent.

For OpenCode, install OpenCode V2 and sign in using `opencode auth login` first.
Agents launches `opencode acp`, which owns a private OpenCode server. It does
not attach to the shared service or mirror a running native OpenCode TUI.
Project configuration, instructions, skills, plugins, and MCP settings remain
OpenCode's responsibility.

Use `--acp-command /absolute/path/to/opencode` if the executable is not on PATH.
Repeat `--acp-arg` to replace the default argument list. Repeat `--root` to add
workspace roots when the agent advertises that capability.

### Claude Code

Install the [Claude Agent ACP bridge](https://github.com/agentclientprotocol/claude-agent-acp)
separately (Node.js 22+), then sign in and select Claude:

```sh
npm install -g @agentclientprotocol/claude-agent-acp
claude-agent-acp --cli auth login
agents --harness claude
```

An existing Claude Code login can also be reused, or you can provide
`ANTHROPIC_API_KEY` through your environment. Agents never stores credentials.
The bridge includes the SDK's compatible Claude executable; a separate native
CLI is not required. `CLAUDE_CODE_EXECUTABLE=/absolute/path/to/claude` optionally
selects a different compatible executable.

Use `/harness` to switch in the TUI; your selection is saved for future launches.
Use `/auth` for the bridge's advertised login flows, `/model`, `/effort`, and
`/mode` for advertised controls, and `/sessions` for history. Supported modes
and effort levels depend on the bridge, model, and account. For local slash
command collisions, `/agent <command>` sends the command directly to Claude.

```sh
agents --harness claude --resume
agents --harness claude --acp-command /absolute/path/to/claude-agent-acp
agents --list-harnesses
```

Agents launches `claude-agent-acp`, **not** `claude acp`. Instructions, skills,
project settings, and model access remain with Claude Code and the bridge.
Each harness owns its sessions; switching never transfers conversation context
or attaches to an existing native CLI process. This is stable ACP support, not
full native UI parity: bridge-specific extensions such as nested subagent
transcripts and interactive terminals are not negotiated.

### Pi

Install [Pi](https://pi.dev) and the separate
[pi-acp bridge](https://github.com/svkozak/pi-acp) (Node.js 22+; Pi 0.81.0+).
Configure your model provider and sign in through Pi, then select Pi:

```sh
npm install -g @earendil-works/pi-coding-agent pi-acp
pi-acp --terminal-login
agents --harness pi
```

If Pi is already configured, its existing credentials are reused. Agents launches
`pi-acp`, which starts `pi --mode rpc`; it does not attach to an existing Pi TUI.
Pi owns its sessions, settings, extensions, and skills. Use `/harness` to switch
agents, `/auth` for the bridge's advertised terminal login, and the session,
model, and mode controls when the bridge advertises them. Existing custom `pi`
entries in `settings.json` keep their command, arguments, and environment.
If the bridge is not on PATH, use `--acp-command /absolute/path/to/pi-acp`
(on Windows, point to `pi-acp.cmd`).

The bridge currently runs Pi's filesystem and shell tools itself; it does not
delegate them through ACP. It accepts ACP MCP server settings but does not
forward them to Pi; configure Pi's MCP integration separately. Availability of
other features depends on what the connected bridge advertises.

## Interface

| Key | Action |
| --- | --- |
| Enter | Send (one active prompt per session) |
| Alt+Enter / Ctrl+J | Newline; Shift+Enter also works in enhanced terminals |
| Tab | Composer/transcript focus; complete `/commands` or `@files` |
| Shift+Tab | Cycle the agent's advertised mode |
| Ctrl+P | Command palette |
| Ctrl+O | Model, effort, mode, and generic session options |
| Ctrl+S | Session history |
| Ctrl+N | New conversation |
| Ctrl+F | Attach workspace file (images are capability-gated) |
| Ctrl+E | External draft editor using `$VISUAL` / `$EDITOR` |
| Ctrl+T | Cycle advertised thinking/effort levels; `/thoughts` toggles reasoning visibility |
| Ctrl+X | Toggle compact / comfortable layout |
| Ctrl+V | Paste a clipboard image into the composer, or fall back to text paste |
| Ctrl+Y | Copy last response using terminal OSC52 clipboard support |
| Ctrl+C | Cancel turn, dismiss overlay, or clear draft |
| Ctrl+D | Quit; confirm again while a turn is active |
| Ctrl+R | Search prompt history (never sends automatically) |
| Alt+R | Reconnect without automatically resending a prompt |
| Alt+Up / Alt+Down | Prompt history |
| PgUp / PgDn | Scroll conversation |

In transcript focus, use j/k to scroll by visual line, arrows to select entries, Enter to expand
or collapse the selected tool/thinking block, and y to copy. Selection follows
the reading line as you scroll with the keyboard, including at the top/bottom of
the viewport. Individual thinking folds override the global reasoning preference.
Scrolling away from the bottom disables automatic
following until you return. Plans, reported changes, settings, authentication,
and harness selection are available through the command palette.

Dialogs are centered over the conversation. Short or narrow terminals use tighter
padding and reserve space for the selected action and close/cancel instructions.
The statusline keeps connection and turn state visible; short-lived notifications
appear separately above it. `/diagnostics` opens full error details and agent setup
instructions. Failed file, skill, and session pickers offer Enter to retry.

The composer grows for both soft-wrapped text and explicit newlines, up to eight
visible rows (less in small terminals). Longer drafts scroll internally and are
not restricted to the visible row count. Pasting multiline text does not send it.
Alt+Enter, Ctrl+J, and enhanced-terminal Shift+Enter insert newlines; Enter sends.

Enable **Vim navigation** in `/settings` (or `"vimNavigation": true` in
`settings.json`) for Esc to enter browse mode and `i` to return to composition.
Ctrl+D/U half-page scroll in browse mode; the quit shortcut still works in the
composer. The default profile retains Tab focus switching and Esc to compose.
Both profiles support these browse-only actions:

| Key | Action |
| --- | --- |
| `gg` / `G` | First / latest entry |
| `/`, `n` / `N` | Literal transcript search, next / previous occurrence |
| `J` / `K` | Next / previous user turn |
| `]` / `[` | Next / previous failed tool |
| `}` / `{` | Next / previous reported edit |
| `o` | Entry detail pager, including full tool input/output |
| `c` | Choose and copy a fenced code block |
| `e` | Review and open a reported workspace file in `$VISUAL` / `$EDITOR` |
| `v` | Export the selected entry to a temporary editor buffer |
| `i` / `?` | Compose / help |

Focus changes preserve the draft cursor, selected entry, and reading position.
The detail pager supports j/k, Ctrl+D/U, gg/G, and J/K for diff hunks. Reported
file locations can include line numbers; editor line jumps use the Vim-compatible
`+line` argument. Editor targets must be existing regular files inside the active
workspace, including after resolving symlinks. Output exports are temporary;
editing them does not change the transcript.

Pickers support fuzzy ranking, highlighted matches, Ctrl+N/P navigation, and
previews on wide terminals. The file picker supports Tab to mark multiple files,
then Enter to attach them. File previews are bounded to 8 KiB and scoped to the
workspace. `/changes` summarizes reported files and opens the individual reports
for repeatedly edited files. Each diff remains separate, with its tool status;
this never pretends to show the current working-tree diff or reapplies changes.
Esc returns from a detail pager or option selector to its parent picker.

Model and option selectors focus and mark the current value. Confirming that
value leaves it unchanged. Settings remain open after toggles and retain the
filter and selection. The command palette groups related actions and explains
unavailable actions instead of silently ignoring them.

Enable **Compact layout** in `/settings` (or `"compact": true`) to reduce message
padding and combine configuration with the statusline. COMPOSE/BROWSE/SEARCH
labels make focus explicit. Shortcut hints in the palette, footer, and help use
the configured keybindings. Ctrl+R now searches history; remap `history` or
`reconnect` to restore your preferred arrangement.
Ctrl+T cycles the active session's advertised thinking/effort choices in order,
wrapping to the first choice. Before a session exists, it prepares an empty
session and opens the thinking selector without sending a task. It does not
change reasoning visibility. Remap `effort` to change this shortcut; `/thoughts` (or a remapped
`thoughts` action) still controls whether reported thinking is expanded.
Working time uses hours, minutes, and seconds with zero units omitted (for
example `41s`, `2m 5s`, or `1h 3m 8s`; a newly started turn shows `0s`).
Press **Ctrl+X** to toggle density without opening settings; the preference is
saved. Remap it with `"keybindings": { "compact": "ctrl+x" }` in `settings.json`.
Compact composition groups consecutive thinking blocks and successful tools,
including patch/read/write calls, and keeps a blank line
around sent user messages to separate them from replies, thinking, and tool
activity. Sent messages also have a persistent pink left rail in both layouts.
Tab into browse mode
reveals each entry for inspection. Failed tools, active tools, and explicitly
expanded tools stay visible. Prose blocks wrap at 96 columns on wide terminals,
including in mixed responses; blocks containing code or tables retain the
available width.
Compact mode keeps one blank line between tool/thinking runs and model responses.

Ctrl+C dismisses an overlay or cancels an interaction before stopping a running
turn. When idle it clears the draft; `/restore` recovers the last cleared draft.
With an empty draft it no longer quits. Enter during a running turn preserves
your draft and explains why it was not sent; there is no implicit prompt queue.

Pending attachments stay with their original conversation or harness while this
client remains open, including file/image reads that finish after switching.
Sending waits for attachment reads to finish. `/attachments` shows full names
and metadata; Enter inspects an attachment and Delete (or Ctrl+X) removes only
that attachment. `/detach` still removes all pending attachments. Attachment
bytes are kept in memory, not written to client storage, and must be reattached
after restarting the client.

An interrupted, cancelled, or failed submission has an in-memory recovery
snapshot. `/recover` restores its text and attachments into an empty draft for
review; it does not overwrite newer input or automatically resend anything.
The agent may already have processed part of that submission. Recovery is local
draft recovery, not context undo or file rollback.

In `/settings`, **Appearance** cycles Graphite, High contrast, and Terminal
default. Terminal default uses your terminal's colors while retaining bold,
reverse-video cursor cues, and textual selection markers. **Reduced motion**
replaces activity animations with static indicators. These can also be set using
`"appearance": "high-contrast"` (or `"terminal"`) and `"reducedMotion": true` in
`settings.json`; omitted preferences keep the original Graphite appearance.
Transcript search highlights the active occurrence; n/N visits occurrences within
an entry before advancing to the next/previous matching entry.

Markdown tables use bold headers, padded columns, and horizontal rules between
data rows. Padding tightens on narrow terminals while long cells still wrap;
rules separate logical rows, not individual wrapped lines.

### Paste images

Copy an image or screenshot, focus the composer, and press **Ctrl+V**. The image
appears in the pending attachments; your draft stays intact. Press Enter to send
it with your text (or by itself), or use `/detach` to remove pending attachments.
Nothing is sent automatically. Images require a harness that advertises image
prompt support.

Use **`/paste-image`** (also available in the command palette) if your terminal
intercepts Ctrl+V. Ordinary bracketed text paste continues to work as before.
Terminal Cmd+V/Ctrl+Shift+V generally sends text, not image bytes; use the client
shortcut or slash command for screenshots. Remap the shortcut using
`"keybindings": { "paste": "alt+v" }` if needed.

Clipboard access uses local OS facilities: macOS AppKit via `osascript`, Windows
PowerShell's desktop clipboard, or `wl-paste` on Wayland / `xclip` on X11. Install
the relevant Linux utility separately; Agents does not install helpers. PNG,
JPEG, and GIF clipboard data are supported; native macOS TIFF screenshots and
Windows bitmap images are converted to PNG. Images are held in memory, limited
to 8 MiB and 40 megapixels, with no temporary image files.

This reads the clipboard **on the machine running Agents**. An SSH session does
not automatically expose your laptop's image clipboard; use `/attach` with an
image file available on the remote machine instead.

Permission choices are supplied by the agent, not invented by the UI. Structured
forms show questions and option descriptions with keyboard-driven single-choice
rows and multi-select checkboxes. Use arrows to move, Space/Enter to select,
Tab/Shift+Tab to change fields, and PgUp/PgDn to scroll long questions or reviews.
Free-text fields (including agent-supplied “Other” answers) remain editable.
Ctrl+S reviews, then Ctrl+S submits; Tab returns from review to editing.
Alt+S skips the entire form (`decline`); Esc cancels it (`cancel`). Drafts and
attachments are preserved, and the footer shows “Waiting for your answer”.
This supports agents that send ACP form elicitation, including Claude ACP's
`AskUserQuestion` mapping. OpenCode's native question tool requires its ACP
bridge to forward questions and replies; this client does not enable that tool
or infer interactive questions from ordinary prose. External interactions display the
full URL and require consent before opening a browser. Diffs are informational:
Agents does not apply a reported edit a second time.

Expanded patch/write/edit tools prefer structured ACP diffs. When those are
absent, the client can display reported unified patches, `apply_patch` payloads,
supplied old/new edit text, and reported file-diff metadata. Write payloads that
only contain new content show a clearly labelled added-line preview: the previous
contents are unknown, so this is not presented as a full-file before/after diff.
Requested edits may still be pending or failed; their preview is not proof that
the file changed. No workspace files are read to guess a tool's earlier state.
Completed patch/read/write calls and thinking blocks join other successful tool
calls in compact compose mode, including tools with diff previews. Browse mode
restores each entry individually; explicitly expanded entries remain visible in
either mode. Group labels show both tool and thinking-block counts.
Pending and running tools show an animated spinner in place of the fold arrow,
including when expanded. Finished tools return to their expand/collapse arrows.
Thinking blocks display only reasoning text actually supplied by the harness.
Empty chunks are labelled "no text supplied by agent" rather than expanding to
blank content; the client cannot recover reasoning a provider does not expose.

The demo's `/permission`, `/question`, and `/slow` commands exercise approvals,
forms, and cancellation through the real ACP adapter.

## Slash commands

Type `/` to open the searchable command picker, or press Tab after a partial
command such as `/mo`. Client and advertised agent commands are shown together,
with their source labeled. Canonical client commands are shown once; aliases are
searchable keywords and appear in the preview and help. Enter inserts the
selected command into the composer;
press Enter again to run it. Esc keeps your typed command for further editing
without adding a completion space. Backspacing does not reopen the picker; Tab
reopens it explicitly, or type a fresh `/` in an empty composer.
You can also type or paste a complete command and run it directly.

| Command | Action |
| --- | --- |
| `/help`, `/commands` | Show help or browse all slash commands |
| `/new`, `/sessions` | Create a conversation or browse session history |
| `/model [value]`, `/effort [value]`, `/mode [value]` | Open the relevant selector, or set an advertised value ID/name |
| `/options`, `/harness`, `/settings`, `/auth` | Open the corresponding controls |
| `/plan`, `/changes` | Inspect the reported plan or diffs |
| `/attach [path]`, `/detach` | Attach a file (or browse files), or remove pending attachments |
| `/attachments` | Inspect pending attachments or remove individual ones |
| `/thoughts`, `/copy`, `/editor` | Toggle reasoning, copy the last response, or compose in your editor |
| `/search`, `/history`, `/inspect`, `/restore` | Search transcript/history, inspect the selected entry, or restore a cleared draft |
| `/recover`, `/diagnostics` | Restore an interrupted submission for review, or inspect connection/error details |
| `/skills [query]`, `/skill [query]` | Browse installed skills and stage a loading request |
| `/paste-image` | Attach an image from the local desktop clipboard |
| `/cancel`, `/reconnect`, `/quit` | Cancel work, reconnect, or quit with active-turn confirmation |
| `/agent [command [args]]` | Browse agent commands, or send one directly to the harness |

Client commands run locally and do not become model prompts or transcript
messages. Existing attachments are preserved with their draft until you remove
them or send a prompt. File paths after `/attach` are literal and can
contain spaces; do not add shell quotes. Relative paths use the active workspace.
For example, `/mode plan` selects an agent-advertised mode and
`/attach docs/design notes.md` attaches that file without sending a prompt.

### Shared command aliases

These aliases have the same client-side meaning with every harness. Searching an
alias in `/commands` finds its canonical command; its preview and `/help` list
the aliases beside that command. Arguments and lifecycle checks are
identical to those of the canonical command.

| Command | Aliases |
| --- | --- |
| `/help` | `/?` |
| `/skills [query]` | `/skill [query]` |
| `/new` | `/clear`, `/reset`, `/new-chat` |
| `/sessions` | `/resume`, `/session`, `/conversations` |
| `/model [value]` | `/models [value]` |
| `/effort [value]` | `/reasoning-effort [value]` |
| `/mode [value]` | `/modes [value]` |
| `/harness` | `/backend` |
| `/settings` | `/config`, `/preferences`, `/prefs` |
| `/changes` | `/diff`, `/diffs` |
| `/attach [path]` | `/add [path]`, `/file [path]`, `/files [path]` |
| `/detach` | `/remove`, `/unattach` |
| `/thoughts` | `/thinking`, `/reasoning` |
| `/copy` | `/yank` |
| `/editor` | `/edit` |
| `/search` | `/find` |
| `/restore` | `/undo-clear` |
| `/inspect` | `/details` |
| `/auth` | `/login` |
| `/reconnect` | `/connect` |
| `/cancel` | `/stop`, `/abort` |
| `/quit` | `/exit`, `/q`, `/bye` |

`/clear` and `/reset` start a **new conversation**, not a display-only clear or
history deletion. `/resume` opens the session picker, not an automatic resume;
`/remove` clears pending attachments without deleting files. `/reasoning` toggles
reported thinking visibility; `/reasoning-effort` controls advertised effort.
Options, authentication, and session operations still depend on the agent's
advertised capabilities; aliases do not invent missing features.

Client aliases take precedence over identically named native commands, just like
canonical commands. Use `/agent clear`, `/agent resume`, or any other
`/agent <name> [args]` to bypass the client and preserve native harness semantics.
Native commands such as `/compact`, `/undo`, and `/permissions` are not mapped to
unrelated local actions; their behavior remains the harness's responsibility.

### Load a skill

Use `/skills` (or `/skills release` to filter) and select a skill with Enter.
Selection adds a **skill-loading request to your draft**, preserving existing
task text and attachments. Press Enter in the composer to send it. The UI does
not claim a skill is loaded before the harness processes that request.

The picker scans these standard local sources:

- Project `.agents/skills`, `.claude/skills`, and `.opencode/skills`, from the
  current workspace up to the nearest Git root (or filesystem root outside Git).
- Global `~/.agents/skills`, `~/.claude/skills`, and
  `$XDG_CONFIG_HOME/opencode/skills` (default `~/.config/opencode/skills`).

Each source can contain `<id>/SKILL.md` (also nested) or root-level `<id>.md`.
YAML `name` and `description` appear in the picker; IDs come from the directory
or filename. Within each source family, nearer project definitions override
global definitions; OpenCode sources take precedence over compatibility sources
for duplicate IDs.
The preview shows the selected source, document path, and base directory.

The staged request prefers the harness's native skill loader and respects its
permissions. If no native loader exists, it asks the agent to read the selected
document and resolve supporting files relative to its directory. A denied
native skill load must not be bypassed. Discovery never executes skill scripts
or sends skill contents to a model. Documents are limited to 128 KiB; nested
symlinks are not followed. Invalid documents produce warnings without hiding
other valid skills.

This is a portable **local-file picker**, not an ACP-native skill registry.
Built-in/remote catalogs, custom harness skill sources, native registration
precedence, and model access remain the harness's responsibility. Use
`/agent skills` to invoke a harness's own command instead.

Client command names take precedence over identically named agent commands.
Use `/agent plan` to send `/plan` to the harness instead of opening the client's
plan viewer. Other commands (for example `/compact`) are passed through unchanged,
even if the agent has not advertised them. Multiline input is always treated as
an agent prompt, not interpreted as a local command.

## Register another harness

Built-in registration is explicit in `internal/cli/registry.go`:

```go
registry.Register(opencode.Descriptor())
registry.Register(claude.Descriptor())
registry.Register(pi.Descriptor())
```

A descriptor provides an ID, name, executable/arguments, and adapter factory.
Standard ACP agents use `acpclient.New`; only harness-specific extensions need
additional adapter code. An adapter emits protocol-independent events and never
imports the TUI.

Alternatively, register a custom agent in `settings.json`:

```json
{
  "harness": "opencode",
  "mouse": true,
  "showThoughts": false,
  "vimNavigation": true,
  "compact": true,
  "keybindings": { "palette": "ctrl+p" },
  "agents": [
    {
      "id": "codex",
      "name": "Codex",
      "command": "codex-acp",
      "args": []
    }
  ]
}
```

Install custom bridges separately; native CLI feature parity is not assumed.
Claude and Pi do not need custom entries. Existing custom entries with the
`claude` or `pi` ID remain supported and retain their executable, arguments,
and environment.
Other duplicate registrations are rejected. No executable is downloaded
automatically.

## Storage and safety

Settings and drafts live under `$XDG_CONFIG_HOME/agents`, or
`os.UserConfigDir()/agents` when XDG_CONFIG_HOME is unset. Use `--config-dir` to
override. Files are atomically replaced with private permissions. The agent's
storage remains authoritative for session history. The demo does not persist its
conversations. Credentials remain with the harness.

The optional `mcpServers` settings field accepts ACP's standard session MCP
configuration. Server commands must be absolute paths and transports must be
supported by the connected agent. Do not put credentials in this file.

Client filesystem requests are scoped to per-session directory handles using
Go's `os.Root`; traversal and symlink escapes are refused. Text reads/writes and
attachments are limited to 8 MiB. Terminal callbacks implement create, output,
wait, kill, and release, with bounded retained output and process-group cleanup
on macOS/Linux. These checks do **not** sandbox local harnesses or shell commands.
Only run trusted agents in trusted workspaces.

Agent text is stripped of terminal control sequences before rendering. Commands
are launched as executable/argument arrays, not shell-concatenated strings
(except the external editor, explicitly selected by your own environment).

`--debug` captures agent stderr in `agent.log` in the settings directory. This
is opt-in because diagnostics may include sensitive content. Protocol bodies are
not recorded. Inspect and redact logs before sharing them.

## Architecture and checks

```text
internal/cli             composition and explicit registration
internal/harness         adapter contracts and neutral event types
internal/acpclient       stable ACP v1 SDK wrapper and normalization
internal/clientservices workspace files and managed command terminals
internal/app             controller and single-writer conversation reducer
internal/tui             interaction and focus management
internal/tui/transcript   cached Markdown, tools, and diff rendering
internal/tui/overlays     searchable selectors and validated forms
internal/storage         preferences, drafts, and recent-session references
```

```sh
make check
```

An optional live integration check validates initialization, session options,
listing, close, and replay against the installed OpenCode without model calls:

```sh
OPENCODE_INTEGRATION=1 go test ./internal/acpclient -run TestOpenCodeIntegration -v
```

The test creates and removes only its own empty session in a temporary workspace.

The Claude bridge has an opt-in check for initialization, authentication-method
discovery, model/mode/effort control updates, listing, and empty-session cleanup,
also without login changes or model calls. Validated with bridge `0.84.0`:

```sh
CLAUDE_INTEGRATION=1 go test ./internal/acpclient -run TestClaudeIntegration -v
```

Set `CLAUDE_ACP_COMMAND=/absolute/path/to/claude-agent-acp` to test a bridge that
is not on PATH.

Stable ACP v1 is intentional: ACP v2 is still draft. The pinned SDK does not
expose experimental `session/fork`. Remote ACP transports, native subagent
session trees, parallel background conversations, side-by-side diffs, and native
rewind/undo are not implemented. Interactive terminal emulation is also outside
the initial scope; ACP terminal callbacks capture managed command output.
