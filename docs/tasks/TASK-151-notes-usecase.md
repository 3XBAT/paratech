# TASK-151: Use case `cases/notes` and the NotesService binding

- **Agent:** go-core (opus)
- **Label:** core
- **Depends on:** TASK-149
- **PRD sections / AC:** ADR-0027 «Model», «Trust, storage, deletion, limits»; ADR-0024; ARCH-20, ARCH-44; hard rules 3, 5
- **Branch:** ralph/<date>-task-151

## Goal
The student attaches files or pasted text to a lecture (recording, schedule lesson, a date or «без даты») and manages
them; the files go through the library.

## Allowed to change
- `paratech/desktop/internal/cases/notes/**` (new)
- `paratech/desktop/internal/cases/library/service.go` (a method the notes port needs, + tests)
- `paratech/desktop/internal/input/wails/{notes.go,notes_test.go,notespicker_windows.go,notespicker_other.go}`
- `paratech/desktop/internal/input/wails/librarydrop.go` (drop target «к этой лекции», + tests)
- `paratech/desktop/internal/app/**` (wiring)
- `paratech/desktop/frontend/src/bindings/notes.ts`

## Out of scope
- UI (TASK-154), bulk import (TASK-159), images (TASK-165).

## Acceptance criteria
- [ ] `Add(anchor, subject, uploads)`, `Paste(anchor, subject, text)` (TXT material ≤ 1 MiB), `ReplacePart`,
      `Reorder`, `Reanchor`, `Retitle`, `Remove`, `Restore`, `ForLesson(recording | lesson | day)`, `ForSubject`.
- [ ] Materials through a port implemented by `library.Service` (wired in `app`): import by sha256 (same bytes =
      «уже добавлено», no second part), a part in «Входящие» is placed in «Лекции»/«Семинары»/«Лабораторные» by lesson
      kind, one filed elsewhere stays there.
- [ ] Anchor checked through ports (recording completed and not marked for deletion; lesson exists).
- [ ] Binding: multi-select open dialog (verify the Wails v3 API in `docs/refs/wails.md` or the source), drop on a
      lecture uses the TASK-138 allow-list; no path crosses back to the page; file names are display text only.
- [ ] A note whose recording was deleted (ADR-0018) or whose lesson was removed reads as its `day` snapshot, resolved
      at read time; the purge of ADR-0018 is not changed and never deletes a note; test.
- [ ] `go vet`, `GOOS=windows go vet`, `go test -race` of the touched packages and the architecture test pass.
