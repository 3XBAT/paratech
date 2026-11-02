# TASK-304 — a date without a time is 20:00, not 23:59

## Done
- Domain: `assignment.DefaultDueHour` / `DefaultDueMinute` (20:00) replace `endOfDayHour`/`endOfDayMinute`. `StudentDue` (form «Добавить задание», «Изменить», `create_assignment`) and the agent's `dueOn` (`edit_due` on an assignment without a deadline) use them; with an old deadline the time of day stays. `parseClock` keeps its own clock bounds (`maxClockHour` 23, `maxClockMinute` 59): validity of a typed time, not the default. No other Go copy of 23:59 remains for deadlines.
- Core has no place that normalizes an extracted day-only deadline: `prompts/output.go` / postprocess accept only a full RFC 3339 timestamp from the model. Nothing to switch there.
- Frontend: `DEFAULT_DUE_TIME = "20:00"` in `homeworkView.ts` (mirrors the Go constant); `dueFields` shows a deadline at 20:00 as an empty time, 23:59 as «23:59» (kept on save). Form hint now «Необязательно. Если время не указано, срок — 20:00.»
- Comments updated in the files listed by the task (`agentpanel.go` comment only).
- Stored deadlines are not rewritten (no migration).

## Decisions to confirm
- The 31-days-back window is about 4 h stricter for a date-only deadline: a date exactly 31 days ago entered after 20:00 is refused (pinned by a test).
- Old assignments at 23:59 (including extracted ones until TASK-305) now show «23:59» in the edit form instead of an empty time.
- UX follow-up NOT done (outside the task's files; agent panel is TASK-342's): several surfaces show a deadline as a date only, which with 20:00 reads as end of day: `todayView.ts` (`dueText`, «Горит» chip, `homeworkText`, `waitingLine`, `nearestDeadlineText`), `agentView.ts` assignment rows (date-only `AssignmentLineView.due`; shows «сегодня» after 20:00 when it is already overdue), `agentConfirmView.ts` «Срок» row. Suggested: show the time on every surface («до пт, 30 окт., 20:00»), add a time to `AssignmentLineView`, mark overdue by the moment, «срок до …» wording in the agent line.
- Suggestion (ux-expert, not done): fill the field with 20:00 instead of leaving it empty.

## Follow-ups
- TASK-305 must change: in `domain/coursework/services/prompts/prompts.go` (~line 67) «If only a day is named, use 23:59 of that day» -> 20:00 and `prompts.Version` 5 -> 6; "only a day" expectations in `desktop/tests/testdata/prompteval/*.json` (T20:00), `prompteval_hub_test.go`, prompts tests by version. Also the Hub panel prompt / `tools.json` wording (TASK-244).
- Pre-existing: agent `editDue` writes the deadline without re-applying the now-31d..now+2y window (auditor's note, not new).

## Changed existing tests
- `assignment/student_test.go` TestStudentDue: «end of the day» 23:59 -> «date only» 20:00 (new rule).
- `homeworkflow/create_test.go`, `edit_test.go`: date-only case wantDue 23:59 -> 20:00 (renamed).
- `input/agent/actions_test.go`: «set a first due» 23:59 -> 20:00; `TestDueOn` added.
- `desktop/tests/agent_assignment_test.go`: create_assignment date-only deadline 23:59 -> 20:00.
- `frontend/test/homeworkedit.test.mjs`: 23:59 -> «23:59» (was ""); 20:00 -> "" and 09:00 added.
- `frontend/test/agentassignmentadded.test.mjs`: fixture time 20:00, expected strings 20:00; a 23:59 case added.
- New: `TestStudentDueWindowWithDefaultTime` (window edges at 19:59/20:00/20:01), «explicit 23:59 stays» in create_test.

## Security
No findings (security-auditor): constant swap, no new input path; time of day never comes from the model. Reviewer: approve (one low, comment width, fixed). ux-expert: «доработать» for the date-only surfaces above, deferred as out of scope (see Decisions).
