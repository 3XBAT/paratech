# TASK-320 — wave 10 on the desktop: assignments dispatch (change_assignment, mark_assignments_done, list_assignments{state}); not served until TASK-326

## Done
- `domain/agent/thread` (supervisor's High from TASK-319): `Admit` classifies with `actionpolicy.Decide` and `Facts.HasCalendarEvent`; `Admission` carries `Card`/`NoUndo`. A delete of an assignment with a Google event is class C (card `delete_assignment`), outside and inside an approved plan (C is not a write, so it uses no plan step, like propose_send). `ProposePlan` classifies steps with `actionpolicy.DecideStep` (zero facts); the step is re-decided with the real fact at execution. `ErrUnknownAction` is a refusal (`invalid_arguments`). `cases/agentturn.Call` carries Card/NoUndo.
- `input/agent`: edit / delete / restore (`assignment_change.go`), `mark_assignments_done` (`assignment_done.go`), `list_assignments{state}` (`assignment_list.go`), the C path that only makes the card (`assignment_card.go`); Check / Apply / Observe / replay for the four compensations (RevertAssignment, RestoreAssignment, DeleteAssignment, RestoreDone). The event fact comes from the local proposals projection (`cases/homeworkflow/agentreads.go` `HasDeadlineEvent`, EXECUTED/CONFIRMED/STALE create_deadline_event, a cut scan counts as "has event"); a read error or no reader refuses the call (fail closed). The U delete re-checks the event and the work-under-way state in prepare and right before deleting.
- `input/wails/agentconfirm_assignment.go`: the `delete_assignment` card; «Да» calls the page's `DeleteAndStop`; `agentconfirm.go` has only the kind branches.
- `adapters/sqlite/agentactions10.go`: the journal stores/loads the four new compensation kinds (additive; round trip on real SQLite).
- Tests assert the compensation kind per action (edit RevertAssignment, delete RestoreAssignment, restore DeleteAssignment, done RestoreDone), also after Recover.

## Decisions to confirm
1. The agent's soft-delete method is named `TrashAssignment` in `input/agent` (app maps it to `DeleteAndStop`): `TestAgentHasNoPathToIrreversibleActions` forbids "Delete" in interface method names and its comment says false positives are fixed by renaming. A soft delete with «Вернуть» is U (ADR-0033 §2). Reviewer accepts the reading.
2. Edit composes `EditAssignment` + `SetAssignmentDue` + `CorrectLesson` in one transaction so a title-only edit keeps the exact due and the subject correction; Undo writes back exactly the prior fields (up to 3 version bumps; stays "edited by student"). The Google event of a changed deadline is NOT updated (ADR-0029 addendum 3 / C-4 event-refresh queue is not built; same as `edit_due` today).
3. U delete is refused with `not_now` while a work/draft job is under way (DeleteAndStop would cancel it and Undo restarts nothing); the student does it on the page where the card warns. Undo of a restore (which deletes again) is refused as «Изменено после» in the same cases and with an event.
4. Restore only of what this chat deleted: the newest DONE change_assignment entry of this thread naming the assignment must be its delete, and the assignment must not have been updated since (clock compare, see follow-ups). A delete confirmed via the card is not journaled as a U action, so the chat cannot restore it (product question: should «Да» complete the entry as DONE/RestoreAssignment?).
5. mark_assignments_done: reads states first; nothing to change -> per-ID rows, no journal entry; otherwise one MarkDone, one entry, one RestoreDone from `DoneMarks.Prior`. Undo is all-or-nothing inside one Tx. A deleted id in the list is a per-row `unknown_id`, the others are marked. Google events untouched (О13 б).
6. list_assignments rows carry `done` only when `CatalogVersion >= 10` or the call has `state`; catalog 9 bytes unchanged (tested).
7. The card's journal target is `preview:<assignment id>` (`NewProposed` accepts only proposal/preview); panel binding `Card/Confirm/Decline("assignment", id)`; «Да» deletes at the version at click time.

## Follow-ups
- MEDIUM, before TASK-326: `input/wails/agentpanel.go` `cardView` has no case for `values.DeleteAssignmentCard` (the panel would drop the card); add a case with target kind "assignment" (TASK-324 or TASK-326) plus frontend rendering.
- MEDIUM, design: add a homeworkflow use case that applies exact edit fields in one aggregate call (instead of EditAssignment clearing the due then SetAssignmentDue); needed when the C-4 event-refresh queue lands (a cleared due would queue a removal).
- `action.Complete` still checks compensation kinds per tool, not per action (needs the action in the journal entry); tests assert per action meanwhile.
- Low: `deletedByChat` compares `UpdatedAt` clocks; `agentjournal.Entry` should expose `TargetVersion` and the check become `v.Version == e.TargetVersion`.
- Low: "Нет" on the delete card is not stored (a reload shows "waiting" again), and `Confirm("assignment", id)` is not tied to a PROPOSED entry; the frontend is trusted and the card is not reachable yet.
- Low: crash between `TrashAssignment` commit and UNDONE leaves the Undo labelled refused (data correct); Recover of mark done could claim a student's concurrent toggle; `"done":true` key in the mark result may be read as the call's state (document in the v10 tool text, TASK-325); STALE deadline proposals count towards "has event", so after ~1000 of them every delete needs the card (safe, noisy).
- TASK-321/322 add their compensation kinds to the same switch in `adapters/sqlite/agentactions.go`.
- Security N2 from TASK-319 (model-made `set_setting{free_work_notice, off}`) is for TASK-322.

## Changed existing tests
None changed or deleted; only the shared test fake `memChanges` (new) and new tests.

## Security
Auditor: no High/Medium; Lows L1 (clock compare), L2 (STALE proposals, fixed), L3 (U delete stops jobs, fixed by refusing while work is under way), L4 (card not tied to a proposal), L5 (Recover claim) recorded above. Reviewer: first round Medium (deleted id refused the whole mark list: fixed; card not mapped by the panel: follow-up; job cancellation: fixed; edit composition: follow-up), re-review found Undo of a restore without the work check: fixed in the last commit.
