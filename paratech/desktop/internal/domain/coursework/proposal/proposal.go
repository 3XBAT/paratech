package proposal

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/shared"
)

// Bounds of the model-supplied fields.
const (
	MaxOutputBytes = 128 << 10 // the whole model answer
	MaxTitleRunes  = 200
	MaxBodyBytes   = 64 << 10
	MaxDueAhead    = 2 * 365 * 24 * time.Hour // a deadline further ahead is treated as a model error
)

// unixEpoch is the earliest due New accepts.
var unixEpoch = time.Unix(0, 0).UTC()

// Proposal is one action the LLM proposed. Fields change only through the methods below.
type Proposal struct {
	version int64 // persisted version for optimistic concurrency; 0 = never stored (ADR-0004)
	id      ID
	kind    Kind
	status  Status

	// Model-supplied data, validated by New.
	assignment shared.AssignmentID
	title      string
	body       string
	due        time.Time

	// Set by the core.
	reason          RejectReason
	recipient       Recipient // the trusted recipient frozen at validation; email proposals only
	attachment      Digest    // the export content pinned at validation; email proposals only
	assignmentTitle string    // the heading of the export file, frozen at validation; export proposals only
	format          Format    // the attachment chosen before validation; email proposals only
	createdAt       time.Time
	updatedAt       time.Time
	confirmedAt     time.Time
	executedAt      time.Time
}

// wire is the only shape the model may answer with. Unknown fields are rejected by the decoder; pointers tell a
// missing field from an empty one, so a field the kind does not allow is rejected even when empty.
type wire struct {
	Kind         *string `json:"kind"`
	AssignmentID *string `json:"assignment_id"`
	Title        *string `json:"title"`
	Body         *string `json:"body"`
	Due          *string `json:"due"`
}

// New parses untrusted model output into a PROPOSED proposal. It is strict: one JSON object of at most
// MaxOutputBytes, no unknown fields, a known kind and exactly the fields that kind uses:
//
//   - send_homework_email: assignment_id;
//   - export_homework: assignment_id, body (non-empty text, at most MaxBodyBytes);
//   - create_deadline_event: assignment_id, title (one line, at most MaxTitleRunes), due (RFC 3339, not before
//     1970-01-01 UTC).
//
// Any failure wraps ErrMalformed. Error messages name fields, never their content.
func New(id ID, out values.UntrustedOutput, now time.Time) (*Proposal, error) {
	if err := id.Validate(); err != nil {
		return nil, err
	}
	if now.IsZero() {
		return nil, fmt.Errorf("%w: no creation time", ErrInvalidProposal)
	}

	w, err := decode(out.Unvalidated())
	if err != nil {
		return nil, err
	}
	p := &Proposal{id: id, status: StatusProposed, createdAt: now, updatedAt: now}
	if err := p.fill(w); err != nil {
		return nil, err
	}
	if p.kind.NeedsAttachment() {
		p.format = FormatText
	}
	return p, nil
}

// ChooseFormat sets the attachment of a PROPOSED send_homework_email proposal (TASK-094): the user picks it in the
// confirmation dialog before the proposal is validated, so the digest pinned at validation is the one of that file.
// Another status yields ErrInvalidTransition, another kind or an unknown format ErrInvalidProposal.
func (p *Proposal) ChooseFormat(f Format) error {
	switch {
	case p.status != StatusProposed:
		return fmt.Errorf("%w: choose the format in %s", ErrInvalidTransition, p.status)
	case !p.kind.NeedsAttachment():
		return fmt.Errorf("%w: kind %s has no attachment", ErrInvalidProposal, p.kind)
	case !f.Valid():
		return fmt.Errorf("%w: unknown attachment format %q", ErrInvalidProposal, string(f))
	}
	p.format = f
	return nil
}

func malformed(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrMalformed, fmt.Sprintf(format, args...))
}

func decode(text string) (wire, error) {
	if len(text) > MaxOutputBytes {
		return wire{}, malformed("output is larger than %d bytes", MaxOutputBytes)
	}
	if !utf8.ValidString(text) {
		return wire{}, malformed("output is not valid UTF-8")
	}

	dec := json.NewDecoder(strings.NewReader(text))
	dec.DisallowUnknownFields()
	var w wire
	if err := dec.Decode(&w); err != nil {
		// The decoder error may quote an unknown field name, which is model text; keep only the class.
		return wire{}, malformed("not a single JSON object with the known fields")
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return wire{}, malformed("trailing data after the JSON object")
	}
	return w, nil
}

