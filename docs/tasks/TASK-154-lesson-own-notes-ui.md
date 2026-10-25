# TASK-154: «Мой конспект» on the lesson page (UI)

- **Agent:** frontend
- **Label:** frontend
- **Depends on:** TASK-151
- **PRD sections / AC:** ADR-0027 «Coexistence», «Trust»; ADR-0012 (visual direction); ARCH-44
- **Branch:** ralph/<date>-task-154

## Goal
On a lecture the student adds files or pasted text as «Мой конспект», sees them next to the AI «Конспект», and can
replace, reorder, remove and undo.

## Allowed to change
- `paratech/desktop/frontend/src/pages/{KnowledgeLessonPage.tsx,OwnNotes*.tsx}`
- `paratech/desktop/frontend/src/{ownNotesView.ts,ownNotesView.test.ts}`
- `paratech/desktop/frontend/src/pages/DropZone.tsx` (a drop target per lecture)

## Out of scope
- Note-only lectures in the subject list and citation labels (TASK-155); photos (TASK-166).

## Acceptance criteria
- [ ] Block «Мой конспект» with «Добавить файлы», «Вставить текст» (textarea, limit shown) and drop; parts listed by
      display name, type and «раздел/стр./фрагмент» count; «уже добавлено» for duplicates.
- [ ] The student's text and the AI «Конспект» are separate blocks with chips «ваш конспект» / «paratech»; nothing is
      merged; texts render as text nodes only (never HTML or Markdown links).
- [ ] Replace, reorder, remove with «Отменить»; refusals shown in Russian from the binding reasons.
- [ ] Tests: view model, duplicate, remove/undo, no HTML injection.
- [ ] `npm run test` and `npm run build` pass.
