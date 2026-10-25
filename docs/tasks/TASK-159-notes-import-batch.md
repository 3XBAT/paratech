# TASK-159: Import batch: stage, propose, fix, commit (core and binding)

- **Agent:** go-core (opus)
- **Label:** core
- **Depends on:** TASK-151, TASK-157, TASK-158
- **PRD sections / AC:** ADR-0027 «Bulk import»; PRD §11.2 (preview/commit pattern), §7 (jobs); ARCH-23; hard rules 5, 7
- **Branch:** ralph/<date>-task-159

## Goal
Many files are added at once: the app stages them, proposes a lecture for each, lets the student fix the proposals,
and commits them as notes in a resumable job.

## Allowed to change
- `paratech/desktop/internal/domain/library/importbatch/**` (new)
- `paratech/desktop/internal/cases/notesimport/**` (new)
- `paratech/desktop/pkg/migrations/<next>_import_batches.sql`
- `paratech/desktop/internal/adapters/sqlite/{importbatches.go,queries/importbatches.sql,sqlcgen/**}` (+ tests)
- `paratech/desktop/internal/input/wails/{notesimport.go,notesimport_test.go,notesimportpicker_windows.go,notesimportpicker_other.go}`
- `paratech/desktop/internal/app/**` (wiring, job handler registration)
- `paratech/desktop/frontend/src/bindings/notesImport.ts`
- `docs/architecture.md` §12 (`importbatch`, `cases/notesimport`)

## Out of scope
- Audio items (TASK-163), images (TASK-165), UI (TASK-160).

## Acceptance criteria
- [ ] Aggregate `importbatch`: ≤ 500 items; statuses draft → committing → done | cancelled as a table; per item the
      proposal (from `notematch`), the student's choice (subject, anchor, skip) and outcome; tests for every move.
- [ ] Start from a multi-select, a folder or a zip (TASK-158) or a drop; bytes staged in the blob store by sha256;
      a file already in a note is `duplicate`; unsupported types are listed with a reason, not staged.
- [ ] `Proposals`, `Choose(item, …)`, `ChooseMany(items, subject)`, `CreateSubjectFromSuggestion` (through the library
      port), `Commit`, `Cancel`; the batch survives a restart.
- [ ] Commit = job `notes_import`, one item per queue step (`job.Yield` as TASK-097); an item is committed once (a
      repeated step finds its note); cancel removes staged blobs no material references.
- [ ] No text, file name or path reaches the Hub; tests on fakes and one scenario test on a real SQLite.
- [ ] `go vet`, `GOOS=windows go vet`, `go test -race` of the touched packages and the architecture test pass.
