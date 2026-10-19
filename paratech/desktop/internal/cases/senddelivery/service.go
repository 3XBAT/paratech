package senddelivery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/delivery/delivery"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
)

// Operation names, used in error messages.
const (
	opSendHomework = "senddelivery: send homework"
	opGet          = "senddelivery: get"
	opList         = "senddelivery: list"
	opListOf       = "senddelivery: list deliveries of an assignment"
	opUnresolved   = "senddelivery: find unresolved deliveries"
	opMarkSent     = "senddelivery: mark as sent"
	opSendAgain    = "senddelivery: send again"
	opReconcile    = "senddelivery: reconcile"
	opRecover      = "senddelivery: recover"
	opResume       = "senddelivery: resume"
	opCredentials  = "senddelivery: credentials"
)

// Service emails a finished homework after the user's explicit confirmation and keeps the delivery result (PRD
// section 10, AC-14, AC-15). It is three things for the app:
//
//   - the homeworkflow.HomeworkMailer (SendHomework satisfies it structurally): the only way to create a delivery,
//     and it takes only a proposal.EmailCommand, which exists only for a CONFIRMED proposal (AC-14, AC-18);
//   - the handler and the reconciler of the delivery job (Handle, Reconcile; jobrunner.Handler and Reconciler);
//   - the operations the UI offers for a delivery: list, mark as sent, send again, reconcile, and the SMTP password.
//
// Sending is a job: SendHomework stores a PENDING delivery and enqueues its job; the job saves SENDING, calls the
// EmailAdapter outside any transaction and saves the outcome (job.go). An ambiguous outcome is UNKNOWN and is never
// sent again until it is resolved (hard rule 7).
type Service struct {
	deliveries  DeliveryRepository
	history     DeliveryHistory
	artifacts   ArtifactStore
	email       EmailAdapter
	jobs        JobEnqueuer
	credentials CredentialStore
	clock       Clock
	ids         IDGenerator
	tx          Tx
	log         *slog.Logger

	subject        string
	credentialName string
	maxAttempts    int
}

// New checks the ports and the settings.
func New(d Deps, cfg Config) (*Service, error) {
	if d.Deliveries == nil || d.History == nil || d.Artifacts == nil || d.Email == nil || d.Jobs == nil ||
		d.Credentials == nil || d.Clock == nil || d.IDs == nil || d.Tx == nil {
		return nil, fmt.Errorf("%w: every port is required", ErrInvalidConfig)
	}
	s := &Service{
		deliveries: d.Deliveries, history: d.History, artifacts: d.Artifacts, email: d.Email, jobs: d.Jobs,
		credentials: d.Credentials, clock: d.Clock, ids: d.IDs, tx: d.Tx, log: d.Log,
		subject: cfg.Subject, credentialName: cfg.CredentialName, maxAttempts: cfg.MaxAttempts,
	}
	if s.subject == "" {
		s.subject = DefaultSubject
	}
	if s.credentialName == "" {
		s.credentialName = DefaultCredentialName
	}
	if s.maxAttempts == 0 {
		s.maxAttempts = DefaultMaxAttempts
	}
	if s.maxAttempts < 1 {
		return nil, fmt.Errorf("%w: max attempts %d", ErrInvalidConfig, cfg.MaxAttempts)
	}
	if s.log == nil {
		s.log = slog.New(slog.DiscardHandler)
	}
	return s, nil
}