func (p *Proposal) fill(w wire) error {
	if w.Kind == nil || !Kind(*w.Kind).Valid() {
		return malformed("missing or unknown kind")
	}
	p.kind = Kind(*w.Kind)

	if w.AssignmentID == nil || shared.AssignmentID(*w.AssignmentID).Validate() != nil {
		return malformed("missing or invalid assignment_id")
	}
	p.assignment = shared.AssignmentID(*w.AssignmentID)

	allowed := map[Kind][3]bool{ // title, body, due
		KindSendHomework:   {false, false, false},
		KindExportHomework: {false, true, false},
		KindDeadlineEvent:  {true, false, true},
	}[p.kind]
	for i, f := range []struct {
		name string
		set  bool
	}{{"title", w.Title != nil}, {"body", w.Body != nil}, {"due", w.Due != nil}} {
		if f.set != allowed[i] {
			if f.set {
				return malformed("field %s is not allowed for kind %s", f.name, p.kind)
			}
			return malformed("field %s is required for kind %s", f.name, p.kind)
		}
	}

	if w.Title != nil {
		if !validTitle(*w.Title) {
			return malformed("title must be one line of 1-%d characters without control characters", MaxTitleRunes)
		}
		p.title = strings.TrimSpace(*w.Title)
	}
	if w.Body != nil {
		if !validBody(*w.Body) {
			return malformed("body must be non-empty text of at most %d bytes", MaxBodyBytes)
		}
		p.body = *w.Body
	}
	if w.Due != nil {
		due, err := time.Parse(time.RFC3339, *w.Due)
		if err != nil {
			return malformed("due is not an RFC 3339 time")
		}
		// A zero due would be stored as "no due" and the row could not be restored; no deadline predates the epoch.
		if due.Before(unixEpoch) {
			return malformed("due is before %s", unixEpoch.Format(time.DateOnly))
		}
		p.due = due.UTC()
	}
	return nil
}

func validTitle(s string) bool {
	t := strings.TrimSpace(s)
	n := utf8.RuneCountInString(t)
	return n > 0 && n <= MaxTitleRunes && !strings.ContainsFunc(t, unicode.IsControl)
}

func validBody(s string) bool {
	if strings.TrimSpace(s) == "" || len(s) > MaxBodyBytes {
		return false
	}
	return !strings.ContainsFunc(s, func(r rune) bool {
		return unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t'
	})
}

// State is the stored form of a proposal, for Restore.
type State struct {
	Version      int64
	ID           ID
	Kind         Kind
	Status       Status
	AssignmentID shared.AssignmentID
	Title        string
	Body         string
	Due          time.Time
	Reason       RejectReason
	Recipient    string
	// The pinned attachment (Digest); empty and 0 when none.
	AttachmentSHA256 string
	AttachmentSize   int64
	AssignmentTitle  string // export proposals once validated; else empty
	AttachmentFormat Format // email proposals; empty: FormatText (stored before TASK-094); else empty
	CreatedAt        time.Time
	UpdatedAt        time.Time
	ConfirmedAt      time.Time
	ExecutedAt       time.Time
}

