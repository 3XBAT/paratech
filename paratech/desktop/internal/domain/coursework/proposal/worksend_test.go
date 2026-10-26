package proposal

import (
	"errors"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/shared"
)

func storedRef(t *testing.T, sha string) shared.ArtifactRef {
	t.Helper()
	ref, err := shared.NewArtifactRef(shared.ArtifactMaterials, sha)
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

// workFacts are the facts of a done work whose .docx is filed in «ДЗ».
func workFacts(t *testing.T) Facts {
	f := goodFacts(t)
	f.ShownFormat, f.AttachmentRef, f.Filed = FormatDocx, storedRef(t, shaA), true
	return f
}

func mustWorkSend(t *testing.T, f Format) *Proposal {
	t.Helper()
	p, err := NewWorkSend("p1", "a1", "w1", f, t0)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// workInStatus is inStatus for the send of a work.
func workInStatus(t *testing.T, s Status) *Proposal {
	t.Helper()
	p := mustWorkSend(t, FormatDocx)
	step := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	if s == StatusProposed {
		return p
	}
	if s == StatusRejected {
		step(p.Validate(Facts{}, t0))
		return p
	}
	step(p.Validate(workFacts(t), t0))
	switch s {
	case StatusDeclined:
		step(p.Decline(t0.Add(time.Minute)))
	case StatusConfirmed, StatusExecuted, StatusStale:
		step(p.Confirm(t0.Add(time.Minute)))
	}
	switch s {
	case StatusExecuted:
		step(p.MarkExecuted(t0.Add(2 * time.Minute)))
	case StatusStale:
		if err := p.Recheck(Facts{}, t0.Add(2*time.Minute)); !errors.Is(err, ErrStale) {
			t.Fatalf("Recheck = %v", err)
		}
	}
	if p.Status() != s {
		t.Fatalf("status = %s, want %s", p.Status(), s)
	}
	return p
}

func TestNewWorkSend(t *testing.T) {
	cases := []struct {
		name string
		id   ID
		a    shared.AssignmentID
		w    shared.WorkID
		f    Format
		now  time.Time
		want error
	}{
		{"paper", "p1", "a1", "w1", FormatDocx, t0, nil},
		{"presentation", "p1", "a1", "w1", FormatPptx, t0, nil},
		{"text is no work file", "p1", "a1", "w1", FormatText, t0, ErrInvalidProposal},
		{"unknown format", "p1", "a1", "w1", "exe", t0, ErrInvalidProposal},
		{"no assignment", "p1", "", "w1", FormatDocx, t0, ErrInvalidProposal},
		{"bad assignment", "p1", "../a", "w1", FormatDocx, t0, ErrInvalidProposal},
		{"no work", "p1", "a1", "", FormatDocx, t0, ErrInvalidProposal},
		{"no time", "p1", "a1", "w1", FormatDocx, time.Time{}, ErrInvalidProposal},
		{"bad id", "", "a1", "w1", FormatDocx, t0, ErrInvalidID},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := NewWorkSend(tc.id, tc.a, tc.w, tc.f, tc.now)
			if tc.want != nil {
				if !errors.Is(err, tc.want) || p != nil {
					t.Fatalf("NewWorkSend = %v, %v; want %v", p, err, tc.want)
				}
				return
			}
			if err != nil || p.Kind() != KindSendWork || p.Status() != StatusProposed || p.WorkID() != tc.w ||
				p.AssignmentID() != tc.a || p.AttachmentFormat() != tc.f {
				t.Fatalf("NewWorkSend = %+v, %v", p, err)
			}
		})
	}
}

// The model can never propose the send of a work: only NewWorkSend makes one.
func TestModelCannotProposeAWorkSend(t *testing.T) {
	for _, out := range []string{
		`{"kind":"send_work_email","assignment_id":"a1"}`,
		`{"kind":"send_work_email","assignment_id":"a1","work_id":"w1"}`,
	} {
		if p, err := New("p1", values.NewUntrustedOutput(out), t0); !errors.Is(err, ErrMalformed) {
			t.Fatalf("New(%s) = %+v, %v; want ErrMalformed", out, p, err)
		}
	}
	if (KindSendWork).fromModel() || !KindSendWork.Valid() || len(AllKinds()) != len(Kinds())+1 {
		t.Fatal("send_work_email must be a known kind of the core only")
	}
	if !KindSendWork.NeedsRecipient() || !KindSendWork.NeedsAttachment() || !KindSendWork.NeedsSubject() ||
		!KindSendWork.NeedsWorkFile() || KindSendHomework.NeedsWorkFile() {
		t.Fatal("needs of send_work_email")
	}
	if err := mustWorkSend(t, FormatDocx).ChooseFormat(FormatPptx); !errors.Is(err, ErrInvalidProposal) {
		t.Fatalf("ChooseFormat on a work send = %v", err)
	}
	if err := mustNew(t, outEmail).ChooseFormat(FormatPptx); !errors.Is(err, ErrInvalidProposal) {
		t.Fatalf("ChooseFormat pptx on a homework send = %v", err)
	}
}

