# TASK-354: «База знаний» folds into «Предметы»: search field, «Пары без предмета» in «Входящие»

- **Agent:** frontend (sonnet)
- **Label:** frontend
- **Depends on:** TASK-353; owner answer В1 of ADR-0034 (this draft follows the recommended «а»)
- **PRD sections / AC:** ADR-0034 §3; ADR-0019; audit А-3.1, 4.8, 9.1, 9.6
- **Агент:** только UI: pure view state
- **Branch:** ralph/<date>-task-354

## Goal
One list of subjects in the app; finding «что говорили на паре» is a search field where the subjects are.

## Allowed to change
- `src/pages/{SubjectsPage,SubjectsParts,KnowledgeBasePage,KnowledgeParts}.tsx`, `src/{nav.ts,App.tsx,agentMentions.ts,
  knowledgeView.ts,libraryView.ts,libraryInboxView.ts}`, `src/styles.css`, tests

## Out of scope
- Core search changes; a command palette (later).

## Acceptance criteria
- [ ] «Предметы» head has a search field (Ctrl+F focuses it); results replace the right side (route `subjects` +
      `search`); a hit opens the lesson screen at its place; under the field: «Нужен ответ, а не место в тексте? Спросите
      в панели справа» with a button that puts the query into the panel input (not sent).
- [ ] «Входящие» shows two groups: files (as TASK-339) and «Пары без предмета» (lessons of «Без предмета» and of
      free-string groups) with «Указать предмет»; the counter counts both.
- [ ] The page `knowledge` and its rail cover entry are removed; «База знаний» and «Билеты» buttons leave LibraryActions;
      answer sources and mentions use the lesson screen; nothing that was reachable is lost (checked list in the progress file).
- [ ] UX check of coordinator-round.md 3a; ux-expert «ок»; `npm run test` and `npm run build` pass.

## Notes
If the owner answers В1 = б or в, this task is rewritten before it starts.
