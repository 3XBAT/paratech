# TASK-353: Subject page with tabs «Пары · Файлы · Задания» and «Билеты к экзамену»

- **Agent:** frontend (sonnet)
- **Label:** frontend
- **Depends on:** TASK-352
- **PRD sections / AC:** ADR-0034 §2, §3; ADR-0024; audit А-4.1, 4.2, 4.7, 4.8 (part)
- **Агент:** только UI: pure view state
- **Branch:** ralph/<date>-task-353

## Goal
Everything about one subject is on its page: its pairs, its files, its assignments, and its exam tickets.

## Allowed to change
- `src/pages/{SubjectsPage,SubjectsParts,KnowledgeBasePage,KnowledgeParts,TicketsPage}.tsx`, `src/{libraryView,
  knowledgeView,ticketsView}.ts`, `src/nav.ts` (Route `subjectTab`), `src/App.tsx`, `src/styles.css`, tests

## Out of scope
- Removing the «База знаний» page and the search (TASK-354).

## Acceptance criteria
- [ ] Head: name, teachers (as today), «Билеты к экзамену» (opens TicketsPage with this subject chosen), «⋯».
- [ ] Tab «Пары»: the knowledge-base lessons of this subject (by `subjectId`), each row opens the lesson screen;
      «Импортировать запись» and «Добавить конспект прошедшей пары» as today; export «Сохранить все конспекты одним
      файлом» in «⋯» of the tab; empty: «Записей по предмету пока нет. [Импортировать запись] [Добавить конспект прошедшей пары]».
- [ ] Tab «Файлы»: the folders of TASK-341/347 unchanged; the tree folders scroll inside this tab.
- [ ] Tab «Задания»: open assignments of the subject by deadline; a row opens the assignment; «Добавить задание» with
      the subject prefilled.
- [ ] The tab is in the route (Alt+← works); the default tab is «Пары» when it has lessons, else «Файлы».
- [ ] UX check of coordinator-round.md 3a; ux-expert «ок»; `npm run test` and `npm run build` pass.
