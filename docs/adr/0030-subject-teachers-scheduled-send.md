# ADR-0030: Teachers of a subject and «Отправить в…»

- **Status:** Accepted (owner 2026-10-27; B-1…B-6 answered 2026-10-27)
- **Date:** 2026-10-27
- **Source:** owner 2026-10-27; ADR-0021 (class C), ADR-0022, ADR-0024 (subjects), ADR-0025 addendum TASK-180 (work
  send), ADR-0004(c) (delivery, UNKNOWN); TASK-192, TASK-226 (`sentAt`). Tasks TASK-235…243, TASK-285, TASK-286.

## Context
Every email goes to the one address from the settings (`settings.HomeworkRecipient`, read through
`homeworkflow.RecipientSource`). A student has different teachers per subject, usually a lecturer and a seminar
(practice) teacher. The send happens right after «Да»; the student wants to pick a time («завтра в 9:00»). The job
queue already supports a first eligible time (`job.Spec.RunAt`), and the delivery flow is idempotent per proposal key
with UNKNOWN reconciled (hard rule 7). TASK-235 built one teacher per subject (`subjects.teacher_*`, migration 0038).

## Decision

### Teachers of a subject (B-4: by lesson kind; B-5: a third one for labs)
- `library/subject` holds up to three teachers keyed by `TeacherRole`: `lecture` (лектор), `practice` (семинары и
  практика) and `lab` (лабораторные). `Teacher{name, email}` keeps the TASK-235 rules (name 1–200 runes, one line, no control or bidi
  characters; email with the same syntax rules as `proposal.NewRecipient`, own copy in `library`, ARCH-11).
  `SetTeacher(role, name, email, now)`, `ClearTeacher(role, now)`, `Teacher(role)`, `Teachers()`.
- Storage: table `subject_teachers(subject_id, role, name, email)`, PK `(subject_id, role)`, part of the subject
  aggregate (no version of its own). The migration copies a TASK-235 teacher into **all three** roles (the same meaning
  as before: every send of the subject went to that address) and blanks `subjects.teacher_*` (kept as unused legacy
  columns; SQLite cannot drop them because of the shared CHECK).
- Filling the lab teacher, the form offers «Как у лектора» and «Как у семинариста»: a one-time copy of that row's
  name and email into the form (a copy, not a link; a later change of the lecturer does not change the lab teacher).
- Set and cleared only by the student (`input/wails`, subject page). The agent has no tool to read or set a teacher;
  the planner never sees a name or address (ADR-0022, `AgentBase` carries no personal data). The teacher text of an
  assignment (extraction, `CorrectLesson`) is never an address.

### Recipient of «Отправить преподавателю»
- For `send_homework_email` and `send_work_email` the subject is the work's subject ID, else the library subject of
  the assignment (its subject name resolved as ADR-0024). The role comes from the lesson kind of the assignment (of
  the work's assignment): lecture → `lecture`, seminar and practice → `practice`, lab → `lab`.
- **B-6: one teacher is everyone.** When exactly one role of the subject has a teacher with an email, that teacher
  gets every send of the subject, whatever the lesson kind (the card still says «Кому: <имя> <адрес>»).
- **B-5: no lesson or an unknown kind → the card asks.** With two or three teachers filled in, the confirmation card
  asks «Кому отправить?» with one option per filled role («Лектор — <имя> <адрес>», …; roles with the same address
  are one option, and when all filled roles share one address there is no question). «Да» stays disabled until a
  choice is made; the confirmation carries the chosen role and the address the card showed; a different current
  address of that role at «Да» → STALE. The choice is the student's click; the agent and the model never pick a role
  (hard rule 3).
- Two or three teachers filled in and the kind's own role empty (for example a lab assignment, lecturer and seminar
  teacher filled, no lab teacher) → `no_teacher_email` with that role (B-1), the card offers «Указать почту».
- `RecipientSource.HomeworkRecipient(ctx)` becomes `Teachers(ctx, subject shared.SubjectID)`; the choice is a pure
  domain function `proposal.ChooseRecipient(teachers, kind)` returning the address and name, or a reason:
  `no_subject`, `no_teacher_email` (with the role) or `choose_teacher` (with the candidate roles); at «Да» a chosen
  role goes through `proposal.ChosenRecipient(teachers, role, shownAddress)`. Implemented in `app` over
  `library.Service`.
