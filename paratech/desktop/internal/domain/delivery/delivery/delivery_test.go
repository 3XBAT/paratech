package delivery

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/shared"
)

var (
	t0      = time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)
	digest  = strings.Repeat("ab", 32)
	errSome = errors.New("some failure")
)

func ref(t *testing.T) shared.ArtifactRef {
	t.Helper()
	r, err := shared.NewArtifactRef(shared.ArtifactExports, "a1/homework.md")
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func spec(t *testing.T) Spec {
	t.Helper()
	att, err := NewAttachment(ref(t), digest, 42)
	if err != nil {
		t.Fatal(err)
	}
	return Spec{
		ID: "d1", AssignmentID: "a1", Recipient: "teacher@example.org", Subject: "Homework", Attachment: att,
		MessageID: "<m1@paratech.local>", Key: "proposal:p1", ConfirmedAt: t0.Add(-time.Minute),
	}
}

func newPending(t *testing.T) *Delivery {
	t.Helper()
	d, err := New(spec(t), t0)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// inStatus builds a stored delivery in status s through the public methods.
func inStatus(t *testing.T, s Status) *Delivery {
	t.Helper()
	d := newPending(t)
	now := t0.Add(time.Second)
	switch s {
	case StatusPending:
	case StatusSending:
		mustOK(t, d.StartSending(now))
	case StatusSent:
		mustOK(t, d.StartSending(now))
		mustOutcome(t, d, nil, OutcomeSent)
	case StatusFailed:
		mustOK(t, d.FailUnsent(ErrAttachmentChanged, now))
	case StatusUnknown:
		mustOK(t, d.StartSending(now))
		mustOK(t, d.Interrupted(now))
	default:
		t.Fatalf("no builder for %s", s)
	}
	return d
}

func mustOK(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func mustOutcome(t *testing.T, d *Delivery, sendErr error, want Outcome) {
	t.Helper()
	got, err := d.RecordResult(sendErr, t0.Add(2*time.Second))
	if err != nil || got != want {
		t.Fatalf("RecordResult(%v) = %v, %v; want %v", sendErr, got, err, want)
	}
}

func TestNewRequiresConfirmation(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Spec)
		now    time.Time
		ok     bool
	}{
		{"valid", func(*Spec) {}, t0, true},
		{"confirmed at creation time", func(s *Spec) { s.ConfirmedAt = t0 }, t0, true},
		{"not confirmed", func(s *Spec) { s.ConfirmedAt = time.Time{} }, t0, false},
		{"confirmed after creation", func(s *Spec) { s.ConfirmedAt = t0.Add(time.Second) }, t0, false},
		{"no creation time", func(*Spec) {}, time.Time{}, false},
		{"bad id", func(s *Spec) { s.ID = "a b" }, t0, false},
		{"bad assignment", func(s *Spec) { s.AssignmentID = "" }, t0, false},
		{"no attachment", func(s *Spec) { s.Attachment = Attachment{} }, t0, false},
		{"recipient with display name", func(s *Spec) { s.Recipient = "T <t@example.org>" }, t0, false},
		{"recipient with CRLF", func(s *Spec) { s.Recipient = "t@example.org\r\nBcc: x@evil.org" }, t0, false},
		{"two recipients", func(s *Spec) { s.Recipient = "t@example.org,x@evil.org" }, t0, false},
		{"recipient without domain dot", func(s *Spec) { s.Recipient = "t@localhost" }, t0, false},
		{"subject with LF", func(s *Spec) { s.Subject = "Homework\nBcc: x@evil.org" }, t0, false},
		{"empty subject", func(s *Spec) { s.Subject = " " }, t0, false},
		{"long subject", func(s *Spec) { s.Subject = strings.Repeat("я", MaxSubjectRunes+1) }, t0, false},
		{"unicode subject", func(s *Spec) { s.Subject = "Домашнее задание" }, t0, true},
		{"message id without brackets", func(s *Spec) { s.MessageID = "m1@host" }, t0, false},
		{"message id without at", func(s *Spec) { s.MessageID = "<m1>" }, t0, false},
		{"message id with space", func(s *Spec) { s.MessageID = "<m 1@host>" }, t0, false},
		{"message id with CR", func(s *Spec) { s.MessageID = "<m1@host\r>" }, t0, false},
		{"empty key", func(s *Spec) { s.Key = "" }, t0, false},
		{"long key", func(s *Spec) { s.Key = strings.Repeat("k", MaxKeyBytes+1) }, t0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := spec(t)
			tt.change(&s)
			d, err := New(s, tt.now)
			if tt.ok {
				if err != nil {
					t.Fatalf("New: %v", err)
				}
				if d.Status() != StatusPending || d.Version() != 0 || d.Attempts() != 0 {
					t.Fatalf("new delivery = %s v%d attempts %d", d.Status(), d.Version(), d.Attempts())
				}
				return
			}
			if !errors.Is(err, ErrInvalidDelivery) {
				t.Fatalf("New error = %v, want ErrInvalidDelivery", err)
			}
		})
	}
}

