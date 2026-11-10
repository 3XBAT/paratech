# TASK-432 — call links per lesson kind (bindings)

ADR-0032 «Addendum 2026-11-10», ADR-0038 D-2. Core is TASK-431; UI is TASK-433.

## Done
- `cases/todayfeed`: port `SubjectCallLinks` is now `CallLinkSets`; a lesson row gets `CallLinks.Join(row.Kind)` as `CallLink` plus `CallLinkKind`. No link: «Добавить ссылку» as before; failure behaviour unchanged.
- `cases/recordingprompt`: port likewise; `CallSubject` returns `Call{Subject, LessonKind}` (zero = offers nothing), link checked with `Join` + `IsZero`; the running-recording case is as TASK-405.
- `input/wails`: `SetSubjectCallLink(subjectID, kind, link)`, `ClearSubjectCallLink(subjectID, kind)` (kind parsed by `subject.ParseCallKind`, bad kind = "invalid", never reaches the use case), `OpenSubjectCallLink(subjectID, lessonKind)` over `OpenJoinLink`. `library.ErrInvalidCallKind` is mapped to "invalid". `LibrarySubject.callLinks` (`{general?, lecture?, seminar?, lab?}`, `{}` when none; `callLink` stays general), `TodayLesson.callLinkKind` (`""` when none), `PromptCallSubject{subjectId, lessonKind}`.
- `cases/library`: old `CallLinks` removed (no callers). `SetCallLink`, `ClearCallLink`, `CallLink`, `OpenCallLink` stay: `app/prompts_calllink_test.go`, `app/e2ehooks_test.go` and the create-with-link path still use them.
- Frontend: `bindings/{library,today,prompt}.ts` parse `callLinks`, `callLinkKind`, `{subjectId, lessonKind}` and take the new arguments; Today join passes the lesson's `kind`, the popup passes the `lessonKind` from `callSubject`; set/clear from the existing flows send `"general"`. No per-kind UI, no text changes. Helpers `callKindOfLesson` / `lessonCallLink` mirror the Go rule only to keep the card's host right after a save (the core decides at the click); a contract test reads `calllinks.go` and pins the mapping.
- Tests: tables on fakes (lecture own link, seminar only general, lab none, practice -> seminar, unknown kind) in todayfeed and recordingprompt; exact JSON of `LibrarySubject`, `TodayLesson`, `PromptCallSubject`; invalid kinds for set/clear; lecture vs seminar open different links (wails fake); scenario `desktop/tests/calllinks_sqlite_test.go` on real SQLite; frontend parsing/wiring tests.

## Decisions to confirm
- `CallSubject` gives the lesson kind as the stable code string (the same as `TodayLesson.kind`).
- `callLinkKind` is set only when `hasCallLink`.
- `cases/recordingprompt/doc.go` (comment only) changed although outside the file list.

## Follow-ups (for TASK-433 / 436)
- ux-expert, must-fix in the task list: TASK-436 (agent `set_call_links`) does not depend on TASK-433. If 436 merges first, the student can have per-kind links that the UI cannot show or edit (card «Изменить» saves the general link with a hint naming the lecture host; «no_link» blanks all cards of the subject; the subject page says «ссылки нет»). Add TASK-433 to the dependencies of TASK-436 (supervisor; tasks.json outside my status line).
- TASK-433 acceptance: card «Изменить»/«Убрать» act on `callLinkKind` with an honest hint for the chosen scope; on «no_link» reread the subject instead of `withoutLink` for all pairs; the subject page shows per-kind links.
- Add a Go-side check that pins the `callLinks` / `callLinkKind` JSON tags to the TS tests (the existing TS test pins only `callLink`).
- Optional: remove the single-link `SetCallLink`/`ClearCallLink`/`CallLink`/`OpenCallLink` when the app tests move to the kind API.

## Changed existing tests
Only signature/fixture updates: todayfeed `fakes_test.go`, `calllink_test.go`; recordingprompt `calllink_test.go`, `calllink_running_test.go` (compare `Call`); wails `library_test.go`, `library_calllink_test.go`, `prompt_test.go`, `prompt_calllink_test.go`, `today_test.go`; `cases/library/calllink_test.go` (`TestCallLinks` deleted with the function), `calllinks_test.go`; `desktop/tests/today_sqlite_test.go` (fake port); frontend `callLink364*.test.mjs`, `popupJoin370.test.mjs` (new arguments, wrapped reply).

## Review
Reviewer (opus): approve, no blocker/high; mediums deferred to TASK-433 (above) plus the Go/TS mirror contract test (added) and a joined-statements slip (fixed). ux-expert: ok for this diff, the sequencing item above. security-auditor (opus): no findings.
Security: kind and link validated on write and read; `lessonKind` only selects among the same subject's links; the popup never sees a link; nothing logs link text; `parseCallLinks` loops over a fixed kind list (no prototype pollution).
