# TASK-351: One lesson screen — route `lesson` and the page

- **Agent:** frontend (sonnet)
- **Label:** frontend
- **Depends on:** TASK-350
- **PRD sections / AC:** ADR-0034 §2; audit А-5.1, 5.2, 5.5, 9.2
- **Агент:** только UI: pure view state
- **Branch:** ralph/<date>-task-351

## Goal
A lesson has one screen with tabs «Мой конспект · Конспект · Текст лекции · Расшифровка · Задания», reachable by route.

## Allowed to change
- `src/nav.ts` (Route gets `lesson` and `tab`), `src/navHistory.ts`, `src/App.tsx`, `src/pages/{KnowledgeLessonPage,
  SummaryPage,SummaryParts,TranscriptPage}.tsx`, `src/{knowledgeView,summaryView}.ts`, `src/styles.css`, tests

## Out of scope
- Rewiring every entry point and removing the overlays (TASK-352).

## Acceptance criteria
- [ ] `Route.lesson` holds the lesson key of the knowledge base (`KnowledgeLesson.key`) and `Route.tab` the tab; the page
      loads the lesson by it (recording lessons by `recordingId` through `lesson()`); if a note-only lesson cannot be
      opened by key with the current binding, stop and report (no binding change in this task).
- [ ] The page is the KnowledgeLessonPage with the summary toolbar of SummaryPage inside the «Конспект» tab: one
      «Скопировать» + «⋯» (Сохранить .txt / .md); the storage paragraph and «Открыть конспект» are gone.
- [ ] Opening tab: «Мой конспект» when it has parts, else «Конспект» when ready, else the first tab with content.
- [ ] «Исправить предмет или тип пары» moves into «⋯» of the head; crumbs «Предметы / <предмет> / <пара>».
- [ ] Opening a lesson is a history entry; switching tabs is not (Alt+← leaves the lesson); the found place of a search
      still scrolls into view.
- [ ] UX check of coordinator-round.md 3a; ux-expert «ок»; `npm run test` and `npm run build` pass.
