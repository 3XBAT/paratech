# TASK-149: Note aggregate and its storage

- **Agent:** go-core (opus)
- **Label:** core
- **Depends on:** -
- **PRD sections / AC:** §5.3, AC-20; ADR-0027 «Model», ADR-0024; hard rule 5
- **Branch:** ralph/<date>-task-149

## Goal
The student's own notes of a lecture exist as an aggregate `library/note` with an anchor and ordered parts, stored in
SQLite.

## Allowed to change
- `paratech/desktop/internal/domain/shared/ids.go` (+ `NoteID`, tests)
- `paratech/desktop/internal/domain/library/note/**` (new)
- `paratech/desktop/pkg/migrations/<next>_notes.sql`
- `paratech/desktop/internal/adapters/sqlite/{notes.go,notes_test.go,queries/notes.sql,sqlcgen/**}`
- `docs/architecture.md` §12 (the `note` row only)

## Out of scope
- Use case, bindings, text, knowledge base (TASK-150…153).

## Acceptance criteria
- [ ] `note.New(spec, now)` / `Restore`: subject required, title cleaned to one line ≤ 120 runes, 1…200 parts
      (material IDs, no duplicates), status active/removed as a transition table (ARCH-19).
- [ ] Anchor value with kinds `recording`, `lesson`, `day`, `undated`; every anchor stores a snapshot (local date,
      lesson kind code, title); `day` needs a date, `undated` none; `FallBackToDay()` turns `recording`/`lesson` into
      `day` from the snapshot (used when the recording or lesson is deleted).
- [ ] Methods: `AddParts`, `ReplacePart(old, new)`, `Reorder`, `Reanchor`, `Retitle`, `Remove`, `Restore`; invalid
      moves return sentinel errors; tests for every allowed and refused transition (ARCH-62).
- [ ] Migration `notes` + `note_parts` (FK to `materials`, position; the anchor's recording and lesson IDs carry no FK,
      so the ADR-0018 purge and a schedule re-import never touch notes), version-guarded `Save`, `Get`, `ListBySubject`,
      `ListByRecording`, `ListByLesson`; adapter tests on a real SQLite.
- [ ] `go vet`, `go test -race` of the touched packages and the architecture test pass; sqlc output regenerated.
