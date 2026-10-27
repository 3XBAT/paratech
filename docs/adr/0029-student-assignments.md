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
  A date without a time is 23:59 local of that day (as `edit_due`); the location is passed in, the domain reads no clock.
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
3 — the due limits stay: a month back (31 days) … two years ahead, a date without a time is 23:59 local.

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
  correction (the name, as `NewByStudent`); date/time follow the `create_assignment` rules (no time → 23:59 local).
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
- `assignment.RestoreWindow` is a domain constant (C-3; until the owner answers, **7 days** — the longest option, so
  nothing goes earlier than any answer allows; this is not a choice of the option). `Restore(now)` after the window →
  `ErrRestoreExpired`; the binding answers `expired`.
- `homeworkflow.PurgeDeleted(ctx)` removes each assignment deleted longer than the window, **hard**: proposals of it,
  deliveries (only final ones can exist: delete is refused while one is unresolved), drafts with their versions, works,
  then the assignment row. Each step is its own transaction and idempotent; the assignment row goes last, so a crash
  leaves a still-deleted (invisible) assignment and the next run finishes. No ARCH-23 exception is needed. Files of
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
- **Agent `edit_due`**: open (C-4); until answered it never touches the event (as today).

### Due limits (6.3)
- Stay as written: due within `now − 31 days … now + 2 years`; a date without a time is 23:59 local.

### Open (owner)
- C-3. Сколько можно «Вернуть» удалённое задание, после чего оно стирается совсем? (а) только до ухода со страницы (стирается при уходе со страницы или при следующем запуске); (б) 1 день; (в) 7 дней. До ответа в коде константа 7 дней.
- C-4. Срок задания поменял агент (просьба «перенеси срок»): (а) та же настройка — при «спрашивать» в строке действия в панели появляется тот же вопрос с «Да» / «Нет»; (б) правка агента событие не трогает никогда, обновить можно только своей правкой срока; (в) правка агента всегда спрашивает, даже при «обновлять сами».
