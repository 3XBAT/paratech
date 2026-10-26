# TASK-217 — desktop on catalog 6: create_assignment

## Done
- values: CatalogVersion 6, `ToolCreateAssignment`, `ArgTime`/`TimeOfDay` (strict HH:MM, canonical), time without due = ErrInvalidArguments (calls and plan steps), plan steps store/compare time, `createdKinds` includes create_assignment, WriteResult accepts an assignment ID.
- Plan matching ignores `note` for create_assignment only (`unplannedArgs`, tested).
- actionpolicy: ClassU, not paid. action: `CompRemoveAssignment`, `RemoveAssignment{Assignment}`; the aggregate requires a non-zero intent for create_assignment and the compensation target equal to `intent.Target`.
- input/agent/assignment.go: ID = "ag-" + hex(first 16 bytes of sha256("ag:<thread>:<turn>:<call_id>")), never from model output (an id/assignment argument is invalid_arguments). Intent PENDING Target=ID, Before 0. Undo: Check = Get, Apply = RemoveCreatedAssignment at the expected version; ErrInUse/ErrVersionConflict/ErrNotByStudent -> ErrTargetChanged -> UNDO_REFUSED changed. Observe: present -> DONE (version 1, so a later edit refuses Undo), absent -> FAILED.
- New narrow `NewAssignments` interface in input/agent (create, remove); app wires homeworkflow.Service.
- Error mapping: unknown subject/lesson -> unknown_id; invalid details -> invalid_arguments.
- Action line: `ActionLine.Assignment` filled by the Driver from local data (title, library subject, due, time); Wails DTO `action_line.assignment {id, title, subjectId, due, time}`; `bindings/agent.ts` parses it. Title never in result_json, journal, logs.
- Tests: values, action, thread, sqlite, input/agent, wails, desktop/tests (scenario, catalog v6 golden, injection), agenteval w12/w13, frontend parse.

## Decisions to confirm
- Scope extensions: `domain/agent/thread/calls.go` (validCreated uses values.IsCreatedKind) and `adapters/sqlite/agentactions.go` (wire form of remove_assignment; JSON in an existing column, no migration).
- Action line always carries a time; a date without time is 23:59 (TASK-220 may show the date only).

## Deploy order (QA)
The Hub with catalog 6 (TASK-216) must be deployed before this desktop.

## Follow-ups
- Hub (small): the v6 prompt lacks "do not give note in a plan step"; the Hub limits plan-step args to 80 chars, so a planned note over 80 is a PROVIDER_ERROR. Desktop ignores note when matching.
- A plan "create_subject -> create_assignment in it" does not bind (subject is optional, not deferred).
- Possible orphan: if the view read fails after the CreateAssignment commit, the journal is FAILED while the assignment exists (no Undo). Low.
- Live agenteval against the Hub not run (needs a key).

## Changed existing tests
values/tool_test (version 6); values/write_test (assignment ID valid in a write result); wails/agent_test (`assignment: null` in card helper); desktop/tests/agent_catalog_test (4->5 test generalised to previous->current); action_test and sqlite agent_test fixtures give create_assignment an intent (now required).

## Security
Review: no high. Auditor: no findings above low (ID derivation, compensation-only removal, Undo races, replay, e-mail text, no title leaks checked). Reviewer medium (invariant in aggregate) fixed.
