# TASK-115: agent domain and port skeletons

Done: `domain/agent/{thread,action,values,services/actionpolicy}`, `domain/shared/agent_ids.go` (ThreadID, ActionID, NameSegmentProblem), port skeletons `cases/agentturn` and `cases/agentjournal`, arch-test additions, golden-catalog test (`desktop/tests/agent_catalog_test.go`).

## Decisions to confirm
- No SubjectID in shared (TASK-121 adds it): agent code uses `values.Ref{kind,id}`; thread key is `thread.Key` (subject ID or `NONE`). Map `shared.SubjectID(ref.ID())` later.
- Lesson and recording are separate ID kinds.
- Persisting names (create_subject/create_folder): letters, digits, single spaces, `.,-()`, <= 80, Windows file-name rules.
- Budget: <= 1 paid job without plan (ADR-0021), not 3 (design doc).
- Plan steps must carry all required args (one-of pairs exactly one); only IDs of kinds created by an earlier create_subject/create_folder step may be omitted. UI tools cannot be plan steps.
- RevisePlan clears approved plan and used flags; a new «Да» is fresh consent (used steps may run again).
- Budget stop cannot be continued; user Stop can.

## Follow-ups (for TASK-116 and later)
- Deferred plan IDs match any known ID of that kind and order is not checked: the case must record IDs created in this request and require deferred args to be created ones (add thread tests).
- Paid R tools (answer_question, inspect_upload) are outside the "<= 1 paid job without plan" count: owner decision.
- Third question round is refused (ErrTooManyRounds), not a stop: confirm.
- Upload vs material refs (ReturnToInbox holds material, place_upload takes upload); ProgressCard.Job is shared.JobID but CancelJob uses Ref: unify.
- ADR-0021 says Subject/Material/Folder IDs live in shared: TASK-121 / mapping.
- action: CheckTarget does not check UNDOING; retried UNDOING may see changed version, compensator must be idempotent.
- History.Recent returns parallel slices; HistoryTurn has no Validate; from/to rule inline in parseArgs; EncodePlanSteps/DecodePlanSteps must store the whole ordered plan as a unit.

## Changed existing tests
Only `desktop/tests/arch_test.go` (rows added). Review fix commit adapted plan-step fixtures in values/args_test.go and thread/thread_test.go (steps now complete; wildcard no longer valid).
