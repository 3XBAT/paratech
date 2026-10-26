# TASK-176: assignment list in the agent reply card

## Done
- Reply card with `assignments` shows a list under the text: title, subject, due. Plain React text only; each row is a `<button>` (keyboard works), click opens the assignment through the existing «Домашние задания» navigation. Empty list renders nothing.
- Due: «сегодня» / «завтра» / «просрочено» (via daysLeftText), otherwise «до сб, 24 окт.». The core sends date-only `YYYY-MM-DD`; it is parsed as a LOCAL day (review finding: UTC parse shifted the day in negative-offset zones). Overdue uses `--run-t`, the urgent colour of «Сегодня».
- Subject names from the existing library subjects binding; unknown id -> subject omitted (own-property lookup).
- Tests: test/agentassignments.test.mjs (parse, wording, SSR, overdue class, HTML injection escaped, empty list, onClick wiring, date-only not shifted).

## Decisions to confirm
- Files outside the list: AgentPanel.tsx (subjects load, onOpenAssignment prop) and App.tsx (one prop). Needed for navigation and subject names. agentState.ts untouched.
- Overdue is coloured text only (no chip background as on «Сегодня»).

## Follow-ups
- Subjects reload only on page change; a subject created mid-conversation shows no name until then.
- Click test reads the element tree (no DOM test library).
- Not run on screen; negative-UTC-offset zone not run in tests.

## Changed existing tests
None.

## Security
Reviewer: medium (date parsing) fixed, rest approve. security-auditor: no findings.
