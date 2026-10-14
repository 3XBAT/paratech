package senddelivery

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"paratech/desktop/internal/domain/delivery/delivery"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/jobs/services/backoff"
	"paratech/desktop/internal/domain/shared"
)

// Steps of a delivery attempt, used in error messages and the log.
const (
	stepPayload    = "senddelivery: payload"
	stepLoad       = "senddelivery: load delivery"
	stepAttachment = "senddelivery: open attachment"
	stepStart      = "senddelivery: save sending"
	stepSend       = "senddelivery: send"
	stepResult     = "senddelivery: save result"
)

// Reasons the reconciler gives the job runner; short fixed texts, stored as the job's last error.
const (
	reasonBadPayload = "invalid delivery job payload"
	reasonNoDelivery = "delivery not found"
	reasonFailed     = "delivery failed"
	reasonUnknown    = "delivery outcome unknown; mark it as sent or send it again"
	reasonSuperseded = "superseded by the job of a new send round"
	reasonExhausted  = "attempts exhausted before the message was handed over"
)

// Handle runs one attempt of a delivery job (jobrunner.Handler). It sends only a PENDING delivery: SENDING is saved
// first, the EmailAdapter is called outside any transaction, and the outcome is saved with a context that is not
// cancelled, so a job timeout or a shutdown during the send still records UNKNOWN. The returned error never lets the
// runner resend a message that may have been accepted: an unknown or unsaved outcome is permanent, and a later
// attempt of the same job finds the delivery no longer PENDING and does nothing (Registration.Reconciler contract).
// On the last attempt of the job (Config.MaxAttempts) a failure that proves nothing was handed over ends the
// delivery FAILED, so it never stays PENDING without a job to send it.
func (s *Service) Handle(ctx context.Context, snap job.Snapshot, _ func(ctx context.Context, p job.Progress) error) error {
	id, err := delivery.ParseJobPayload(snap.Payload())
	if err != nil {
		return backoff.Permanent(fmt.Errorf("%s: %w", stepPayload, err))
	}
	last := snap.Attempt() >= s.maxAttempts

	d, err := s.deliveries.Get(ctx, id)
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return backoff.Permanent(fmt.Errorf("%s: %w", stepLoad, err))
	case err != nil:
		return fmt.Errorf("%s: %w", stepLoad, err)
	case !d.Sendable():
		// Sent, failed, unknown, or in flight in another attempt: never sent from here.
		return nil
	}

	attachment, err := s.artifacts.Open(ctx, d.Attachment().Ref())
	if errors.Is(err, shared.ErrNotFound) {
		return s.failUnsent(ctx, d, ErrAttachmentMissing)
	}
	if err != nil {
		return s.notHandedOver(ctx, id, last, stepAttachment, err)
	}
	defer func() { _ = attachment.Close() }()

	if err := d.StartSending(s.clock.Now()); err != nil {
		return backoff.Permanent(fmt.Errorf("%s: %w", stepStart, err))
	}
	if err := s.tx.Do(ctx, func(ctx context.Context) error { return s.deliveries.Save(ctx, d) }); err != nil {
		// Nothing was handed over; a version conflict means someone else changed it, the next attempt re-reads.
		return s.notHandedOver(ctx, id, last, stepStart, err)
	}

	sendErr := s.email.Send(ctx, d, attachment)
	record := d.RecordResult
	if last {
		record = d.RecordFinalResult
	}
	outcome, err := record(sendErr, s.clock.Now())
	if err != nil {
		return backoff.Permanent(fmt.Errorf("%s: %w", stepResult, err))
	}

	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), resultSaveTimeout)
	defer cancel()
	if err := s.tx.Do(saveCtx, func(ctx context.Context) error { return s.deliveries.Save(ctx, d) }); err != nil {
		s.log.Error("delivery result not stored; it stays SENDING until startup recovery",
			slog.String("delivery", id.String()), slog.String("status", d.Status().String()), slog.Any("error", err))
		return backoff.Permanent(fmt.Errorf("%s: %w", stepResult, ErrResultNotStored))
	}

	switch outcome {
	case delivery.OutcomeSent:
		return nil
	case delivery.OutcomeRetry:
		return backoff.Transient(fmt.Errorf("%s: %w", stepSend, sendErr))
	default: // failed or unknown: retrying the job cannot help, and for unknown it must not happen
		return backoff.Permanent(fmt.Errorf("%s: %w", stepSend, sendErr))
	}
}

// failUnsent fails a PENDING delivery whose attachment cannot be sent; nothing was handed to the provider.
func (s *Service) failUnsent(ctx context.Context, d *delivery.Delivery, reason error) error {
	if err := d.FailUnsent(reason, s.clock.Now()); err != nil {
		return backoff.Permanent(fmt.Errorf("%s: %w", stepAttachment, err))
	}
	if err := s.tx.Do(ctx, func(ctx context.Context) error { return s.deliveries.Save(ctx, d) }); err != nil {
		return fmt.Errorf("%s: %w", stepAttachment, err)
	}
	return backoff.Permanent(fmt.Errorf("%s: %w", stepAttachment, reason))
}

