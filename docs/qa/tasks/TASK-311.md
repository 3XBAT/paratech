# TASK-311 — check steps

Binding and TypeScript parsers only: nothing is visible in the app yet; the button and the review window come in TASK-312, the agent card in TASK-323. On a cold app the student sees no change.

1. From a cold checkout run `go -C <repo>/paratech test -race -count=1 ./desktop/internal/input/wails/... ./desktop/internal/app/...`. Expected: ok.
2. Read `TestCreateFromScheduleProblems` (every problem code), `TestCreateFromScheduleRetryMerges` (a retry never loses what the first run made) and `TestUndoFromScheduleUnknownToken` (an unknown token answers `invalid`, the use case is not called).
3. Read `TestScheduleOneCallAtATime`: a second create or undo answers `busy` at once while the subjects list stays callable.
4. Read `TestScheduleTextNotLogged`: a hostile schedule string appears in no log record and no error.
5. Run `npm --prefix <repo>/paratech/desktop/frontend run test`; `libraryschedule.test.mjs` checks the parsers (unknown code = parse error).
