# TASK-123: reversible agent tools, plan, Undo, Journal data

Done: step A (material version bump on lesson purge + test, ADR-0024 settled); 10 U tools in input/agent (journal PENDING -> use case -> DONE|FAILED, idempotent per (thread, turn, call_id)); replacing a ready result needs a plan; Undo with version check and refusal reasons (changed, result_ready, cannot_stop, name_taken, not_found); «Отменить всё» with domain Rebase; Recover settles PENDING from a stored intent (incl. intended new values); Thread.Interrupt keeps real outcomes of calls that ran; agentjournal.List = one merged Journal list; migration 0023_agent_undo; library.Material getter (outside the listed files); ADR-0021 notes.

Review: reviewer found B1 (tx and small rules in input/agent: accepted as a documented deviation in ADR-0021), H1 (Undo all refused earlier actions: fixed with Rebase), M1-M4, L2 fixed; re-review approved after one more medium (create_folder version check) fixed. Security auditor: no High/Medium (3 lows; the paid-rerun "created" one fixed).

## Decisions to confirm
- Replace-ready result: class U with a mandatory plan (Classify removed), Undo only stops a running job. Changes ADR-0021 (reviewer H3: owner sign-off).
- Input/agent.Actions owns the compare-and-swap tx for compensations (ADR-0021 deviation from ARCH-23/41).
- Use cases take no key; at-most-once through the journal entry.
- Paid job left PENDING by a crash becomes UNKNOWN. A stale UNDOING (1 min) can be retried.

## Follow-ups
- Journal binding to input/wails (List, Undo, UndoRequest) is NOT done: left to TASK-124 (reviewer H2).
- make_summary / find_homework on a material unavailable until TASK-122; Undo of a finished paid job refused (result_ready).
- edit_due Undo keeps the "edited by student" flag. Rowid order would be lost by a VACUUM (use a sequence column then). Observe for create_subject matches by name (auditor L3).
- UndoRequest could skip recording an Apply that returned after=0.

## Changed existing tests
actionpolicy TestClassify removed (Classify removed) and replace-ready cases expect plan_required (also two tests/ cases); action_test, undo_test (CanUndo/Undoable take now), agentjournal/service_test, agentturn tests and fakes, sqlite/agent_test, migrate_test (v23), tests/agent_sqlite_test (ListThread), homeworkflow rerun_test, wails/activity_test and tests/postprocess_sqlite_test (Retry/RedoSummary return created), tests/agent_actions_test env.
