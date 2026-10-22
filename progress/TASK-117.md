# TASK-117: input/agent Driver, read tools, offline

Done: `input/agent` Driver (per-operation goroutine, Sink updates, narrow interfaces), R-tools (list_subjects, find_lessons, list_assignments, job_status, delivery_status, search; IDs/dates/enums/counts only), real `Facts` (replaces UnknownFacts) and `BaseSubjects` (names failing ParseName, e.g. with a colon, are dropped), offline stop + Continue, `agentturn.Interrupt` (ctx end -> STOPPED/offline, continuable), `activity.JobStates` (job_status sees every job type), read-result mappers in `domain/agent/values/read.go`, arch test (no Confirm/Execute/Delete/Send in input/agent), SQLite + fake-Hub scenarios incl. injection. Stop retries a version conflict up to 3 times.

## Decisions to confirm
- The text «Не завершено: нет связи · Продолжить» comes from the panel template (TASK-119/120); the Driver publishes only the enum card.
- ctx given to Driver operations must live as long as the app (TASK-119 handoff).
- Unavailable tools (answer_question, inspect_upload, U and C tools) answer `unavailable`.
- Cost estimates unknown (zero): every paid write needs a plan.
- Review/security: reviewer found 1 high (job_status blind to chain jobs), 2 medium (ctx end cancelled, empty Thread in problems) - fixed and re-reviewed (approve). Security auditor: no High/Medium.

## Follow-ups
- find_lessons lists only recorded lessons (no schedule read use case): add one, then list unrecorded lessons (review medium, deferred).
- Facts reads readiness before provenance check (auditor low); needs a port change.
- Step-limit path: publish the cancelled step to the panel (review L1); comment on non-Hub errors at ctx end (L2); JobStates fails on one unreadable row (L3); Interrupt rule could move into the Thread aggregate when write tools arrive, and must keep results of calls that did run (L4).
- Performance: find_lessons without a subject loads recordings per group; search subject filter runs after global caps.
- TASK-135 fixtures marked `pending_driver` in paratech/shared/testdata/agent are not wired into the Driver scenarios.
- TASK-116 follow-ups still open: Compensator already-compensated, `historyFrom`, racing Stop vs plan/answer clicks, 90-day purge, request size cap, global `mu` across the Hub call.
- Full `go test -race ./desktop/...` and full vet left to CI.

## Changed existing tests
`agentturn/fakes_test.go`, `tests/agent_sqlite_test.go` (zeroFacts instead of UnknownFacts), `app/agent_test.go` (driver wired), `activity/service_test.go` (fake store Get), Driver cancel/close tests now expect Interrupt, `TestDriverCancelLeavesThreadUsable` became `TestDriverContextEndLeavesRequestContinuable`.
