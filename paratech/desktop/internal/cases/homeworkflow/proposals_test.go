package homeworkflow

import (
	"context"
	"errors"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/shared"
)

const (
	outEmail    = `{"kind":"send_homework_email","assignment_id":"a1"}`
	outExport   = `{"kind":"export_homework","assignment_id":"a1","body":"1. Solve 3x = 9."}`
	outDeadline = `{"kind":"create_deadline_event","assignment_id":"a1","title":"Algebra homework","due":"2026-10-20T18:00:00Z"}`
)

func propose(t *testing.T, e *env, out string) ProposalView {
	t.Helper()
	v, err := e.svc.Propose(context.Background(), values.NewUntrustedOutput(out))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestNewRequiresEveryPort(t *testing.T) {
	if _, err := New(Deps{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("New(Deps{}) = %v, want ErrInvalidConfig", err)
	}
}

func TestProposeValidatesAgainstLocalState(t *testing.T) {
	cases := []struct {
		name   string
		out    string
		setup  func(e *env)
		status proposal.Status
		reason proposal.RejectReason
	}{
		{"email ready", outEmail, nil, proposal.StatusValidated, ""},
		{"export ready", outExport, nil, proposal.StatusValidated, ""},
		{"deadline ready", outDeadline, nil, proposal.StatusValidated, ""},
		{"unknown assignment", outEmail, func(e *env) { delete(e.assignments, "a1") }, proposal.StatusRejected, proposal.ReasonUnknownAssignment},
		{"no recipient configured", outEmail, func(e *env) { e.recipient = "" }, proposal.StatusRejected, proposal.ReasonNoRecipient},
		{"nothing exported yet", outEmail, func(e *env) {
			ref, _ := proposal.ExportRef("a1")
			_ = e.files.Delete(context.Background(), ref)
		}, proposal.StatusRejected, proposal.ReasonNoArtifact},
		{"deadline in the past", outDeadline, func(e *env) { e.clock.Advance(30 * 24 * time.Hour) }, proposal.StatusRejected, proposal.ReasonDueOutOfRange},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t).withHomework(t)
			if tc.setup != nil {
				tc.setup(e)
			}
			v := propose(t, e, tc.out)
			if v.Status != tc.status || v.Reason != tc.reason {
				t.Fatalf("view %+v, want %s %q", v, tc.status, tc.reason)
			}
			stored, err := e.svc.Get(context.Background(), v.ID)
			if err != nil || stored != v {
				t.Fatalf("stored %+v, %v; want %+v", stored, err, v)
			}
			if n := e.sideEffects(); n != 0 {
				t.Fatalf("Propose caused %d side effects", n)
			}
		})
	}
}