func TestNewAttachment(t *testing.T) {
	tests := []struct {
		name string
		ref  shared.ArtifactRef
		sha  string
		size int64
		ok   bool
	}{
		{"valid", ref(t), digest, 0, true},
		{"no ref", shared.ArtifactRef{}, digest, 1, false},
		{"uppercase hex", ref(t), strings.ToUpper(digest), 1, false},
		{"short hex", ref(t), digest[:63], 1, false},
		{"negative size", ref(t), digest, -1, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := NewAttachment(tt.ref, tt.sha, tt.size)
			if (err == nil) != tt.ok {
				t.Fatalf("NewAttachment = %v, want ok %v", err, tt.ok)
			}
			if tt.ok && (!a.Matches(tt.sha, tt.size) || a.Matches(tt.sha, tt.size+1)) {
				t.Fatal("Matches does not compare digest and size")
			}
		})
	}
}

// Every pair of statuses: the allowed transitions are exactly the table (ARCH-62).
func TestTransitionTable(t *testing.T) {
	allowed := map[[2]Status]bool{
		{StatusPending, StatusSending}: true, {StatusPending, StatusFailed}: true,
		{StatusSending, StatusSent}: true, {StatusSending, StatusPending}: true, {StatusSending, StatusFailed}: true,
		{StatusSending, StatusUnknown}: true,
		{StatusUnknown, StatusSent}:    true, {StatusUnknown, StatusPending}: true,
	}
	for _, from := range Statuses() {
		for _, to := range Statuses() {
			if got := CanTransition(from, to); got != allowed[[2]Status{from, to}] {
				t.Errorf("CanTransition(%s, %s) = %v", from, to, got)
			}
		}
	}
	if len(transitions) != len(Statuses()) {
		t.Fatalf("table has %d statuses, Statuses has %d", len(transitions), len(Statuses()))
	}
}

// IsTerminal and IsUnresolved are derived from the table (ADR-0004(f)).
func TestStatusClassification(t *testing.T) {
	tests := []struct {
		status     Status
		terminal   bool
		unresolved bool
	}{
		{StatusPending, false, true},
		{StatusSending, false, true},
		{StatusUnknown, false, true},
		{StatusSent, true, false},
		{StatusFailed, true, false},
		{Status("BOGUS"), false, false},
	}
	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if tt.status.IsTerminal() != tt.terminal || tt.status.IsUnresolved() != tt.unresolved {
				t.Fatalf("%s: terminal %v unresolved %v", tt.status, tt.status.IsTerminal(), tt.status.IsUnresolved())
			}
			if tt.status.Valid() && tt.status.IsTerminal() != (len(transitions[tt.status]) == 0) {
				t.Fatal("IsTerminal disagrees with the table")
			}
		})
	}
}

