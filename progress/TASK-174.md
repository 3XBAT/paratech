# TASK-174: «Отменить» on a finished paid job (core)

## Done
- `domain/agent/action/undo.go`: `Undoable(now, Target)` returns `(false, NoUndoResultReady)` for a DONE CancelJob entry whose job is final; new `UndoNeedsTarget` and `CheckUndo`. Zero `Target` = not read (old behavior). StopWork and UNDO_REFUSED entries unchanged, old entries not rewritten.
- `cases/agentjournal`: `targetOf` (read-only `Compensator.Check`, only for DONE CancelJob, bounded by MaxList) feeds `List` and `ListThread`. `Undo` checks before `StartUndo`: a ready result returns `action.ErrResultReady`, entry stays DONE, nothing saved. `UndoRequest` skips ready entries and cancels the rest. The race (finish between check and compensation) still ends UNDO_REFUSED with result_ready.
- `input/wails` mapping already gave refusal result_ready; only a test row was added.

## Decisions to confirm
- An unreadable or missing job is listed as undoable; Undo then settles it (UNDONE for missing, error for unreadable).

## Follow-ups
- `UndoRequest` where all actions are ready returns an empty result without a Problem: UX question for the frontend.
- `Service.BeginUndo`/`FinishUndo` skip `checkUndo` (no production callers); `Find`/`Begin`/`Propose` entries carry a CanUndo that ignores job state (panel and journal use List/ListThread).
- `actions_test.go` uses `tt.name == "finished"` branching; a `wantErr` field would be cleaner.

## Changed existing tests
- `domain/agent/action/undo_test.go`: `Undoable` calls pass `Target{}` (signature).
- `input/agent/actions_test.go` (TestUndoOfAPaidJob/finished) and `material_test.go` (TestMaterialWorkAndUndo): now expect `ErrResultReady` and entry DONE instead of UNDO_REFUSED. Outside the task's file list, test-only, forced by the new behavior.

## Security
Auditor: no findings. Undo is stricter only; the check is read-only; user-initiated.