// SendHomework creates the delivery of a confirmed send_homework_email proposal and enqueues its job (PRD section 10
// steps 7-8). It is idempotent by the command's key: a repeat finds the delivery made the first time, whatever its
// status, and creates nothing (hard rule 7). The attachment must still be the confirmed content; otherwise nothing is
// created and the error wraps proposal.ErrStale (the job checks the bytes it sends once more). Another unresolved
// delivery of the same assignment yields ErrUnresolvedDelivery.
func (s *Service) SendHomework(ctx context.Context, cmd proposal.EmailCommand) error {
	if cmd.IsZero() {
		return fmt.Errorf("%s: %w", opSendHomework, ErrZeroCommand)
	}

	existing, err := s.deliveries.GetByIdempotencyKey(ctx, cmd.IdempotencyKey())
	switch {
	case err == nil:
		_, err := s.ensureJob(ctx, existing, opSendHomework)
		return err
	case !errors.Is(err, shared.ErrNotFound):
		return fmt.Errorf("%s: %w", opSendHomework, err)
	}

	d, err := s.newDelivery(ctx, cmd)
	if err != nil {
		return fmt.Errorf("%s: %w", opSendHomework, err)
	}

	err = s.tx.Do(ctx, func(ctx context.Context) error {
		unresolved, err := s.deliveries.ListByStatus(ctx, unresolvedStatuses()...)
		if err != nil {
			return err
		}
		for _, other := range unresolved {
			if other.Blocks(d.AssignmentID()) {
				return fmt.Errorf("%w: %s is %s", ErrUnresolvedDelivery, other.ID(), other.Status())
			}
		}
		return s.deliveries.Save(ctx, d)
	})
	if errors.Is(err, shared.ErrDuplicate) {
		// A concurrent call with the same key won; continue with its delivery.
		existing, gerr := s.deliveries.GetByIdempotencyKey(ctx, cmd.IdempotencyKey())
		if gerr != nil {
			return fmt.Errorf("%s: %w", opSendHomework, errors.Join(err, gerr))
		}
		_, err := s.ensureJob(ctx, existing, opSendHomework)
		return err
	}
	if err != nil {
		return fmt.Errorf("%s: %w", opSendHomework, err)
	}
	_, err = s.ensureJob(ctx, d, opSendHomework)
	return err
}

// newDelivery checks the attachment against the confirmed digest and builds the PENDING delivery.
func (s *Service) newDelivery(ctx context.Context, cmd proposal.EmailCommand) (*delivery.Delivery, error) {
	confirmed := cmd.AttachmentDigest()
	att, err := delivery.NewAttachment(cmd.Attachment(), confirmed.SHA256(), confirmed.Size())
	if err != nil {
		return nil, err
	}
	sum, size, err := s.digest(ctx, cmd.Attachment())
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return nil, fmt.Errorf("%w: %w", proposal.ErrStale, ErrAttachmentMissing)
	case err != nil:
		return nil, err
	case !att.Matches(sum, size):
		return nil, fmt.Errorf("%w: %w", proposal.ErrStale, delivery.ErrAttachmentChanged)
	}

	// The core builds the subject per assignment (TASK-092); the configured one is the fallback.
	subject := cmd.Subject()
	if subject == "" {
		subject = s.subject
	}
	return delivery.New(delivery.Spec{
		ID: s.ids.NewDeliveryID(), AssignmentID: cmd.AssignmentID(), Recipient: cmd.Recipient().Address(),
		Subject: subject, Attachment: att, MessageID: s.ids.NewMessageID(), Key: cmd.IdempotencyKey(),
		ConfirmedAt: cmd.ConfirmedAt(),
	}, s.clock.Now())
}

// digest returns the SHA-256 and the size of an artifact.
func (s *Service) digest(ctx context.Context, ref shared.ArtifactRef) (string, int64, error) {
	rc, err := s.artifacts.Open(ctx, ref)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = rc.Close() }()

	h := sha256.New()
	n, err := io.Copy(h, rc)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// unresolvedStatuses are the statuses in which a delivery blocks another one of its assignment.
func unresolvedStatuses() []delivery.Status {
	var out []delivery.Status
	for _, st := range delivery.Statuses() {
		if st.IsUnresolved() {
			out = append(out, st)
		}
	}
	return out
}

// HasUnresolved reports whether assignment a has a delivery that blocks a new one (PENDING, SENDING or UNKNOWN). It
// satisfies homeworkflow.UnresolvedDeliveries structurally: the export file of a is the attachment of such a delivery,
// so the drafts must not overwrite it (hard rule 7).
func (s *Service) HasUnresolved(ctx context.Context, a shared.AssignmentID) (bool, error) {
	unresolved, err := s.deliveries.ListByStatus(ctx, unresolvedStatuses()...)
	if err != nil {
		return false, fmt.Errorf("%s %s: %w", opUnresolved, a, err)
	}
	for _, d := range unresolved {
		if d.Blocks(a) {
			return true, nil
		}
	}
	return false, nil
}

