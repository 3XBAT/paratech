# TASK-350: One way back — breadcrumbs above the title

- **Agent:** frontend (sonnet)
- **Label:** frontend
- **Depends on:** TASK-346, TASK-347, TASK-348
- **PRD sections / AC:** ADR-0034 §2; audit А-0.8, 0.9
- **Агент:** только UI: pure view state
- **Branch:** ralph/<date>-task-350

## Goal
Every nested screen shows where it is and how to go up, in the same place and style.

## Allowed to change
- new `src/ui/Breadcrumbs.tsx`, `src/pages/{HomeworkPage,TicketsPage,KnowledgeBasePage,KnowledgeLessonPage,SummaryPage,
  TranscriptPage,SubjectsPage}.tsx`, `src/styles.css`, tests

## Out of scope
- The lesson route (TASK-351); the crumbs of the lesson page are finished there.

## Acceptance criteria
- [ ] One component: a line left above h1, links for every level but the last («Задания / Реферат по истории»,
      «Предметы / Матанализ / Билеты»); a link navigates like the current back buttons (route history keeps working).
- [ ] The back buttons «Все задания», «Назад», «Предметы», «Все предметы» are removed where a crumb replaces them.
- [ ] The over-line (`.label` above h1) is used only for the crumb; explanations become one muted line under h1.
- [ ] Keyboard: crumbs are links in the tab order; aria `nav` «Путь».
- [ ] UX check of coordinator-round.md 3a; `npm run test` and `npm run build` pass.
