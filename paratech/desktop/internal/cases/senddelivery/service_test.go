package senddelivery

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/delivery/delivery"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/jobs/services/backoff"
	"paratech/desktop/internal/domain/shared"
)

func TestNewChecksPortsAndConfig(t *testing.T) {
	e := newEnv(t)
	full := Deps{Deliveries: e.st, Artifacts: e.files, Email: e.email, Jobs: e.st, Credentials: e.creds, Clock: e.clock, IDs: e.st, Tx: e.st}
	tests := []struct {
		name string
		deps func() Deps
		cfg  Config
		ok   bool
	}{
		{"complete", func() Deps { return full }, Config{}, true},
		{"no email", func() Deps { d := full; d.Email = nil; return d }, Config{}, false},
		{"no credentials", func() Deps { d := full; d.Credentials = nil; return d }, Config{}, false},
		{"negative attempts", func() Deps { return full }, Config{MaxAttempts: -1}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.deps(), tt.cfg)
			if (err == nil) != tt.ok || (err != nil && !errors.Is(err, ErrInvalidConfig)) {
				t.Fatalf("New = %v, want ok %v", err, tt.ok)
			}
		})
	}
}

// AC-14: the only input is a command, and only a CONFIRMED proposal has one.
func TestSendHomeworkNeedsAConfirmedCommand(t *testing.T) {
	e := newEnv(t)
	if err := e.svc.SendHomework(context.Background(), proposal.EmailCommand{}); !errors.Is(err, ErrZeroCommand) {
		t.Fatalf("zero command = %v, want ErrZeroCommand", err)
	}
	if _, err := validated(t, "p1", "a1", homework).EmailCommand(); !errors.Is(err, proposal.ErrNotConfirmed) {
		t.Fatalf("command of an unconfirmed proposal = %v, want ErrNotConfirmed", err)
	}
	if len(e.st.deliveries) != 0 || len(e.st.jobs) != 0 || e.email.count() != 0 {
		t.Fatal("something was created or sent without confirmation")
	}
}

func TestSendHomeworkCreatesPendingDeliveryAndJob(t *testing.T) {
	e := newEnv(t)
	cmd := command(t, "p1", "a1", homework)
	if err := e.svc.SendHomework(context.Background(), cmd); err != nil {
		t.Fatal(err)
	}

	st := e.st.only()
	if st.Status != delivery.StatusPending || st.Key != "proposal:p1" || st.AssignmentID != "a1" ||
		st.Recipient != "teacher@school.example" || st.Subject != DefaultSubject || st.Attachment != cmd.Attachment() ||
		st.SHA256 != cmd.AttachmentDigest().SHA256() || st.Size != cmd.AttachmentDigest().Size() ||
		!st.ConfirmedAt.Equal(cmd.ConfirmedAt()) || st.MessageID == "" {
		t.Fatalf("delivery = %+v", st)
	}
	snap := e.st.snapshot(t, "send_email:"+string(st.ID)+":0")
	if snap.Type() != JobType || snap.Payload() != `{"delivery_id":"`+string(st.ID)+`"}` {
		t.Fatalf("job %s %s", snap.Type(), snap.Payload())
	}
	if e.email.count() != 0 {
		t.Fatal("SendHomework sent synchronously; sending is the job's work")
	}
}

// A repeat with the same key, in any status of the first delivery, creates nothing and sends nothing (hard rule 7).
func TestSendHomeworkIsIdempotent(t *testing.T) {
	for _, status := range []delivery.Status{delivery.StatusPending, delivery.StatusUnknown, delivery.StatusSent, delivery.StatusFailed} {
		t.Run(string(status), func(t *testing.T) {
			e := newEnv(t)
			id, snap := e.created(t)
			switch status {
			case delivery.StatusUnknown:
				e.email.result = func(context.Context, *delivery.Delivery) error { return delivery.ErrOutcomeUnknown }
				_ = e.svc.Handle(context.Background(), snap, noReport)
			case delivery.StatusSent:
				_ = e.svc.Handle(context.Background(), snap, noReport)
			case delivery.StatusFailed:
				e.email.result = func(context.Context, *delivery.Delivery) error { return delivery.ErrRejected }
				_ = e.svc.Handle(context.Background(), snap, noReport)
			}
			sends := e.email.count()

			if err := e.svc.SendHomework(context.Background(), command(t, "p1", "a1", homework)); err != nil {
				t.Fatalf("repeat: %v", err)
			}
			if e.st.only().ID != id || len(e.st.jobKeys()) != 1 || e.st.status(id) != status || e.email.count() != sends {
				t.Fatalf("repeat changed something: %d jobs %v, status %s", len(e.st.jobKeys()), e.st.jobKeys(), e.st.status(id))
			}
		})
	}
}

