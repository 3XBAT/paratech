# ADR-0001: Technology stack

- **Status:** Accepted
- **Source:** PRD v0.4 §4.3-4.4 and the architecture discussion of the same date

## Decision
Go everywhere (desktop and Hub). Desktop shell: Wails v3 (beta, accepted risk) with a React + TypeScript frontend.
SQLite (`modernc.org/sqlite`, `sqlc`, `goose`), custom SQLite-backed job queue. Local STT: whisper.cpp CLI sidecar, one process per job, CPU-only.
Client-Hub contract: Connect-RPC (protobuf, `buf`). Hub: Go, `chi`, Postgres (`pgx`). Calendar: Google only. Email: SMTP (proposed).
MVP has no user login: Hub access via per-tester API keys.

## Consequences
- No CGO in the desktop binary unless a later ADR (audio capture spike S-002) decides otherwise.
- Apple Calendar is not integrated; users view events through their Google account.
- No cloud STT, no auto-update, no hardware-specific optimization in MVP.
