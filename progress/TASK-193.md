# TASK-193 — follow-ups of 26.10 (frontend only)

## Done
- «Отправить преподавателю» in the done WorkCard (WorkSend.tsx, sendWorkView.ts): only when `sendable`; proposeSendWork then the existing ConfirmCard (the «Да» stays a deliberate click); no assignment -> noAssignmentText; "already sent" warning with «Отправить ещё раз».
- «Сегодня»: `send_work_email` -> «Работа преподавателю».
- Failed turnkey start shows the reason (triageReason) on the assignment page (page seeds the card with the start reply).
- «Спросить по шагу»: no "backend missing" mode; three new error texts (offline/quota/refused) appended to errorTexts; answers/errors as text.
- Failed free work: phase `failed` with «Обновить»; no «Повторить» after any failed stop call.
- WorkCard shows «Предмет · Тема» from DTO (subject name via library, «Входящие» if gone).
- Owner remarks: triage card shows «Разобрать задание» before any attempt, reason + «Попробовать снова» after a failed one; «Сегодня» right column `clamp(24rem, 40%, 32rem)` inside the unchanged 62rem container query (TASK-175).
- Tests: test/followups193.test.mjs, test/today193css.test.mjs. npm test 731/731, build OK.

## Changed existing tests
- turnkey.test.mjs: step help "unavailable" -> error (mode removed); triage card without problem now expects «Разобрать задание».
- homeworkturnkey.test.mjs: two deepEqual now include `work: { id: "w2" }` (state carries the reply).

## Decisions to confirm / follow-ups (need core work)
- The core does not persist the last triage problem/message: the panel and a restarted app show a failed start as a fresh «Разобрать задание» with no reason until the next attempt. Follow-up: persist problem/message in the work DTO.
- "Already sent" warning is session-local; ProposeSendWork returns only a waiting duplicate. Follow-up: a "sent / last sent at" field in the work DTO. A `confirmed` card is also worded as already sent.
- `ErrTurnkeyNoStepHelp` text stays in errorTexts (Go-source guard test); shown as an ordinary error.
- Nit: confirm card is nested inside the done work card; owner to look on screen.

## Security
Auditor: no findings. Send only via a deliberate «Да»; all model/student text as text nodes; error texts from allowlists.