func TestSendHomeworkRefusesAChangedAttachment(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(t *testing.T, e *env)
		want    error
	}{
		{"changed", func(t *testing.T, e *env) { e.writeHomework(t, "a1", homework+"edited") }, delivery.ErrAttachmentChanged},
		{"missing", func(t *testing.T, e *env) {
			ref, _ := proposal.ExportRef("a1")
			_ = e.files.Delete(context.Background(), ref)
		}, ErrAttachmentMissing},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			cmd := command(t, "p1", "a1", homework)
			tt.prepare(t, e)
			err := e.svc.SendHomework(context.Background(), cmd)
			if !errors.Is(err, proposal.ErrStale) || !errors.Is(err, tt.want) {
				t.Fatalf("SendHomework = %v, want ErrStale and %v", err, tt.want)
			}
			if len(e.st.deliveries) != 0 || len(e.st.jobs) != 0 {
				t.Fatal("a delivery was created for a stale attachment")
			}
		})
	}
}

// After an unknown outcome, a new proposal for the same homework (for example with an edited file) is refused until
// the first delivery is resolved, so the reconciliation cannot be bypassed (hard rule 7).
func TestUnresolvedDeliveryBlocksANewOneOfTheSameAssignment(t *testing.T) {
	e := newEnv(t)
	id, snap := e.created(t)
	e.email.result = func(context.Context, *delivery.Delivery) error { return delivery.ErrOutcomeUnknown }
	_ = e.svc.Handle(context.Background(), snap, noReport)

	e.writeHomework(t, "a1", homework+"edited")
	err := e.svc.SendHomework(context.Background(), command(t, "p2", "a1", homework+"edited"))
	if !errors.Is(err, ErrUnresolvedDelivery) {
		t.Fatalf("new delivery while UNKNOWN = %v, want ErrUnresolvedDelivery", err)
	}

	e.writeHomework(t, "a2", homework)
	if err := e.svc.SendHomework(context.Background(), command(t, "p3", "a2", homework)); err != nil {
		t.Fatalf("other assignment: %v", err)
	}

	if _, err := e.svc.MarkSent(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.SendHomework(context.Background(), command(t, "p2", "a1", homework+"edited")); err != nil {
		t.Fatalf("after resolving: %v", err)
	}
}

func TestHandleOutcomes(t *testing.T) {
	tests := []struct {
		name      string
		result    error
		status    delivery.Status
		errClass  backoff.Class
		wantErr   bool
		resendsOn bool // whether a second run of the same job sends again
	}{
		{"accepted", nil, delivery.StatusSent, 0, false, false},
		{"transient", fmt.Errorf("smtp: connect failed: %w", delivery.ErrTransient), delivery.StatusPending, backoff.ClassTransient, true, true},
		{"rejected", fmt.Errorf("smtp: recipient refused: %w", delivery.ErrRejected), delivery.StatusFailed, backoff.ClassPermanent, true, false},
		{"attachment changed", fmt.Errorf("smtp: %w", delivery.ErrAttachmentChanged), delivery.StatusFailed, backoff.ClassPermanent, true, false},
		{"unknown", fmt.Errorf("smtp: no final reply: %w", delivery.ErrOutcomeUnknown), delivery.StatusUnknown, backoff.ClassPermanent, true, false},
		{"unclassified", errors.New("boom"), delivery.StatusUnknown, backoff.ClassPermanent, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			id, snap := e.created(t)
			e.email.result = func(context.Context, *delivery.Delivery) error { return tt.result }

			err := e.svc.Handle(context.Background(), snap, noReport)
			if (err != nil) != tt.wantErr || (err != nil && backoff.Classify(err) != tt.errClass) {
				t.Fatalf("Handle = %v (class %v), want error %v class %v", err, backoff.Classify(err), tt.wantErr, tt.errClass)
			}
			if got := e.st.status(id); got != tt.status {
				t.Fatalf("status %s, want %s", got, tt.status)
			}
			if e.email.count() != 1 || e.email.sent[0] != homework {
				t.Fatalf("sent %v", e.email.sent)
			}

			// The runner retries a transient error; everything else must not reach the provider again.
			_ = e.svc.Handle(context.Background(), snap, noReport)
			if want := map[bool]int{true: 2, false: 1}[tt.resendsOn]; e.email.count() != want {
				t.Fatalf("second run sent %d times in total, want %d", e.email.count(), want)
			}
		})
	}
}

