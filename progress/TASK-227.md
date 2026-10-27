# TASK-227 — subject page and lastProblem/sentAt in the UI (frontend only)

## Done
- Bindings `library.ts`: `subjectOverview`, `markSubjectOpened` with strict parsers (unknown status, weekday outside 1-7, negative duration rejected; null lists are empty). Calls by name, no Wails regeneration.
- Subject page: schedule line under the title, «Ближайшее», «Занятия» (status chips, «новое»), «новое» on folders and files. `SubjectOverviewFlow`: Overview, then Mark once per opening (not on a file-drop reload; stale/closed openings and a failed Overview make no Mark; failed Mark is silent). Overview failure shows a Russian line; folders and files still show. All texts are text nodes.
- `TurnkeyWork`: strict `lastProblem`, `lastMessage`, `sentAt`. `triageReason` uses the call's message, then the stored `lastMessage`. «Отправлено <дата>» and «Отправить ещё раз» now come from `sentAt`.
- Tests: `test/subjectpage227.test.mjs` (19). 828/828 frontend tests, build green.

## Decisions to confirm
1. TASK-226 decision 5 (failed after 3 refusals, `WorkOf` skips failed): acceptable for the UI. After restart the assignment page offers a fresh «Разобрать задание»; the old refusal reason is not shown there.
2. Session flag `sentBefore` stays only as a bridge for a send confirmed in the open card before the work is re-read; `sentAt` wins.
3. The assignment page (`HomeworkTurnkey`) has no send button, so sentAt is not shown there; lastProblem is, via the shared triage/failed cards.
4. Chips: «сегодня»/«завтра» amber, «Конспект готов» green, «Ошибка»/«просрочено» red; the mockup uses grey chips. Owner to judge on screen.
5. Lessons are one list «Занятия», not grouped by kind.

## Follow-ups
- Refresh the outdated comments in `workView.ts` (`acceptWork`) and `homeworkTurnkey.ts:44` ("core does not store the problem").
- `sentAt` shows seconds (`formatTime`); date and minutes would read better.

## Changed existing tests
- `test/library.test.mjs`: the method-name check against Go also reads `subjectoverview.go`.

## Security
Auditor: no findings. Reviewer: approve, minors above.
