# TASK-435: batch of call links from rows the model extracted — domain service and use case

- **Agent:** go-core (opus)
- **Label:** core
- **Depends on:** TASK-431
- **PRD sections / AC:** ADR-0038 §4, D-3, D-4; ADR-0032 addendum 2026-11-10; ADR-0021 (journal, Undo), ARCH-23
- **Агент:** the core of `set_call_links` (C, wave 11); dispatch and card in TASK-436/437
- **Branch:** ralph/<date>-task-435

## Goal
Given rows the model extracted from the student's message, the desktop decides for each row what would happen, with
the same parser as the page, and applies the rows the student ticks, one subject per transaction, idempotently.

## Contract
```go
// domain/library/services/calllinkbatch (pure, ARCH-17)
type Status string // "link" | "in_person" | "unconfirmed" | "none" | "clear"
type Row struct {
	Subject     shared.SubjectID // exactly one of Subject and SubjectName
	SubjectName string           // label as the student wrote it
	Kind        string           // general | lecture | seminar | practice | lab
	Link        string           // raw text, may be empty
	Status      Status
}
type Outcome string // ready, same, clear, in_person, unconfirmed, no_link, bad_link, unknown_subject,
                    // ambiguous_subject, conflict
type Item struct {
	Index    []int            // the input rows merged into this item
	Subject  shared.SubjectID // "" for unknown_subject / ambiguous_subject
	Name     string           // the subject's library name; the label for unknown_subject
	Kind     subject.CallKind // practice already mapped to seminar
	Link     subject.CallLink // zero when the row has none or it does not parse
	Old      subject.CallLink // the link it replaces (ready) or keeps (same)
	Outcome  Outcome
	Ticked   bool             // default choice on the card: only ready and clear
	CanCreate bool            // unknown_subject with a valid link (D-4)
}
func Decide(rows []Row, subjects []Subject /* id, name, aliases, CallLinks */) []Item
```
- Name resolution: `subjectmatch` over names and aliases; no match → retry with a trailing «(…)» cut (the teacher in
  «Социология культуры (Иванова Е.)»); one match → that subject; several → `ambiguous_subject`; none →
  `unknown_subject`.
- Statuses: `in_person` and `none` never apply; `unconfirmed` with a valid link → `unconfirmed`, unticked; `clear` →
  clears that kind (`same` when already empty); `link` with a link that does not parse → `bad_link`, without one →
  `no_link`.
- Merge: rows with the same subject, kind and canonical link become one item; two different links for one subject and
  kind → `conflict` items, both unticked (the card lets the student tick one).
- Order: the input order of the first row of each item.

```go
// cases/library
func (s *Service) ProposeCallLinks(ctx, rows []calllinkbatch.Row) (CallLinkProposal, error) // Token over the items
func (s *Service) ApplyCallLinks(ctx, token string, choices []Choice) (CallLinksApplied, error)
func (s *Service) UndoCallLinks(ctx, applied CallLinksApplied) (UndoCallLinksResult, error)
```
- `Choice{Item int, Apply bool, Create bool}`; an item may be applied only if its outcome allows it (ready, clear,
  unconfirmed, one side of a conflict, CanCreate with Create); anything else → `ErrInvalidChoice`.
- Apply re-reads every subject: the token must still match (else `ErrStale`, nothing applied); then one transaction
  per subject (`SetCallLinkOf` / `ClearCallLinkOf` / `CreateSubjectWithCallLink`); a repeat of the same token applies
  nothing twice (the value is already there → unchanged). The result keeps, per subject, the kind, the previous and
  the new canonical link (local SQLite only, never logged), and the created subject IDs.
- Undo: per item, restore the previous value only if the current one is still the one set, else that item reports
  `changed`; created subjects are removed only if still empty (as `UndoFromSchedule`).
- At most 40 rows (`ErrTooManyRows`).

## Allowed to change
- `paratech/desktop/internal/domain/library/services/calllinkbatch/*` (new) + tests
- `paratech/desktop/internal/cases/library/{calllinkbatch.go (new),types.go,errors.go,ports.go}` + tests
- `paratech/desktop/tests/*` (one SQLite scenario: the owner's 15-row timetable fixture, example data only)
- `docs/architecture.md` §12 `library` row (add `calllinkbatch`)

## Acceptance criteria
- [ ] Table test of `Decide` with the owner-shaped fixture: 15 rows, two subjects with different lecture and seminar
      links, one `in_person` row, one `unconfirmed` row, one teacher in brackets, one duplicate, one conflict, one
      unknown subject, one bad link.
- [ ] Apply: stale token, a forbidden choice, a partial apply after a store failure (the done subjects stay, the
      result says which), a repeat applies nothing twice, create-with-link.
- [ ] Undo: restored, changed since, created subject removed only if empty.
- [ ] No link or name in any log line (log handler test).
- [ ] `go vet`, `go test -race` for the touched packages and the architecture test pass.