// Each method from each status: it succeeds exactly where the table allows its target.
func TestMethodsFollowTheTable(t *testing.T) {
	now := t0.Add(time.Minute)
	methods := []struct {
		name string
		to   func(Status) bool // whether the method is allowed from the status
		call func(*Delivery) error
		want Status
	}{
		{"StartSending", func(s Status) bool { return s == StatusPending }, func(d *Delivery) error { return d.StartSending(now) }, StatusSending},
		{"FailUnsent", func(s Status) bool { return s == StatusPending }, func(d *Delivery) error { return d.FailUnsent(errSome, now) }, StatusFailed},
		{"Interrupted", func(s Status) bool { return s == StatusSending }, func(d *Delivery) error { return d.Interrupted(now) }, StatusUnknown},
		{"MarkSent", func(s Status) bool { return s == StatusUnknown }, func(d *Delivery) error { return d.MarkSent(now) }, StatusSent},
		{"SendAgain", func(s Status) bool { return s == StatusUnknown }, func(d *Delivery) error { return d.SendAgain(now) }, StatusPending},
		{"RecordResult", func(s Status) bool { return s == StatusSending }, func(d *Delivery) error {
			_, err := d.RecordResult(nil, now)
			return err
		}, StatusSent},
		{"RecordFinalResult", func(s Status) bool { return s == StatusSending }, func(d *Delivery) error {
			_, err := d.RecordFinalResult(fmt.Errorf("x: %w", ErrTransient), now)
			return err
		}, StatusFailed},
	}
	for _, m := range methods {
		for _, from := range Statuses() {
			t.Run(m.name+"/"+string(from), func(t *testing.T) {
				d := inStatus(t, from)
				err := m.call(d)
				if m.to(from) {
					if err != nil || d.Status() != m.want {
						t.Fatalf("%s from %s = %v, status %s; want %s", m.name, from, err, d.Status(), m.want)
					}
					if !d.UpdatedAt().Equal(now) {
						t.Fatalf("updated at %v, want %v", d.UpdatedAt(), now)
					}
					return
				}
				if !errors.Is(err, ErrInvalidTransition) || d.Status() != from {
					t.Fatalf("%s from %s = %v, status %s; want ErrInvalidTransition and no change", m.name, from, err, d.Status())
				}
			})
		}
	}
}

