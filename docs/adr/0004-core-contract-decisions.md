# ADR-0004: Core contract decisions (ports, schema, shared kernel)

- **Status:** Accepted
- **Source:** review of TASK-003 (use case ports, SQL schema draft, shared kernel)

## Context
TASK-003 declared the ports of the seven desktop use cases, the shared kernel types and the SQL schema draft. Its review found
places where the contract either needed a documented exception to `docs/architecture.md` or left a decision implicit that
later tasks (TASK-005, 008, 013, 018, 044) would otherwise re-decide differently. This ADR records those decisions in one place
so that doc comments in `ports.go` and `docs/contracts/schema.sql` can point to a durable reference.

## Decision

### a. Batch import of lessons is an exception to ARCH-23
`LessonRepository.ApplyChanges(upserts, deleted)` stores a batch of aggregates of the same type (lessons) in ONE transaction.
This is an allowed exception to the "one aggregate per transaction" rule of ARCH-23: the import preview is shown to the user as a
whole and confirmed as a whole, so a partial commit would leave a schedule the user never saw (PRD section 11.2, section 14
case 9, AC-16). The exception covers only same-type aggregates of one context written by one use case method. Everything that
follows the commit stays a separate, repeatable, idempotent step: in particular the calendar synchronization job is enqueued
after `ApplyChanges` returns (a crash between the two is repaired by the next enqueue, because pending lessons are found by
state, not by the job).

### b. `ArtifactRef` lives in `domain/shared`
`shared.ArtifactRef` (kind + relative path) is a shared-kernel exception to "keep `domain/shared` to typed IDs and base errors".
Reason: every context that owns a file (recording, transcript, model, export) must name it, ports of several use cases take it,
and ARCH-11 forbids contexts importing each other. A per-context type would force conversions at every boundary and duplicate
the path-safety rules. It is a pure value object with no I/O. The `<kind>/<path>` form is an identifier convention of this
project defined by TASK-006 (PRD section 5.2 only requires that large artifacts stay outside SQLite and go through the Storage
Manager). Segments must be safe Windows file names (no `\ / : * ? < > | "`, no trailing dot or space, no reserved device names
such as CON or NUL.txt); identifier `Validate` methods apply the same rules because ids are used as file-name segments.

### c. Delivery statuses
The PRD names only `UNKNOWN` (sections 10 and 14). The contract uses `PENDING`, `SENDING`, `SENT`, `FAILED`, `UNKNOWN`:
- `SENDING` is persisted BEFORE the message is handed to the provider. A crash during the attempt therefore leaves `SENDING`,
  which startup recovery turns into `UNKNOWN`; the system never guesses and never blindly retries (hard rule 7, AC-15).
- A transient failure (`delivery.ErrTransient`, definitely not accepted) returns `SENDING` to `PENDING` for a later attempt.
- `delivery.ErrRejected` leads to `FAILED`; an ambiguous result leads to `UNKNOWN`, which is reconciled (`EmailAdapter.Reconcile`,
  best effort because SMTP cannot look a message up) or resolved by the user ("mark as sent" or "send again").
- The full transition table is owned by TASK-044; this list is the starting point and needs PRD confirmation.

### d. Optimistic concurrency: version compare-and-swap for `Save`
Every aggregate that has a `Save` port carries an integer `version`, stored in a `version INTEGER NOT NULL DEFAULT 0` column of
its table. `Save` is a compare-and-swap: the update succeeds only if the stored version equals the version the aggregate was
loaded with, then increments it; otherwise nothing is written and the error wraps `shared.ErrConflict`. The aggregate carries the
version (`Restore` takes it, a `Version()` accessor returns it, added by the owning task); the use case treats a conflict as
"reload and decide again". This prevents lost updates between the UI, background jobs and import.

Job ownership follows from it: from `Claim` on, a job is written only by the `jobrunner` use case. The claim itself is done by
the adapter by restoring the aggregate, calling the domain transition QUEUED to RUNNING and saving with a guarded
`UPDATE ... WHERE id = ? AND status = 'QUEUED' AND version = ?` (never a raw status update; ARCH-14, ARCH-19). Handlers do not
receive a job repository; they get a read-only job snapshot and a `report func(ctx, job.Progress) error` callback, and the runner
applies and saves progress. Which jobs a worker may claim is the domain value `job.ClaimFilter` (allowed types, heavy types,
whether heavy work is allowed on the current power source), with the "Run now" override carried by the job (`run_now`).

### e. Soft delete of lessons
Removing a lesson from the schedule marks it deleted (`deleted_at`) instead of deleting the row, so that the calendar
synchronization can still delete the Google event of a lesson the user no longer has (PRD section 11.3, AC-16). Deleted lessons
are invisible to `Get` and `ListStartingBetween`, visible to `ListPendingCalendarSync`, and removed for good by
`LessonRepository.PurgeDeleted` after the event deletion is confirmed. The event id is derived deterministically from the lesson id
by the calendar adapter, so no event id column exists. Whether a lesson is out of date in the calendar is decided with revision
counters (`content_revision`, `calendar_synced_revision`) rather than timestamps; the lesson aggregate exposes the same predicate.
`source_uid` is unique among live rows only, so re-importing a deleted occurrence creates a new lesson.

### f. Aggregates expose only `ID()` and `Status()` until their owning tasks arrive
The TASK-003 aggregates are skeletons so that ports can name the types. They deliberately have no `New…`/`Restore…` factories,
transitions or other accessors yet (ARCH-14); the owning tasks add them (jobs TASK-008, recording TASK-013, lesson TASK-018,
delivery TASK-044, and so on) together with their tests. Adapters cannot construct aggregates before then, which is intended:
TASK-005 and later adapters are written against the factories their owning tasks define.

## Consequences
- `docs/architecture.md` ARCH-23 points here for the one exception (a).
- TASK-005 turns `docs/contracts/schema.sql` into goose migrations and must execute them against SQLite in its tests (the contract
  test in `desktop/tests` has no SQL engine). It implements the version guard and the guarded claim in the sqlite adapter.
- TASK-008, TASK-013, TASK-018, TASK-044 must add `Version()`/`Restore` parameters and keep the status tables in sync with the
  schema CHECK lists.
- Changing any of (a) to (f) needs a new ADR.
