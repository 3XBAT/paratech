# TASK-429: recorded lessons in the folders of their subject — read model and binding

- **Agent:** go-core (opus)
- **Label:** core
- **Depends on:** none (parallel with TASK-428 and the sprint tasks; no file of TASK-424/425)
- **PRD sections / AC:** §6 (knowledge base), ADR-0034 amendment 2026-11-10, ADR-0027 (filing of note parts)
- **Агент:** R, view only; `find_lessons` already lists the lessons (row added in `docs/agent-parity.md`)
- **Branch:** ralph/<date>-task-429

## Goal
The core tells, for a library subject, which of its recorded lessons belong to which of its system folders, so the
page can show them as link rows. Nothing is stored; no migration.

## Contract (fixed; TASK-430 builds on it)
```go
// cases/knowledgebase/types.go
type FolderLesson struct {
	Folder shared.FolderID // subject.SystemFolderID of the subject's system folder
	System subject.System  // SystemLectures | SystemSeminars | SystemLabs
	Lesson kb.Lesson       // a recorded lesson, as Lessons returns it (no notes)
}

// cases/knowledgebase
func (s *Service) FolderLessons(ctx context.Context, sub shared.SubjectID) ([]FolderLesson, error)

// input/wails
type KnowledgeFolderLesson struct {
	FolderID string          `json:"folderId"`
	System   string          `json:"system"` // "lectures" | "seminars" | "labs"
	Lesson   KnowledgeLesson `json:"lesson"` // anchor "recording", notes []
}
func (s *KnowledgeBaseService) FolderLessons(ctx context.Context, subjectID string) ([]KnowledgeFolderLesson, error)
```
- Order: the core's order of `Lessons` (newest first).
- `sub` that does not validate → `ErrInvalidRequest` → binding text «Неверный идентификатор предмета.»
  (`ErrKnowledgeInvalidSubject`, also in `errorMessages.invalidSubjectId` of the TS binding, checked by the Go test).
- Unknown or removed subject → empty list, no error. Store failure → `ErrKnowledgeUnavailable`.

## Rule (ADR-0034 amendment)
- Only lessons with `librarySubjectState = matched` to `sub`; `none`, `ambiguous`, no subject and other subjects out.
- Folder: `filing.Target(sub, s, filing.NoteFolder(kind))` with the effective kind (correction wins), `s` from the
  existing `SubjectLister`; not ok → the lesson is left out. No new rule in the use case (ARCH-22).
- Recordings marked for deletion, silent or not completed are not listed (the `LessonStore` contract).

## Allowed to change
- `paratech/desktop/internal/cases/knowledgebase/{types.go,service.go or a new folders.go}` + tests
- `paratech/desktop/internal/domain/library/services/filing/note.go` (doc comment only: recorded lessons use it too)
- `paratech/desktop/internal/input/wails/knowledgebase.go` + `knowledgebase_test.go`
- `paratech/desktop/internal/app/*` (wiring only, if anything is missing)
- `paratech/desktop/frontend/src/bindings/knowledgeBase.ts` (`methods.folderLessons`, `KnowledgeFolderLesson`,
  `parseFolderLesson`, `errorMessages.invalidSubjectId`) + its test
- `paratech/desktop/tests/folderlessons_sqlite_test.go` (new)

## Acceptance criteria
- [ ] Table test on fakes: lecture → «Лекции» id; seminar and practice → «Семинары»; lab → «Лабораторные»; "" and
      unknown kind → «Лекции»; a corrected kind wins over the schedule one; another subject, `none`, `ambiguous`, no
      subject → absent; subject without that system folder → absent; unknown / removed subject → `[]`; invalid id →
      `ErrInvalidRequest`; a store failure wraps the unavailable error.
- [ ] Scenario (`desktop/tests`, real SQLite): subject «Матанализ», a completed recording with that subject and kind
      lecture → one row in its «Лекции»; `SetLessonInfo` kind seminar → the row is in «Семинары»; the recording marked
      for deletion → no row. No migration (schema version unchanged).
- [ ] Binding: exact JSON of one row; error texts; the TS parse rejects a row whose `system` is not one of the three,
      an empty `folderId`, or a lesson whose anchor is not `recording`; the existing method-name Go test covers the new
      method.
- [ ] `go test -race ./desktop/internal/cases/knowledgebase/... ./desktop/internal/domain/library/... ./desktop/internal/input/wails/... ./desktop/tests/...`
      and the architecture test; `GOOS=windows go vet ./desktop/...`; `golangci-lint run ./desktop/...`;
      `npm --prefix paratech/desktop/frontend run test`.

## Notes
- Read cost: one `Lessons` read (recordings + readiness) and one subject list per call; the page calls it once per
  subject opening and on its existing re-reads.
