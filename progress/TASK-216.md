# TASK-216 — panel catalog/prompt v6: create_assignment, time type

## Done
- `hub/pkg/prompts/agent/panel/v6` (tools.json = v5 + `create_assignment`, class U, free, agent_can_execute; `types.time`; date description extended), golden `shared/testdata/tool_catalog_v6.json` (byte-identical). v1..v5 untouched and still served (the Hub loads every embedded folder; no wiring change).
- system.md v6 = v5 + "Adding an assignment" (title from the student's words, IDs only from context/results, ambiguous date -> ask_questions, no date -> no deadline and say so, time only if named, names stay text, no propose_send, calendar only on request, several -> show_plan, short reply). v5 rules kept; the "Добавить задание" button is not mentioned (TASK-219).
- toolcatalog: `typeTime`, pattern `^([01][0-9]|2[0-3]):[0-5][0-9]$` shared by provider schema and check; `time` in a catalog below 6 is ErrInvalidCatalog. "time only with due" is not checked on the Hub (desktop, is_error).
- Tests: catalogv6_test, TestPanelV6Prompt, shared contract pins and TestToolCatalogV5IsFrozen, eval cases (matan with due, "завтра в 9" with time, Friday-on-Friday ask_questions, address injection, bad time shapes).

## Decisions to confirm
- Hub cannot stop an address typed inside free-text title/note; the prompt and the desktop (TASK-217) keep it out.

## Follow-ups
- DESKTOP (TASK-217): move `CatalogVersion` to 6; deploy the Hub with catalog 6 first. Desktop must treat title/note as display text only.
- Plan-step arguments are 1..80 characters on the Hub: a planned create_assignment step with a note over 80 characters is an invalid call (permanent PROVIDER_ERROR). Decide before v6 is released: add a prompt line (plan step title/note <= 80) or keep notes out of plan steps in TASK-217.
- Eval cases replay recorded answers; the owner's live run (level 2) is the real check of the date/address rules.

## Changed existing tests
prompts_test TestEmbeddedPanel (6 versions); shared contract_test (current catalog 6, `time` arg type and create_assignment pinned, note as query, v4 frozen compared to the v5 file); toolcatalog textreply_test (v6 added).

## Security
Auditor: no findings. Reviewer: approve (two lows, above). Hub tests needing Postgres skipped locally (no Postgres code touched).
