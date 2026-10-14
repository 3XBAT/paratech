package delivery

import (
	"errors"
	"fmt"
	"time"

	"paratech/desktop/internal/domain/shared"
)

// Status is the state of a delivery. The set of values is mirrored by the CHECK constraint of
// deliveries.status in docs/contracts/schema.sql; a test keeps both in sync. The transitions are in transitions.go.
//
// PRD sections 10 and 14 (case 7) name only UNKNOWN; the rest is derived from the section 10 flow and hard rule 7
// (ADR-0004(c)): PENDING (to be sent), SENDING (an attempt is in flight; saved before the message is handed to the
// provider, so a crash leaves SENDING, which startup recovery turns into UNKNOWN), SENT (accepted), FAILED (refused,
// nothing was sent), UNKNOWN (ambiguous outcome, reconciled or resolved by the user before anything is sent again).
type Status string

// Delivery statuses.
const (
	StatusPending Status = "PENDING"
	StatusSending Status = "SENDING"
	StatusSent    Status = "SENT"
	StatusFailed  Status = "FAILED"
	StatusUnknown Status = "UNKNOWN"
)

// Statuses returns every status.
func Statuses() []Status {
	return []Status{StatusPending, StatusSending, StatusSent, StatusFailed, StatusUnknown}
}

// String returns the status as stored and shown.
func (s Status) String() string { return string(s) }

// Fixed last errors set by the aggregate itself.
const (
	msgInterrupted = "interrupted while sending; the outcome is unknown"
)

// Delivery is an attempt to send an artifact to a recipient (PRD section 10). Fields change only through the methods
// below (ARCH-14).
type Delivery struct {
	version int64 // persisted version for optimistic concurrency; 0 = never stored (ADR-0004)
	id      shared.DeliveryID
	status  Status

	assignment shared.AssignmentID
	recipient  string
	subject    string
	attachment Attachment
	messageID  string
	key        string

	attempts  int
	round     int // send round: 0 for the first job, +1 for every SendAgain (JobKey)
	lastError string

	confirmedAt time.Time
	sentAt      time.Time
	createdAt   time.Time
	updatedAt   time.Time
}

// Spec is what the creator of a delivery provides to New.
type Spec struct {
	ID           shared.DeliveryID
	AssignmentID shared.AssignmentID
	Recipient    string     // the trusted recipient the user confirmed
	Subject      string     // one line, set by the core
	Attachment   Attachment // the file and the content the user confirmed
	MessageID    string     // "<left@right>", generated before the first attempt and kept for every attempt
	Key          string     // idempotency key: one key is one delivery for ever
	ConfirmedAt  time.Time  // the explicit user confirmation (AC-14)
}

// New creates a PENDING delivery. A delivery cannot exist without the user's confirmation: ConfirmedAt is required and
// cannot be later than now (AC-14).
func New(spec Spec, now time.Time) (*Delivery, error) {
	if now.IsZero() {
		return nil, fmt.Errorf("%w: no creation time", ErrInvalidDelivery)
	}
	if spec.ConfirmedAt.IsZero() || spec.ConfirmedAt.After(now) {
		return nil, fmt.Errorf("%w: no user confirmation before creation", ErrInvalidDelivery)
	}
	d := &Delivery{
		id: spec.ID, status: StatusPending, assignment: spec.AssignmentID, recipient: spec.Recipient,
		subject: spec.Subject, attachment: spec.Attachment, messageID: spec.MessageID, key: spec.Key,
		confirmedAt: spec.ConfirmedAt, createdAt: now, updatedAt: now,
	}
	if err := d.validate(); err != nil {
		return nil, err
	}
	return d, nil
}

func (d *Delivery) validate() error {
	if err := d.id.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidDelivery, err)
	}
	if err := d.assignment.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidDelivery, err)
	}
	if d.attachment == (Attachment{}) {
		return fmt.Errorf("%w: no attachment", ErrInvalidDelivery)
	}
	return errors.Join(checkRecipient(d.recipient), checkSubject(d.subject), checkMessageID(d.messageID), checkKey(d.key))
}

