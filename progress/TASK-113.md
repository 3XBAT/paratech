# TASK-113: AgentStep proto, operation types, golden tool catalog v1

Done: `GatewayService.AgentStep` + messages (ADR-0023), `OPERATION_TYPE_AGENT_STEP/INSPECT_MATERIAL/ANSWER/TRIAGE_ASSIGNMENT`, buf generate/lint/breaking green, Hub handler stays unimplemented (behind API key), contract tests (no url/address/path fields, string allowlist, pinned golden catalog `shared/testdata/tool_catalog_v1.json`, 27 tools, validator with negative tests).

## Decisions to confirm
- `AgentBase` has no profile fields (name, university) on purpose; subjects (id+name), `today`, `time_zone` only.
- `AgentScope.lesson_id` (not lecture_id); empty `subject_id` = no-subject thread. Lesson vs recording ID provenance to clarify in TASK-115.
- `date` in the catalog = end of day in student's TZ; clearing a deadline is not an agent action.
- `answer_question.question` / `search.query` are labels (<= 80 chars): may be too short for real questions (decide before TASK-118/121).
- `list_assignments.status` (upcoming/overdue/any) is new; desktop filters.
- "one of" argument pairs are both optional in the catalog; the desktop parser (TASK-115) enforces exactly one.
- `agent_can_execute=false` for class C means: proposal card is created, nothing happens without a click.
- Tool name exception `link_material` in the forbidden-word check.

## Follow-ups
- README still says only CompleteStream is unimplemented.
- Low (auditor): wrapper types (StringValue/BytesValue) from other packages are not caught by the agent string allowlist walk.
- Low: `create_subject.name` / `create_folder.name` model text persists; check in TASK-115.
- Postgres tests in hub/tests were not run here (vet only); CI covers them.

## Changed existing tests
Contract tables (`wantFields`, `wantStringFields`, `wantRPCs`, public RPC list, enums) extended for the new contract; new cases added to `TestCompleteMapsOperations`, `TestGatewayInvalidRequestsAgainstPostgres`, auth-middleware procedure list. Nothing weakened.