func TestProposeEmailFreezesTrustedRecipient(t *testing.T) {
	e := newEnv(t).withHomework(t)
	v := propose(t, e, outEmail)
	if v.Recipient != "teacher@school.example" {
		t.Fatalf("recipient = %q", v.Recipient)
	}

	// Changing the settings afterwards does not change what the user confirms and what is sent.
	e.recipient = "other@school.example"
	if _, err := e.svc.Confirm(context.Background(), v.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Execute(context.Background(), v.ID); err != nil {
		t.Fatal(err)
	}
	if got := e.mailer.Calls()[0].Recipient().Address(); got != "teacher@school.example" {
		t.Fatalf("sent to %q", got)
	}
}

func TestProposeMalformedStoresNothing(t *testing.T) {
	e := newEnv(t).withHomework(t)
	_, err := e.svc.Propose(context.Background(), values.NewUntrustedOutput(`{"kind":"delete_all","assignment_id":"a1"}`))
	if !errors.Is(err, proposal.ErrMalformed) {
		t.Fatalf("err = %v, want ErrMalformed", err)
	}
	if len(e.proposals) != 0 {
		t.Fatalf("stored %d proposals", len(e.proposals))
	}
}

// Test step 3: a valid proposal without user confirmation is not executed.
func TestValidProposalWithoutConfirmationIsNotExecuted(t *testing.T) {
	for _, out := range []string{outEmail, outExport, outDeadline} {
		e := newEnv(t).withHomework(t)
		v := propose(t, e, out)
		if v.Status != proposal.StatusValidated {
			t.Fatalf("%s: status %s", v.Kind, v.Status)
		}

		for range 3 {
			if _, err := e.svc.Execute(context.Background(), v.ID); !errors.Is(err, proposal.ErrNotConfirmed) {
				t.Fatalf("%s: Execute = %v, want ErrNotConfirmed", v.Kind, err)
			}
		}
		if n := e.sideEffects(); n != 0 {
			t.Fatalf("%s: %d side effects without confirmation", v.Kind, n)
		}
		if got, _ := e.svc.Get(context.Background(), v.ID); got.Status != proposal.StatusValidated {
			t.Fatalf("%s: status changed to %s", v.Kind, got.Status)
		}
	}
}

func TestRejectedAndDeclinedAreNeverExecuted(t *testing.T) {
	ctx := context.Background()

	e := newEnv(t).withHomework(t)
	e.recipient = ""
	rejected := propose(t, e, outEmail)
	if _, err := e.svc.Confirm(ctx, rejected.ID); !errors.Is(err, proposal.ErrInvalidTransition) {
		t.Fatalf("Confirm of a rejected proposal = %v", err)
	}
	if _, err := e.svc.Execute(ctx, rejected.ID); !errors.Is(err, proposal.ErrNotConfirmed) {
		t.Fatalf("Execute of a rejected proposal = %v", err)
	}

	declined := propose(t, e, outExport)
	if v, err := e.svc.Decline(ctx, declined.ID); err != nil || v.Status != proposal.StatusDeclined {
		t.Fatalf("Decline = %+v, %v", v, err)
	}
	if _, err := e.svc.Confirm(ctx, declined.ID); !errors.Is(err, proposal.ErrInvalidTransition) {
		t.Fatalf("Confirm after Decline = %v", err)
	}
	if _, err := e.svc.Execute(ctx, declined.ID); !errors.Is(err, proposal.ErrNotConfirmed) {
		t.Fatalf("Execute after Decline = %v", err)
	}

	if n := e.sideEffects(); n != 0 {
		t.Fatalf("%d side effects", n)
	}
}

// With confirmation every kind reaches exactly its own port exactly once, also when Execute is repeated.
func TestConfirmedProposalExecutesOnce(t *testing.T) {
	ctx := context.Background()
	for _, out := range []string{outEmail, outExport, outDeadline} {
		e := newEnv(t).withHomework(t)
		v := propose(t, e, out)

		e.clock.Advance(time.Minute)
		c, err := e.svc.Confirm(ctx, v.ID)
		if err != nil || c.Status != proposal.StatusConfirmed || !c.ConfirmedAt.Equal(t0.Add(time.Minute)) {
			t.Fatalf("%s: Confirm = %+v, %v", v.Kind, c, err)
		}
		if _, err := e.svc.Confirm(ctx, v.ID); !errors.Is(err, proposal.ErrInvalidTransition) {
			t.Fatalf("%s: second Confirm = %v", v.Kind, err)
		}

		for range 3 {
			x, err := e.svc.Execute(ctx, v.ID)
			if err != nil || x.Status != proposal.StatusExecuted {
				t.Fatalf("%s: Execute = %+v, %v", v.Kind, x, err)
			}
		}

		mail, exp, cal := e.mailer.Calls(), e.exporter.Calls(), e.calendar.Calls()
		want := map[proposal.Kind][3]int{
			proposal.KindSendHomework:   {1, 0, 0},
			proposal.KindExportHomework: {0, 1, 0},
			proposal.KindDeadlineEvent:  {0, 0, 1},
		}[v.Kind]
		if got := [3]int{len(mail), len(exp), len(cal)}; got != want {
			t.Fatalf("%s: calls (mail, export, calendar) = %v, want %v", v.Kind, got, want)
		}

		ref, _ := proposal.ExportRef("a1")
		switch v.Kind {
		case proposal.KindSendHomework:
			if mail[0].Attachment() != ref || mail[0].Recipient().Address() != "teacher@school.example" {
				t.Fatalf("email command %+v", mail[0])
			}
		case proposal.KindExportHomework:
			if exp[0].Target() != ref || exp[0].Content() != "1. Solve 3x = 9." {
				t.Fatalf("export command %+v", exp[0])
			}
		case proposal.KindDeadlineEvent:
			if cal[0].Title() != "Algebra homework" || !cal[0].Due().Equal(time.Date(2026, 10, 20, 18, 0, 0, 0, time.UTC)) {
				t.Fatalf("calendar command %+v", cal[0])
			}
		}
	}
}

// A failing port leaves the proposal CONFIRMED; the retry repeats the same idempotency key (hard rule 7).
func TestExecuteRetriesWithTheSameKey(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t).withHomework(t)
	v := propose(t, e, outEmail)
	if _, err := e.svc.Confirm(ctx, v.ID); err != nil {
		t.Fatal(err)
	}

	boom := errors.New("smtp down")
	e.mailer.Fail(boom)
	if _, err := e.svc.Execute(ctx, v.ID); !errors.Is(err, boom) {
		t.Fatalf("Execute = %v, want the port error", err)
	}
	if got, _ := e.svc.Get(ctx, v.ID); got.Status != proposal.StatusConfirmed {
		t.Fatalf("status after a failed send = %s", got.Status)
	}

	// The port succeeded but storing EXECUTED failed, as after a crash: the repeat must reuse the key.
	e.mailer.Fail(nil)
	e.saveErr = shared.ErrStorageBusy
	if _, err := e.svc.Execute(ctx, v.ID); !errors.Is(err, shared.ErrStorageBusy) {
		t.Fatalf("Execute = %v, want the save error", err)
	}
	e.saveErr = nil
	if x, err := e.svc.Execute(ctx, v.ID); err != nil || x.Status != proposal.StatusExecuted {
		t.Fatalf("Execute = %+v, %v", x, err)
	}

	calls := e.mailer.Calls()
	if len(calls) != 3 {
		t.Fatalf("%d calls, want 3", len(calls))
	}
	for _, c := range calls {
		if c.IdempotencyKey() != calls[0].IdempotencyKey() || c.IdempotencyKey() == "" {
			t.Fatalf("keys differ: %q vs %q", c.IdempotencyKey(), calls[0].IdempotencyKey())
		}
	}
}

func TestUnknownProposal(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := e.svc.Get(ctx, "nope"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("Get = %v", err)
	}
	if _, err := e.svc.Confirm(ctx, "nope"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("Confirm = %v", err)
	}
	if _, err := e.svc.Execute(ctx, "nope"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("Execute = %v", err)
	}
}
