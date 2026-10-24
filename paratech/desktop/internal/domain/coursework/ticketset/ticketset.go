package ticketset

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"

	"paratech/desktop/internal/domain/coursework/answer"
	"paratech/desktop/internal/domain/shared"
)

// stepKeyPrefix starts the key of a ticket step; the version changes with the key's shape.
const stepKeyPrefix = "tkt:v1:"

// Result is what a ticket got: the answer with citations (none when no passage matched or the answer was refused)
// and its coverage. The texts and the places stay in the answer.
type Result struct {
	Answer   shared.AnswerID `json:"answer"`
	Coverage Coverage        `json:"coverage"`
}

func (r Result) valid() bool {
	switch {
	case !r.Coverage.Valid():
		return false
	case r.Answer != "" && r.Answer.Validate() != nil:
		return false
	case r.Coverage == CoverageCovered || r.Coverage == CoveragePartial:
		return r.Answer != ""
	case r.Coverage == CoverageFailed:
		return r.Answer == ""
	}
	return true
}

// Ticket is one ticket of the set with its result once answered.
type Ticket struct {
	Number int     `json:"number"`
	Title  string  `json:"title"`
	Result *Result `json:"result"`
}

// File is the .docx of a done set: the library material that holds it and its artifact.
type File struct {
	Material shared.MaterialID `json:"material"`
	Artifact string            `json:"artifact"` // shared.ArtifactRef in its string form
}

// ArtifactRef returns the artifact of the file; zero for a malformed one.
func (f File) ArtifactRef() shared.ArtifactRef {
	r, _ := shared.ParseArtifactRef(f.Artifact)
	return r
}

// Accepted is the estimate and the cap the confirm card showed when the student confirmed the run. A stored set is
// checked against these figures, never against the current constants, so a later change of the pricing keeps every
// stored set readable.
type Accepted struct {
	EstimateMicroUSD int64 `json:"estimate_micro_usd"`
	CapMicroUSD      int64 `json:"cap_micro_usd"`
}

// overCap reports whether the run was above the cap the student saw.
func (a Accepted) overCap() bool { return a.EstimateMicroUSD > a.CapMicroUSD }

// Content is the stored body of a set beside its columns: the tickets with their results, the figures the student
// confirmed, their acceptance of a cost above the cap and the file. Stored as canonical JSON (Encode), read back with
// ParseContent.
type Content struct {
	Tickets      []Ticket  `json:"tickets"`
	Accepted     *Accepted `json:"accepted"`
	CostAccepted bool      `json:"cost_accepted"`
	File         *File     `json:"file"`
}

// StepKind is what the next step of a run does.
type StepKind string

// Step kinds.
const (
	StepNone     StepKind = "none"     // nothing to do: not answering
	StepTicket   StepKind = "ticket"   // answer Ticket
	StepAssemble StepKind = "assemble" // build and file the .docx
)

// Step is the next step of a run. Key is a staleness token of a ticket step, a digest of the set, the ticket and its
// title: Record keeps a result only for the step it was asked for. It is not the Hub's idempotency key, which is the
// key of the answer request (answer.Request.Key in cases/answering).
type Step struct {
	Kind   StepKind
	Ticket int
	Key    string
}

// Set is a set of exam tickets. Fields change only through the methods (ARCH-14).
type Set struct {
	version int64 // persisted version for optimistic concurrency; 0 = never stored (ADR-0004)

	id        shared.TicketSetID
	subject   shared.SubjectID  // the subject whose lessons answer the tickets and whose «Билеты» gets the file
	source    shared.MaterialID // the material the list was read from; "" for a pasted list
	status    Status
	content   Content
	createdAt time.Time
	updatedAt time.Time
}

