# BUG-20261110-ci-flakes: two CI flakes on PR #451 (frontend-only), both passed on rerun

No tasks.json entry.

## 1. `checks`: `paratech/desktop/internal/app` «test timed out after 10m0s»
**Cause.** Not a hang: the dump shows the running test at 0 s, inside `sqlite.Open` → goose `Up` (parsing an
`ALTER TABLE ADD COLUMN`), and no blocked goroutine. The package as a whole ran out of go test's 10 min default.
Most of its ~190 tests open a core on a fresh data directory, and `buildCore` migrates a new database each time (51
migrations); under -race the modernc SQLite is instrumented Go and one migration run takes seconds. Measured on the
dev machine before the fix: `go test -race -count=1 ./desktop/internal/app/` = **586 s**, 14 s under the limit; the CI
runner is slower. `adapters/sqlite` already avoids this with a migrated template (`openAt`/`seedFromTemplate`); the app
package did not.

**Fix (tests only).** `app/dbseed_test.go`: a database migrated once per test process, copied into a data directory
that has none; `buildCore` still runs `Migrate`, which finds it up to date. Seeded at the test env's instance lock
(`main_test.go`: openCore takes it right before `buildCore`, after creating the data dir) and before the direct
`buildCore`/`sqlite.Open(Migrate)` calls of `prompts_test.go`, `pipeline_test.go`, `lecturechain_test.go`,
`schedule_test.go`, `agent_test.go`. A missing data dir or an existing database is left as is, so
`TestHeldLockOpensNothing` (own lock, no database) and `TestOpenCoreStoresSettings` (dir created by openCore) keep
their meaning. No timeout raised.

**Proof.** After: `go test -race -count=1 ./desktop/internal/app/` = **77 s** (was 586 s; a core-opening test ~0.3 s
instead of ~4.5 s). `TestRunNowTranscribesAndEnqueuesPostProcessing -race -count=30`: pass (50 s). `-tags e2e`: pass.

## 2. `windows`: `hubclient` TestAnswerQuestionCallerEndIsNoTimeout/caller_deadline
**Cause (production race).** The caller's 100 ms deadline goes to the Hub as `Connect-Timeout-Ms`, truncated to
whole milliseconds (connect `protocol_connect.go:353`), so the Hub's context ends up to 1 ms before the caller's. Its
`deadline_exceeded`/`canceled` came back before the client's own timer fired: `answerError` (`answer.go:158`) saw
`ctx.Err() == nil` and `mapCode` (`errors.go`) turned the wire code into `ErrTimeout`, a Hub timeout instead of the
caller's deadline. Reproduced locally: 1 failure in 300 runs.

**Fix.** `errors.go` `callerEnd(ctx)`: the caller's error, or `context.DeadlineExceeded` when ctx's deadline is less
than a millisecond away (the truncation window). Used at the same caller-end check of all seven call paths
(`answerError`, `stepError`, Complete, inspect, summary, quota, triage): the same bug in each.

**Proof.** New `TestHubEndOfCallerDeadlineIsNoTimeout` (a Hub wire `deadline_exceeded`/`canceled` 0.9 ms before the
caller's deadline is the caller's, for the answer and the step; an hour before it is still a Hub timeout): fails 5/5
without the fix, passes `-race -count=50`. `TestAnswerQuestionCallerEndIsNoTimeout -count=500`: 500/500 (before: 1/300
failed). `go test -race` and `go test -tags e2e` of hubclient pass.

## Commands
- `go vet` app + hubclient (windows, and GOOS=linux CGO_ENABLED=0), `GOOS=windows go vet -tags e2e` hubclient: clean.
- golangci-lint v2.14.0 on app and hubclient: 0 issues.

## Changed existing tests
- app `main_test.go` (testEnv lock seeds the database), `prompts_test.go`, `pipeline_test.go`, `lecturechain_test.go`,
  `schedule_test.go`, `agent_test.go`: seed before opening; no assertion changed. Nothing skipped or deleted.

## Follow-ups
- `adapters/sqlite` takes ~200 s under -race on CI and `audiocapture` ~110 s; not near the limit yet.