// ensureJob enqueues the job of the current send round of a PENDING delivery and returns its id: the key is per round,
// so a repeat or a restart creates nothing and returns the job that exists, in whatever state. Deliveries in other
// statuses need no job (empty id).
func (s *Service) ensureJob(ctx context.Context, d *delivery.Delivery, op string) (shared.JobID, error) {
	if !d.Sendable() {
		return "", nil
	}
	j, err := job.New(job.Spec{
		ID: s.ids.NewJobID(), Type: JobType, Key: d.JobKey(), Payload: d.JobPayload(), MaxAttempts: s.maxAttempts,
	}, s.clock.Now())
	if err != nil {
		return "", fmt.Errorf("%s: %w", op, err)
	}
	id, _, err := s.jobs.Enqueue(ctx, j)
	if err != nil {
		return "", fmt.Errorf("%s: enqueue: %w", op, err)
	}
	return id, nil
}

// Get returns a delivery; a missing one wraps shared.ErrNotFound.
func (s *Service) Get(ctx context.Context, id shared.DeliveryID) (View, error) {
	d, err := s.deliveries.Get(ctx, id)
	if err != nil {
		return View{}, fmt.Errorf("%s: %w", opGet, err)
	}
	return viewOf(d), nil
}

// List returns the deliveries in the given statuses, oldest first; for example the UNKNOWN ones the user has to
// resolve (AC-15).
func (s *Service) List(ctx context.Context, statuses ...delivery.Status) ([]View, error) {
	ds, err := s.deliveries.ListByStatus(ctx, statuses...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", opList, err)
	}
	out := make([]View, 0, len(ds))
	for _, d := range ds {
		out = append(out, viewOf(d))
	}
	return out, nil
}

// ListForAssignment returns the deliveries of assignment a, newest first, at most MaxListedDeliveries (PRD section 10:
// the stored delivery result, AC-15). None is an empty list.
func (s *Service) ListForAssignment(ctx context.Context, a shared.AssignmentID) ([]View, error) {
	ds, err := s.history.ListByAssignment(ctx, a, MaxListedDeliveries)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", opListOf, a, err)
	}
	out := make([]View, 0, len(ds))
	for _, d := range ds {
		out = append(out, viewOf(d))
	}
	return out, nil
}

// Subject returns the default subject of a homework email, used when the command carries none: the core sets it,
// never the model.
func (s *Service) Subject() string { return s.subject }

// MarkSent records the user's decision that an UNKNOWN delivery was sent, after checking the mailbox
// (UNKNOWN -> SENT). Any other status yields delivery.ErrInvalidTransition.
func (s *Service) MarkSent(ctx context.Context, id shared.DeliveryID) (View, error) {
	v, err := s.change(ctx, id, func(d *delivery.Delivery) error { return d.MarkSent(s.clock.Now()) })
	if err != nil {
		return View{}, fmt.Errorf("%s: %w", opMarkSent, err)
	}
	return v, nil
}

// SendAgain records the user's decision to send an UNKNOWN delivery again, accepting a possible duplicate
// (UNKNOWN -> PENDING), which starts a new send round, and enqueues the job of that round. Any other status yields
// delivery.ErrInvalidTransition.
func (s *Service) SendAgain(ctx context.Context, id shared.DeliveryID) (View, error) {
	return s.resend(ctx, id, opSendAgain)
}

func (s *Service) resend(ctx context.Context, id shared.DeliveryID, op string) (View, error) {
	var d *delivery.Delivery
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		var err error
		if d, err = s.deliveries.Get(ctx, id); err != nil {
			return err
		}
		if err := d.SendAgain(s.clock.Now()); err != nil {
			return err
		}
		return s.deliveries.Save(ctx, d)
	})
	if err != nil {
		return View{}, fmt.Errorf("%s: %w", op, err)
	}
	if _, err := s.ensureJob(ctx, d, op); err != nil {
		return View{}, err
	}
	return viewOf(d), nil
}

// change loads a delivery, applies one aggregate method and saves it in one transaction.
func (s *Service) change(ctx context.Context, id shared.DeliveryID, apply func(*delivery.Delivery) error) (View, error) {
	var v View
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		d, err := s.deliveries.Get(ctx, id)
		if err != nil {
			return err
		}
		if err := apply(d); err != nil {
			return err
		}
		if err := s.deliveries.Save(ctx, d); err != nil {
			return err
		}
		v = viewOf(d)
		return nil
	})
	return v, err
}