// New proposes a set of the parsed items of a list (ParseList) for a subject: nothing is paid until Confirm.
// Error: ErrInvalidSet.
func New(id shared.TicketSetID, sub shared.SubjectID, source shared.MaterialID, items []Item, now time.Time) (*Set, error) {
	tickets := make([]Ticket, 0, len(items))
	for _, it := range items {
		tickets = append(tickets, Ticket{Number: it.Number, Title: it.Title})
	}
	s := &Set{
		id: id, subject: sub, source: source, status: StatusProposed, content: Content{Tickets: tickets},
		createdAt: now, updatedAt: now,
	}
	if err := s.validate(); err != nil {
		return nil, err
	}
	return s, nil
}

// Budget returns the estimate of the run of the set: the figures the student confirmed once confirmed, else the
// current estimate.
func (s *Set) Budget() Budget {
	n := len(s.content.Tickets)
	if a := s.content.Accepted; a != nil {
		return Budget{Tickets: n, EstimateMicroUSD: a.EstimateMicroUSD, CapMicroUSD: a.CapMicroUSD, OverCap: a.overCap()}
	}
	return Estimate(n)
}

// Confirm is the student's «Подтвердить» of a proposed set: the paid run may start. A run whose estimate is over the
// cap needs acceptCost (the card says so). The figures shown are kept with the set. Errors: ErrInvalidTransition,
// ErrCostNotAccepted.
func (s *Set) Confirm(acceptCost bool, now time.Time) error {
	if s.status != StatusProposed {
		return fmt.Errorf("%w: confirm a %s set", ErrInvalidTransition, s.status)
	}
	b := Estimate(len(s.content.Tickets))
	if b.OverCap && !acceptCost {
		return ErrCostNotAccepted
	}
	next := s.clone()
	next.content.Accepted = &Accepted{EstimateMicroUSD: b.EstimateMicroUSD, CapMicroUSD: b.CapMicroUSD}
	next.content.CostAccepted = b.OverCap
	return s.move(next, StatusConfirmed, now)
}

// Start begins the answering of a confirmed set (the first step of its job). Error: ErrInvalidTransition.
func (s *Set) Start(now time.Time) error { return s.move(s.clone(), StatusAnswering, now) }

// Cancel is the student's «Отменить»: a set not finished stops; its job is cancelled by the use case. Error:
// ErrInvalidTransition for a finished set.
func (s *Set) Cancel(now time.Time) error { return s.move(s.clone(), StatusCancelled, now) }

// Fail ends a run whose file cannot be built. Error: ErrInvalidTransition.
func (s *Set) Fail(now time.Time) error { return s.move(s.clone(), StatusFailed, now) }

// Next returns the next step of the run: the first ticket without its result, then the file.
func (s *Set) Next() Step {
	if s.status != StatusAnswering {
		return Step{Kind: StepNone}
	}
	for _, t := range s.content.Tickets {
		if t.Result == nil {
			return Step{Kind: StepTicket, Ticket: t.Number, Key: s.stepKey(t)}
		}
	}
	return Step{Kind: StepAssemble}
}

// Record keeps the result of the due ticket step st; any other step is ErrStaleStep. Error: ErrInvalidSet for a
// result that breaks its rules.
func (s *Set) Record(st Step, r Result, now time.Time) error {
	if st.Kind != StepTicket || s.Next() != st {
		return ErrStaleStep
	}
	if !r.valid() {
		return fmt.Errorf("%w: ticket result", ErrInvalidSet)
	}
	next := s.clone()
	for i, t := range next.content.Tickets {
		if t.Number == st.Ticket {
			res := r
			next.content.Tickets[i].Result = &res
		}
	}
	return s.move(next, StatusAnswering, now)
}

// Finish keeps the filed file and marks the set done. Errors: ErrStaleStep when the file is not due, ErrInvalidSet for
// a bad file.
func (s *Set) Finish(f File, now time.Time) error {
	if s.Next().Kind != StepAssemble {
		return ErrStaleStep
	}
	next := s.clone()
	file := f
	next.content.File = &file
	return s.move(next, StatusDone, now)
}

