# TASK-219 — «Добавить задание» form (Задания, Сегодня)

## Done
- `AddAssignmentDialog` (HomeworkDialogs.tsx), pure logic in `homeworkView.ts` (`validateAddForm`, `addOutcome`, `addRequest`, `addSession`).
- Button in the «Задания» header and next to «Горит на 7 дней»; on success «Задания» opens the new assignment, «Сегодня» reloads the feed.
- requestId: randomUUID on open; same on retry; regenerated after an edit following a refused outcome, and at once when the core rejects the token; kept after a thrown call (unknown outcome). Double submit = one call.
- Field errors for title/note/due/time; `requestId`, no field and other problems -> general impersonal text. Text only.

## Decisions to confirm
- Kept the id after a thrown call: if the call actually stored the assignment and the student then edits, the next attempt gets `failed` and the following edit a new id (possible second assignment). Accepted.

## Follow-ups (reviewer lows)
- Busy flag may reset while the first call runs when a second submit returns null (HomeworkDialogs.tsx ~273-290); practically unreachable.
- Dialog focuses «Отмена» on open, not «Что сделать».
- Wiring test is regex over sources; no test for the busy state; local `setItems` prepend in HomeworkPage is redundant.

## Changed existing tests
None.

## Security
Reviewer: approve, 3 lows. Auditor: no findings (text-only rendering, token handling).