// State is the stored form of a delivery, for Restore.
type State struct {
	Version      int64
	ID           shared.DeliveryID
	Status       Status
	AssignmentID shared.AssignmentID
	Recipient    string
	Subject      string
	Attachment   shared.ArtifactRef
	SHA256       string
	Size         int64
	MessageID    string
	Key          string
	Attempts     int
	Round        int
	LastError    string
	ConfirmedAt  time.Time
	SentAt       time.Time // zero unless SENT
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Restore rebuilds a stored delivery without side effects. It checks the invariants, so a tampered row cannot produce
// a delivery without confirmation, a header-injecting recipient or subject, or a SENT delivery without a send time.
func Restore(s State) (*Delivery, error) {
	bad := func(what string) (*Delivery, error) {
		return nil, fmt.Errorf("%w: %s", ErrInvalidDelivery, what)
	}
	if s.Version < 1 {
		return bad("stored version must be at least 1")
	}
	if !s.Status.Valid() {
		return bad("unknown status")
	}
	att, err := NewAttachment(s.Attachment, s.SHA256, s.Size)
	if err != nil {
		return nil, err
	}
	if s.ConfirmedAt.IsZero() || s.CreatedAt.Before(s.ConfirmedAt) || s.UpdatedAt.Before(s.CreatedAt) {
		return bad("inconsistent confirmation and timestamps")
	}
	if (s.Status == StatusSent) == s.SentAt.IsZero() {
		return bad("a send time exists exactly for a sent delivery")
	}
	if s.Attempts < 0 || len(s.LastError) > MaxLastErrorBytes {
		return bad("attempts or last error out of range")
	}
	// Every round but the first follows an UNKNOWN attempt, so there are at least as many attempts as later rounds.
	if s.Round < 0 || s.Round > s.Attempts {
		return bad("send round out of range")
	}
	if s.Attempts == 0 && (s.Status == StatusSending || s.Status == StatusSent || s.Status == StatusUnknown) {
		return bad("a delivery that was handed over has at least one attempt")
	}

	d := &Delivery{
		version: s.Version, id: s.ID, status: s.Status, assignment: s.AssignmentID, recipient: s.Recipient,
		subject: s.Subject, attachment: att, messageID: s.MessageID, key: s.Key, attempts: s.Attempts,
		round: s.Round, lastError: s.LastError, confirmedAt: s.ConfirmedAt, sentAt: s.SentAt, createdAt: s.CreatedAt, updatedAt: s.UpdatedAt,
	}
	if err := d.validate(); err != nil {
		return nil, err
	}
	return d, nil
}

// State returns the stored form of the delivery.
func (d *Delivery) State() State {
	return State{
		Version: d.version, ID: d.id, Status: d.status, AssignmentID: d.assignment, Recipient: d.recipient,
		Subject: d.subject, Attachment: d.attachment.ref, SHA256: d.attachment.sha256, Size: d.attachment.size,
		MessageID: d.messageID, Key: d.key, Attempts: d.attempts, Round: d.round, LastError: d.lastError,
		ConfirmedAt: d.confirmedAt, SentAt: d.sentAt, CreatedAt: d.createdAt, UpdatedAt: d.updatedAt,
	}
}

func (d *Delivery) move(to Status, now time.Time) error {
	if !CanTransition(d.status, to) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, d.status, to)
	}
	if now.IsZero() {
		return fmt.Errorf("%w: no time", ErrInvalidDelivery)
	}
	d.status = to
	d.updatedAt = now
	return nil
}

// StartSending begins an attempt: PENDING -> SENDING. The caller saves it BEFORE handing the message to the provider,
// so a crash during the attempt leaves SENDING, never PENDING (hard rule 7).
func (d *Delivery) StartSending(now time.Time) error {
	if err := d.move(StatusSending, now); err != nil {
		return err
	}
	d.attempts++
	return nil
}

// Outcome is what an attempt led to.
type Outcome int

// Outcomes of an attempt.
const (
	OutcomeSent    Outcome = iota + 1 // accepted: SENT
	OutcomeRetry                      // definitely not accepted, may succeed later: back to PENDING
	OutcomeFailed                     // definitely refused, or the attachment changed: FAILED
	OutcomeUnknown                    // may have been accepted: UNKNOWN, never sent again before it is resolved
)