func TestOutcomeOf(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want Outcome
	}{
		{"accepted", nil, OutcomeSent},
		{"transient", fmt.Errorf("smtp: connect: %w", ErrTransient), OutcomeRetry},
		{"rejected", fmt.Errorf("smtp: refused: %w", ErrRejected), OutcomeFailed},
		{"attachment changed", fmt.Errorf("smtp: %w", ErrAttachmentChanged), OutcomeFailed},
		{"unknown", fmt.Errorf("smtp: no reply: %w", ErrOutcomeUnknown), OutcomeUnknown},
		{"unknown wins over transient", errors.Join(ErrTransient, ErrOutcomeUnknown), OutcomeUnknown},
		{"unclassified error", errSome, OutcomeUnknown},
		{"bare deadline", context.DeadlineExceeded, OutcomeUnknown},
		{"bare cancel", context.Canceled, OutcomeUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := OutcomeOf(tt.err); got != tt.want {
				t.Fatalf("OutcomeOf(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestRecordResult(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		status  Status
		lastErr string
	}{
		{"accepted", nil, StatusSent, ""},
		{"transient goes back to pending", fmt.Errorf("smtp: connect failed: %w", ErrTransient), StatusPending, "smtp: connect failed: delivery failed transiently"},
		{"rejected fails", fmt.Errorf("smtp: refused: %w", ErrRejected), StatusFailed, "smtp: refused: delivery rejected"},
		{"timeout after data is unknown", fmt.Errorf("smtp: no final reply: %w", ErrOutcomeUnknown), StatusUnknown, "smtp: no final reply: delivery outcome unknown"},
		{"control characters are removed", fmt.Errorf("a\r\nb: %w", ErrTransient), StatusPending, "a  b: delivery failed transiently"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := inStatus(t, StatusSending)
			now := t0.Add(time.Hour)
			if _, err := d.RecordResult(tt.err, now); err != nil {
				t.Fatal(err)
			}
			if d.Status() != tt.status || d.LastError() != tt.lastErr || d.Attempts() != 1 {
				t.Fatalf("status %s, last error %q, attempts %d", d.Status(), d.LastError(), d.Attempts())
			}
			if (tt.status == StatusSent) != d.SentAt().Equal(now) {
				t.Fatalf("sent at %v", d.SentAt())
			}
		})
	}
}

func TestLastErrorIsBounded(t *testing.T) {
	d := inStatus(t, StatusSending)
	if _, err := d.RecordResult(fmt.Errorf("%s: %w", strings.Repeat("é", MaxLastErrorBytes), ErrTransient), t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if len(d.LastError()) > MaxLastErrorBytes || !strings.HasPrefix(d.LastError(), "é") || strings.HasSuffix(d.LastError(), "\xc3") {
		t.Fatalf("last error of %d bytes not cut on a rune boundary", len(d.LastError()))
	}
}

// An UNKNOWN delivery is never sent again without a resolution: the only way back to PENDING is SendAgain, and a new
// attempt then increments the attempts and keeps the Message-ID.
func TestUnknownNeedsResolutionBeforeAnotherAttempt(t *testing.T) {
	d := inStatus(t, StatusSending)
	mustOutcome(t, d, fmt.Errorf("x: %w", ErrOutcomeUnknown), OutcomeUnknown)
	if err := d.StartSending(t0.Add(time.Hour)); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("StartSending from UNKNOWN = %v, want ErrInvalidTransition", err)
	}
	mustOK(t, d.SendAgain(t0.Add(time.Hour)))
	mustOK(t, d.StartSending(t0.Add(2*time.Hour)))
	if d.Attempts() != 2 || d.MessageID() != "<m1@paratech.local>" {
		t.Fatalf("attempts %d, message id %s", d.Attempts(), d.MessageID())
	}
}

func TestStateRoundTrip(t *testing.T) {
	for _, s := range Statuses() {
		t.Run(string(s), func(t *testing.T) {
			d := inStatus(t, s)
			d.Persisted(3)
			r, err := Restore(d.State())
			if err != nil {
				t.Fatalf("Restore: %v", err)
			}
			if r.State() != d.State() {
				t.Fatalf("round trip\n got %+v\nwant %+v", r.State(), d.State())
			}
		})
	}
}

func TestRestoreRejectsInconsistentState(t *testing.T) {
	sent := inStatus(t, StatusSent)
	sent.Persisted(1)
	tests := []struct {
		name   string
		change func(*State)
	}{
		{"version 0", func(s *State) { s.Version = 0 }},
		{"unknown status", func(s *State) { s.Status = "LOST" }},
		{"no confirmation", func(s *State) { s.ConfirmedAt = time.Time{} }},
		{"confirmed after creation", func(s *State) { s.ConfirmedAt = s.CreatedAt.Add(time.Second) }},
		{"updated before created", func(s *State) { s.UpdatedAt = s.CreatedAt.Add(-time.Second) }},
		{"sent without send time", func(s *State) { s.SentAt = time.Time{} }},
		{"send time while pending", func(s *State) { s.Status = StatusPending }},
		{"sent without attempts", func(s *State) { s.Attempts = 0 }},
		{"negative attempts", func(s *State) { s.Attempts = -1 }},
		{"negative round", func(s *State) { s.Round = -1 }},
		{"more rounds than attempts", func(s *State) { s.Round = s.Attempts + 1 }},
		{"long last error", func(s *State) { s.LastError = strings.Repeat("x", MaxLastErrorBytes+1) }},
		{"empty digest", func(s *State) { s.SHA256 = "" }},
		{"no attachment", func(s *State) { s.Attachment = shared.ArtifactRef{} }},
		{"injected recipient", func(s *State) { s.Recipient = "t@example.org\nBcc: x@evil.org" }},
		{"injected subject", func(s *State) { s.Subject = "x\r\nBcc: x@evil.org" }},
		{"bad message id", func(s *State) { s.MessageID = "" }},
		{"bad key", func(s *State) { s.Key = "a key" }},
		{"bad id", func(s *State) { s.ID = "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := sent.State()
			tt.change(&st)
			if _, err := Restore(st); !errors.Is(err, ErrInvalidDelivery) {
				t.Fatalf("Restore = %v, want ErrInvalidDelivery", err)
			}
		})
	}
}

func TestJobPayloadAndKey(t *testing.T) {
	d := newPending(t)
	if d.JobPayload() != `{"delivery_id":"d1"}` || d.JobKey() != "send_email:d1:0" {
		t.Fatalf("payload %s key %s", d.JobPayload(), d.JobKey())
	}
	if id, err := ParseJobPayload(d.JobPayload()); err != nil || id != "d1" {
		t.Fatalf("ParseJobPayload = %q, %v", id, err)
	}

	mustOK(t, d.StartSending(t0.Add(time.Second)))
	mustOK(t, d.Interrupted(t0.Add(time.Second)))
	mustOK(t, d.SendAgain(t0.Add(time.Second)))
	if d.JobKey() != "send_email:d1:1" {
		t.Fatalf("key after send again = %s, want a new round", d.JobKey())
	}

	for _, bad := range []string{``, `{}`, `[]`, `{"delivery_id":""}`, `{"delivery_id":"d1","x":1}`, `{"delivery_id":"d1"}{}`, `{"delivery_id":"a/b"}`} {
		if _, err := ParseJobPayload(bad); !errors.Is(err, ErrInvalidPayload) {
			t.Errorf("ParseJobPayload(%q) = %v, want ErrInvalidPayload", bad, err)
		}
	}
}

func TestBlocks(t *testing.T) {
	for _, s := range Statuses() {
		t.Run(string(s), func(t *testing.T) {
			d := inStatus(t, s)
			if got := d.Blocks("a1"); got != s.IsUnresolved() {
				t.Fatalf("Blocks(own assignment) in %s = %v", s, got)
			}
			if d.Blocks("a2") {
				t.Fatalf("Blocks(other assignment) in %s", s)
			}
		})
	}
}

// On the last attempt of the job a retry outcome fails the delivery (nothing was accepted); the others are unchanged.
func TestRecordFinalResult(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		out     Outcome
		status  Status
		lastErr string
	}{
		{"accepted", nil, OutcomeSent, StatusSent, ""},
		{"transient is exhausted", fmt.Errorf("smtp: connect failed: %w", ErrTransient), OutcomeFailed, StatusFailed,
			"attempts exhausted: smtp: connect failed: delivery failed transiently"},
		{"rejected", fmt.Errorf("smtp: refused: %w", ErrRejected), OutcomeFailed, StatusFailed, "smtp: refused: delivery rejected"},
		{"unknown stays unknown", fmt.Errorf("smtp: no final reply: %w", ErrOutcomeUnknown), OutcomeUnknown, StatusUnknown,
			"smtp: no final reply: delivery outcome unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := inStatus(t, StatusSending)
			out, err := d.RecordFinalResult(tt.err, t0.Add(time.Hour))
			if err != nil || out != tt.out || d.Status() != tt.status || d.LastError() != tt.lastErr {
				t.Fatalf("RecordFinalResult = %v, %v; status %s, last error %q", out, err, d.Status(), d.LastError())
			}
		})
	}
	if _, err := inStatus(t, StatusPending).RecordFinalResult(nil, t0); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("RecordFinalResult of PENDING = %v, want ErrInvalidTransition", err)
	}
}