// notHandedOver handles a failure of step before the message was handed over. The runner retries it; on the last
// attempt the delivery, reloaded because a failed save may leave the instance stale, ends FAILED. If that fails too,
// the job still ends and the delivery stays PENDING: nothing more can be done in this process.
func (s *Service) notHandedOver(ctx context.Context, id shared.DeliveryID, last bool, step string, cause error) error {
	err := fmt.Errorf("%s: %w", step, cause)
	if !last {
		return err
	}

	d, gerr := s.deliveries.Get(ctx, id)
	if gerr != nil {
		return errors.Join(err, gerr)
	}
	if !d.Sendable() {
		return backoff.Permanent(err)
	}
	if ferr := d.FailUnsent(fmt.Errorf("%w: %s", delivery.ErrAttemptsExhausted, step), s.clock.Now()); ferr != nil {
		return backoff.Permanent(errors.Join(err, ferr))
	}
	if serr := s.tx.Do(ctx, func(ctx context.Context) error { return s.deliveries.Save(ctx, d) }); serr != nil {
		return errors.Join(err, serr)
	}
	return backoff.Permanent(err)
}

// Reconcile decides the fate of a delivery job a previous process left RUNNING (jobrunner.Reconciler). It never sends.
// The delivery decides: PENDING requeues the job if it is the job of the current round with attempts left; SENT
// completes it; FAILED fails it; SENDING becomes UNKNOWN and, like UNKNOWN, is looked up with EmailAdapter.Reconcile:
// accepted completes the job (SENT), not accepted starts a new round (SendAgain) with its own job and fails this one,
// undecided fails the job and leaves the delivery UNKNOWN for the user. Errors leave the job RUNNING for the next
// Recover, and a repeat after a crash continues from the state it left.
func (s *Service) Reconcile(ctx context.Context, snap job.Snapshot) (job.Status, string, error) {
	id, err := delivery.ParseJobPayload(snap.Payload())
	if err != nil {
		return job.StatusFailed, reasonBadPayload, nil
	}
	d, err := s.deliveries.Get(ctx, id)
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return job.StatusFailed, reasonNoDelivery, nil
	case err != nil:
		return "", "", fmt.Errorf("%s: %w", opReconcile, err)
	}

	switch d.Status() {
	case delivery.StatusPending:
		return s.reconcilePending(ctx, snap, d)
	case delivery.StatusSent:
		return job.StatusCompleted, "", nil
	case delivery.StatusFailed:
		return job.StatusFailed, reasonFailed, nil
	case delivery.StatusSending:
		if err := d.Interrupted(s.clock.Now()); err != nil {
			return "", "", fmt.Errorf("%s: %w", opReconcile, err)
		}
		if err := s.tx.Do(ctx, func(ctx context.Context) error { return s.deliveries.Save(ctx, d) }); err != nil {
			return "", "", fmt.Errorf("%s: %w", opReconcile, err)
		}
	}

	accepted, err := s.email.Reconcile(ctx, d)
	if err != nil {
		return job.StatusFailed, reasonUnknown, nil
	}
	if accepted {
		err = d.MarkSent(s.clock.Now())
	} else {
		err = d.SendAgain(s.clock.Now())
	}
	if err == nil {
		err = s.tx.Do(ctx, func(ctx context.Context) error { return s.deliveries.Save(ctx, d) })
	}
	if err != nil {
		return "", "", fmt.Errorf("%s: %w", opReconcile, err)
	}
	if accepted {
		return job.StatusCompleted, "", nil
	}
	return s.reconcilePending(ctx, snap, d)
}

// reconcilePending settles a stale job of a PENDING delivery. The job of the current round (found by its key, which
// also enqueues it if a crash came first) is requeued or, without attempts left, the delivery fails unsent. A job of
// an earlier round is superseded and fails, so a delivery never has two live jobs.
func (s *Service) reconcilePending(ctx context.Context, snap job.Snapshot, d *delivery.Delivery) (job.Status, string, error) {
	current, err := s.ensureJob(ctx, d, opReconcile)
	if err != nil {
		return "", "", err
	}
	if current != snap.ID() {
		return job.StatusFailed, reasonSuperseded, nil
	}
	if snap.Attempt() < s.maxAttempts {
		return job.StatusQueued, "", nil
	}

	if err := d.FailUnsent(fmt.Errorf("%w: %s", delivery.ErrAttemptsExhausted, opReconcile), s.clock.Now()); err != nil {
		return "", "", fmt.Errorf("%s: %w", opReconcile, err)
	}
	if err := s.tx.Do(ctx, func(ctx context.Context) error { return s.deliveries.Save(ctx, d) }); err != nil {
		return "", "", fmt.Errorf("%s: %w", opReconcile, err)
	}
	return job.StatusFailed, reasonExhausted, nil
}
