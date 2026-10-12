# ADR-0004: Core contract decisions (ports, schema, shared kernel)

- **Status:** Accepted
- **Source:** review of TASK-003 (use case ports, SQL schema draft, shared kernel)

## Context
TASK-003 declared the ports of the seven desktop use cases, the shared kernel types and the SQL schema draft. Its review found
places where the contract either needed a documented exception to `docs/architecture.md` or left a decision implicit that
later tasks (TASK-005, 008, 013, 018, 044) would otherwise re-decide differently. This ADR records those decisions in one place
so that doc comments in `ports.go` and `docs/contracts/schema.sql` can point to a durable reference.

## Decision

### a. Two explicit exceptions to ARCH-23 (one aggregate per transaction)
ARCH-23 says a transaction changes one aggregate. Exactly two use case methods may change more, and the contract names them,
so a third one needs a new ADR.

1. **Batch import of lessons.** `LessonRepository.ApplyChanges(upserts, deleted)` stores a batch of aggregates of the same type
   (lessons) in ONE transaction. The import preview is shown to the user as a whole and confirmed as a whole, so a partial
   commit would leave a schedule the user never saw (PRD section 11.2, section 14 case 9, AC-16). The exception covers only
   same-type aggregates of one context written by one use case method. Everything that follows the commit stays a separate,
   repeatable, idempotent step: in particular the calendar synchronization job is enqueued after `ApplyChanges` returns (a crash
   between the two is repaired by the next enqueue, because pending lessons are found by state, not by the job).
2. **Deleting a recording together with its transcripts.** `RecordingTranscripts.DeleteByRecording` and
   `RecordingRepository.Delete` run in one `Tx.Do` (PRD section 13.1: deleting a recording deletes its transcripts). This is a
   delete of a parent with its dependents: there is no invariant that spans both aggregates, and a partial commit would leave
   transcripts without a recording or a recording that claims transcripts it no longer has. The flow is: (i) the recording
   aggregate decides FIRST whether deletion is allowed (it refuses in STARTING, RECORDING, STOPPING and FINALIZING; the method is
   added by TASK-013) and no file is touched if it refuses; (ii) the files are deleted through the artifact store, idempotently,
   outside any transaction because it is file I/O; (iii) inside the transaction the recording is reloaded, the aggregate is asked
   again (the guard is re-checked), then both deletes run, and `RecordingRepository.Delete(r)` is version-guarded like `Save`.
   The small window between (i) and (iii) can only be opened by a status change after (i); the failure then rolls back the rows
   and a repeated deletion cleans up whatever file removal already happened.

Changing the settings default model when a model is removed is NOT such an exception, see (g).

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
its table. One protocol, used by every repository:

- **The aggregate knows its version.** Each aggregate has `Version() int64` and `Persisted(version int64)`. `Restore` takes the
  stored version (added by the owning task). `Persisted` is a plain setter with no logic; it is called by the repository
  adapter only, after a successful write, and never by use cases, handlers or the domain itself. This keeps persistence logic
  out of aggregates (ARCH-14) while letting the in-memory instance follow its own writes.
- **`Save(ctx, agg) error`.** New versus stale is told by the version alone: `Version() == 0` means the aggregate was never
  stored and is INSERTed with version 1 (an existing row with the same id means someone else created it first, so the result is
  `shared.ErrVersionConflict`). `Version() >= 1` means it was stored: the adapter runs `UPDATE ... SET version = version + 1
  WHERE id = ? AND version = Version()`; zero rows changed means the row changed or was purged meanwhile and the result is
  `shared.ErrVersionConflict`. A stored aggregate whose row is gone is never re-inserted. No row is ever stored with version 0.
  On success the adapter calls `agg.Persisted(newVersion)`, so the same instance can be saved again: the runner saves one
  `*job.Job` after every progress report and once more for the final transition, and the calendar synchronization marks a
  lesson synchronized and saves the same instance, without reloading in between. `Save` returns only an error because the new
  version is recorded on the aggregate; there is exactly one place where it is set. `Persisted` is applied inside `Save`, before the
  outermost commit: when the enclosing `Do` returns an error (rollback or failed commit), the aggregates saved inside it must be
  reloaded, not reused. If that proves error-prone, TASK-007/008 may add an on-commit hook that applies `Persisted` after the commit.
- **Other writers follow the same rule.** `JobRepository.Enqueue` stores a version-0 job and calls `Persisted(1)` when it was
  created (not when the idempotency key already existed); `LessonRepository.ApplyChanges` applies the rule to each lesson and
  calls `Persisted` on each after the commit; `RecordingRepository.Delete(r)` deletes `WHERE id = ? AND version = r.Version()`.
  The settings row is a singleton: `Load` without a row returns the defaults with version 0 and the first `Save` inserts it.
