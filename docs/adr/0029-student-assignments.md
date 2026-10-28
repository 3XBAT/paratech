# ADR-0029: Assignments the student adds (by hand or through the agent)

- **Status:** Accepted
- **Date:** 2026-10-26
- **Source:** owner 2026-10-26 («заводи и запускай»): «была такая-то пара, нужно сделать вот это к такому числу»,
  «завтра в 9 скинуть проверочную Иванову»; ADR-0004, ADR-0021, ADR-0022, ADR-0023, ADR-0024. Tasks TASK-215…220.

## Context
Assignments come only from extraction (transcript post-processing, `find_homework` on a material): `assignment.NewExtracted`
needs an `Origin` (transcript or material + index). The agent has no `create_assignment` (catalog 4) and the UI has no
button. The student cannot record a task that was said in class but not recorded, or a reminder to themself.

## Decision
### Domain (`coursework/assignment`)
- New constructor `NewByStudent(StudentSpec, now)`: ID, optional lesson, `Details` (title, note as description, due),
  optional subject name. Stored with `source = -1`, `prompt_version = 0`, no transcript/material, `user_edited = 1`
  (reprocessing never touches it), the subject as the student's correction (`user_subject`, teacher empty).
  `ByStudent()` = not extracted (`source == -1`). **No migration**: the `assignments` table already holds this.
- Extra rules for a student assignment, in the domain: title 1…200 runes one line (as `NewDetails`), note ≤ 2000 runes;
  no control characters except line breaks in the note, no bidi override/isolate characters (U+202A–202E,
  U+2066–2069) in either; due, when given, within `now − 31 days … now + 2 years` (`ErrInvalidDetails` otherwise).
  A date without a time is 20:00 local of that day (as `edit_due`; was 23:59 until addendum 3); the location is passed
  in, the domain reads no clock.
- The subject is a library subject ID resolved by the use case to its current name (ADR-0024 resolves names back at
  read time, so «Сегодня», «Задания», the subject page and `list_assignments` show it with no reader change). No
  subject and no lesson: «Без предмета». A lesson with no subject argument: the subject of the lesson, as for extracted
  ones. A subject argument wins over the lesson's (it is the student's word).
- Teacher names, people and places stay plain text of the title or note. They are never resolved to an address and
  never fill the teacher correction.

### Use case (`cases/homeworkflow`)
- `CreateAssignment(ctx, NewAssignment)` and `RemoveCreatedAssignment(ctx, id, expectedVersion)`.
- Idempotency without a new column: the caller may pass the ID. The agent derives it from (thread, turn, call_id), the
  UI from a per-form request token, both hashed to a valid `AssignmentID`. An insert of an existing ID returns the
  stored one when it is a student assignment with the same details, else `shared.ErrDuplicate`.
- `RemoveCreatedAssignment` removes only a student assignment still at `expectedVersion` and not in use: no draft,
  delivery, turnkey work, or proposal other than declined/rejected (new repository method `DeleteUnused`, one
  transaction; `works` cascade on delete, so it must be checked, not left to the FK). Otherwise `assignment.ErrInUse` /
  `shared.ErrVersionConflict`.
- Subject names come through a port declared in `homeworkflow`, implemented by `library.Service` (wired in `app`).

### Agent (catalog after TASK-200's version, expected 6)
- Tool `create_assignment`, class **U**, not paid: `title` (label, required), `subject` (id, optional), `lesson` (id,
  optional), `due` (date, optional), `time` (new type `time`, `HH:MM` 24 h, optional, only with `due`), `note` (query,
  optional). Every ID passes the typed provenance check. No address, path or URL field (ADR-0022).
- Undo: compensation `remove_assignment` = `RemoveCreatedAssignment` at the version the action left; a student edit, a
  draft, a work or a calendar proposal since → `UNDO_REFUSED` (`changed`). After a crash, PENDING settles by the
  pre-computed ID: present → `DONE`, absent → `FAILED`.
- `create_assignment` creates an assignment ID for later plan steps (`createdKinds`), so a plan «добавить задание →
  предложить в календарь» works. The calendar stays the separate `propose_deadline` card with «Да»; it is offered only
  when the student asked for the calendar.
