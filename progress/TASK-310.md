# TASK-310 — subjects from the schedule: use case and query

## Done
- Port `SubjectStrings.ListScheduleSubjectStrings(ctx) []schedulesubjects.Row` (cases/library/ports.go); sqlite: one static query, UNION ALL of live `schedule_entries` and recordings not marked for deletion (the student's subject correction wins), grouped by the exact string, counts per kind (seminar + practice together) and recordings. No migration; sqlc output regenerated, no drift.
- `cases/library/schedule.go`:
  - `ScheduleSubjects(ctx)` -> `ScheduleProposal` (+ `Omitted`) and token = sha256 over a version tag, the groups/members and the subject list (length-prefixed fields). Empty proposal is not an error.
  - `CreateFromSchedule(ctx, token, choices)`: the proposal is rebuilt from the store (never taken from the caller), a token mismatch is `ErrStale`, then `schedulesubjects.Apply`. One transaction per subject, ID = `DeriveSubjectID(token, key)`, an existing ID counts as created; `name_taken`, `too_many_aliases`, `subject_gone` skips; aliases go the `AddAlias` way; six system folders. A failure midway returns what is done plus the error; a retry with the same token finishes (tested at each of three saves).
  - `UndoFromSchedule(ctx, token, result)`: subjects removed only when ID == `DeriveSubjectID(token, key)` and through `RemoveSubject` (notes/files stay: `has_notes` / `has_files`); aliases removed with `subject.RemoveAlias`; anything else `ErrInvalidRequest`; all validation before any change.
- Row cap `maxScheduleRows` = 200 (matching cost grows with the square of the rows); beyond it the rows with most lessons + recordings are kept, deterministic; the rest is reported in `Omitted`.
- Logs hold counts only; no schedule string is in any error (ADR-0022), tested.

## Result types for TASK-311 / TASK-323
`ScheduleResult{Created, Aliased, Skipped}`; `ScheduleUndo{Removed, Kept, Unaliased, KeptAliases}`; `ScheduleKeptAliases{Subject, Aliases, Reason}`; reasons are the typed `ScheduleReason` constants (`name_taken`, `too_many_aliases`, `subject_gone`, `stale`, `has_files`, `has_notes`). `ErrStale` is returned by `CreateFromSchedule` only.

## Decisions to confirm
- Token check after a partial run: once part of a package is applied, the subjects differ, so a plain recomputation would not match the token. `scheduleBase` leaves out the subjects with derived IDs and tries stripping up to `maxBaseGuesses` = 64 trailing aliases that equal schedule spellings; no match -> `ErrStale` (safe: nothing is done twice). Consequence (reviewer, medium, accepted): an alias the student adds by hand that equals a schedule spelling does not make the old token stale; creates and additions re-check the current index (`name_taken`), so nothing unsafe follows.
- «Отменить всё» in a changed state (reviewer medium, fixed): empty derived-ID subjects are always removed; if the proposal can no longer be rebuilt, the added spellings are NOT removed and are reported in `KeptAliases` with reason `stale` (all-or-nothing). `UndoFromSchedule` never returns `ErrStale`.
- Undo `Removed` keeps «already gone counts as removed» so a repeated undo answers the same.

## Follow-ups
- TASK-312 (UI) and TASK-323 (agent card): show `KeptAliases` / `stale` in Russian with a next step (no silent outcome); show `Omitted` when rows were cut.
- TASK-311: pass the Undo result through unchanged; the retry/Undo search can take a few seconds in the worst case (up to 64 `Propose` runs on 200 similar strings, ctx-bounded): call from a background binding.
- Security note (auditor, below Medium): `UndoFromSchedule` trusts the caller's `Aliased` list only after checking each spelling is one the proposal gave that subject; a forged list could still remove a student-added alias that equals such a spelling (needs a real token, a specific order of edits). Re-check if the agent card builds the undo result from model output; the robust fix is to store the result locally and not take it from the caller.
- Low (reviewer): a test for the non-stale undo where the student already removed one of the spellings; a per-alias length bound in `checkScheduleAliases`; about 70 lines of alias reasoning in `scheduleBase` could move to `schedulesubjects` later.
- `sqlc` was verified by the implementer (v1.31.1, no drift).

## Changed existing tests
None. (`fakeStrings` in fakes_test.go gained a field and the new method.)

## Security
Auditor: no High/Medium; the note above. Reviewer: approve; the medium (Undo dead after a state change) was fixed and re-reviewed (approve).
