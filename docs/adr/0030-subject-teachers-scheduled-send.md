# ADR-0030: Teacher of a subject and «Отправить в…»

- **Status:** Accepted (owner 2026-10-27); the items under «Open» wait for the owner
- **Date:** 2026-10-27
- **Source:** owner 2026-10-27; ADR-0021 (class C), ADR-0022, ADR-0024 (subjects), ADR-0025 addendum TASK-180 (work
  send), ADR-0004(c) (delivery, UNKNOWN); TASK-192, TASK-226 (`sentAt`). Tasks TASK-235…243.

## Context
Every email goes to the one address from the settings (`settings.HomeworkRecipient`, read through
`homeworkflow.RecipientSource`). A student has a different teacher per subject. The send happens right after «Да»;
the student wants to pick a time («завтра в 9:00»). The job queue already supports a first eligible time
(`job.Spec.RunAt`), and the delivery flow is idempotent per proposal key with UNKNOWN reconciled (hard rule 7).

## Decision

### Teacher of a subject
- `library/subject` gains an optional `Teacher{name, email}`: name 1–200 runes, one line, no control or bidi
  characters; email checked with the same syntax rules as `proposal.NewRecipient` (own check in `library`, ARCH-11).
  Set and cleared only by the student (`input/wails`, subject page). Storage: `subjects.teacher_name`,
  `subjects.teacher_email` (ADD COLUMN, default ''). An address is not a secret (as the settings recipient).
- The agent has no tool to read or set it; the planner never sees a teacher name or address (ADR-0022, `AgentBase`
  carries no personal data). The teacher text of an assignment (extraction, `CorrectLesson`) is never an address.

### Recipient of «Отправить преподавателю»
- For `send_homework_email` and `send_work_email` the recipient is the teacher email of the subject: the work's
  subject ID, else the library subject of the assignment (its subject name resolved as ADR-0024). No teacher email
  (or no subject): Open B-1.
- `RecipientSource.HomeworkRecipient(ctx)` becomes `Recipient(ctx, subject shared.SubjectID)` returning the address
  and the teacher name; the choice «teacher, else …» is a pure domain function in `coursework/proposal`, implemented
  in `app` over `library.Service` and the settings.
- Unchanged: frozen at validation, checked again at «Да» (another address → STALE), the card shows «Кому: <имя>
  <адрес>».

### «Отправить в…» (send at a chosen time)
- The confirmation card offers «Отправить сейчас» and «Отправить в…» (presets «Сегодня в …», «Завтра в 9:00», a date
  and a time). One «Да» confirms content, recipient and time; there is never a send without it (hard rule 3).
- The time is local, to the minute, from now + 5 min to now + 14 days, checked in the domain at confirmation
  (`proposal` confirm with `sendAt` and `now`). Stored as `proposals.send_at`; the email command carries it; the
  delivery gets `notBefore` (`deliveries.not_before`); its job is enqueued at «Да» with `RunAt = sendAt`. A
  CONFIRMED proposal resumed after a crash uses the stored time.
- Everything is frozen at «Да»: a later teacher-address change does not redirect a scheduled delivery.
- A scheduled delivery is PENDING: it blocks another send of the same assignment (`ErrUnresolvedDelivery`), the
  regeneration of its export file (`HasUnresolved`) and the delete of the assignment until it is sent (or cancelled,
  Open B-3). The UI shows «Запланировано на 28.10 в 9:00». `sentAt` (TASK-226) is the real send time.
- It works only while the app runs (also minimised to the tray): the queue is in-process. App closed or PC asleep at
  that time: Open B-2. Hard rule 7 unchanged: one key = one delivery, UNKNOWN reconciled, never resent blindly.

## Consequences
- Migrations (next free numbers): `subjects` teacher columns; `proposals.send_at`, `deliveries.not_before` (a
  cancel, if chosen, adds status `CANCELLED` and rebuilds `deliveries` for the CHECK).
- `homeworkflow` facts take the subject; `app/settingsbridge.go` gets the library.
- The subject page (TASK-225/227) shows the teacher.
- A long schedule keeps the export file locked; the 14-day cap bounds that.

## Open (owner)
- B-1. У предмета нет почты преподавателя (или у задания нет предмета): (а) взять общий адрес из настроек — дешевле всего; (б) не отправлять, попросить указать почту преподавателя; (в) как (а), но в карточке пометка «адрес из настроек».
- B-2. Приложение было закрыто или компьютер спал в выбранное время: (а) отправить сразу при следующем запуске — дешевле всего, очередь уже так работает; (б) не отправлять, показать «Не отправлено вовремя» и спросить заново; (в) отправить при запуске, если опоздание не больше N часов, иначе спросить.
- B-3. Можно ли отменить запланированную отправку до её времени? (а) да, кнопка «Отменить отправку»; (б) нет — дешевле.
- B-4. Один преподаватель на предмет или отдельно лектор и семинарист? (а) один — дешевле; (б) по виду пары.