- Result to the planner: the new assignment ID only (`WriteRow`). The title is never sent back. The panel line shows
  the assignment from local data (title, subject, due) with a link to it.

### ADR-0022 and ADR-0021 deltas
- The planner **writes** an assignment title and note only here, and only as the student's own words from the request
  (`label`/`query`); it still never **reads** a title (ADR-0022 «Planner never sees» stands).
- New compensation kind and U tool per the ADR-0021 criterion (local SQLite, typed compensation).

## Consequences
- Hub before desktop: the desktop on the new catalog needs a Hub that serves it.
- Plan steps must store the `time` type; the Hub schema gets the `time` pattern.
- A future subject rename must keep the old name as an alias, or student assignments lose their subject.

## Open (questions to the owner)
Answered 2026-10-27: 1 — «Удалить» for every assignment (addendum below); 2 — no notification now (deferred);
4 — in a request without a subject the agent asks (addendum below); 5 — yes, no date means no deadline and no question;
3 — the due limits stay: a month back (31 days) … two years ahead; a date without a time was 23:59 local, changed to
20:00 by addendum 3.

## Addendum (2026-10-27): delete and edit any assignment, ask for the subject
Source: owner 2026-10-27. Tasks TASK-228…234.

### «Удалить» for every assignment (soft delete)
- Any assignment — extracted from a recording (Hub `EXTRACT_HOMEWORK_DEADLINES`), found in a material (agent `find_homework`), added
  by the student (button) or by the agent (`create_assignment`) — gets «Удалить». It is a **soft delete**:
  `assignment.Delete(now)` sets `deletedAt` (migration: `assignments.deleted_at`), `assignment.Restore(now)` clears
  it; both bump the version. Rows are never removed by this path (`RemoveCreatedAssignment` stays only for the
  agent's Undo of its own creation).
- **Refused** (`assignment.ErrInUse`) while the assignment has an unresolved delivery (PENDING — including a
  scheduled one of ADR-0030 —, SENDING, UNKNOWN) or an unfinished draft/writing/triage job. Drafts, works, sent
  deliveries and proposals stay attached and come back with «Вернуть».
- **Hidden everywhere**: every reader (lists, «Сегодня», «Горит на 7 дней», subject page, deadlines, calendar sync,
  agent tools `list_assignments` and the ID check) skips deleted rows; an agent call naming one gets `unknown_id`.
- **Reprocessing** never re-creates or revives a deleted extracted assignment: a deleted row counts as touched by the
  student (as `user_edited`) and stays a tombstone.
- **Proposals**: «Да» on a proposal of a deleted assignment goes STALE (reason `assignment_deleted`); nothing is sent
  or exported. Checked at «Да», so delete touches one aggregate (ARCH-23).
- **Google Calendar** (hard rule 6, Google is a view): the delete writes a `deadline_event_removals` row in its
  transaction (a queue row, not an aggregate — the ADR-0018 pattern, extended here to this use case) and requests a
  sync, which deletes the event (404/410 = success) and then the row. «Вернуть» removes a row still waiting; after
  the sync the assignment returns without the event and the page offers «Предложить в календарь» again. A restore
  racing the sync may leave it without the event (accepted).
- **Agent journal**: the delete is the student's own click (`input/wails`), not a journal line. An agent Undo of an
  action on a deleted assignment is refused as `changed` (version moved).
- **Recording deletion** (ADR-0018) does not offer a deleted assignment and keeps it (transcript link NULL).
- **UI**: after «Удалить» the list shows «Задание удалено» with «Вернуть». No list of deleted ones (C-2 → а).
- **Retention** (owner 2026-10-27, C-2 → а): «Вернуть» works only for `assignment.RestoreWindow` after the delete; then
  the assignment is **purged completely** (addendum 2 below).

### Edit after creation
- The student edits title, note, deadline (date and optional time) and subject of any assignment from the UI.
  `homeworkflow.EditAssignment` grows: subject is a library subject ID (or none) stored as the student's subject
  correction (the name, as `NewByStudent`); date/time follow the `create_assignment` rules (no time → 20:00 local,
  addendum 3).
  Validation by source, in the domain: a student assignment keeps the ADR-0029 rules; an extracted one `NewDetails`
  plus the same control/bidi ban. The edit marks `user_edited`; `expectedVersion` guards concurrent edits.
- A deleted assignment cannot be edited (`ErrDeleted`).

### Ask for the subject (agent)
- Panel **v8** (= v7 + one rule; tools unchanged, desktop catalog 8; v7 is long replies, TASK-223): when the request
  scope has no subject and the student names none, `create_assignment` is preceded by `ask_questions` «К какому
  предмету относится задание?» with the subjects from `list_subjects` as options and **«Без предмета» as the last
  option** (C-1 → а); the answer's subject ID is used, «Без предмета» means no `subject`. A scope with a subject keeps
  the ADR-0029 rule. Only the prompt asks; the desktop does not refuse a subject-less `create_assignment`.

### Deferred
- Windows notifications at the deadline: not now (owner 2026-10-27).

### Open (owner)
Answered 2026-10-27: C-1 — (а) «Без предмета» is the last option; C-2 — (а) only «Вернуть» for a short time, then the
assignment is purged completely (duration: C-3).

## Addendum 2 (2026-10-27): purge after the restore window, deadline change and the Google event
Source: owner answers of 2026-10-27 (З1–З3, 6.3). Tasks TASK-274…281 (C-1: TASK-233/234).

### Purge of a deleted assignment (З2)
- `assignment.RestoreWindow` is a domain constant: **3 days** (owner 2026-10-27, C-3). `Restore(now)` after the window
  → `ErrRestoreExpired`; the binding answers `expired`.
- `homeworkflow.PurgeDeleted(ctx)` removes each assignment deleted longer than the window, **hard**: proposals of it,
  deliveries (only final ones can exist: delete is refused while one is unresolved), drafts with their versions, works,
  then the assignment row. The steps are idempotent and the assignment row goes last, so a crash leaves a
  still-deleted (invisible) assignment and the next run finishes. **Amended in TASK-274 (review):** the steps of one
  assignment run in ONE write transaction (`BEGIN IMMEDIATE`), every delete repeats the "deleted before the cutoff"
  guard, and the files are removed after the rows and before the commit. Separate transactions let a «Вернуть» that
  wins a clock race lose the children of the restored assignment; one transaction closes that race. This is a third
  ARCH-23 exception in the sense of ADR-0004(a): delete-only, one assignment at a time, nothing is created or updated
  (to be listed in `docs/architecture.md` ARCH-23 by the architect/supervisor). A failed commit after the file removal
  leaves a due, invisible assignment with some files missing; the next run finishes it (a file delete of a missing
  file succeeds). The write lock is held for one assignment (bounded by 1000 deliveries). Files of
  drafts/works/exports go through the existing storage ports; a kind without such a port is reported, not deleted with
  `os` (hard rule 5).
- Kept: agent journal lines (IDs only; an Undo naming a purged ID is refused), a still waiting
  `deadline_event_removals` row (no foreign key; the sync deletes the event later).
- The sent-email record goes with it (the owner said «completely»; it was kept «as the history of what was sent»).
- Runs as a startup cleanup and once a day; no migration (rows are deleted).

### Deadline change and the Google event (З3)
- Google is only a view (hard rule 6). An assignment **has an event** when it has an EXECUTED `create_deadline_event`
  proposal and no waiting removal; the event id is derived from the assignment (`googlecal.DeadlineEventID`), the
  write is an upsert.
- **Setting** (settings aggregate, migration): «События в календаре при смене срока» = `ask` (default, «спрашивать») |
  `update` («обновлять сами») | `keep` («не обновлять»).
- **Policy**: a pure domain service `coursework/services/eventrefresh`: (has event, due changed, policy) → nothing |
  ask | queue. The use case maps the setting to its input; no condition in the use case (ARCH-22).
- **Student edit** (`homeworkflow.EditAssignment`): `queue` writes a `deadline_event_updates` row (queue table, the
  ADR-0018 pattern as `deadline_event_removals`) in the edit's transaction and requests a sync after commit; `ask`
  returns `calendarQuestion` with the result. A removed deadline queues a removal instead of an update.
- **The question**: «Обновить событие в Google Календаре?» with «Да» / «Нет» and the checkbox «Запомнить». «Да» →
  `UpdateDeadlineEvent(id)` (queue row + sync). «Запомнить» stores `update` or `keep` through the settings use case
  (another aggregate, another call); then the question never comes again and the choice acts. The confirmation is the
  student's click (or the student's setting), never the model's (hard rule 3).
