# Live session API

`mulch web` opens the embedded coding workspace on an available loopback port. `mulch serve` exposes the same interface and JSON API on `http://127.0.0.1:4141` by default. Live controls are enabled when `MULCH_PROVIDER_API_KEY` is available; recorded sessions remain readable without it. Requests with bodies use `Content-Type: application/json`; cross-origin mutations are rejected.

| Operation | Request | Success |
| --- | --- | --- |
| Configuration | `GET /api/config` | Workspace, model, mode, policy, readiness; no credentials |
| Session capabilities | `GET /api/sessions/{id}` | Saved session plus owner and can_resume/can_stop/can_steer/can_rename |
| Start | `POST /api/sessions` with `{"task":"...","request_id":"unique-id","opts":{"workdir":"/absolute/path"}}` | `202 {"id":"..."}` |
| Follow-up | `POST /api/sessions/{id}/resume` with `{"text":"...","request_id":"unique-id"}` | `202 {"id":"same-id"}` |
| Rename | `PATCH /api/sessions/{id}` with `{"label":"..."}` | `202 {"id":"..."}` |
| Steer next turn | `POST /api/sessions/{id}/steer` with `{"text":"..."}` | `202 {"id":"..."}` |
| Branch and continue | `POST /api/sessions/{id}/branch` with `{"at":42}` | `202 {"id":"child-id"}` |
| Cancel | `DELETE /api/sessions/{id}` | `202 {"id":"..."}` |

Invalid JSON or fields return `400`, unavailable live controls return `503`, and a rejected state transition returns `409`.

Start/resume request IDs persist in SQLite. Repeating the same ID and content returns its original acceptance; changing content under the same ID returns `409`. An interrupted reservation without a settled response stays indeterminate, preventing silent duplicate execution. Omitting the ID retains compatibility for older callers but provides no deduplication. Omitted start workspace uses the launch workspace. Resume uses the saved workspace and model. Running sessions cannot be resumed concurrently; execution leases also guard across processes.

Connect to `GET /ws/sessions/{id}?from=N` for JSON-encoded committed events. The stream first reads durable events beginning at sequence `N`, then follows new commits in sequence order. Reconnect with the last received sequence plus one. A completed session can still be replayed from durable history.

Add `&follow=1` to keep following after session end and observe later tasks in the same session. It also tails SQLite commits from a standalone terminal process. Backfill missing sequences from `GET /api/sessions/{id}/events?from=N`; deduplicate by session ID and sequence. WebSocket origin checks remain enabled. Browser disconnection does not cancel daemon work.

`run.config` records effective execution configuration. New `score.health` events include freshness per dimension, `score.partial` records failure categories, and `intervene.fire` includes exact `score_seq`/`context_seq` where applicable. Legacy missing metadata must remain unknown, not inferred from today's configuration or duplicate payload text.

`mulch run` probes `MULCH_DAEMON_URL` (default `http://127.0.0.1:4141`). It submits the task to a healthy daemon and prints the session ID; when no healthy daemon is present, it retains the standalone behavior.

Dashboard-started and resumed sessions use the shared health-scoring and intervention runtime. `mulch serve --race` enables candidate repair comparison; `--no-intervene` retains scoring only; `--policy path.json` selects a policy. These are server-wide settings. The default enables the intervention ladder.

### Workspace organization and history deletion

- `GET /api/workspaces`: remembered and session-derived directory paths.
- `POST /api/workspaces {"path":"/absolute/project"}`: validate and remember an existing directory; returns its resolved path.
- `DELETE /api/sessions/{id}/history`: permanently delete a stopped conversation and all descendant branches/events. Returns `{"deleted":true}`; rejects any running or leased descendant with 409. Leaves project files untouched. Existing `DELETE /api/sessions/{id}` still means cancel, for compatibility.
- `/api/config.can_manage` controls workspace and history mutations independently of provider readiness. Attached terminal viewers have it disabled. Session details expose `can_delete`; the deletion transaction also checks descendants.

Deleted IDs remain tombstoned, and durable request receipts are retained so retries cannot execute deleted work again. Workspace changes and deletions use the same-origin mutation protections.
