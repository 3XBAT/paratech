# TASK-321 — wave 10 on the desktop: «Мой конспект» (list_notes, note); not served until TASK-326

## Done
- `input/agent/notes.go`, `notes_list.go` (+ one-line hooks in `actions.go`, `write.go`, `tools.go`): `list_notes{lesson|subject}` (R; IDs, anchor kind and date, part counters; no titles, no text, no file names; capped) and `note{add_text|add_upload|move|rename|remove}` (U) with Check / Apply / Observe / replay / Recover for RemoveNotePart, RemoveNote, ReanchorNote, RetitleNote, RestoreNote.
- `add_text` saves the model's text (<= 4000 runes) through `cases/notes.Service.Paste` (same path as the page); the text is in no intent, journal row, log, error or planner result (the planner gets `{done, changed, id, created, parts}`).
- `add_upload`: the upload ID is checked (thread provenance as inspect_upload, re-read in prepare: removed or foreign-subject upload refused); a «Входящие» upload is filed into the note folder of the subject, as `FileNotePart` does.
- `cases/notes`: `AddMaterial`, `RemovePart`, `Note`, `Anchor`, `LessonSubject`; port `Materials` gets `Place` and `MoveToInbox` (both exist in library.Service; wiring unchanged). A failed add_upload that did the Place puts the upload back to «Входящие» (best effort).
- `domain/library/note`: `RemovePart` (last part stays, removed note refused) and `CleanTitle`. `domain/agent/values/wave10notes.go`: `NotesResult`/`NoteResult` shapes (WriteResult refuses note IDs).
- `adapters/sqlite/agentactions10.go`: codec for the five note kinds (round trip on real SQLite).
- `app`: wiring only (`agentReads.notes`, `ownNotes`). CatalogVersion stays 9; tests pin that note tools are invisible at 9.
- Tests assert the compensation kind per action, also after Recover.

## Decisions to confirm
1. Subject of an add: `note` -> the note's own subject; `lesson` -> the library subject the lesson names, else the chip subject (request scope), else refused `invalid_arguments`; `date` -> the chip subject, none -> refused. No `subject` argument was added (TASK-319 open question resolved this way; ADR §10 unchanged). Move keeps the note's subject; the lesson's subject is not compared with the note's.
2. Removed subjects (TASK-308): notes of a removed subject and dates of one answer `unknown_id`; list_notes of one is empty; Undo after removal is «Изменено после».
3. Expected version = the note's repository version: move/rename/remove check it and run in one Tx; Undo compares it in Apply's Tx. add_* checks it before Paste/AddMaterial but is not one transaction (material and note are two aggregates, ARCH-23).
4. Undo of add_upload removes the part (or the new note) but leaves the file filed under the lecture folder (not back to «Входящие»); Undo of add_text keeps the stored TXT file in the library (ADR-0027 keeps files on note removal).
5. Recover: move/rename/remove settle from `Intent.Next`; an add that made a note is claimed only if it is the one note of that lecture+subject made after the entry, has exactly one part, and that part is this call's upload (or a TXT created after the entry). Intent key = `<lecture key>|<upload id>`; the lesson key carries the subject.

## Follow-ups
- GAP (TASK-324 / frontend): the panel result row cannot show the saved text («первые строки») — the core emits `ActionLine{Action}` only. Needs a note line in `values.ActionLine` (like `AssignmentLine`) filled from the note's TXT part (RemoveNotePart carries the part's material ID, RemoveNote the note), a case in `input/wails/agentpanel.go` `cardView` and rendering.
- add_text into an existing note after a crash is UNKNOWN with no Undo (the part's material ID is not known in advance).
- Low residuals: a text the student pastes into the same lecture between the journal entry and a crash can be claimed by Recover (closing it needs a hash of the text in the intent; text is meant never to be journaled); `backToInbox` is unconditional (a narrow race can undo the student's own filing: a `material.Unplace(sub, folder)` conditional method would close it); the `ErrAlreadyAdded` race paths (AddMaterial skips the return; the `alreadyAdded` effect) have no test; move to a lesson does not check the lesson's subject against the note's; Undo of a move to a gone recording/lesson answers «Изменено после».
- `docs/agent-parity.md` unchanged (row already lists TASK-321); dispatch tasks update it when served.
- TASK-322 adds its kinds to the same switch in `adapters/sqlite/agentactions10.go`.

## Changed existing tests
None changed or weakened. The `cases/notes` test fake gained `Place`/`MoveToInbox`/`inbox`; the `input/agent` fake `noteMaterials` gained a clock so new materials are dated by the moving test clock (needed by the Recover check).

## Security
Auditor: no High/Medium/Low findings (IDs through provenance, removed subjects hidden, text not in logs/journal/planner result, codec validated). Reviewer: approve; first round two Mediums (failed add_upload left the file filed; Recover could claim a student's note) fixed in 202ff199 and re-reviewed (approve, Lows recorded above).