// Restore rebuilds a stored proposal without side effects; it checks that the state is consistent, so a tampered row
// cannot produce a confirmed proposal without a confirmation time or an email command without a recipient and a
// pinned attachment.
func Restore(s State) (*Proposal, error) {
	bad := func(what string) (*Proposal, error) {
		return nil, fmt.Errorf("%w: %s", ErrInvalidProposal, what)
	}
	if s.Version < 1 {
		return bad("stored version must be at least 1")
	}
	if err := s.ID.Validate(); err != nil {
		return nil, err
	}
	if !s.Status.Valid() {
		return bad("unknown status")
	}
	if s.CreatedAt.IsZero() || s.UpdatedAt.Before(s.CreatedAt) {
		return bad("inconsistent timestamps")
	}

	// The model-supplied part is checked by the same rules as New.
	p := &Proposal{}
	w := wire{Kind: ptr(string(s.Kind)), AssignmentID: ptr(s.AssignmentID.String())}
	if s.Title != "" {
		w.Title = ptr(s.Title)
	}
	if s.Body != "" {
		w.Body = ptr(s.Body)
	}
	if !s.Due.IsZero() {
		w.Due = ptr(s.Due.UTC().Format(time.RFC3339Nano))
	}
	if err := p.fill(w); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidProposal, err)
	}

	if hasReason := s.Status == StatusRejected || s.Status == StatusStale; hasReason != s.Reason.validFor(s.Status) ||
		(!hasReason && s.Reason != "") {
		return bad("a fitting reason exists exactly for a rejected or stale proposal")
	}
	past := map[Status]bool{StatusValidated: true, StatusConfirmed: true, StatusDeclined: true, StatusExecuted: true, StatusStale: true}
	var recipient Recipient
	if s.Recipient != "" {
		r, err := NewRecipient(s.Recipient)
		if err != nil {
			return nil, err
		}
		recipient = r
	}
	if wantRecipient := p.kind.NeedsRecipient() && past[s.Status]; wantRecipient == recipient.IsZero() {
		return bad("a recipient exists exactly for a validated email proposal")
	}
	var attachment Digest
	if s.AttachmentSHA256 != "" || s.AttachmentSize != 0 {
		d, err := NewDigest(s.AttachmentSHA256, s.AttachmentSize)
		if err != nil {
			return nil, err
		}
		attachment = d
	}
	if wantAttachment := p.kind.NeedsAttachment() && past[s.Status]; wantAttachment == attachment.IsZero() {
		return bad("a pinned attachment exists exactly for a validated email proposal")
	}
	if wantTitle := p.kind.NeedsAssignmentTitle() && past[s.Status]; wantTitle != (s.AssignmentTitle != "") ||
		(wantTitle && !validTitle(s.AssignmentTitle)) {
		return bad("an assignment title exists exactly for a validated export proposal")
	}
	format := s.AttachmentFormat
	switch {
	case p.kind.NeedsAttachment() && format == "":
		format = FormatText
	case p.kind.NeedsAttachment() && !format.Valid(), !p.kind.NeedsAttachment() && format != "":
		return bad("an attachment format exists exactly for an email proposal")
	}
	confirmed := s.Status == StatusConfirmed || s.Status == StatusExecuted || s.Status == StatusStale
	if confirmed == s.ConfirmedAt.IsZero() || (s.Status == StatusExecuted) == s.ExecutedAt.IsZero() {
		return bad("confirmation and execution times do not match the status")
	}

	p.version = s.Version
	p.id = s.ID
	p.status = s.Status
	p.reason = s.Reason
	p.recipient = recipient
	p.attachment = attachment
	p.assignmentTitle = strings.TrimSpace(s.AssignmentTitle)
	p.format = format
	p.createdAt, p.updatedAt = s.CreatedAt, s.UpdatedAt
	p.confirmedAt, p.executedAt = s.ConfirmedAt, s.ExecutedAt
	return p, nil
}

func ptr(s string) *string { return &s }

// State returns the stored form of the proposal.
func (p *Proposal) State() State {
	return State{
		Version: p.version, ID: p.id, Kind: p.kind, Status: p.status, AssignmentID: p.assignment,
		Title: p.title, Body: p.body, Due: p.due, Reason: p.reason, Recipient: p.recipient.Address(),
		AttachmentSHA256: p.attachment.SHA256(), AttachmentSize: p.attachment.Size(), AssignmentTitle: p.assignmentTitle,
		AttachmentFormat: p.format, CreatedAt: p.createdAt, UpdatedAt: p.updatedAt, ConfirmedAt: p.confirmedAt, ExecutedAt: p.executedAt,
	}
}

func (p *Proposal) move(to Status, now time.Time) error {
	if !CanTransition(p.status, to) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, p.status, to)
	}
	if now.IsZero() {
		return fmt.Errorf("%w: no time", ErrInvalidProposal)
	}
	p.status = to
	p.updatedAt = now
	return nil
}