// SENDING is stored before the adapter is called, so a crash during the call is recognisable (ADR-0004(c)).
func TestHandleSavesSendingBeforeTheSend(t *testing.T) {
	e := newEnv(t)
	id, snap := e.created(t)
	e.email.onSend = func(d *delivery.Delivery) {
		if got := e.st.status(id); got != delivery.StatusSending {
			t.Errorf("stored status during the send = %s, want SENDING", got)
		}
	}
	if err := e.svc.Handle(context.Background(), snap, noReport); err != nil {
		t.Fatal(err)
	}
	if st := e.st.only(); st.Attempts != 1 || st.SentAt.IsZero() {
		t.Fatalf("after send: %+v", st)
	}
}

// A job timeout or a shutdown cancels the context during the send; the outcome is still stored (UNKNOWN), and the job
// error is permanent, so the runner does not resend.
func TestHandleStoresTheOutcomeAfterTheContextEnded(t *testing.T) {
	e := newEnv(t)
	id, snap := e.created(t)
	ctx, cancel := context.WithCancel(context.Background())
	e.email.result = func(context.Context, *delivery.Delivery) error {
		cancel()
		return fmt.Errorf("smtp: interrupted after data: %w", delivery.ErrOutcomeUnknown)
	}
	err := e.svc.Handle(ctx, snap, noReport)
	if backoff.Classify(err) != backoff.ClassPermanent {
		t.Fatalf("Handle = %v, want permanent", err)
	}
	if got := e.st.status(id); got != delivery.StatusUnknown {
		t.Fatalf("status %s, want UNKNOWN", got)
	}
}

func TestHandleResultNotStoredIsPermanent(t *testing.T) {
	e := newEnv(t)
	id, snap := e.created(t)
	e.email.onSend = func(*delivery.Delivery) { e.st.saveErr = errors.New("disk full") }
	err := e.svc.Handle(context.Background(), snap, noReport)
	if !errors.Is(err, ErrResultNotStored) || backoff.Classify(err) != backoff.ClassPermanent {
		t.Fatalf("Handle = %v, want permanent ErrResultNotStored", err)
	}
	e.st.saveErr = nil
	if got := e.st.status(id); got != delivery.StatusSending {
		t.Fatalf("status %s, want SENDING (startup recovery makes it UNKNOWN)", got)
	}
	if err := e.svc.Handle(context.Background(), snap, noReport); err != nil || e.email.count() != 1 {
		t.Fatalf("second run = %v, sent %d", err, e.email.count())
	}
}

func TestHandleAttachmentProblems(t *testing.T) {
	t.Run("missing fails the delivery unsent", func(t *testing.T) {
		e := newEnv(t)
		id, snap := e.created(t)
		ref, _ := proposal.ExportRef("a1")
		_ = e.files.Delete(context.Background(), ref)
		err := e.svc.Handle(context.Background(), snap, noReport)
		if !errors.Is(err, ErrAttachmentMissing) || backoff.Classify(err) != backoff.ClassPermanent {
			t.Fatalf("Handle = %v", err)
		}
		if st := e.st.only(); st.Status != delivery.StatusFailed || st.Attempts != 0 || e.email.count() != 0 || st.ID != id {
			t.Fatalf("delivery %+v, sent %d", st, e.email.count())
		}
	})
	t.Run("bad payload", func(t *testing.T) {
		e := newEnv(t)
		j, _ := job.New(job.Spec{ID: "j9", Type: JobType, Key: "k", Payload: `{"x":1}`, MaxAttempts: 1}, t0)
		if err := e.svc.Handle(context.Background(), j.Snapshot(), noReport); backoff.Classify(err) != backoff.ClassPermanent {
			t.Fatalf("Handle = %v, want permanent", err)
		}
	})
	t.Run("unknown delivery", func(t *testing.T) {
		e := newEnv(t)
		j, _ := job.New(job.Spec{ID: "j9", Type: JobType, Key: "k", Payload: `{"delivery_id":"nope"}`, MaxAttempts: 1}, t0)
		if err := e.svc.Handle(context.Background(), j.Snapshot(), noReport); !errors.Is(err, shared.ErrNotFound) ||
			backoff.Classify(err) != backoff.ClassPermanent {
			t.Fatalf("Handle = %v, want permanent not found", err)
		}
	})
}

