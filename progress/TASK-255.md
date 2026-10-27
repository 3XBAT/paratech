# TASK-255 — small fixes after TASK-227 (frontend only)

## Done
- Comments in `workView.ts` (`acceptWork`), `homeworkTurnkey.ts` and `agent/WorkCard.tsx` no longer claim the core does not store the problem (it keeps lastProblem/lastMessage).
- `formatTimeMinutes` in `activityView.ts` (date and minutes, same fallback as `formatTime`); used for «Отправлено» in `homeworkView.ts` and `sendWorkView.ts`. `formatTime` unchanged for other callers.
- Tests: `homework.test.mjs`, `subjectpage227.test.mjs`. 864/864, build green.

## Decisions to confirm
- The email delivery hint (`homeworkView.ts`) also lost its seconds, same wording.

## Follow-ups
None.

## Changed existing tests
- `subjectpage227.test.mjs` "sentAt drives «Отправить ещё раз»": compares with `formatTimeMinutes` instead of `formatTime`.
- `homework.test.mjs`: the «Отправлено» hint assertion expects no seconds.

## Security
Auditor: nothing found. Reviewer: stale WorkCard comment and a formatting slip, both fixed.
