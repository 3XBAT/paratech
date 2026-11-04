# TASK-286 — library logs carry a cause class, never the raw error

## Done
- `Subject` String/Format/LogValue without teachers: already done in TASK-285 (`TestPrintingHidesTeachers`); re-checked, no gap.
- `input/wails/library.go` `LibraryService.fail`: known errors log the sentinel's own text; the rest log `result` + `cause` ("local storage" for `library.ErrUnavailable`, else "unexpected") via the new `libraryCause`. What the page receives is unchanged.
- Same leak fixed in the same package: `library_discard.go` (`subjectProblem`, `removedVersions`) and `subjectoverview.go` (`overviewFail`, which also logged the whole chain for known errors).
- `subjectoverview.go`: stale comment «No teacher: it waits for the owner's decision» replaced by «It carries no teacher.».
- `input/wails/README.md`: convention «class or a sentinel's text, never the wrapped chain».
- Tests (`library_logs_test.go`): leaking errors through `Subjects`, `DiscardSubject`, `SubjectOverview`, `MarkSubjectOpened`; the log holds none of the fake name, e-mail, path or inner cause.

## Decisions to confirm
- Scope widened slightly to the other raw-chain logs of library use case errors in the same package (discard, overview).

## Follow-ups (lows)
- Security F1 (Low): `input/wails/library_calllink.go:132` logs the opener's chain, which can carry the `%TEMP%` path with the Windows account name (from `weblink_windows.go` `os.CreateTemp`). Log a class (`cause`) or strip the path in the adapter.
- `removedVersions` log line has no test of its own; `overviewFail` repeats the `libraryCause` logic inline (an `overviewCause` helper would keep the strings together).
- About 100 other `"err", err` logs in non-library pages of input/wails were not traced.

## Changed existing tests
None.

## Security
Auditor: no high/medium, one low (above). Reviewer: approve, lows only. Verified: go vet (also GOOS=windows), `go test -race` of input/wails and domain/library, desktop/tests (architecture), lint v2.14.0 0 issues.
