# TASK-173: desktop on catalog 3 (Agent core)

## Done
- values.CatalogVersion = 3; UI arg type `card_ids` (0-10 assignment IDs, duplicates dropped, order kept); `reply.assignments` optional.
- Provenance of EVERY reply ID: `Thread.CheckRefs` (aggregate, same known-set as Admit/ProposePlan), called from cases/agentturn/step.go. One unknown ID refuses the whole reply with `unknown_id`, no card, the planner continues.
- ReplyCard carries `Assignments` refs and `Lines` (assignment, title, subject, due). The Driver (input/agent/reply.go) fills lines from the existing ListAssignments / ResolveSubjects ports; deleted or blank/corrupt-title assignments are skipped; read failures degrade to a reply without the list. The planner result stays `{"shown":true}`; no titles or texts in history or log.
- Wails card DTO `assignments [{id,title,subjectId,due}]`; frontend/src/bindings/agent.ts parses it (drawing is TASK-176).
- agentturn.MaxOutputTokens 4096. ADR-0022 addendum.
- Evals on v3: desktop agenteval (catalog golden v3, live cases b01-b05 with the four beta questions plus a topic-only request, Address and list metrics, `also` alternative classes), hub/tests/agent_eval_test.go (v3 beta questions, reply card_ids shape, injection sweep on v3).
- Topic-only request: triage_assignment without an ID is `invalid_arguments`, with an unseen ID `unknown_id`; the turn ends with an honest reply (test). No free-topic flow built; nothing depends on generate_draft.

## Decisions to confirm
- Files outside the task list: domain/agent/thread (CheckRefs, review ARCH finding), hub/tests/agent_eval_test.go (supervisor asked for v3 hub evals), one line in adapters/hubclient/agent_test.go (hardcoded CatalogVersion 2 -> agent.CatalogVersion; TASK-142 may need to rebase).
- b05 (topic without assignment) accepts questions, read or reply (prompt allows an honest reply; scorer counts list_assignments + reply as `read`).
- Duplicate IDs are dropped, not refused (the Hub accepts them).

## Follow-ups
- desktop/tests/agent_injection_test.go does not put injected text inside card_ids arrays.
- TASK-176 must render titles as plain text.
- replyLines reads ListAssignments (cap 200): an older scope assignment would be skipped as missing.
- Nit: the "valid line" rule is duplicated in input/agent/reply.go and ReplyCard.Validate; an AssignmentLine.Validate would remove it.
- Live agenteval run (needs Hub + tester key) not run.

## Changed existing tests
values_test TestToolsAreUniqueAndParse (card_ids is ID-carrying); wails/agent_test (card helper has `assignments`); hubclient/agent_test (CatalogVersion); hub/tests injection sweep v2 -> v3; agenteval TestBetaQuestionsInTheCaseSet, TestParseCaseSetRejects, TestReport (extended for `also`).

## Security
Reviewer: changes requested (ARCH: provenance in use case) -> fixed, re-review approve. security-auditor: no findings.
