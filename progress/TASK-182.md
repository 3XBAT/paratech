# TASK-182: planner panel v4 (Hub)

## Done
- hub/pkg/prompts/agent/panel/v4 (system.md, tools.json) + shared/testdata/tool_catalog_v4.json (golden). v1-v3 unchanged and served.
- v4 = v3 minus generate_draft, plus optional `topic` (query, <=500) on triage_assignment; notes.one_of: exactly one of assignment|upload|topic.
- system.md v4: answer only the last message (history = context), planner never writes drafts (triage_assignment -> write_work), topic without assignment -> triage_assignment{topic} not cannot_do, screens «Быстрый черновик» + «Сделать под ключ». Removed v3's "ask which assignment" line.
- Hub enforces one_of groups from catalog 4 (toolcatalog.go); catalogs 1-3 leave pairs to the desktop. Tool description gets "Give exactly one of: ..." from catalog 4.
- ADR-0021 addendum (catalog 4).
- Tests: toolcatalog (TestCatalogV4, bad one_of notes), prompts (TestPanelV4Prompt), shared (v3 frozen, one_of groups), hub/tests eval cases (draft request, topic without assignment, only the last).

## Decisions to confirm
- Hub-side one_of enforcement covers make_summary, find_homework, place_upload too. A violation is a charged permanent PROVIDER_ERROR instead of a correctable desktop is_error result (reviewer, medium). Kept: the task requires invalid calls for triage_assignment; the description hint lowers the risk.

## Follow-ups
- TASK-184 (desktop): desktop/tests/agent_catalog_test.go parses one_of with a pairs-only regex; update for the 3-way group. Sanitize `topic` wherever it reaches file names/titles (Windows reserved names, `..`, Cf/bidi chars).
- Eval "answers only the last" uses echoTurns; switch to agent_reply turns after TASK-181 lands.
- agentstep TestCatalogVersionsServedSideBySide has no v4 case (outside file list).
- one_of groups are parsed from note prose by regex (also in shared/contract_test.go); a structured field in a later catalog would be sturdier.
- Postgres-backed hub tests skipped locally; CI runs them.

## Changed existing tests
toolcatalog_test (golden -> v4, goldenV3, counts 26/22, generate_draft cases -> create_subject/create_folder, version literals), prompts_test (v1-v4; v3 looked up by number), shared/contract_test (current = v4, pinnedTools without generate_draft, topic in queryArguments, V2 frozen vs v3 file, validator cases target triage_assignment), agent_eval_test (newEvalHubAt helper; old tests unchanged in behavior). All because generate_draft left the current catalog.

## Security
Reviewer: approve (2 medium: decision above, follow-up above). security-auditor: no findings.
