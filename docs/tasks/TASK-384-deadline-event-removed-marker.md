# TASK-384: Marker of an executed deadline event removal (migration 0052, sqlite)

- **Agent:** go-core (opus)
- **Label:** core
- **Depends on:** TASK-276 (done)
- **PRD sections / AC:** §11.3; ADR-0029 «Дополнение 08.11: состояние события срока»
- **Агент:** только ядро (no tool, no binding)
- **Branch:** ralph/<date>-task-384
- Id: next free at the time tasks.json gets it; TASK-277 and TASK-278 depend on it.

## Goal
Make «the assignment has a deadline event» false after the calendar sync executed a removal, until a new
`create_deadline_event` is executed, so a re-set due never re-creates or asks about a missing event.

## Allowed to change
- `paratech/desktop/pkg/migrations/0052_deadline_events_removed.sql` (new; next free number at push time)
- `docs/contracts/schema.sql` (mirror)
- `paratech/desktop/internal/adapters/sqlite/queries/deadline_events.sql`, `queries/recording_deletions.sql`,
  `queries/assignments.sql` (only the marker delete of the purge), `sqlcgen/*`
- `paratech/desktop/internal/adapters/sqlite/deadlineevents*.go`, `recordingdeletions*.go`,
  `assignments_purge*.go` (only the marker delete), `migrate_test.go`
- `paratech/desktop/tests/schema_test.go`, `paratech/desktop/tests/*` (+ tests)

## Out of scope
- Use cases (`cases/homeworkflow`, `cases/schedulesync`) and app wiring: TASK-277, TASK-278.
- `eventrefresh.Decide` (unchanged).

## Acceptance criteria
- [ ] Migration: `deadline_events_removed (assignment_id TEXT PRIMARY KEY CHECK (length(assignment_id) > 0),
      removed_at INTEGER NOT NULL)`, no foreign key, no version; Down drops it; `schema.sql` mirrors it verbatim;
      `migrate_test` newest version 52, the table in `contractTables` and `unversionedTables`.
- [ ] `AssignmentHasDeadlineEvent`: EXECUTED `create_deadline_event` AND no `deadline_event_removals` row AND no
      `deadline_events_removed` row (one statement).
- [ ] Adapter `MarkDeadlineEventRemoved(ctx, id, at)`: in one transaction (the caller's or its own) deletes the
      removal row and upserts the marker only when the assignment row exists; repeat is safe; a missing removal row is
      not an error.
- [ ] Adapter `ClearDeadlineEventRemoved(ctx, id)`: deletes the marker in the caller's transaction; missing is not
      an error.
- [ ] The assignment purge (ADR-0029 addendum 2) deletes the marker in its transaction.
- [ ] Tests (sqlite): every transition of the ADR state table — none, present, removing, removed, removed → present
      after a new EXECUTED proposal and the clear; mark for a hard-deleted assignment writes nothing; purge removes the
      marker; sqlc generate twice without drift.
- [ ] `go vet`, `GOOS=windows go vet`, `go test -race` of `adapters/sqlite`, `desktop/tests`, architecture test;
      golangci-lint v2.14.0.

## Notes
Not in parallel with another task that adds a migration (shared `schema.sql`, `migrate_test`). No backfill (ADR).
