# TASK-380: E2E pack «Агент» (scripted)

- **Agent:** tester (sonnet)
- **Label:** test
- **Depends on:** TASK-378
- **PRD sections / AC:** ADR-0035 §1, §6; ADR-0021, ADR-0023, ADR-0028, ADR-0032, ADR-0033; AC-18; TASK-366, TASK-369
- **Агент:** нет: тестовая инфраструктура
- **Branch:** ralph/<date>-task-380

## Goal
The four agent failures the owner found by hand become deterministic checks of the app's side: what the tools gave the
model, what the cards show, where the links lead.

## Allowed to change
- `paratech/e2e/specs/agent/**`, `paratech/e2e/scripts/llm/agent/**`

## Out of scope
- Judging the model's own wording (TASK-382, live).

## Acceptance criteria
- [ ] **Recording titles**: «какие у меня записи?» → the scripted planner calls the listing tool; the recorded
      `tool_result` sent to the provider contains the titles of the TASK-378 recordings; the reply card renders.
- [ ] **Link question**: «дай ссылку на пару по матанализу» → the tool result contains the synthetic link; every
      navigation card/button of the reply opens a screen that shows «Подключиться» for «Матанализ»; clicking it
      writes the link to `opened.jsonl`. A reply that opens a screen without the link fails the row.
- [ ] **«что было на паре по литературе»** → answer card with a source; the source opens the lesson page of the
      TASK-378 recording on the right tab.
- [ ] **«добавь предмет по последней записи»** → a U action: the subject appears in «Предметы» and the lesson moves
      under it; «Отменить» restores the previous state.
- [ ] **Tool failure honesty**: a scripted call that fails in the app (e.g. an unknown recording id) → the recorded
      next request carries `is_error` with the app's real reason text; the card shown is the error/`cannot_do` card,
      not a success. A C-class tool proposal shows a confirm card and nothing is executed without the click (AC-18).
- [ ] No unscripted LLM call in the pack; pack ≤ 5 min; `npm test` passes; run log in progress.

## Notes
Tool names and argument shapes are taken from the current catalog in `hub/pkg/prompts/agent/panel/v<N>/tools.json`;
the script pins the catalog version it was written for and fails clearly when the app sends another.
