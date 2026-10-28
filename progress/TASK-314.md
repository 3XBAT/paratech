# TASK-314: «Выполнено» use case — bulk MarkDone with undo, state filter, Today without done

## Done
- `cases/homeworkflow/done.go`: `MarkDone(ctx, ids, done) (DoneMarks, error)` and `RestoreDone(ctx, prior)`. One transaction per assignment (ARCH-23); 1..50 ids (`ErrInvalidRequest`), duplicates handled once; per-ID code `changed | unchanged | not_found | deleted | conflict | failed`; a malformed id is `not_found` without a transaction. `Prior` lists only the changed ones (previous state + the version the call saved); a repeat call changes nothing and has no `Prior` (idempotent). `RestoreDone` expects that saved version (`conflict` otherwise) and returns its own `Prior`, so undo of undo works.
- `ListAssignments` = open only (unchanged callers, incl. the agent's `list_assignments`: format unchanged, no `done` field). New `ListAssignmentsByState(open | done | all, empty = open)`; port `AssignmentList.ListRecent` replaced by `ListRecentByState`; SQL filter before the LIMIT (done rows never crowd out open ones), `all` = open first. `AssignmentView` gets `Done`, `DoneAt`.
- «Сегодня»: `today.BurningOf` skips done, `CardOf` counts them but gives no earliest deadline and no calendar hint; `todayfeed.PostView.HomeworkDone` marks them in the subject history. «Горит» in production is fed by the open-only list; `Deadline.Done` is a second guard.
- Google Calendar untouched on done and on undo (test; ADR-0033 О-13 open, decided nothing).

## Decisions to confirm
- `RestoreDone` back to done gives the time of the restore as `done_at` (the domain cannot put an earlier one back).
- «Отменить» fails with `conflict` after ANY version bump (also a background reprocess), as the task says; UI copy of TASK-316 should cover it.
- Reprocessing keeps the mark (Reextract, SetHints, Edit, Correct, Delete/Restore), positionally: a re-extraction that reorders items moves the mark with the position (Low; either accept or treat done like `userEdited`).

## Follow-ups
- **Medium, before TASK-316 ships the button:** `assignment.Superseded` (domain, TASK-313 scope) lets a re-extraction with fewer items hard-delete a never-edited extracted assignment even if it is done, which breaks "stays in the history". Fix: `&& !a.Done()` in `Superseded` + domain table case + `postprocess` test. Not done here (domain is outside this task's files). The doc comment on `MarkDone` says so. Nothing can mark done yet.
- Readers that call `ListAssignments` and are now open-only: `input/agent/reply.go` (`replyLines`), `input/wails/agentmentions.go`, the homework screen `input/wails/homework.go`; TASK-315/320 should use `ListAssignmentsByState(all)` where a done one must be found.
- TASK-315 maps `PostView.HomeworkDone` into wails `TodayPost`.
- Adapter `ListRecent`/`ListRecentAssignments` is now only used by tests (cleanup).
- TASK-320: the `prior` for `RestoreDone` must come from the action journal, not from the model.

## Changed existing tests
- `homeworkflow/create_test.go`, `screen_test.go`: fakes adapted to the new port signature (`ListRecentByState`), same behaviour for open assignments.
- `domain/agent/services/today/today_test.go`: positional `Homework{...}` literals became keyed (struct gained `Done`).

## Security
Security-auditor: no findings. Reviewer: approve; Medium (Superseded, above, follow-up) and Lows recorded above.
