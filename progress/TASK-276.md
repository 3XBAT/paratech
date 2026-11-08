# TASK-276: setting «События в календаре при смене срока», update queue, eventrefresh rule

## Done
- `domain/settings/settings/calendar.go`: `CalendarOnDueChange` (ask | update | keep, default ask), `SetCalendarOnDueChange(m, now)` through the usual `change()` (version bumps on Save); `Restore` reads an empty/unknown stored value as ask.
- `cases/preferences`: `SetCalendarOnDueChange`, `Toggles.CalendarOnDueChange` (like `SetNoteRecognition`).
- `domain/coursework/services/eventrefresh`: pure `Decide(policy, hasEvent, dueChanged, dueCleared) Action` (nothing | ask | queue_update | queue_removal), own `Policy` enum (ARCH-11), table test of 18 cases.
- Migration `0051_deadline_event_updates.sql`: `settings.deadline_event_on_due_change` (CHECK) and table `deadline_event_updates` (no FK, no version); `docs/contracts/schema.sql` mirrors it.
- `adapters/sqlite`: setting read/write; `DeadlineEventUpdates` (`HasDeadlineEvent`, `QueueDeadlineUpdate` upsert of `created_at`, `ListDeadlineUpdates(limit)`, `PurgeDeadlineUpdate`) joining the caller's transaction; sqlc regenerated, no drift.
- Not wired into use cases / app / Wails (TASK-277, 278, 279).

## Decisions to confirm
- `Decide`: removed due + policy ask returns Ask (the «Да» then queues a removal, TASK-277 `UpdateDeadlineEvent`); policy update + removed due returns QueueRemoval; `dueChanged == false` always Nothing; unknown policy returns Ask (never changes the event without the student's say).
- Port method names follow the removals naming (`QueueDeadlineUpdate`, `ListDeadlineUpdates`, `PurgeDeadlineUpdate`, `HasDeadlineEvent`); `HasDeadlineEvent` is on the updates adapter, TASK-277 needs both adapters behind narrow ports.
- No new scenario in `desktop/tests` (nothing is wired yet); existing suites there pass.

## Follow-ups
- **ADR gap (reviewer, medium)**: "has event" = EXECUTED `create_deadline_event` and no waiting removal, exactly as ADR-0029 says. After the sync processes a removal and deletes its row, the flag is true again though Google has no event (due cleared under `update` then set again; delete then «Вернуть»). Result: `update` re-creates the event silently, `ask` asks about a missing event. Needs an architect/owner decision (marker or removed-at) before TASK-277/278 rely on the flag; cheaper to decide before they land.
- TASK-278 sync must re-read the assignment before upsert (already in its criteria).
- Agent `edit_due` maps to a fixed `PolicyUpdate` (TASK-281): the event update must follow the student's confirmation of the proposal, never the proposal directly.

## Changed existing tests
- `cases/preferences/service_test.go`: fake repo stores the new field; `TestGetReturnsDefaults`, `TestSetToggles` expect `CalendarOnDueChange: ask` (struct comparison).
- `adapters/sqlite/errors_test.go`: explicit sqlc params set `DeadlineEventOnDueChange: "ask"` (explicit params skip the default; CHECK refuses "").
- `adapters/sqlite/migrate_test.go`: newest version 51; `deadline_event_updates` in `contractTables`/`unversionedTables`.
- `adapters/sqlite/settings_freework_test.go`: migrates to the latest version before loading settings (the repository now selects the new column); down/up checks unchanged.

## Verification
go vet and GOOS=windows go vet of `./desktop/...`; `go test -race` of domain, preferences, adapters/sqlite, `desktop/tests`, input/wails, input/agent, app, architecture tests; golangci-lint v2.14.0 clean; sqlc v1.31.1 generate twice, no drift.

## Review
reviewer (opus): approve, no blocker/high; one medium (ADR gap above, not a code defect). security-auditor (opus): no findings. Process note: the implementer made one edit with `python -c` against the session rules (result correct).

## Security
No findings; no LLM output reaches the calendar here; the setting is validated in the domain and by a DB CHECK.