// Validate checks a PROPOSED proposal against trusted local state and moves it to VALIDATED or REJECTED. A rejection
// is an outcome, not an error: the reason is kept for the user. The checks are:
//
//   - the assignment exists locally;
//   - send_homework_email: a recipient is configured and the export of the assignment exists; the recipient and the
//     digest of the export are frozen into the proposal, so the user confirms exactly the address and the content
//     that will be sent;
//   - export_homework: the stored title of the assignment is frozen into the proposal as the heading of the file (the
//     model may not send one);
//   - create_deadline_event: the due time is not in the past and at most MaxDueAhead ahead.
//
// Facts of an existing assignment without a valid title, for an export, are an error of the caller.
func (p *Proposal) Validate(f Facts, now time.Time) error {
	if p.status != StatusProposed {
		return fmt.Errorf("%w: validate in %s", ErrInvalidTransition, p.status)
	}
	if p.kind.NeedsAssignmentTitle() && f.AssignmentExists && !validTitle(f.AssignmentTitle) {
		return fmt.Errorf("%w: the facts lack the assignment title", ErrInvalidProposal)
	}

	reason := p.check(f, now)
	if reason != "" {
		if err := p.move(StatusRejected, now); err != nil {
			return err
		}
		p.reason = reason
		return nil
	}

	if err := p.move(StatusValidated, now); err != nil {
		return err
	}
	if p.kind.NeedsRecipient() {
		p.recipient = f.Recipient
	}
	if p.kind.NeedsAttachment() {
		p.attachment = f.Attachment
	}
	if p.kind.NeedsAssignmentTitle() {
		p.assignmentTitle = strings.TrimSpace(f.AssignmentTitle)
	}
	return nil
}

func (p *Proposal) check(f Facts, now time.Time) RejectReason {
	if !f.AssignmentExists {
		return ReasonUnknownAssignment
	}
	if p.kind.NeedsRecipient() && f.Recipient.IsZero() {
		return ReasonNoRecipient
	}
	if p.kind.NeedsAttachment() && f.Attachment.IsZero() {
		return ReasonNoArtifact
	}
	if p.kind == KindDeadlineEvent && (p.due.Before(now) || p.due.After(now.Add(MaxDueAhead))) {
		return ReasonDueOutOfRange
	}
	return ""
}

// Recheck compares a CONFIRMED proposal with fresh trusted facts right before execution. If what the user confirmed
// no longer holds (the assignment is gone; for an email the recipient, the export content or the subject differs),
// the proposal moves to the terminal STALE status with the reason and Recheck returns an error wrapping ErrStale; the
// caller saves the proposal and calls no port. Any other status yields ErrNotConfirmed. The due time is not checked
// again: the user confirmed it.
func (p *Proposal) Recheck(f Facts, now time.Time) error {
	if p.status != StatusConfirmed {
		return fmt.Errorf("%w: status %s", ErrNotConfirmed, p.status)
	}

	reason := p.staleReason(f)
	if reason == "" {
		return nil
	}
	if err := p.move(StatusStale, now); err != nil {
		return err
	}
	p.reason = reason
	return fmt.Errorf("%w: %s", ErrStale, reason)
}

func (p *Proposal) staleReason(f Facts) RejectReason {
	switch {
	case !f.AssignmentExists:
		return ReasonUnknownAssignment
	case p.kind.NeedsRecipient() && f.Recipient != p.recipient:
		return ReasonRecipientChanged
	case p.kind.NeedsAttachment() && f.Attachment.IsZero():
		return ReasonNoArtifact
	case p.kind.NeedsAttachment() && f.Attachment != p.attachment:
		return ReasonArtifactChanged
	case p.kind.NeedsAttachment() && f.ShownFormat != p.format:
		return ReasonFormatChanged
	case p.kind.NeedsSubject() && f.Subject != f.ShownSubject:
		return ReasonSubjectChanged
	}
	return ""
}

// Confirm records the explicit user approval of a VALIDATED proposal (AC-14).
func (p *Proposal) Confirm(now time.Time) error { return p.confirm(now) }

// confirm is the body of Confirm; ConfirmOnce calls it so the user-only source test does not see a Confirm call here.
func (p *Proposal) confirm(now time.Time) error {
	if err := p.move(StatusConfirmed, now); err != nil {
		return err
	}
	p.confirmedAt = now
	return nil
}