- **Sync** (`cases/schedulesync`): for each queued row, reads the assignment again; deleted, no due or no event → the
  row is dropped; else `UpsertDeadline` with the confirmed event title of the executed proposal and the current due,
  then the row goes. Idempotent by the event id; a lost answer is retried safely (hard rule 7).
- **Agent `edit_due`**: updates the event without asking (C-4, addendum 3).

### Due limits (6.3)
- Due within `now − 31 days … now + 2 years`; a date without a time is 20:00 local (addendum 3; was 23:59).

### Open (owner)
Answered 2026-10-27: C-3 — «Вернуть» works for 3 days, then the assignment is purged; C-4 — the agent's deadline
change updates the Google event too, without asking (addendum 3).

## Addendum 3 (2026-10-27): 20:00 for a date without a time, the agent's deadline change and the event
Source: owner answers of 2026-10-27 (6.3 changed, C-4). Tasks TASK-304, TASK-305, TASK-281.

### A date without a time is 20:00 local
- One domain constant in `coursework/assignment` (the hour and minute of a date-only deadline, 20:00) replaces
  `endOfDayHour/Minute`; `StudentDue` (button form, edit, `create_assignment`) and the agent's `edit_due` for an
  assignment without a deadline (`input/agent` `dueOn`) use it; no other copy of the value in Go.
