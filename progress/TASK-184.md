# TASK-184: desktop on catalog 4

## Done
- `values.CatalogVersion = 4`. generate_draft is out of the catalog (unknown tool for the planner); the constant, the actionpolicy class, the CancelJob compensation and journal/Undo of stored actions stay. Executor branch removed.
- triage_assignment has `topic` (query, <= 500, printable, one line, no bidi/markup). One-of groups are lists (assignment | upload | topic); `OneOf()` returns `[][]string`. Calls and plan steps validate "exactly one" for groups of any size.
- A call with `topic` answers `{"error":"unavailable"}`: nothing journaled, started, logged or stored (until TASK-188).
- Old-request Continue/Retry: the Hub refuses the old seal, desktop maps it to StopFailed (no hang); a new request works. Test: `TestAgentRequestOfAnOlderCatalogAfterTheUpdate`.
- agenteval on `tool_catalog_v4.json`, source scoring (w08 -> triage{assignment}, b05 -> triage{topic}, w06 upload).

## Decisions to confirm
1. A topic call outside a plan is `plan_required` (paid U with unknown cost always needs a plan, Admit in the aggregate); after the student approves a plan with a topic step it answers `unavailable`. So one «Да» can be spent on a step that does nothing until TASK-188. Kept: bypassing Admit would move an invariant out of the aggregate. TASK-188 must design the free-work card against this Admit path.
2. `Tool.Valid()` accepts the catalog plus retired tools (restore paths only: `DecodePlanSteps`, open calls, `PlanCard.Validate`); planner input goes through `ParseTool`/`ParseCall`/`parsePlanSteps`, which reject retired tools.
3. No new stop reason for the old seal: the existing «не удалось» stop is used.

## Follow-ups
- Drop `ActionDeps.Drafts` (kept, ignored) from app wiring and tests.
- Rename/doc `Tool.Valid()` ("never for planner input") — low.
- Add a thread/agentturn-level test: free topic call -> plan_required, approved topic step -> unavailable (only an Actions-level test now).
- Frontend agentView.ts/journalView.ts keep the generate_draft label for stored journal entries.
- Open question: does a paid write with unknown cost always need a plan (MaxPaidUnplanned unreachable)?
- Live agenteval not run (needs Hub and key).

## Changed existing tests
thread/paidwork_test.go (triage_assignment as the paid write instead of generate_draft), actionpolicy/policy_test.go (rule count incl. retired), values args_test/answer_test (groups, label via ParseLabel), input/agent actions_test (generate_draft rows removed), tests/agent_injection_test.go (three-way group), agenteval score_test.

## Security
Auditor: no findings. Reviewer: approve, 1 medium (decision 1, accepted), lows above.