// The job key changes only when SendAgain starts a new round: retries after transient failures keep the key, so a
// repeated request or a restart cannot add a second job for the same round.
func TestJobKeyChangesOnlyOnSendAgain(t *testing.T) {
	d := newPending(t)
	for i := 0; i < 3; i++ {
		mustOK(t, d.StartSending(t0.Add(time.Second)))
		mustOutcome(t, d, fmt.Errorf("x: %w", ErrTransient), OutcomeRetry)
	}
	if d.JobKey() != "send_email:d1:0" || d.Round() != 0 || d.Attempts() != 3 {
		t.Fatalf("after transient failures: key %s round %d attempts %d", d.JobKey(), d.Round(), d.Attempts())
	}

	mustOK(t, d.StartSending(t0.Add(time.Second)))
	mustOutcome(t, d, fmt.Errorf("x: %w", ErrOutcomeUnknown), OutcomeUnknown)
	mustOK(t, d.SendAgain(t0.Add(time.Second)))
	if d.JobKey() != "send_email:d1:1" || d.Round() != 1 {
		t.Fatalf("after send again: key %s round %d", d.JobKey(), d.Round())
	}

	d.Persisted(2)
	r, err := Restore(d.State())
	if err != nil || r.Round() != 1 || r.JobKey() != d.JobKey() {
		t.Fatalf("Restore keeps the round: %v, round %d", err, r.Round())
	}
}

func TestStatusPredicates(t *testing.T) {
	for _, s := range Statuses() {
		t.Run(string(s), func(t *testing.T) {
			d := inStatus(t, s)
			if d.Sendable() != (s == StatusPending) || d.InFlight() != (s == StatusSending) ||
				d.OutcomeUnknown() != (s == StatusUnknown) {
				t.Fatalf("predicates in %s: sendable %v in flight %v unknown %v", s, d.Sendable(), d.InFlight(), d.OutcomeUnknown())
			}
		})
	}
}
