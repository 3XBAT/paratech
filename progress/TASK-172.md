# TASK-172: planner panel v3 (Hub)

## Done
- hub/pkg/prompts/agent/panel/v3 (system.md, tools.json) + shared/testdata/tool_catalog_v3.json (golden). v1 and v2 unchanged and served.
- New UI arg type `card_ids` (0-10 valid IDs); `reply.assignments` optional of that type. Reason: `ids` type is not allowed on UI tools in the parser/contract.
- values.StopToolUse; AgentAnswer carries the stop reason; failure "output cap" on max_tokens; `agent step` log has stop_reason (no texts).
- DefaultMinOutput 2048, planner default claude-haiku-5-5:4096; ADR-0023 updated (output floor, streaming note).
- README: HUB_AGENT_SEAL_KEYS required for the panel, key example, planner cap, HUB_AGENT_PLANNER_ALIAS.
- docs/qa/tasks/TASK-172.md: the four beta questions.

## Decisions to confirm
- `card_ids` type name; TASK-173 must add it to the desktop catalog (reply.assignments, kind assignment) with provenance checks (unknown ID = refusal/absent).
- Prompt addition: topic works/presentations are the app's own job (triage_assignment -> write_work), never your_part.
- Turnkey work location in prompt: .docx in Предметы -> subject -> ДЗ (or Входящие) plus the panel card.
- Log value for end of turn stays `end`.

## Follow-ups
- Eval harnesses (hub/tests/agent_eval_test.go, desktop/tests/agenteval) still pin catalog v2; add v3 cases with TASK-173.
- Postgres-backed hub tests skipped locally (no HUB_TEST_DATABASE_URL); CI runs them.

## Changed existing tests
agentstep service_test (planner cap fixture 4096, clamp test 3000 / 1024->2048, output-cap and tool_use log asserts), config_test/llm_test (cap 4096), toolcatalog_test (golden v3), contract_test (v1 frozen vs v2 file; v3 current), values_test (StopToolUse name).

## Security
Reviewer: approve; security-auditor: no findings. Provenance of assignment IDs is TASK-173's job.