// OutcomeOf classifies the result of EmailAdapter.Send. Only a result that proves nothing was accepted allows another
// attempt; anything the adapter did not classify is UNKNOWN (PRD section 14 case 7, AC-15). ErrOutcomeUnknown wins
// over every other marker.
func OutcomeOf(err error) Outcome {
	switch {
	case err == nil:
		return OutcomeSent
	case errors.Is(err, ErrOutcomeUnknown):
		return OutcomeUnknown
	case errors.Is(err, ErrRejected), errors.Is(err, ErrAttachmentChanged):
		return OutcomeFailed
	case errors.Is(err, ErrTransient):
		return OutcomeRetry
	}
	return OutcomeUnknown
}

// RecordResult ends the attempt in flight (SENDING) with the result of the send and returns its outcome: SENT,
// PENDING (retry later), FAILED or UNKNOWN. The error text becomes the last error; adapters give short fixed texts.
func (d *Delivery) RecordResult(sendErr error, now time.Time) (Outcome, error) {
	return d.record(sendErr, false, now)
}

// RecordFinalResult is RecordResult for the last attempt of the job: a retry outcome ends FAILED ("attempts
// exhausted") instead of PENDING, because no attempt is left to send it (PRD section 7.3: bounded retries). This is
// safe: a retry outcome proves nothing was accepted. Every other outcome is the same as with RecordResult.
func (d *Delivery) RecordFinalResult(sendErr error, now time.Time) (Outcome, error) {
	return d.record(sendErr, true, now)
}

func (d *Delivery) record(sendErr error, final bool, now time.Time) (Outcome, error) {
	if d.status != StatusSending {
		return 0, fmt.Errorf("%w: record a result in %s", ErrInvalidTransition, d.status)
	}
	out := OutcomeOf(sendErr)
	msg := ""
	if sendErr != nil {
		msg = sendErr.Error()
	}
	if out == OutcomeRetry && final {
		out, msg = OutcomeFailed, ErrAttemptsExhausted.Error()+": "+msg
	}
	to := map[Outcome]Status{
		OutcomeSent: StatusSent, OutcomeRetry: StatusPending, OutcomeFailed: StatusFailed, OutcomeUnknown: StatusUnknown,
	}[out]
	if err := d.move(to, now); err != nil {
		return 0, err
	}
	if out == OutcomeSent {
		d.sentAt = now
		d.lastError = ""
		return out, nil
	}
	d.lastError = limitError(msg)
	return out, nil
}

// FailUnsent fails a PENDING delivery, which is never in the hands of the provider: its attachment is gone or changed,
// or its job used up its attempts before handing it over (reason wraps ErrAttemptsExhausted).
func (d *Delivery) FailUnsent(reason error, now time.Time) error {
	if d.status != StatusPending {
		return fmt.Errorf("%w: fail an unsent delivery in %s", ErrInvalidTransition, d.status)
	}
	if reason == nil {
		return fmt.Errorf("%w: no reason", ErrInvalidDelivery)
	}
	if err := d.move(StatusFailed, now); err != nil {
		return err
	}
	d.lastError = limitError(reason.Error())
	return nil
}

// Interrupted marks an attempt that a previous process left in flight: SENDING -> UNKNOWN. The message may or may not
// have been accepted, so it is never sent again on its own (startup recovery, PRD section 14 case 7).
func (d *Delivery) Interrupted(now time.Time) error {
	if d.status != StatusSending {
		return fmt.Errorf("%w: interrupted in %s", ErrInvalidTransition, d.status)
	}
	if err := d.move(StatusUnknown, now); err != nil {
		return err
	}
	d.lastError = msgInterrupted
	return nil
}

// MarkSent resolves an UNKNOWN delivery as accepted: reconciliation found the message, or the user checked the
// mailbox and marked it as sent. UNKNOWN -> SENT.
func (d *Delivery) MarkSent(now time.Time) error {
	if d.status != StatusUnknown {
		return fmt.Errorf("%w: mark as sent in %s", ErrInvalidTransition, d.status)
	}
	if err := d.move(StatusSent, now); err != nil {
		return err
	}
	d.sentAt = now
	return nil
}