func TestReconcileInterruptedJob(t *testing.T) {
	tests := []struct {
		name      string
		prepare   func(t *testing.T, e *env, snap job.Snapshot)
		reconcile func(*delivery.Delivery) (bool, error)
		next      job.Status
		status    delivery.Status
	}{
		{"crash before sending", func(*testing.T, *env, job.Snapshot) {}, nil, job.StatusQueued, delivery.StatusPending},
		{"sent", func(t *testing.T, e *env, snap job.Snapshot) { _ = e.svc.Handle(context.Background(), snap, noReport) },
			nil, job.StatusCompleted, delivery.StatusSent},
		{"failed", func(t *testing.T, e *env, snap job.Snapshot) {
			e.email.result = func(context.Context, *delivery.Delivery) error { return delivery.ErrRejected }
			_ = e.svc.Handle(context.Background(), snap, noReport)
		}, nil, job.StatusFailed, delivery.StatusFailed},
		{"crash while sending, undecided", crashWhileSending, nil, job.StatusFailed, delivery.StatusUnknown},
		{"crash while sending, provider found it", crashWhileSending,
			func(*delivery.Delivery) (bool, error) { return true, nil }, job.StatusCompleted, delivery.StatusSent},
		{"crash while sending, provider says not accepted", crashWhileSending,
			func(*delivery.Delivery) (bool, error) { return false, nil }, job.StatusFailed, delivery.StatusPending},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			id, snap := e.created(t)
			tt.prepare(t, e, snap)
			sends := e.email.count()
			e.email.reconcile = tt.reconcile

			next, reason, err := e.svc.Reconcile(context.Background(), snap)
			if err != nil || next != tt.next {
				t.Fatalf("Reconcile = %s, %q, %v; want %s", next, reason, err, tt.next)
			}
			if next == job.StatusFailed && reason == "" {
				t.Fatal("a failed reconciliation needs a reason")
			}
			if got := e.st.status(id); got != tt.status {
				t.Fatalf("delivery %s, want %s", got, tt.status)
			}
			if e.email.count() != sends {
				t.Fatal("Reconcile sent a message")
			}
			// Idempotent: asking again gives the same answer.
			if again, _, err := e.svc.Reconcile(context.Background(), snap); err != nil || (tt.next != job.StatusQueued && again != next) {
				t.Fatalf("second Reconcile = %s, %v", again, err)
			}
		})
	}
}

