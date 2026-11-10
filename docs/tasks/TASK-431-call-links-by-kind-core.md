# TASK-431: call links per lesson kind — domain, migration, use case

- **Agent:** go-core (opus)
- **Label:** core
- **Depends on:** none (parallel with TASK-326; files disjoint from TASK-415…430)
- **PRD sections / AC:** §6.1 (recording prompt), ADR-0032 addendum 2026-11-10, ADR-0038 §4, D-2
- **Агент:** `set_call_links` (C, wave 11) — TASK-434/435/436; this task is the UI-usable core
- **Branch:** ralph/<date>-task-431

## Goal
A subject keeps a general call link and one link per kind (lecture, seminar, lab); one pure rule tells which link a
lesson of a kind opens. Callers of today's single link keep working (it is the general link).

## Contract (fixed; TASK-432 and TASK-435 build on it)
```go
// domain/library/subject
type CallKind string
const (
	CallGeneral CallKind = "general"
	CallLecture CallKind = "lecture"
	CallSeminar CallKind = "seminar"
	CallLab     CallKind = "lab"
)
func ParseCallKind(s string) (CallKind, error)        // ErrInvalidCallKind (new sentinel)
func CallKindOfLesson(lessonKind string) CallKind      // lecture→lecture; seminar, practice→seminar; lab→lab; else general

type CallLinks struct{ General, Lecture, Seminar, Lab CallLink } // value; zero = none
func (c CallLinks) Of(k CallKind) CallLink
// Join: the link of CallKindOfLesson(lessonKind) if set, else General if set, else zero; the CallKind returned is the
// kind whose link was taken. A seminar never takes the lecture link (D-2).
func (c CallLinks) Join(lessonKind string) (CallLink, CallKind)

func (s *Subject) CallLinks() CallLinks
func (s *Subject) SetCallLinkOf(k CallKind, link string, now time.Time) (changed bool, err error)
func (s *Subject) ClearCallLinkOf(k CallKind, now time.Time) (changed bool, err error)
// SetCallLink / ClearCallLink / CallLink stay and mean CallGeneral.
// Restore… takes the four stored strings; a stored value that no longer parses is read as no link (as today).

// cases/library
func (s *Service) SetCallLinkOf(ctx, id shared.SubjectID, k subject.CallKind, link string) (*subject.Subject, error)
func (s *Service) ClearCallLinkOf(ctx, id shared.SubjectID, k subject.CallKind) (*subject.Subject, error)
func (s *Service) OpenJoinLink(ctx, id shared.SubjectID, lessonKind string) error // ErrNoCallLink when Join is zero
func (s *Service) CallLinkSets(ctx, ids []shared.SubjectID) (map[shared.SubjectID]subject.CallLinks, error)
// CallLinks(ctx, ids) stays until TASK-432 moves its callers, then is removed there.
```
- Same error mapping as the existing call link methods (`ErrInvalidCallLink` before the subject is read, no version
  check, last save wins); nothing logged but reason codes.

## Allowed to change
- `paratech/desktop/internal/domain/library/subject/*` (calllink.go, new calllinks.go, subject.go, errors.go + tests)
- `paratech/desktop/pkg/migrations/<next>_subject_call_links_by_kind.sql` (next free number at push)
- `paratech/desktop/internal/adapters/sqlite/*` (library queries, sqlcgen, library.go + tests)
- `paratech/desktop/internal/cases/library/{calllink.go,types.go,errors.go}` + tests
- `paratech/desktop/tests/*` (the cross-check of the two ParseCallLink copies stays green; a SQLite scenario)

## Out of scope
- Bindings, todayfeed, recordingprompt (TASK-432); UI (TASK-433); the batch (TASK-435); any agent code.

## Acceptance criteria
- [ ] Migration adds `call_link_lecture`, `call_link_seminar`, `call_link_lab TEXT NOT NULL DEFAULT ''` to `subjects`;
      `call_link` untouched; goose up/down tested; schema version test updated.
- [ ] Table tests: `CallKindOfLesson` for lecture, seminar, practice, lab, "", "unknown"; `Join` for every
      combination of set links (kind only, general only, both, none, seminar with only a lecture link → none).
- [ ] Set/clear per kind: same value → unchanged, bad link → `ErrInvalidCallLink` and nothing changed, version bump
      only on change; `CallLinks()` round-trips through SQLite.
- [ ] Logging: a set link prints only its host (`Format`, `LogValue`) for every kind.
- [ ] `go vet`, `go test -race` for the touched packages and the architecture test pass.
