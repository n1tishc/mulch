# Live session API

`mulch serve` exposes the viewer and JSON API on `http://127.0.0.1:4141` by default. Live controls are enabled when `MULCH_PROVIDER_API_KEY` is available; recorded sessions remain readable without it.

| Operation | Request | Success |
| --- | --- | --- |
| Start | `POST /api/sessions` with `{"task":"...","opts":{"workdir":"."}}` | `202 {"id":"..."}` |
| Steer next turn | `POST /api/sessions/{id}/steer` with `{"text":"..."}` | `202 {"id":"..."}` |
| Branch and continue | `POST /api/sessions/{id}/branch` with `{"at":42}` | `202 {"id":"child-id"}` |
| Cancel | `DELETE /api/sessions/{id}` | `202 {"id":"..."}` |

Invalid JSON or fields return `400`, unavailable live controls return `503`, and a rejected state transition returns `409`.

Connect to `GET /ws/sessions/{id}?from=N` for JSON-encoded committed events. The stream first reads durable events beginning at sequence `N`, then follows new commits in sequence order. Reconnect with the last received sequence plus one. A completed session can still be replayed from durable history.

`mulch run` probes `MULCH_DAEMON_URL` (default `http://127.0.0.1:4141`). It submits the task to a healthy daemon and prints the session ID; when no healthy daemon is present, it retains the standalone behavior.
