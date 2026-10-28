# TASK-309 — check steps

Domain only: nothing is visible in the app yet (the use case, button and agent card come in TASK-310, 311, 312, 323).

1. From a cold checkout run `go -C <repo>/paratech test -race -count=1 ./desktop/internal/domain/library/...`. Expected: all packages ok, including `schedulesubjects` and `subject`.
2. Read the table `TestObviousGroup` and `TestAbbreviationsJoinTheLongName` in `schedulesubjects_test.go`: «Математический анализ (лек.)» and «Матанализ, сем.» become one subject, «матан» joins «Математический анализ» as doubtful, «Физика» and «Физкультура» stay separate.
3. Run `TestShortFormIsPolynomial`. Expected: passes in well under a second.
