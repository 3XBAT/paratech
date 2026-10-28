# TASK-310 — check steps

Use case and one SQL query only: nothing is visible in the app yet (the binding, button and agent card come in TASK-311, 312, 323).

1. From a cold checkout run `go -C <repo>/paratech test -race -count=1 ./desktop/internal/cases/library/... ./desktop/internal/adapters/sqlite/...`. Expected: ok.
2. In `schedule_test.go` read `TestCreateFromScheduleRetryAfterFailure` (a failure at each of three saves, then a retry with the same token gives the result of one clean run, nothing is created twice).
3. Read `TestUndoFromSchedule` and `TestUndoFromScheduleStale`: empty subjects are removed, subjects with notes or files stay and are listed, a foreign ID is refused, and when the schedule changed after the «Да» the empty subjects are still removed while the added spellings are reported as kept (`stale`).
4. Run `TestScheduleSubjectStrings` in the sqlite package: deleted lessons and deleted recordings are not counted, kinds are counted separately.
