# TASK-160: Import review screen (UI)

- **Agent:** frontend
- **Label:** frontend
- **Depends on:** TASK-159, TASK-155
- **PRD sections / AC:** ADR-0027 «Bulk import», Open 9; PRD §11.2 (preview before commit); ADR-0012
- **Branch:** ralph/<date>-task-160

## Goal
The student sees where each imported file will go, fixes wrong matches quickly and commits.

## Allowed to change
- `paratech/desktop/frontend/src/pages/{NotesImportPage.tsx,NotesImportParts.tsx}` (new)
- `paratech/desktop/frontend/src/{notesImportView.ts,notesImportView.test.ts}` (new)
- `paratech/desktop/frontend/src/pages/{SubjectsPage.tsx,KnowledgeBasePage.tsx}` (entry «Импортировать конспекты»)

## Out of scope
- Any matching logic (core only).

## Acceptance criteria
- [ ] Entry from «Предметы» and «База знаний»: «Выбрать файлы», «Выбрать папку», drop.
- [ ] Tiles like the schedule import (точно / вероятно / не определено / дубликаты / не поддерживается) as filters.
- [ ] Row: display name, relative folder, kind (конспект; «запись» when the core sends it), proposed subject and
      lecture («предполагаемая пара» marked), reasons in Russian; fix per row and for selected rows (subject, past
      lesson, date, «без даты», «пропустить»); «создать предмет» from a schedule suggestion.
- [ ] «Добавить N» commits; progress per item; the batch reopens after a restart; «Отменить импорт».
- [ ] Texts as text nodes only; tests for filters, bulk choose, commit state; `npm run test` and `npm run build` pass.