// crashWhileSending leaves the delivery SENDING, as a process killed during the send does.
func crashWhileSending(t *testing.T, e *env, _ job.Snapshot) {
	t.Helper()
	st := e.st.only()
	d, err := delivery.Restore(st)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.StartSending(t0.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := e.st.Do(context.Background(), func(ctx context.Context) error { return e.st.Save(ctx, d) }); err != nil {
		t.Fatal(err)
	}
}

func TestRecoverTurnsSendingIntoUnknown(t *testing.T) {
	e := newEnv(t)
	id, snap := e.created(t)
	crashWhileSending(t, e, snap)

	n, err := e.svc.Recover(context.Background())
	if err != nil || n != 1 || e.st.status(id) != delivery.StatusUnknown {
		t.Fatalf("Recover = %d, %v; status %s", n, err, e.st.status(id))
	}
	if n, err := e.svc.Recover(context.Background()); err != nil || n != 0 {
		t.Fatalf("second Recover = %d, %v", n, err)
	}
	if err := e.svc.Handle(context.Background(), snap, noReport); err != nil || e.email.count() != 0 {
		t.Fatalf("job after recovery = %v, sent %d", err, e.email.count())
	}
}

func TestUserResolution(t *testing.T) {
	unknown := func(t *testing.T) (*env, shared.DeliveryID) {
		e := newEnv(t)
		id, snap := e.created(t)
		e.email.result = func(context.Context, *delivery.Delivery) error { return delivery.ErrOutcomeUnknown }
		_ = e.svc.Handle(context.Background(), snap, noReport)
		e.email.result = nil
		return e, id
	}

	t.Run("mark as sent", func(t *testing.T) {
		e, id := unknown(t)
		v, err := e.svc.MarkSent(context.Background(), id)
		if err != nil || v.Status != delivery.StatusSent || v.SentAt.IsZero() {
			t.Fatalf("MarkSent = %+v, %v", v, err)
		}
		if _, err := e.svc.MarkSent(context.Background(), id); !errors.Is(err, delivery.ErrInvalidTransition) {
			t.Fatalf("MarkSent twice = %v", err)
		}
	})
	t.Run("send again", func(t *testing.T) {
		e, id := unknown(t)
		v, err := e.svc.SendAgain(context.Background(), id)
		if err != nil || v.Status != delivery.StatusPending {
			t.Fatalf("SendAgain = %+v, %v", v, err)
		}
		snap := e.st.snapshot(t, "send_email:"+string(id)+":1")
		if err := e.svc.Handle(context.Background(), snap, noReport); err != nil || e.st.status(id) != delivery.StatusSent || e.email.count() != 2 {
			t.Fatalf("new round = %v, status %s, sent %d", err, e.st.status(id), e.email.count())
		}
	})
	t.Run("send again only from unknown", func(t *testing.T) {
		e := newEnv(t)
		id, _ := e.created(t)
		if _, err := e.svc.SendAgain(context.Background(), id); !errors.Is(err, delivery.ErrInvalidTransition) {
			t.Fatalf("SendAgain of PENDING = %v", err)
		}
	})
	t.Run("reconcile undecided stays unknown", func(t *testing.T) {
		e, id := unknown(t)
		v, err := e.svc.ReconcileDelivery(context.Background(), id)
		if err != nil || v.Status != delivery.StatusUnknown {
			t.Fatalf("ReconcileDelivery = %+v, %v", v, err)
		}
	})
	t.Run("reconcile found it", func(t *testing.T) {
		e, id := unknown(t)
		e.email.reconcile = func(*delivery.Delivery) (bool, error) { return true, nil }
		if v, err := e.svc.ReconcileDelivery(context.Background(), id); err != nil || v.Status != delivery.StatusSent {
			t.Fatalf("ReconcileDelivery = %+v, %v", v, err)
		}
	})
	t.Run("list unknown", func(t *testing.T) {
		e, id := unknown(t)
		vs, err := e.svc.List(context.Background(), delivery.StatusUnknown)
		if err != nil || len(vs) != 1 || vs[0].ID != id || vs[0].LastError == "" {
			t.Fatalf("List = %+v, %v", vs, err)
		}
	})
}

func TestResumeEnqueuesPendingDeliveries(t *testing.T) {
	e := newEnv(t)
	_, _ = e.created(t)
	e.st.jobs = map[string]*job.Job{} // the job was lost (for example it used up its attempts and was purged)
	if err := e.svc.Resume(context.Background()); err != nil || len(e.st.jobKeys()) != 1 {
		t.Fatalf("Resume = %v, jobs %v", err, e.st.jobKeys())
	}
	if err := e.svc.Resume(context.Background()); err != nil || len(e.st.jobKeys()) != 1 {
		t.Fatalf("second Resume = %v, jobs %v", err, e.st.jobKeys())
	}
}

func TestMailPassword(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if ok, err := e.svc.MailPasswordConfigured(ctx); err != nil || ok {
		t.Fatalf("configured before Set = %v, %v", ok, err)
	}
	if err := e.svc.SetMailPassword(ctx, "fake-password"); err != nil {
		t.Fatal(err)
	}
	if got, err := e.creds.Get(ctx, DefaultCredentialName); err != nil || got != "fake-password" {
		t.Fatalf("stored under %s: %q, %v", DefaultCredentialName, got, err)
	}
	if ok, _ := e.svc.MailPasswordConfigured(ctx); !ok {
		t.Fatal("not configured after Set")
	}
	if err := e.svc.RemoveMailPassword(ctx); err != nil {
		t.Fatal(err)
	}
	if ok, _ := e.svc.MailPasswordConfigured(ctx); ok {
		t.Fatal("configured after Remove")
	}
	if err := e.svc.SetMailPassword(ctx, ""); !errors.Is(err, shared.ErrInvalidSecret) {
		t.Fatalf("empty password = %v", err)
	}
}

var errTransient = fmt.Errorf("smtp: connect failed: %w", delivery.ErrTransient)

// After a transient failure the delivery is PENDING while its job waits for the retry. A repeated request and the
// startup Resume find that job by the key of the round and add none, so the retries stay bounded (PRD section 7.3).
func TestTransientFailureKeepsOneJob(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	id, _ := e.created(t)
	e.email.result = func(context.Context, *delivery.Delivery) error { return errTransient }

	if err := e.svc.Handle(ctx, e.st.claim(t, jobKey(id, 0)), noReport); backoff.Classify(err) != backoff.ClassTransient {
		t.Fatalf("Handle = %v, want transient", err)
	}
	if st := e.st.only(); st.Status != delivery.StatusPending || st.Attempts != 1 {
		t.Fatalf("delivery %s after %d attempts, want PENDING after 1", st.Status, st.Attempts)
	}

	for range 3 {
		if err := e.svc.SendHomework(ctx, command(t, "p1", "a1", homework)); err != nil {
			t.Fatalf("repeated SendHomework: %v", err)
		}
		if err := e.svc.Resume(ctx); err != nil {
			t.Fatalf("Resume: %v", err)
		}
	}
	if keys := e.st.jobKeys(); !slices.Equal(keys, []string{jobKey(id, 0)}) {
		t.Fatalf("jobs %v, want only the job of round 0", keys)
	}
}

// On the last attempt of its job a delivery that was not handed over ends FAILED instead of staying PENDING without a
// job, and then no longer blocks a new delivery of the same assignment.
func TestLastAttemptEndsTheDelivery(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(e *env) // before every attempt
		sends   int
		lastErr string
	}{
		{"transient on every attempt", func(e *env) {
			e.email.result = func(context.Context, *delivery.Delivery) error { return errTransient }
		}, 2, "attempts exhausted: smtp: connect failed"},
		{"attachment unreadable", func(e *env) { e.files.failOpen(errDisk) }, 0, "attempts exhausted: senddelivery: open attachment"},
		{"sending not stored", func(e *env) { e.st.saveFails = 1 }, 0, "attempts exhausted: senddelivery: save sending"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnvWith(t, Config{MaxAttempts: 2})
			ctx := context.Background()
			id, _ := e.created(t)

			tt.prepare(e)
			if err := e.svc.Handle(ctx, e.st.claim(t, jobKey(id, 0)), noReport); err == nil ||
				backoff.Classify(err) == backoff.ClassPermanent {
				t.Fatalf("first attempt = %v, want a retryable error", err)
			}
			if got := e.st.status(id); got != delivery.StatusPending {
				t.Fatalf("after the first attempt %s, want PENDING", got)
			}

			tt.prepare(e)
			if err := e.svc.Handle(ctx, e.st.claim(t, jobKey(id, 0)), noReport); backoff.Classify(err) != backoff.ClassPermanent {
				t.Fatalf("last attempt = %v, want permanent", err)
			}
			st := e.st.only()
			if st.Status != delivery.StatusFailed || !strings.HasPrefix(st.LastError, tt.lastErr) || e.email.count() != tt.sends {
				t.Fatalf("delivery %s, last error %q, sent %d; want FAILED, %q, %d", st.Status, st.LastError, e.email.count(),
					tt.lastErr, tt.sends)
			}

			e.files.failOpen(nil)
			e.writeHomework(t, "a1", homework+"v2")
			if err := e.svc.SendHomework(ctx, command(t, "p2", "a1", homework+"v2")); err != nil {
				t.Fatalf("new delivery of the assignment: %v", err)
			}
		})
	}
}