// ReconcileDelivery asks the provider whether an UNKNOWN delivery was accepted (EmailAdapter.Reconcile). Accepted:
// SENT. Known not accepted: PENDING with a new job. Undecided (always so for plain SMTP): it stays UNKNOWN and the
// returned view says so; the user then marks it as sent or sends it again. A delivery that is not UNKNOWN is returned
// unchanged.
func (s *Service) ReconcileDelivery(ctx context.Context, id shared.DeliveryID) (View, error) {
	d, err := s.deliveries.Get(ctx, id)
	if err != nil {
		return View{}, fmt.Errorf("%s: %w", opReconcile, err)
	}
	if !d.OutcomeUnknown() {
		return viewOf(d), nil
	}

	accepted, err := s.email.Reconcile(ctx, d)
	if err != nil {
		s.log.Info("delivery outcome stays unknown", slog.String("delivery", id.String()), slog.Any("error", err))
		return viewOf(d), nil
	}
	if !accepted {
		return s.resend(ctx, id, opReconcile)
	}
	v, err := s.change(ctx, id, func(d *delivery.Delivery) error { return d.MarkSent(s.clock.Now()) })
	if err != nil {
		return View{}, fmt.Errorf("%s: %w", opReconcile, err)
	}
	return v, nil
}

// errNotInFlight skips a delivery that left SENDING between the listing and the change.
var errNotInFlight = errors.New("delivery is no longer in flight")

// Recover turns the deliveries a previous process left SENDING into UNKNOWN (PRD section 14 case 7). At startup the
// app calls jobrunner.Recover (it reconciles the RUNNING delivery jobs through Reconcile), then this Recover, then
// Resume, and only then starts the runner: a live attempt is SENDING too. Only this Recover finds a SENDING delivery
// whose job already ended, for example when the result of an attempt could not be stored (ErrResultNotStored). It
// returns how many it changed; one delivery that fails does not stop the others, and calling it again changes nothing.
func (s *Service) Recover(ctx context.Context) (int, error) {
	stale, err := s.deliveries.ListByStatus(ctx, delivery.StatusSending)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", opRecover, err)
	}
	var errs []error
	changed := 0
	for _, d := range stale {
		_, err := s.change(ctx, d.ID(), func(d *delivery.Delivery) error {
			if !d.InFlight() {
				return errNotInFlight
			}
			return d.Interrupted(s.clock.Now())
		})
		if errors.Is(err, errNotInFlight) {
			continue
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s %s: %w", opRecover, d.ID(), err))
			continue
		}
		changed++
	}
	return changed, errors.Join(errs...)
}

// Resume enqueues the job of the current send round of every PENDING delivery that has none, for example when the
// app stopped between storing the delivery and enqueueing its job. A delivery whose round has a job, in any state,
// gets nothing: only SendAgain starts a new round, so calling Resume at every start never adds jobs or attempts. A job
// that ends on its last attempt fails its delivery (Handle), so no PENDING delivery is left without a live job.
func (s *Service) Resume(ctx context.Context) error {
	pending, err := s.deliveries.ListByStatus(ctx, delivery.StatusPending)
	if err != nil {
		return fmt.Errorf("%s: %w", opResume, err)
	}
	var errs []error
	for _, d := range pending {
		_, err := s.ensureJob(ctx, d, opResume)
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// SetMailPassword stores or replaces the SMTP password in the credential store, never in SQLite (PRD section 12,
// AC-19). The smtp adapter reads it from there; it is never read back into the core.
func (s *Service) SetMailPassword(ctx context.Context, password string) error {
	if err := s.credentials.Set(ctx, s.credentialName, password); err != nil {
		return fmt.Errorf("%s: %w", opCredentials, err)
	}
	return nil
}

// MailPasswordConfigured reports whether an SMTP password is stored, without revealing it.
func (s *Service) MailPasswordConfigured(ctx context.Context) (bool, error) {
	ok, err := s.credentials.Exists(ctx, s.credentialName)
	if err != nil {
		return false, fmt.Errorf("%s: %w", opCredentials, err)
	}
	return ok, nil
}

// RemoveMailPassword deletes the stored SMTP password (revocation); a missing one is not an error.
func (s *Service) RemoveMailPassword(ctx context.Context) error {
	if err := s.credentials.Delete(ctx, s.credentialName); err != nil {
		return fmt.Errorf("%s: %w", opCredentials, err)
	}
	return nil
}
