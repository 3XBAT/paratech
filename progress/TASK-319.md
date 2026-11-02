# TASK-319 — wave 10 on the desktop: tool specs, policy, compensations (declared, not served)

## Done
- `values`: `CatalogVersion` stays 9. `Tools/ParseTool/SpecOf/ParseCall/DecodePlanSteps` are exactly catalog 9; wave tools only via `ToolsAt/ParseToolAt/SpecAt/ParseCallAt` with version >= 10 (`CatalogWave10`). Test pins "v9 = v8" and that no wave tool is visible at 9 (ParseTool, SpecOf, ParseCall, a show_plan step, a stored plan).
- `wave10.go`: specs of propose_subjects_from_schedule, change_assignment, mark_assignments_done, list_notes, note, get_settings, set_setting, quota_status, recording, offer_button; `list_assignments.state` (open|done|all). New arg type `bool`, `ArgSpec.MaxItems` (ids, 50 for mark_assignments_done). Per-action required/forbidden fields, also inside plan steps. set_setting value is checked per setting (toggles on/off, recognition_load gentle/normal/max). `note.text` = reply text (<= 4000). ID kinds `note`, `schedule_group`; card kinds `schedule_subjects`, `delete_assignment`, `offer_button`.
- `actionpolicy`: classes by ADR §10; `Decide(call, Facts)` gives class, card and `NoUndo` per call. change_assignment delete + `Facts.HasCalendarEvent` -> C (card delete_assignment), otherwise U; recording stop U with `NoUndo`; `Of(tool)` keeps one class per tool (golden).
- `action`: new compensations RevertAssignment, RestoreAssignment, DeleteAssignment (undo of restore), RestoreDone (1..50 marks, fixed array), RemoveNotePart, RemoveNote, ReanchorNote, RetitleNote, RestoreNote, RestoreSetting, StopRecording. Prior values of revert/reanchor/retitle/setting must equal `Intent.Prior` recorded in the journal before the action; the types hold no model value types (reflection test); forged data test. recording{stop} may complete without a compensation (DONE only; no undo).

## Decisions to confirm
1. show_plan is not split per catalog: 200 steps / 32 KiB already apply to every catalog since TASK-317; the Hub keeps v9 at 8.
2. Order of catalog 10: v9 order with list_assignments replaced, then the §10 table order; the Hub golden (TASK-325/326) decides the final one. The Hub golden needs a `bool` arg type.
3. offer_button ids: open_material -> material; open_call_link/copy_call_link -> lesson; open_summary/open_lecture_text/open_transcript -> recording; save_as -> work; the rest none. `undo_action` takes no id (the desktop undoes the chat's latest undoable action). Confirm save_as on a work (or a recording).
4. `note.date` needs a subject but §10 has no `subject` argument: either add `subject` (required with `date`) before the Hub golden, or record that the subject comes from the request scope (and what happens without one). Product/contract decision for TASK-321/325.
5. Edit of an assignment with a Google event stays U (C-4); `time` without `due` is refused (as create_assignment).
6. change_assignment{restore} compensates with DeleteAssignment (not in the criteria list; a U action needs an Undo).

## Follow-ups (route before TASK-326)
- HIGH for dispatch: `thread.Admit`/`ProposePlan` still classify with `actionpolicy.Of` (U for change_assignment) and `Facts` has no `HasCalendarEvent`; `Decide` has no caller. Before catalog 10 is served, admission and plan-step classing must use `Decide` with the fact read from local data (delete with an event -> C card, outside and inside a plan) and carry `Card`/`NoUndo`. `domain/agent/thread` is not in TASK-320's file list: add it or give it its own task. Consider making `Of` refuse tools with per-action rules.
- MEDIUM: compensation kinds are checked per tool, not per action (edit could complete with DeleteAssignment; recording start could complete with no compensation). Fix needs the action stored in the journal entry (sqlite) — outside this task; TASK-320/321/322 tests must assert the kind per action meanwhile.
- Security N2 (low, owner): a model-made `set_setting{free_work_notice, off}` is U and turns off the card guarding paid free-topic work; decide whether it needs «Да» or only the student's own click.
- Low: `checkTrusted` compares `time.Time` with `==` (normalise to UTC in the codec); `ToolsAt` maps versions < 10 to catalog 9 (refuse < 9); RestoreDone's `PlanUndo` checks the highest mark version, per-assignment conflict is in the use case (TASK-314) — its test must change a lower-versioned mark; `Decide`'s card map duplicates thread/input mappings.
- Not done: sqlite storage of new compensation kinds and the compensator in `input/agent`; `docs/agent-parity.md` update (dispatch tasks).

## Changed existing tests
`values/answer_test.go` (unexported `parsePlanSteps` takes a catalog version now; passes `CatalogVersion`); `actionpolicy/policy_test.go` `TestEveryToolHasARule` (counts the newest catalog's tools plus retired ones).

## Security
Auditor: no High/Medium. Precondition N1 (= the High follow-up above) and N2 (low). Reviewer: approve with follow-ups; the High is a precondition for serving catalog 10, not a defect of this PR (nothing is served).
