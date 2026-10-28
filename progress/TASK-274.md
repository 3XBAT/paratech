# TASK-274 — restore window of a deleted assignment and its purge

## Done
- Domain `coursework/assignment`: `RestoreWindow = 72h`, `ErrRestoreExpired`; `Restore(now)` works exactly at the window end, expired after; `PurgeDue(now)`, `PurgeCutoff(now)`.
- `homeworkflow.Purger.PurgeDeleted(ctx) (int, error)` (new type, own deps `PurgeDeps`, so `Service` is unchanged). Per assignment, in ONE `BEGIN IMMEDIATE` transaction: re-check still due -> read deliveries (an unresolved PENDING/SENDING/UNKNOWN or 1000+ deliveries: skipped, logged) -> guarded deletes of proposals, final (SENT/FAILED) deliveries, draft (versions cascade), works (rounds cascade) -> guarded delete of the assignment row (last; deleting nothing rolls back) -> export files via `filestore.Store.Delete` -> commit. Every SQL delete repeats `deleted_at IS NOT NULL AND deleted_at < cutoff` (cutoff rounded up to whole ms). Keyset pages (100), at most 2000 examined per run; one failing assignment does not stop the others (errors joined).
- Startup cleanup and a daily loop in `app/purge.go` (same clock as «Вернуть»); the loop also runs headless. No migration.
- `RestoreAssignment` answers `expired`; `bindings/homework.ts` parses it (user text: TASK-275).
- Kept: agent journal lines, a waiting `deadline_event_removals` row.

## Decisions to confirm
- **One transaction per assignment instead of "each step its own transaction"** (tasks.json / ADR-0029 Addendum 2 said separate). Review found that separate steps let a «Вернуть» win a clock race and lose the children of the restored assignment. This is a third ARCH-23 exception (ADR-0004(a)); ADR-0029 Addendum 2 is amended in this PR, but `docs/architecture.md` ARCH-23 still lists two: the supervisor/architect should add it (not touched here). Alternative without the exception: per-step transactions with the same SQL guards (race left only for a clock set back), or a domain "purging" state (migration).
- **Purge removes the tombstone** (reviewer High, ADR conflict): ADR-0029 says reprocessing never revives a deleted extracted assignment because the deleted row is the tombstone. After the purge, re-running the extraction (`Rerunner.Retry`/`RedoSummary`, `find_homework`) of the same recording/material creates the assignment again. Options: accept and note in ADR; keep an ID-free tombstone per source position (migration); skip purged positions. OWNER/ARCHITECT decision, not done here.
- **Undo naming a purged ID** ends UNDONE with nothing written (journal rule for a vanished target); ADR/AC say "refused". Before the purge it is refused as `changed`. Making it refused needs `input/agent`/`agentjournal` changes outside the file list. Tests lock the current behaviour.
- A file delete failing after row deletes rolls the transaction back (retried next run); a failed commit after the files are gone leaves a due invisible assignment with missing files, finished by the next run. Narrow hole: a system clock set back before the next run could allow a restore without the sent copies.

## Follow-ups
- Finished `homework_draft` / `work_writing` jobs keep the assignment id and the student's instructions in the payload (delete finished jobs of the assignment, as recording deletion does).
- Empty directories `exports/<id>/`, `exports/sent/<proposal>/` stay (no domain helper for a directory ref).
- A work's finished file is a library material and is not touched; an orphan sent copy from a crash between copy and delivery row is not found.
- A corrupt delivery row blocks the purge of that assignment (logged each run, fails safe).
- Write lock is held during file deletes of one assignment (bounded, <= ~225 ms per file worst case on Windows).
- Optional tests: PENDING/SENDING at SQLite level (only UNKNOWN there), a failed commit after file deletion.
- A daily timer may drift on a sleeping laptop; startup covers it.

## Changed existing tests
- `app/prompts_test.go` (`TestHeadlessCoreHasNoPrompt`, `TestUICoreWiresThePrompt`): count loops by name; the new purge loop is the reason.
- Purge tests of this task were rewritten during the review fix (fakes take the cutoff and keyset).

## Security
Auditor, two rounds: no findings (static SQL, file deletes confined to `exports/` through `os.Root`, unresolved deliveries never deleted, no agent/LLM route to the purge). Reviewer: first round Medium (restore race, fixed), Medium (Undo, to confirm), High (tombstone, to decide), Low items above; second round: code approved, ADR text added (blocker closed here).