// SendAgain resolves an UNKNOWN delivery as not accepted, so it is sent again: reconciliation proved the message was
// not accepted, or the user chose to send again accepting a possible duplicate. UNKNOWN -> PENDING. It starts a new
// send round, so the delivery gets a new job (JobKey) with a full set of attempts. The Message-ID stays the same, so a
// receiving system can still recognise a duplicate.
func (d *Delivery) SendAgain(now time.Time) error {
	if d.status != StatusUnknown {
		return fmt.Errorf("%w: send again in %s", ErrInvalidTransition, d.status)
	}
	if err := d.move(StatusPending, now); err != nil {
		return err
	}
	d.round++
	return nil
}

// Blocks reports whether this delivery prevents a new delivery of assignment a: it belongs to a and is unresolved
// (PENDING, SENDING or UNKNOWN), so its message may still go out or may already have gone out. A new delivery of the
// same homework while one is UNKNOWN would bypass the reconciliation (hard rule 7).
func (d *Delivery) Blocks(a shared.AssignmentID) bool {
	return d.assignment == a && d.status.IsUnresolved()
}

// Sendable reports whether a job may hand the message to the provider now: only a PENDING delivery is sent.
func (d *Delivery) Sendable() bool { return d.status == StatusPending }

// InFlight reports whether an attempt was started and its result is not stored yet (SENDING).
func (d *Delivery) InFlight() bool { return d.status == StatusSending }

// OutcomeUnknown reports whether the delivery waits for reconciliation or the user's decision (UNKNOWN).
func (d *Delivery) OutcomeUnknown() bool { return d.status == StatusUnknown }

// ID returns the delivery identifier.
func (d *Delivery) ID() shared.DeliveryID { return d.id }

// Status returns the current status.
func (d *Delivery) Status() Status { return d.status }

// AssignmentID returns the assignment whose homework is delivered.
func (d *Delivery) AssignmentID() shared.AssignmentID { return d.assignment }

// Recipient returns the email address the message goes to.
func (d *Delivery) Recipient() string { return d.recipient }

// Subject returns the subject line.
func (d *Delivery) Subject() string { return d.subject }

// Attachment returns the file and the content the user confirmed.
func (d *Delivery) Attachment() Attachment { return d.attachment }

// MessageID returns the RFC 5322 Message-ID ("<left@right>") of every attempt; reconciliation looks for it.
func (d *Delivery) MessageID() string { return d.messageID }

// IdempotencyKey returns the key the delivery was created under.
func (d *Delivery) IdempotencyKey() string { return d.key }

// Attempts returns how many attempts were handed to the provider.
func (d *Delivery) Attempts() int { return d.attempts }

// Round returns the send round: 0 until the first SendAgain, then one more for each.
func (d *Delivery) Round() int { return d.round }

// LastError returns the reason of the last failed or unknown attempt; empty after success.
func (d *Delivery) LastError() string { return d.lastError }

// ConfirmedAt returns when the user confirmed the sending (AC-14).
func (d *Delivery) ConfirmedAt() time.Time { return d.confirmedAt }

// SentAt returns when the message was accepted or marked as sent; zero unless SENT.
func (d *Delivery) SentAt() time.Time { return d.sentAt }

// CreatedAt returns when the delivery was created.
func (d *Delivery) CreatedAt() time.Time { return d.createdAt }

// UpdatedAt returns the time of the last change.
func (d *Delivery) UpdatedAt() time.Time { return d.updatedAt }

// Version returns the version this instance was last loaded or saved with: 0 for an aggregate that was never
// stored, otherwise the stored version, which is at least 1 (optimistic concurrency, ADR-0004). The repository
// compares it with the stored one in Save.
func (d *Delivery) Version() int64 { return d.version }

// Persisted records the version the store assigned after a successful Save. It is called by the repository adapter
// only, after the write succeeded, so that saving the same instance again compares against its own earlier write
// (ADR-0004). It holds no persistence logic and is not a state change of the business entity: use cases and
// handlers never call it.
func (d *Delivery) Persisted(version int64) { d.version = version }