- The frontend form reads a deadline at exactly 20:00 as «no time» (the empty time field), as 23:59 before.
- Stored deadlines are **not** rewritten (no migration): they are instants the student saw and Google events may
  show; an old assignment at 23:59 now shows the time 23:59 in the form and keeps it on save.
- Extraction from a recording or a material (the `EXTRACT_HOMEWORK_DEADLINES` prompt says «If only a day is named,
  use 23:59 of that day»): Open C-5, task TASK-305 waits for it.
- The planner never computes the time (the desktop does), so served panel versions stay. Panel v9 (TASK-244) says
  «Without time the deadline is 20:00 of that day» in `system.md`; the `date` type text in `tools.json` («the end of
  that day») stays byte-identical until a catalog changes tool texts anyway (the v9 = v8 tools rule of ADR-0028).

### The agent's `edit_due` and the Google event (C-4)
- After a successful `edit_due` on an assignment that has an event, the use case queues a `deadline_event_updates`
  row in the same transaction and requests a sync after commit (the student-edit path with the fixed policy
  `update`; `eventrefresh.Decide` with that policy, no condition in the use case). The setting «События в календаре
  при смене срока» applies to the student's own edits only; the agent path ignores it, including `keep` (owner: «the
  agent changes it for a reason»).
- Hard rule 3 holds through policy: the event was created by the student's «Да»; the update only mirrors a local,
  validated, undoable change (hard rule 6, Google is a view); the title stays the confirmed one and no model text
  reaches Google. Idempotent as every sync row (hard rule 7).
- **Undo** of the agent's `edit_due` (the student's click) restores the old deadline and queues the update again, so
  the event returns to the old time. An Undo refused as `changed` touches nothing.
- The panel line of the action says «Срок изменён; событие в Google Календаре обновится» when an update was queued.

### Open (owner)
- C-5. Извлечение заданий из записи и материала: если в лекции назван только день («к пятнице»), сейчас срок 23:59. Тоже ставить 20:00? (а) да, везде одинаково — 20:00 (рекомендуется); (б) нет, у извлечённых оставить 23:59. Уже извлечённые задания не меняются в обоих вариантах. До ответа TASK-305 не запускается.