- **Errors.** A stale version is `shared.ErrVersionConflict`; a uniqueness violation (the delivery idempotency key, the model
  name and version) is `shared.ErrDuplicate`; both wrap the general `shared.ErrConflict`, so `errors.Is(err, shared.ErrConflict)`
  holds for either. Use cases check the narrow sentinel: on a version conflict they reload and decide again, on a duplicate they
  load the existing row (for a delivery, `GetByIdempotencyKey`). `DeliveryRepository.Save` returns the first for a stale
  delivery and the second for a second delivery with an existing idempotency key. A plain `ErrConflict` is for other state
  preconditions, such as a foreign key that still references the row or purging a lesson that is not soft-deleted.

The use case treats a conflict as "reload and decide again". This prevents lost updates between the UI, background jobs and import.

Job ownership follows from it: from `Claim` on, a job is written only by the `jobrunner` use case. The claim itself is done by
the adapter by restoring the aggregate, calling the domain transition QUEUED to RUNNING and saving with a guarded
`UPDATE ... WHERE id = ? AND status = 'QUEUED' AND version = ?` (never a raw status update; ARCH-14, ARCH-19); the returned job
carries the new version. Handlers do not receive a job repository or the aggregate: they get `job.Snapshot`, an immutable value
copy of the job with accessors only, and a `report func(ctx, job.Progress) error` callback, and the runner applies and saves
progress. Which jobs a worker may claim is the domain value `job.ClaimFilter` (allowed types, heavy types, whether heavy work is
allowed on the current power source), with the "Run now" override carried by the job (`run_now`).

### e. Soft delete of lessons
Removing a lesson from the schedule marks it deleted (`deleted_at`) instead of deleting the row, so that the calendar
synchronization can still delete the Google event of a lesson the user no longer has (PRD section 11.3, AC-16). Deleted lessons
are invisible to `Get` and `ListStartingBetween`, visible to `ListPendingCalendarSync`, and removed for good by
`LessonRepository.PurgeDeleted` after the event deletion is confirmed. The event id is derived deterministically from the lesson id
by the calendar adapter, so no event id column exists. Whether a lesson is out of date in the calendar is decided with revision
counters (`content_revision`, `calendar_synced_revision`) rather than timestamps; the lesson aggregate exposes the same predicate.
`source_uid` is unique among live rows only, so re-importing a deleted occurrence creates a new lesson.

### f. Aggregates expose only identity, status and version until their owning tasks arrive
The TASK-003 aggregates are skeletons so that ports can name the types. They deliberately have no `New`/`Restore` factories,
transitions or other accessors yet (ARCH-14), apart from `Version()`/`Persisted()` of (d); the owning tasks add the rest
(jobs TASK-008, recording TASK-013, lesson TASK-018, delivery TASK-044, and so on) together with their tests. Adapters cannot
construct aggregates before then, which is intended: TASK-005 and later adapters are written against the factories their
owning tasks define.

Status classification must have one source. `job.Status.IsTerminal` (and so `job.ActiveStatuses`) is a classification of the
enumeration today. When TASK-008 adds the transition table (ARCH-19) it must derive one from the other (a terminal status is one
with no outgoing transition) and keep a test that they agree; likewise for the recording and delivery tables (TASK-013, TASK-044).

### g. Removing the default STT model
`settings.default_stt_model_id` references `stt_models` with `ON DELETE RESTRICT` (not SET NULL), because the default is state of
the versioned settings aggregate and the database must not change it behind the aggregate's version. To remove a model that is
the default, the `modelcatalog` use case first clears the default through the settings aggregate and `SettingsRepository.Save`,
as its own transaction (one aggregate per transaction, ARCH-23), then runs the pin check and `ModelRepository.Delete` in one
`Tx.Do`. Both steps are repeatable. If the user selects the model as default again in between, the database rejects the delete;
the adapter maps the foreign key violation to an error wrapping `shared.ErrConflict` and the use case retries or reports that the
model is in use. No PRD flow needs the database to clear the default (PRD section 8: select, install, remove). References that
only name a model as a copy of identity (`transcripts.model_id`, `lesson_id`, `transcript_id` on other tables) stay
`ON DELETE SET NULL`: the rows keep the pinned model name and version (AC-08), and a stale save that still points at a removed
model fails the foreign key check and surfaces as `shared.ErrConflict`.

## Consequences
- `docs/architecture.md` ARCH-23 points here for the two exceptions (a).
- TASK-005 turns `docs/contracts/schema.sql` into goose migrations and must execute them against SQLite in its tests (the contract
  test in `desktop/tests` has no SQL engine). It provides the `VersionCAS`, `DuplicateAsVersionConflict` and `IsPrimaryKeyViolation` helpers in the sqlite adapter; the guarded claim
  belongs to TASK-008 and every repository write must run through `DB.Do`.
- TASK-008, TASK-013, TASK-018, TASK-044 (and the owners of the assignment, transcript, model and settings aggregates) make
  `Restore` take the stored version, keep `Version()`/`Persisted()` as they are, and keep the status tables in sync with the
  schema CHECK lists. The contract test `desktop/tests/save_contract_test.go` fails if a storing port takes a type without them.
- Changing any of (a) to (g) needs a new ADR.
