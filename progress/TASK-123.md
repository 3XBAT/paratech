# TASK-123: reversible agent tools, plan, Undo, Journal data (PAUSED, not finished)

State at pause: implementation done and committed on ralph/20261022-task-123 (8 commits after c8419cd9). Not pushed, no PR.

## Done
- Step A: material version bump on lesson purge (+test), ADR-0024 settled (resolver port sentence, no backfill).
- Step B: 10 U tools in input/agent (journal PENDING -> use case -> DONE/FAILED), replace-ready needs a plan, Undo with version check and refusal reasons (changed, result_ready, cannot_stop, name_taken, not_found), Undo all, Recover settles PENDING from stored intent, Thread.Interrupt keeps real outcomes, agentjournal.List = one merged Journal list, migration 0023 (agent_undo), library.Material getter (outside the listed scope), ADR-0021 notes.
- Verified by implementer: go vet ./desktop/..., GOOS=windows vet of touched packages, go test -race of touched packages, lint (re-run after last small lint fix NOT done).

## Left
1. Re-run lint on touched packages (heavy.sh) and the touched-scope tests.
2. reviewer + security-auditor in parallel (opus); fix blocker/high/medium; re-review if needed.
3. Check migration number against origin/master (TASK-119 may have taken 0023): renumber file, migrate_test, docs/contracts/schema.sql.
4. tasks.json status line, docs/qa/tasks/TASK-123.md, commit "ralph: TASK-123 done (...)", rebase if master moved, push, gh pr create (body-file), run .github/scripts/audit-pr.sh, report. Do not merge.

## Decisions to confirm
- Replace-ready result goes through a plan (Classify removed); Undo of it only stops a running job.
- No idempotency key in use cases: the journal entry per (thread, turn, call_id) makes retries act once.
- set_lesson_info uses a digest of the correction as its version.
- Paid job left PENDING by a crash becomes UNKNOWN.

## Follow-ups
- make_summary / find_homework on a material: unavailable until TASK-122.
- Undo of a finished paid job refused (result_ready): no use case removes a summary/lecture/homework/draft version.
- edit_due Undo keeps the assignment marked as student-edited.
- Journal binding for UI is left to TASK-119/124.

## Changed existing tests
actionpolicy TestClassify removed (Classify removed); thread replace-ready cases expect plan_required (also two tests/ cases); action_test, agentjournal/service_test, agentturn tests and fakes, sqlite/agent_test, migrate_test (v23), tests/agent_sqlite_test (ListThread).