func TestValidateWorkSend(t *testing.T) {
	cases := []struct {
		name   string
		facts  func(f *Facts)
		reason RejectReason // empty: VALIDATED
	}{
		{"filed in ДЗ", nil, ""},
		{"no assignment", func(f *Facts) { f.AssignmentExists = false }, ReasonUnknownAssignment},
		{"no recipient", func(f *Facts) { f.Recipient = Recipient{} }, ReasonNoRecipient},
		{"no file", func(f *Facts) { f.Attachment, f.AttachmentRef = Digest{}, shared.ArtifactRef{} }, ReasonNoArtifact},
		{"file is not its content", func(f *Facts) { f.AttachmentRef = storedRef(t, shaB) }, ReasonNoArtifact},
		{"moved from ДЗ", func(f *Facts) { f.Filed = false }, ReasonFileMoved},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := mustWorkSend(t, FormatDocx)
			f := workFacts(t)
			if tc.facts != nil {
				tc.facts(&f)
			}
			if err := p.Validate(f, t0); err != nil {
				t.Fatal(err)
			}
			if tc.reason == "" {
				src, err := p.AttachmentSource()
				if p.Status() != StatusValidated || p.Recipient() != f.Recipient || p.AttachmentDigest() != f.Attachment ||
					err != nil || src != f.AttachmentRef {
					t.Fatalf("status %s, %+v, source %s %v", p.Status(), p.State(), src, err)
				}
				return
			}
			if p.Status() != StatusRejected || p.Reason() != tc.reason {
				t.Fatalf("status %s reason %q, want REJECTED %q", p.Status(), p.Reason(), tc.reason)
			}
		})
	}
}

// What the student confirmed must still hold right before the send: the same recipient, file, place, format and
// subject; else STALE and no command.
func TestRecheckWorkSend(t *testing.T) {
	cases := []struct {
		name   string
		facts  func(f *Facts)
		reason RejectReason
	}{
		{"unchanged", nil, ""},
		{"recipient removed", func(f *Facts) { f.Recipient = Recipient{} }, ReasonRecipientChanged},
		{"file deleted", func(f *Facts) { f.Attachment, f.AttachmentRef = Digest{}, shared.ArtifactRef{} }, ReasonNoArtifact},
		{"file changed", func(f *Facts) { f.Attachment, f.AttachmentRef = mustDigest(t, shaB, 11), storedRef(t, shaB) }, ReasonArtifactChanged},
		{"file replaced, same bytes elsewhere", func(f *Facts) { f.AttachmentRef = storedRef(t, shaB) }, ReasonArtifactChanged},
		{"moved from ДЗ", func(f *Facts) { f.Filed = false }, ReasonFileMoved},
		{"format changed", func(f *Facts) { f.ShownFormat = FormatPptx }, ReasonFormatChanged},
		{"subject changed", func(f *Facts) { f.Subject, f.ShownSubject = "Химия", "Физика" }, ReasonSubjectChanged},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := workInStatus(t, StatusConfirmed)
			f := workFacts(t)
			if tc.facts != nil {
				tc.facts(&f)
			}
			err := p.Recheck(f, t0.Add(time.Hour))
			if tc.reason == "" {
				if err != nil || p.Status() != StatusConfirmed {
					t.Fatalf("err = %v, status %s", err, p.Status())
				}
				return
			}
			if !errors.Is(err, ErrStale) || p.Status() != StatusStale || p.Reason() != tc.reason {
				t.Fatalf("err = %v, status %s, reason %q; want %q", err, p.Status(), p.Reason(), tc.reason)
			}
			if _, err := p.WorkEmailCommand("Реферат.docx"); !errors.Is(err, ErrNotConfirmed) {
				t.Fatalf("command from a stale proposal: %v", err)
			}
		})
	}
}

