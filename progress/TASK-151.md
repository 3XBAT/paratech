# TASK-151 — cases/notes and the NotesService binding

## Done
- `cases/notes`: Add, Paste (TXT <= 1 MiB), ReplacePart (+ UndoReplace), Reorder, Reanchor, Retitle, Remove, Restore, ForLesson (recording | lesson | day), ForSubject. Ports declared in the use case; materials through `library.Service.FileNotePart` (wired in `app`).
- Parts must be active materials of the note's subject; lists skip removed notes (`Active()`); a full note refuses before anything is imported.
- Anchor checks through ports (recording COMPLETED and not deleting; lesson live). A purged recording or removed lesson reads as its `day` snapshot at read time (`Detached`), nothing is written back, ADR-0018 purge unchanged.
- Binding `NotesService` (`input/wails/notes.go`), multi-select open dialog (`notespicker_windows.go`, Windows only, up to 200 files), drop on a lecture via the TASK-138 allow-list (`librarydrop.go`, `data-note-*` attributes), `frontend/src/bindings/notes.ts`. No paths cross back to the page.
- Subject deletion: `RemoveSubject` refuses a subject with notes (also removed ones) with `library.ErrHasNotes` (wraps `ErrNotEmpty`; agent Undo of `create_subject` reads it as "changed"). Notes are checked first; fails closed if notes are unreadable.

## Decisions to confirm (owner)
- Subject with notes cannot be deleted; text «У предмета есть конспекты, в том числе удалённые: такой предмет удалить нельзя.» There is no permanent delete of notes yet, so a subject with only removed notes stays undeletable until «Удалить навсегда» exists. Alternative: delete/move notes with the subject.
- Add/Paste without a note ID append to the first active note of that lecture (same subject); «без даты» always makes a new note.
- Same bytes already filed under another subject: refused (`ErrOtherSubject`).
- Replacing a part keeps the old material in the library (removed from the note only); Undo restores it.
- Folder by lesson kind: practice -> «Семинары», unknown -> «Лекции»; `filing.NoteFolder` in the domain.
- A drop stays at 10 files (TASK-138); the dialog takes 200. Per-file refusals do not stop the batch; a later unexpected error keeps earlier results and stops (later files not tried).
- ADR-0027 Open 1-10 untouched (limits are constants: `MaxPastedBytes` 1 MiB, 200 parts).

## Follow-ups
- `.md` not in dialog filter / drop allow-list (TASK-150 area).
- `Reanchor`/`Add` do not check the target lesson/recording belongs to the note's subject; `ForLesson` returns notes of all subjects for that lesson (ADR silent).
- `UndoReplace` accepts any active material of the subject sent by the page.
- Scenario test with real SQLite: purge a recording with a note, then read as day.
- A count port for notes per subject (RemoveSubject lists all).
- No frontend `.mjs` test for notes.ts (Go test pins its texts); `domain/library/doc.go` glossary not updated.
- Re-adding an existing file to a note with 200 parts says "too many" instead of «уже добавлено».

## Files outside the task's list (justified)
`cases/library/{ports,types,errors}.go`, `input/wails/{shell,shell_windows,library}.go`, `frontend/src/bindings/library.ts`, `domain/library/services/filing/note.go`, `adapters/random`, `app/env.go`, `desktop/tests/schema_test.go` (TASK-149 follow-up).

## Changed existing tests
`input/wails/shell_test.go` (Core.Notes, fake picker), `settings_test.go` (missing-notes row), `library_test.go` (ErrHasNotes row), `adapters/random/random_test.go` (NoteID row), `desktop/tests/schema_test.go` (notes in version/status checks). All additive.

## Security
Auditor: no high/medium findings (allow-list reuse, one-by-one bounded reads, no paths in payloads/logs, cross-subject parts refused). Reviewer: approve; two mediums (full note imports, lost results in multi-file add) and one low fixed in a9cd74bc.
