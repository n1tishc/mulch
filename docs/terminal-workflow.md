# Terminal coding workflow

Mulch opens an interactive terminal interface when launched without a subcommand. It combines the agent/tool loop and SQLite sessions with an editable prompt, command menu, transcript, and execution status. Run `mulch web` for the browser coding workspace, or `/inspect` to view the current terminal conversation in a read-only browser inspector.

## Start

From the Mulch checkout:

```sh
make ui
go build -o dist/mulch ./cmd/mulch
./dist/mulch --workdir /absolute/path/to/project
```

Set `MULCH_PROVIDER_API_KEY`, `MULCH_PROVIDER_BASE_URL`, and `MULCH_MODEL` as described in the README, or use `.env` in the launch directory. The interface uses this configuration; there is no in-app provider login flow. `mulch chat` launches the same interface explicitly. Once installed on PATH, simply run `mulch` from a project directory.

Describe a task, inspect the streamed response and tool results, then send a follow-up in the same session. While Mulch works, you can edit the next message; sending it queues it for after the current task. The queue is in memory. Esc/Ctrl+C cancels current work and clears queued messages. It does not undo file changes already made by tools.

## Keyboard controls

| Key | Action |
|---|---|
| Enter | Send a message or execute the selected slash command |
| Alt+Enter / Ctrl+J | Insert a newline |
| Paste | Insert text without automatically sending it |
| Left / Right, Home / End | Move the editor cursor |
| Ctrl+A / Ctrl+E | Beginning / end of the prompt |
| Ctrl+U / Ctrl+K | Delete before / after the cursor |
| Up / Down | Recall prompt history; select commands when the menu is open |
| Tab | Complete the selected slash command |
| PgUp / PgDn | Scroll the transcript |
| Esc / Ctrl+C | Cancel work and clear queued follow-ups; clear editor when idle |
| Ctrl+D | Exit with an empty editor; cancel and exit if working |

Type `/` to browse commands, or type a prefix to filter. Terminal history is local to the open application; saved conversation messages live in SQLite and are restored when reopening a session.

## Commands

| Command | Behavior |
|---|---|
| `/help` | List commands and controls |
| `/new` or `/clear` | Start a fresh conversation without deleting previous history |
| `/sessions` | List saved session IDs, labels, statuses, and tasks |
| `/resume ID` | Switch to a saved session and show its conversation |
| `/session` | Show the current ID and database |
| `/status` | Show model, workspace, repair mode, recorded health, incomplete scoring, and primary-agent usage |
| `/model NAME` | Start a fresh session with that model ID; `/model` shows the current model |
| `/mode plain` | Disable scoring and repair for subsequent tasks |
| `/mode control` | Score without interventions |
| `/mode intervention` | Enable the repair ladder |
| `/mode race` | Enable experimental repair comparison |
| `/rename NAME` | Label the current saved session |
| `/history` | Show recorded user/assistant messages |
| `/diff` | Show tracked changes relative to Git HEAD and untracked names; requires an initial commit |
| `/inspect` | Open the current session in a read-only browser; terminal retains control, and exiting the terminal closes this viewer |
| `/web` | Open the live execution tree in the browser; alias for `/inspect`, also available mid-task |
| `/cancel` | Cancel a running task in the terminal interface |
| `/exit` or `/quit` | Leave, cancelling current work first if necessary |

Session-changing commands wait until the current task is finished or cancelled. Unknown commands are rejected locally. Commands such as `/status`, `/diff`, and `/sessions` do not ask the model to produce their answers. `/diff` reflects all workspace changes, including changes made before Mulch started; it is not an attribution of every change to the agent.

Reopen a session from the shell with:

```sh
./dist/mulch --resume SESSION_ID
# When using a custom database:
./dist/mulch --db /path/to/events.db --resume SESSION_ID
```

Saved model and workspace are restored. Repair mode/policy are launch configuration: use the same flags or `/mode` after reopening. A session is saved after its first model task begins. Exiting an unused session does not create an empty conversation. The application refuses to reopen a session still marked running.

## Runtime and limits

The terminal uses the same runtime as dashboard/evaluation sessions. Tool output previews and long transcripts are bounded on screen; committed session events remain replayable from SQLite. `/status` labels its token count as primary-agent usage: auxiliary judge, summary, and candidate costs are not included in that display. Recorded health can include reused values; incomplete scoring is surfaced explicitly.

Each task still has the runtime's turn limit. Interactive tasks do not use the eval campaign's token-spend guard. Idle UI and local slash commands make no model requests. Redirected input/output and `TERM=dumb` use line mode; multiline editing, command menus, queuing, and live screen controls require a terminal.

This interface does not yet include provider OAuth, selectable approval policies, file-mention attachment/completion, plugins/MCP menus, or automatic code rollback. No commands pretend those capabilities exist. The pilot's scorer reliability and repair-routing findings remain unresolved by the UI work.

## Design references

The design draws on documented interactive workflows: Codex's local session/status commands, Claude Code's editor and history controls, OpenCode's TUI command flow, and Pi's terminal session approach. These references inform the interaction pattern; Mulch is not a feature-compatible replacement for all four tools. Sources checked 2026-09-08: [Codex commands](https://developers.openai.com/codex/cli/slash-commands/), [Claude Code interactive mode](https://code.claude.com/docs/en/interactive-mode), [OpenCode TUI](https://opencode.ai/docs/tui/), [Pi documentation](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/README.md).

The terminal implementation uses pinned Bubble Tea and Lip Gloss dependencies. Deterministic tests cover editor/paste behavior, command dispatch, saved-session continuity, model changes, and cancellation; those checks do not establish a live-model correctness advantage.

## Browser trace and saved setup

`/web` aliases `/inspect` and works during a task. `/web --no-open` prints the URL. The browser opens the recorded execution tree for this conversation; the terminal retains execution control. Exiting the terminal closes its attached viewer.

Run `mulch config` before launching to see setup commands. Save `base-url` and `model` with `mulch config set KEY VALUE`; save the key with `mulch config set api-key` (hidden prompt). `mulch config show` redacts secrets. Restart the interface after configuration changes.

The terminal uses a compact header, bounded reading width, a separate prompt and status area, and scroll anchoring while reading older output. The cursor highlights the existing character instead of inserting a visual space into the prompt. Use PgUp/PgDn for history, Alt+Enter for multiline input, and `/web` for the detailed execution tree.
