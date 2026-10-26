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
1. A «Удалить» for an assignment the student added (beyond Undo)? *Proposed: later, own task.*
2. A Windows notification at the due time («в 9 напомнить»)? *Proposed: no; the assignment shows on «Сегодня» and in
   «Горит на 7 дней» only (no-toast decision).*
3. Due limits −31 days … +2 years and 23:59 for a date without a time. *Proposed: as written.*
4. No subject named in a subject thread: the planner passes the thread's subject. *Proposed: yes; in «Без предмета»
   thread it stays without a subject.*
5. A request without any date: added without a deadline, no question. *Proposed: yes; a date with two readings
   («к пятнице» on a Friday) is asked.*
