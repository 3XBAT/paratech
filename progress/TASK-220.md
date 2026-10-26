# TASK-220 — panel line «Задание добавлено» (create_assignment)

## Done
- `agentView.ts`: `addedAssignmentText` builds «Задание добавлено: <название> · <предмет> · срок <день>, <время>» (subject/due/time left out when absent or unknown subject); `create_assignment` label «Добавление задания». `journalView.ts`: «Добавлено задание».
- `AgentCards.tsx`: the action card shows the line and «Открыть» (existing `actions.openAssignment`, so «Назад» history works); «Отменить» uses the existing Undo path; after undo «Отменено» with no title and no link; `assignment` null (deleted) -> old text, no link.
- Title is a text node only. Tests: `test/agentassignmentadded.test.mjs` (8).

## Decisions to confirm
- The DTO does not tell whether the student named a time (date only = 23:59), so date and time are always shown («срок пт, 30 окт., 23:59»). Dates use the `dueText`/`daysLeftText` wording of Today («сегодня», «завтра»).

## Follow-ups (reviewer nits)
- `addedAssignmentText` cuts the «до » prefix off `dueText`; export a day-date helper from `todayView.ts` instead.
- No tests for «сегодня» and a past due.
- «Открыть» stays clickable while an undo is in flight (harmless).
- Long inline ternary in AgentCards.tsx.

## Changed existing tests
None.

## Security
Reviewer: approve, nits only. Auditor: no findings (text-only rendering, navigation by id only, Undo unchanged).