- **B-1 (б): no address → no send.** The proposal is refused with that reason (not an error of the model); the card
  says «Нет почты преподавателя (<лектор|семинарист|лабораторные>) предмета «<имя>»» with «Указать почту», which opens the
  teacher field of that role on the subject page; with no subject it says «У задания не указан предмет» and offers the
  subject choice (TASK-232). The settings address is never used as a fallback for these two kinds.
- Unchanged: frozen at validation, checked again at «Да» (another address → STALE), the card shows «Кому: <имя>
  <адрес>». Hard rule 3: there is never a send without «Да».

### «Отправить в…» (send at a chosen time)
- The confirmation card offers «Отправить сейчас» and «Отправить в…» (presets «Сегодня в …», «Завтра в 9:00», a date
  and a time). One «Да» confirms content, recipient and time.
- The time is local, to the minute, from now + 5 min to now + 14 days, checked in the domain at confirmation
  (`proposal` confirm with `sendAt` and `now`). Stored as `proposals.send_at`; the email command carries it; the
  delivery gets `notBefore` (`deliveries.not_before`); its job is enqueued at «Да» with `RunAt = notBefore`. A
  CONFIRMED proposal resumed after a crash uses the stored time.
- Everything except the time is frozen at «Да»: a later teacher-address change does not redirect a scheduled send.
- The send job reads the delivery when it runs: before `notBefore` it waits (re-enqueues itself at `notBefore`), a
  CANCELLED delivery ends the job with nothing sent, after the late limit see B-2.
- A scheduled delivery (PENDING or MISSED) blocks another send of the same assignment (`ErrUnresolvedDelivery`), the
  regeneration of its export file (`HasUnresolved`) and the delete of the assignment. The UI shows «Запланировано на
  28.10 в 9:00». `sentAt` (TASK-226) is the real send time.
- It works only while the app runs (also minimised to the tray): the queue is in-process.

### Late scheduled send (B-2 (б): ask again, never send late silently)
- `delivery.Start(now)` refuses a scheduled delivery when `now > notBefore + 5 min` (tolerance for the queue and a
  wake from sleep) and moves it to the new status **MISSED**; nothing is sent. The same check runs once at app start
  for PENDING deliveries, so a missed one is shown at once.
- The student resolves a MISSED delivery with one of: «Отправить сейчас», «Выбрать новое время», «Отменить отправку».
  The card shows the frozen recipient and the planned time; the click is the confirmation (hard rule 3). Shown at the
  assignment or work and as a line on «Сегодня» until resolved.
- A delivery without `notBefore` («Отправить сейчас») is unchanged: it never becomes MISSED.

### Changing and cancelling a scheduled send (B-3: full CRUD until it starts)
- List: the bindings return every scheduled delivery (PENDING with `notBefore`, MISSED) with time and recipient.
- Edit time: `delivery.Reschedule(at, now)` for PENDING or MISSED, the same window as at «Да» (now + 5 min … now + 14
  days); «now» (from MISSED) means `notBefore = now`. The key, Message-ID and content stay; the job is re-enqueued.
- Cancel: `delivery.Cancel(now)` → **CANCELLED** for PENDING or MISSED. A cancelled delivery does not block a new send.
- Both are refused once the delivery is SENDING or later (`ErrAlreadySending`): the email is never lost or doubled;
  UNKNOWN is reconciled as before (hard rule 7).
- Status machine: PENDING → SENDING | MISSED | CANCELLED; MISSED → PENDING (rescheduled) | CANCELLED; the existing
  transitions unchanged. In the delivery aggregate, by table (ARCH-19).

## Consequences
- Migrations (next free numbers, one task at a time): `subject_teachers` (TASK-285); `proposals.send_at` and a rebuild
  of `deliveries` with `not_before` and the statuses MISSED and CANCELLED in its CHECK (TASK-239).
- `homeworkflow` facts take the subject and the lesson kind; `app/settingsbridge.go` gets the library.
- The subject page (TASK-225/227) shows three teacher rows: «Лектор», «Семинары и практика», «Лабораторные».
- A long schedule keeps the export file locked; the 14-day cap bounds that.

## Open (owner)
Answered 2026-10-27: B-5 — labs get a third teacher (with «Как у лектора» / «Как у семинариста» when filling it in);
no lesson or an unknown kind — the card asks which teacher; B-6 — when only one teacher is filled in, that teacher
gets every send. Tasks: TASK-285 (three roles), TASK-237 (choice), TASK-238 (UI).