func TestWorkEmailCommand(t *testing.T) {
	p := workInStatus(t, StatusConfirmed)
	cmd, err := p.WorkEmailCommand("Реферат: история «Руси».docx")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.IdempotencyKey() != "proposal:p1" || cmd.AssignmentID() != "a1" || cmd.Recipient().Address() != "teacher@school.example" ||
		cmd.Attachment().String() != "exports/sent/p1/Реферат_ история «Руси».docx" || cmd.AttachmentDigest() != mustDigest(t, shaA, 11) ||
		!cmd.ConfirmedAt().Equal(p.ConfirmedAt()) {
		t.Fatalf("command: %+v", cmd)
	}
	if _, err := p.EmailCommand(); !errors.Is(err, ErrWrongKind) {
		t.Fatalf("EmailCommand of a work send = %v", err)
	}
	if _, err := inStatus(t, outEmail, StatusConfirmed).WorkEmailCommand("x"); !errors.Is(err, ErrWrongKind) {
		t.Fatalf("WorkEmailCommand of a homework send = %v", err)
	}
	for _, s := range Statuses() {
		if s == StatusConfirmed {
			continue
		}
		if c, err := workInStatus(t, s).WorkEmailCommand("x"); !errors.Is(err, ErrNotConfirmed) || !c.IsZero() {
			t.Errorf("%s: %v", s, err)
		}
	}
}

func TestCopyName(t *testing.T) {
	cases := []struct {
		name string
		f    Format
		want string
	}{
		{"Реферат.docx", FormatDocx, "Реферат.docx"},
		{"Доклад по истории.pptx", FormatPptx, "Доклад по истории.pptx"},
		{`a/b\c:d*e?.docx`, FormatDocx, "a_b_c_d_e_.docx"},
		{"CON.docx", FormatDocx, "_CON.docx"},
		{"", FormatDocx, "работа.docx"},
		{"...", FormatPptx, "работа.pptx"},
		{"line\nbreak", FormatDocx, "line break.docx"},
		{strings.Repeat("я", 100) + ".docx", FormatDocx, strings.Repeat("я", maxCopyNameRunes) + ".docx"},
	}
	for _, tc := range cases {
		if got := CopyName(tc.name, tc.f); got != tc.want {
			t.Errorf("CopyName(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestWorkSendRestore(t *testing.T) {
	for _, s := range Statuses() {
		st := workInStatus(t, s).State()
		st.Version = 2
		got, err := Restore(st)
		if err != nil || got.State() != st {
			t.Fatalf("%s: %v\n got %+v\nwant %+v", s, err, got, st)
		}
	}
	base := func(s Status) State {
		st := workInStatus(t, s).State()
		st.Version = 1
		return st
	}
	bad := []struct {
		name string
		st   State
		edit func(s *State)
	}{
		{"no work", base(StatusValidated), func(s *State) { s.WorkID = "" }},
		{"text format", base(StatusValidated), func(s *State) { s.AttachmentFormat = FormatText }},
		{"no format", base(StatusValidated), func(s *State) { s.AttachmentFormat = "" }},
		{"with a title", base(StatusValidated), func(s *State) { s.Title = "x" }},
		{"with a body", base(StatusValidated), func(s *State) { s.Body = "x" }},
		{"without the file", base(StatusConfirmed), func(s *State) { s.AttachmentSHA256, s.AttachmentSize = "", 0 }},
		{"without the recipient", base(StatusConfirmed), func(s *State) { s.Recipient = "" }},
		{"homework send with a work", inStatus(t, outEmail, StatusValidated).State(), func(s *State) { s.Version, s.WorkID = 1, "w1" }},
		{"homework send as pptx", inStatus(t, outEmail, StatusValidated).State(), func(s *State) { s.Version, s.AttachmentFormat = 1, FormatPptx }},
		{"rejected with a stale reason", base(StatusRejected), func(s *State) { s.Reason = ReasonArtifactChanged }},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			st := tc.st
			tc.edit(&st)
			if p, err := Restore(st); err == nil {
				t.Fatalf("Restore accepted %+v as %s", st, p.Status())
			}
		})
	}
	st := base(StatusRejected)
	st.Reason = ReasonFileMoved
	if _, err := Restore(st); err != nil {
		t.Fatalf("a send rejected as moved: %v", err)
	}
}

func TestWaitsAsWorkSend(t *testing.T) {
	a, b := workInStatus(t, StatusValidated), mustWorkSend(t, FormatDocx)
	b.id = "p2"
	if err := b.Validate(workFacts(t), t0); err != nil {
		t.Fatal(err)
	}
	if !a.WaitsAsWorkSend(b) {
		t.Fatal("the same send of the same file must be reused")
	}
	if a.WaitsAsWorkSend(a) {
		t.Fatal("a proposal does not wait as itself")
	}
	other := mustWorkSend(t, FormatDocx)
	other.id = "p3"
	f := workFacts(t)
	f.Attachment, f.AttachmentRef = mustDigest(t, shaB, 11), storedRef(t, shaB)
	if err := other.Validate(f, t0); err != nil {
		t.Fatal(err)
	}
	if a.WaitsAsWorkSend(other) || workInStatus(t, StatusConfirmed).WaitsAsWorkSend(b) {
		t.Fatal("another file or a confirmed send is no waiting duplicate")
	}
}