// A job timeout on the last attempt before the hand-over still ends the delivery FAILED, not PENDING without a job.
func TestLastAttemptEndsTheDeliveryAfterTheContextEnded(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(e *env, cancel context.CancelFunc)
		lastErr string
	}{
		{"timeout opening the attachment", func(e *env, cancel context.CancelFunc) {
			cancel()
			e.files.failOpen(context.DeadlineExceeded)
		}, "attempts exhausted: senddelivery: open attachment"},
		{"timeout saving sending", func(e *env, cancel context.CancelFunc) {
			// The attachment opens, then the deadline ends before SENDING is saved.
			e.files.afterOpen = cancel
		}, "attempts exhausted: senddelivery: save sending"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnvWith(t, Config{MaxAttempts: 1})
			id, _ := e.created(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			tt.prepare(e, cancel)
			if err := e.svc.Handle(ctx, e.st.claim(t, jobKey(id, 0)), noReport); backoff.Classify(err) != backoff.ClassPermanent {
				t.Fatalf("last attempt = %v, want permanent", err)
			}
			st := e.st.only()
			if st.Status != delivery.StatusFailed || !strings.HasPrefix(st.LastError, tt.lastErr) || e.email.count() != 0 {
				t.Fatalf("delivery %s, last error %q, sent %d; want FAILED, %q, 0", st.Status, st.LastError,
					e.email.count(), tt.lastErr)
			}
		})
	}
}

