# TASK-238 — teachers on the subject page, recipient and choice on send cards (frontend only)

## Done
- Subject page: block «Преподаватели» (Лектор / Семинары и практика / Лабораторные) under the title: name and e-mail or «Не указан», «Указать»/«Изменить» open a small form (имя, почта, «Сохранить», «Отмена», «Убрать» when filled). The lab form has «Как у лектора» / «Как у семинариста» (one-time copy, disabled with a reason when the source row is empty). `version` and `teachers` come from `Subjects()`; the list is re-read after `failed`, `changed` and `not_found`. «Убрать» is undoable: «Вернуть» next to «Преподаватель убран.» (memory only).
- Send cards (`SendDialog`, agent `ConfirmCard`, so also the work card): «Кому: <имя> <адрес>» (address only without a name). `choose_teacher`: radio group «Кому отправить?» (one radio for one option too), «Да»/«Отправить» disabled until a click; the clicked role and the SHOWN address go as the two trailing args; nothing is sent when no choice was asked. The choice is dropped when the proposal or the option's address changes. 0 options: «У предмета не осталось почты преподавателей» + «Указать почту».
- `no_teacher_email`: the core's text (role and subject name) + «Указать почту» opens the subject page with that role's form open and focused. `no_subject`: «Выбрать предмет» opens the assignment with its «Изменить» dialog (TASK-232) already open. Both are one-shot requests in `App` state (`teacherFocus`, `assignmentEdit`), dropped when the route leaves; «Назад» does not reopen.
- `confirmTexts.sendNote` no longer says «на адрес из настроек».
- Pure logic in `teachersView.ts`, `recipientView.ts`, `recipientNav.ts`; tests `teachers238.test.mjs`, `recipient238.test.mjs`. 1082 frontend tests, build green.

## Decisions to confirm
- One-shot requests live in `App` state, not in the Route (a history re-entry must not reopen a form).
- When the core names no role, «Указать почту» opens the lecturer's form.
- The assignment page has no «Кому» line (only the delivery list); the settings recipient field is untouched (still editable, unused by sends).

## Files outside the task's file list (all needed for the criteria)
`App.tsx` (nav context, one-shot requests), `pages/HomeworkDialogs.tsx` (SendDialog), `pages/HomeworkPage.tsx` (refusal text with the way out; edit dialog open on request), `agent/ConfirmCard.tsx`, `recipientNav.ts`, `pages/RecipientParts.tsx`, `styles.css`, `tsconfig.test.json`.

## Follow-ups
- App/HomeworkPage wiring of the one-shot intents is tested by logic (`keepAssignmentEdit`, `teacherFocusStep`) and source checks; real focus behaviour (WebView2) only on screen.
- `SendDialog` with a zero-option or `no_email` view shows text only (the page path normally refuses earlier with a button).
- TASK-237 follow-ups stay: `input/agent/propose.go` mapping to `RefuseNoRecipient`, retire the settings address, persisted role (M1).

## Changed existing tests
- `test/homework.test.mjs`: the single-call-site check of `confirmSend` now includes the trailing `choice` argument (not weakened).

## Security
Auditor: none found (text nodes only, no HTML/href from data; the choice is the student's click and the shown address; no logging or storage of names/e-mails; late replies guarded by epoch). Its note about a kept role across a new preparation was fixed. Reviewer: first pass 3 medium UX findings (dead end with one option, no undo for «Убрать», no_subject way out) fixed; re-review approve, one medium (focus after «Вернуть») and lows fixed.