// ConfirmOnce is Confirm for a repeated user click: a proposal that is already CONFIRMED or EXECUTED stays as it is
// and changed is false; any other status behaves as Confirm.
func (p *Proposal) ConfirmOnce(now time.Time) (changed bool, err error) {
	if p.status == StatusConfirmed || p.status == StatusExecuted {
		return false, nil
	}
	if err := p.confirm(now); err != nil {
		return false, err
	}
	return true, nil
}

// Decline records that the user refused a VALIDATED proposal; it is never executed.
func (p *Proposal) Decline(now time.Time) error { return p.move(StatusDeclined, now) }

// MarkExecuted records that the side effect of a CONFIRMED proposal was performed.
func (p *Proposal) MarkExecuted(now time.Time) error {
	if err := p.move(StatusExecuted, now); err != nil {
		return err
	}
	p.executedAt = now
	return nil
}

// ID returns the proposal identifier.
func (p *Proposal) ID() ID { return p.id }

// Kind returns what the proposal asks for.
func (p *Proposal) Kind() Kind { return p.kind }

// Status returns the current status.
func (p *Proposal) Status() Status { return p.status }

// Executed reports whether the side effect was already performed, so a repeated execution does nothing.
func (p *Proposal) Executed() bool { return p.status == StatusExecuted }

// Attaches reports whether the proposal is a VALIDATED send whose pinned attachment is exactly d: the agent's send of
// a draft version (TASK-127) is kept only when the export file holds that version.
func (p *Proposal) Attaches(d Digest) bool {
	return p.kind == KindSendHomework && p.status == StatusValidated && !d.IsZero() && p.attachment == d
}

// WaitsAsDeadline reports whether the proposal is a VALIDATED deadline event of assignment a with this title and due
// time: proposing the same deadline again reuses it instead of a second «Да» (TASK-127).
func (p *Proposal) WaitsAsDeadline(a shared.AssignmentID, title string, due time.Time) bool {
	return p.kind == KindDeadlineEvent && p.status == StatusValidated && p.assignment == a &&
		p.title == strings.TrimSpace(title) && p.due.Equal(due)
}

// AssignmentID returns the assignment the proposal refers to.
func (p *Proposal) AssignmentID() shared.AssignmentID { return p.assignment }

// Title returns the model-supplied event title (create_deadline_event only). It is bounded data, not an instruction.
func (p *Proposal) Title() string { return p.title }

// Body returns the model-supplied homework text (export_homework only). It is bounded data, not an instruction.
func (p *Proposal) Body() string { return p.body }

// Due returns the model-supplied deadline (create_deadline_event only), in UTC.
func (p *Proposal) Due() time.Time { return p.due }

// Reason returns why the proposal was rejected or went stale; empty unless REJECTED or STALE.
func (p *Proposal) Reason() RejectReason { return p.reason }

// Recipient returns the trusted recipient frozen at validation (send_homework_email only).
func (p *Proposal) Recipient() Recipient { return p.recipient }

// AttachmentDigest returns the export content pinned at validation (send_homework_email only).
func (p *Proposal) AttachmentDigest() Digest { return p.attachment }

// AttachmentFormat returns the attachment the user chose (send_homework_email only; empty for the other kinds).
func (p *Proposal) AttachmentFormat() Format { return p.format }

// AssignmentTitle returns the trusted heading of the export file, frozen at validation (export_homework only).
func (p *Proposal) AssignmentTitle() string { return p.assignmentTitle }

// CreatedAt returns when the proposal was parsed.
func (p *Proposal) CreatedAt() time.Time { return p.createdAt }

// ConfirmedAt returns when the user confirmed it; zero if never.
func (p *Proposal) ConfirmedAt() time.Time { return p.confirmedAt }

// Version returns the version this instance was last loaded or saved with: 0 for an aggregate that was never
// stored, otherwise the stored version, which is at least 1 (optimistic concurrency, ADR-0004).
func (p *Proposal) Version() int64 { return p.version }

// Persisted records the version the store assigned after a successful Save. It is called by the repository adapter
// only, after the write succeeded (ADR-0004); use cases and handlers never call it.
func (p *Proposal) Persisted(version int64) { p.version = version }