// The default attempts of a delivery job must fit the job runner's cap, or the last attempt the use case counts on
// never runs and the delivery stays PENDING.
func TestDefaultMaxAttemptsFitsTheRunner(t *testing.T) {
	if runner := backoff.DefaultSpec().MaxAttempts; DefaultMaxAttempts > runner {
		t.Fatalf("DefaultMaxAttempts = %d, above the runner's default %d", DefaultMaxAttempts, runner)
	}
}

// The job reconciler: "not accepted" starts a new round with its own job and fails the stale one, so one delivery
// never has two live jobs; asking again (a crash in between) and Resume add nothing; the new job sends it.
func TestReconcileNotAcceptedStartsANewRound(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	id, snap := e.created(t)
	crashWhileSending(t, e, snap)
	e.email.reconcile = func(*delivery.Delivery) (bool, error) { return false, nil }

	for range 2 {
		next, reason, err := e.svc.Reconcile(ctx, snap)
		if err != nil || next != job.StatusFailed || reason != reasonSuperseded {
			t.Fatalf("Reconcile = %s, %q, %v; want FAILED superseded", next, reason, err)
		}
	}
	if err := e.svc.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	keys := e.st.jobKeys()
	slices.Sort(keys)
	if !slices.Equal(keys, []string{jobKey(id, 0), jobKey(id, 1)}) || e.email.count() != 0 {
		t.Fatalf("jobs %v, sent %d", keys, e.email.count())
	}

	if err := e.svc.Handle(ctx, e.st.claim(t, jobKey(id, 1)), noReport); err != nil || e.st.status(id) != delivery.StatusSent {
		t.Fatalf("new round = %v, status %s", err, e.st.status(id))
	}
}

// A stale job of a PENDING delivery without attempts left fails the delivery instead of leaving it PENDING forever.
func TestReconcileLastAttemptFailsThePendingDelivery(t *testing.T) {
	e := newEnvWith(t, Config{MaxAttempts: 1})
	id, _ := e.created(t)
	snap := e.st.claim(t, jobKey(id, 0)) // the crash came before SENDING was stored

	next, reason, err := e.svc.Reconcile(context.Background(), snap)
	if err != nil || next != job.StatusFailed || reason != reasonExhausted {
		t.Fatalf("Reconcile = %s, %q, %v; want FAILED exhausted", next, reason, err)
	}
	st := e.st.only()
	if st.Status != delivery.StatusFailed ||
		!strings.HasPrefix(st.LastError, delivery.ErrAttemptsExhausted.Error()) || e.email.count() != 0 {
		t.Fatalf("delivery %s, last error %q, sent %d", st.Status, st.LastError, e.email.count())
	}
}
