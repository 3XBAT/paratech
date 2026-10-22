# TASK-114: Hub AgentStep

Done: `GatewayService.AgentStep` end to end (ADR-0023): `hub/pkg/prompts` (agent/panel/v1, tools.json = golden catalog, test ties them), `domain/llm/values` agent types and limits, `services/seal` (HMAC, version byte, constant-time), `services/toolcatalog` (schema check of returned calls), `cases/agentstep`, Anthropic adapter (tools, tool_choice any, tool_use/tool_result, cache_control), Connect handler, config (`HUB_AGENT_SEAL_KEYS`, `HUB_AGENT_PLANNER_ALIAS`, `HUB_AGENT_STRONG_ALIAS`), migration 0006, testenv fakes, README, docs/refs/anthropic.md section 7. Server-prompt mechanism for INSPECT_MATERIAL/ANSWER/TRIAGE_ASSIGNMENT through Complete is in place; the `ops/*` prompt files are not (TASK-118/121/129 add them and `ops` to the embed line).

## Decisions to confirm
- AgentStep is off (unimplemented) when no seal keys are configured, not a start-up failure.
- `plan_steps` / `questions` inner shapes are fixed by the Hub (the catalog does not define them): plan_steps `[{tool: non-UI tool, arguments: {snake_case: string <=80}}]` 1-8; questions `[{kind: choice|text, question, options[], default}]` 1-5, choice needs 2-6 options. The desktop parser (TASK-115) must match.
- Security fix F1: unusable answers that the provider billed (max_tokens stop, bad or schema-invalid calls) are charged and stored as the failure marker `[]`; a retry with the same key returns the same permanent error without a provider call. Refusals and errors without usage are released. `max_output_tokens` below 256 is raised to 256.
- Catalog 80-char limits left as is (TASK-118 raises them).

## Follow-ups
- ADR-0023 does not yet describe the charge-on-unusable rule and the output floor (reviewer M1, docs only).
- Desktop client: a user-started retry after a failed step must use a new idempotency key (replay returns the same error for ~24 h).
- To verify live (refs section 7): minimum cacheable prefix for the planner model, cache prices, thinking vs `tool_choice: any`, `tool_use.id` format. If thinking conflicts, every step fails.
- Low: pre-checks of turn/call counts in the use case duplicate domain rules; `.gitattributes` eol=lf for `hub/pkg/prompts/**` (CRLF in system.md on autocrlf checkouts); rate-limit flag help still says "Complete calls"; `Profile.MinOutput` not configurable from app; no Postgres scenario for the failure-marker replay.
- CI does not set `HUB_TEST_DATABASE_URL`, so the Postgres tests never run in CI (pre-existing).

## Changed existing tests
Extended only: config_test/llm_test (new defaults and agent fields), connect/gateway_test (operation mapping, error cases), account/reservation_test (new operations). After F1: `TestInvalidReturnedCalls` now expects a charge, `TestStepClampsTheOutputCap` cap 100 became 300 (floor 256) plus a tiny-cap case, malformed tool_use cases in the adapter moved to a test that checks usage is kept.
