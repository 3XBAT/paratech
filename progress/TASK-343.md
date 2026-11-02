# TASK-343 — deadline time visible everywhere

## Done
- Go (binding only): agent reply lines now carry the deadline time. `input/agent/reply.go` `dueOf` (shared with `actionLine`) sets `AssignmentLine.DueTime`; `AgentAssignmentView.time` ("HH:MM", "" none) in `input/wails/agentpanel.go`. Domain/storage unchanged (comment in `values/card.go` only).
- Frontend: one wording through `dueText(due, now)`: «сегодня до 20:00», «завтра до 18:30», «до сб, 24 окт., 18:30», passed «просрочено, пт, 23 окт., 20:00» (never «сегодня»/«до» for a past moment). Used by «Сегодня» (homeworkText, waitingLine, post line, calendar button), `deadlineChip` («сегодня до 20:00», «завтра до 18:30», «через 3 дня, до 18:30», «просрочено»; days counted from the local clock, the core's daysLeft only for an unreadable due), `nearestDeadlineText` («Ближайший срок — пт, 23 окт., 20:00», passed ones skipped), agent rows (`assignmentRow` with `AssignmentLineView.time`; passed -> «просрочено», overdue accent), confirm card «Срок» (date + time).
- Homework form: the time field is prefilled with 20:00 (hint «По умолчанию — 20:00.»); clearing the date with the default time removes the deadline; setting a date on a deadline-less task fills 20:00; a time other than 20:00 without a date: «Укажите дату или очистите время, чтобы убрать срок.»; a half-typed date (`validity.badInput`, read at submit through `withDateBad`) is «Дата указана неверно.», never a silent removal.

## Decisions to confirm
- Prefill 20:00 instead of an empty field with a placeholder (a native time input has no placeholder); ux-expert agreed. Storage is unchanged (empty time is still 20:00 in the core; the form now sends "20:00" explicitly).
- Passed deadline wording «просрочено, <день>, <время>» on Today lines; the agent row says just «просрочено».
- `ui/controls.tsx` `Input` prop type is now `ComponentProps<"input">` so a ref can be passed (React 19), no behaviour change.

## Follow-ups
- Agent «added» line still reads «срок завтра, 18:30» (third wording); agent overdue row loses the date; «Задания» screen format «до 18.09.2026, 18:00» (UX nice-to-haves).
- «Да, в календарь» is still offered next to a passed deadline (reviewer, low).
- Time-dependent chips and agent rows refresh on the next render, not on a timer (low).
- Other date inputs (KnowledgeParts, RecordingImportPanel) do not read `badInput` (low).
- «завтра до 00:00» for a midnight deadline (product call).
- The word «Горит» is renamed by TASK-344 (glossary); texts here follow the current UI.

## Changed existing tests
- today.test.mjs: dueText/homeworkText/waitingLine/deadlineLine/«Горит» chips/nearestDeadlineText expectations now carry time and the new wording; `BurningSection` gets a fixed `now` (the chip depends on the clock now).
- screens224.test.mjs: nearest deadline text with time and «окт.».
- agent.test.mjs, agentassignments.test.mjs: AssignmentLineView carries `time`.
- homeworkedit.test.mjs: `dueFields` 20:00 -> "20:00" (prefill); error texts for time without a date.
- homeworkadd.test.mjs: hint text. Go: input/agent reply_test (`TestReplyShowsTitlesFromLocalData` expects DueTime), input/wails agent_test JSON contract (`"time"`).

## Review
Reviewer: approve; ux-expert «доработать» (4 must-fix) -> fixed -> «ок»; re-review found a HIGH (half-typed date deleting a deadline) -> fixed with submit-time `badInput` -> approve. Security-auditor: no findings.
