# TASK-169: Check recognized pages (UI) (BLOCKED)

- **Agent:** frontend
- **Label:** frontend
- **Depends on:** TASK-166, TASK-167; **owner decision ADR-0027 Open 3**
- **PRD sections / AC:** ADR-0027 «Handwriting and scans»
- **Branch:** ralph/<date>-task-169

## Goal
The student sees the photo and the recognized text side by side, fixes it and accepts it.

## Blocked
Waits for the engine decision (TASK-167).

## Allowed to change
- `paratech/desktop/frontend/src/pages/{OwnNotes*.tsx,PageViewer.tsx,RecognizedPage*.tsx}`
- `paratech/desktop/frontend/src/{ownNotesView.ts,ownNotesView.test.ts}`

## Acceptance criteria
- [ ] «Распознать» (with the opt-in and cost for the Hub engine), progress per page, «Принять» / edit; uncertain lines
      highlighted; accepted pages get «ваш конспект · распознано».
- [ ] Tests; `npm run test` and `npm run build` pass.