// move checks the transition and the state next would have, then takes it.
func (s *Set) move(next *Set, to Status, now time.Time) error {
	if to != s.status && !CanTransition(s.status, to) {
		return fmt.Errorf("%w: %s to %s", ErrInvalidTransition, s.status, to)
	}
	next.status, next.updatedAt = to, now
	if err := next.validate(); err != nil {
		return err
	}
	*s = *next
	return nil
}

func (s *Set) clone() *Set {
	c := *s
	c.content = s.content.clone()
	return &c
}

func (c Content) clone() Content {
	out := c
	out.Tickets = slices.Clone(c.Tickets)
	for i, t := range out.Tickets {
		if t.Result != nil {
			r := *t.Result
			out.Tickets[i].Result = &r
		}
	}
	if c.Accepted != nil {
		a := *c.Accepted
		out.Accepted = &a
	}
	if c.File != nil {
		f := *c.File
		out.File = &f
	}
	return out
}

// stepKey is the key of the step of ticket t.
func (s *Set) stepKey(t Ticket) string {
	h := sha256.New()
	for _, part := range []string{string(s.id), strconv.Itoa(t.Number), t.Title} {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	return fmt.Sprintf("%s%x", stepKeyPrefix, h.Sum(nil))
}

// validate checks the invariants of the whole state.
func (s *Set) validate() error {
	bad := func(why string) error { return fmt.Errorf("%w: %s", ErrInvalidSet, why) }
	switch {
	case s.id.Validate() != nil:
		return bad("set id")
	case s.subject.Validate() != nil:
		return bad("subject")
	case s.source != "" && s.source.Validate() != nil:
		return bad("source")
	case !s.status.Valid():
		return bad("status")
	case s.createdAt.IsZero() || s.updatedAt.IsZero():
		return bad("missing time")
	case len(s.content.Tickets) == 0 || len(s.content.Tickets) > MaxTickets:
		return bad("tickets count")
	}
	answered := s.status == StatusAnswering || s.status == StatusDone || s.status == StatusFailed ||
		s.status == StatusCancelled
	for i, t := range s.content.Tickets {
		switch {
		case t.Number < 1 || t.Number > MaxNumber || (i > 0 && t.Number <= s.content.Tickets[i-1].Number):
			return bad("ticket numbers")
		case !validTitle(t.Title):
			return bad("ticket title")
		case t.Result != nil && (!answered || !t.Result.valid()):
			return bad("ticket result")
		case t.Result == nil && s.status == StatusDone:
			return bad("a done set has every result")
		}
	}
	// The acceptance is checked against the stored figures, never the current constants (Accepted).
	if a := s.content.Accepted; a == nil {
		switch {
		case s.content.CostAccepted:
			return bad("a cost accepted without its figures")
		case s.status != StatusProposed && s.status != StatusCancelled:
			return bad("a run without the figures the student confirmed")
		}
	} else {
		switch {
		case s.status == StatusProposed:
			return bad("figures of a set not confirmed")
		case a.EstimateMicroUSD <= 0 || a.CapMicroUSD <= 0:
			return bad("confirmed figures")
		case s.content.CostAccepted != a.overCap():
			return bad("a cost acceptance that does not match the confirmed figures")
		}
	}
	switch {
	case (s.status == StatusDone) != (s.content.File != nil):
		return bad("a file exactly when done")
	}
	if f := s.content.File; f != nil {
		if f.Material.Validate() != nil {
			return bad("file material")
		}
		if _, err := shared.ParseArtifactRef(f.Artifact); err != nil {
			return bad("file artifact")
		}
	}
	return nil
}

// validTitle is a title as ParseList gives it: a question the reader takes (answer.ParseQuestion) unchanged.
func validTitle(t string) bool {
	q, err := answer.ParseQuestion(t)
	return err == nil && q == t
}

// --- reads ----------------------------------------------------------------------------------------------

// ID returns the identifier.
func (s *Set) ID() shared.TicketSetID { return s.id }

// Subject returns the subject of the set.
func (s *Set) Subject() shared.SubjectID { return s.subject }

// Source returns the material the list was read from; "" for a pasted list.
func (s *Set) Source() shared.MaterialID { return s.source }

// Status returns the status.
func (s *Set) Status() Status { return s.status }

// Tickets returns a copy of the tickets with their results.
func (s *Set) Tickets() []Ticket { return s.content.clone().Tickets }

// CostAccepted reports whether the student accepted a cost above the cap.
func (s *Set) CostAccepted() bool { return s.content.CostAccepted }

// File returns the file of a done set; ok is false before.
func (s *Set) File() (File, bool) {
	if s.content.File == nil {
		return File{}, false
	}
	return *s.content.File, true
}

// Progress returns how many tickets have their result of how many.
func (s *Set) Progress() (done, total int) {
	for _, t := range s.content.Tickets {
		if t.Result != nil {
			done++
		}
	}
	return done, len(s.content.Tickets)
}

// Report counts the tickets by coverage.
func (s *Set) Report() Report {
	var r Report
	for _, t := range s.content.Tickets {
		if t.Result == nil {
			r.Pending++
			continue
		}
		switch t.Result.Coverage {
		case CoverageCovered:
			r.Covered++
		case CoveragePartial:
			r.Partial++
		case CoverageNotCovered:
			r.NotCovered++
		case CoverageFailed:
			r.Failed++
		}
	}
	return r
}

// CreatedAt returns when the set was proposed.
func (s *Set) CreatedAt() time.Time { return s.createdAt }

// UpdatedAt returns when the set last changed.
func (s *Set) UpdatedAt() time.Time { return s.updatedAt }

// Version returns the version this instance was last loaded or saved with: 0 for one never stored.
func (s *Set) Version() int64 { return s.version }

// Persisted records the version the store assigned after a successful Save; called by the repository adapter only.
func (s *Set) Persisted(version int64) { s.version = version }

// --- stored form ----------------------------------------------------------------------------------------

// State is the stored form, for Restore and the repositories.
type State struct {
	Version   int64
	ID        shared.TicketSetID
	Subject   shared.SubjectID
	Source    shared.MaterialID
	Status    Status
	Content   Content
	CreatedAt time.Time
	UpdatedAt time.Time
}

// State returns the stored form.
func (s *Set) State() State {
	return State{
		Version: s.version, ID: s.id, Subject: s.subject, Source: s.source, Status: s.status,
		Content: s.content.clone(), CreatedAt: s.createdAt, UpdatedAt: s.updatedAt,
	}
}

// Restore rebuilds a stored set without side effects; it only checks that the state is consistent. The content must
// have been read back with ParseContent.
func Restore(st State) (*Set, error) {
	if st.Version < 1 {
		return nil, fmt.Errorf("%w: stored version %d", ErrInvalidSet, st.Version)
	}
	s := &Set{
		version: st.Version, id: st.ID, subject: st.Subject, source: st.Source, status: st.Status,
		content: st.Content.clone(), createdAt: st.CreatedAt, updatedAt: st.UpdatedAt,
	}
	if err := s.validate(); err != nil {
		return nil, err
	}
	return s, nil
}

// Encode returns the canonical JSON of the content.
func (c Content) Encode() string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(c) // strings, ints, bools and pointers of them only: encoding cannot fail
	return strings.TrimSuffix(b.String(), "\n")
}

// ParseContent reads stored content strictly (no unknown field, nothing after it); Restore checks its bounds, so a
// stored title passes the same checks as a fresh one. Error: ErrInvalidSet.
func ParseContent(s string) (Content, error) {
	d := json.NewDecoder(strings.NewReader(s))
	d.DisallowUnknownFields()
	var c Content
	if err := d.Decode(&c); err != nil {
		return Content{}, fmt.Errorf("%w: content: %w", ErrInvalidSet, err)
	}
	if _, err := d.Token(); !errors.Is(err, io.EOF) {
		return Content{}, fmt.Errorf("%w: content: data after the object", ErrInvalidSet)
	}
	return c, nil
}
