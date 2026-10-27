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
4 — in a request without a subject the agent asks (addendum below); 5 — yes, no date means no deadline and no question.
3. Due limits −31 days … +2 years and 23:59 for a date without a time. *Not answered; implemented as written.*

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
- **UI**: after «Удалить» the list shows «Задание удалено» with «Вернуть». Retention and a list of deleted ones: Open C-2.

### Edit after creation
- The student edits title, note, deadline (date and optional time) and subject of any assignment from the UI.
  `homeworkflow.EditAssignment` grows: subject is a library subject ID (or none) stored as the student's subject
  correction (the name, as `NewByStudent`); date/time follow the `create_assignment` rules (no time → 23:59 local).
  Validation by source, in the domain: a student assignment keeps the ADR-0029 rules; an extracted one `NewDetails`
  plus the same control/bidi ban. The edit marks `user_edited`; `expectedVersion` guards concurrent edits.
- A deleted assignment cannot be edited (`ErrDeleted`).

### Ask for the subject (agent)
- Panel v7 (= v6 + one rule; tools unchanged, desktop catalog 7): when the request scope has no subject and the
  student names none, `create_assignment` is preceded by `ask_questions` «К какому предмету относится задание?» with
  the subjects from `list_subjects` as options (whether «Без предмета» is one of them: Open C-1); the answer's
  subject ID is used. A scope with a subject keeps the ADR-0029 rule. Only the prompt asks; the desktop does not
  refuse a subject-less `create_assignment`.

### Deferred
- Windows notifications at the deadline: not now (owner 2026-10-27).

### Open (owner)
- C-1. В вопросе «К какому предмету?» есть вариант «Без предмета»? (а) да, последним; (б) нет, только предметы. Цена одинаковая.
- C-2. Удалённые задания: (а) только «Вернуть» сразу после удаления, хранятся бессрочно и невидимо — дешевле всего; (б) плюс раздел «Удалённые» на странице «Задания»; (в) очищать навсегда через 30 дней.
