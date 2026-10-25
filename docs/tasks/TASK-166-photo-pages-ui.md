# TASK-166: Photo pages in «Мой конспект» (UI)

- **Agent:** frontend
- **Label:** frontend
- **Depends on:** TASK-154, TASK-165
- **PRD sections / AC:** ADR-0027 «Handwriting and scans» (option E); ADR-0012
- **Branch:** ralph/<date>-task-166

## Goal
The student adds photos of pages, puts them in order and reads them in the app.

## Allowed to change
- `paratech/desktop/frontend/src/pages/{OwnNotes*.tsx,PageViewer.tsx}`
- `paratech/desktop/frontend/src/{ownNotesView.ts,ownNotesView.test.ts}`

## Acceptance criteria
- [ ] Thumbnails, reorder, a full-size viewer with keyboard paging; «текст не распознан» with «Вставить текст» to type
      the page content by hand (stored as a TXT part next to it).
- [ ] Images only from the binding URL by ID; alt texts in Russian.
- [ ] Tests; `npm run test` and `npm run build` pass.
