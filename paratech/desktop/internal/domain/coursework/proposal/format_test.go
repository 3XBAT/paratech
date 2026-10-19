package proposal

import (
	"errors"
	"testing"
	"time"
)

// TASK-094: the user chooses the attachment of a send before it is validated; the confirmed format is pinned.
func TestChooseFormat(t *testing.T) {
	p := mustNew(t, outEmail)
	if p.AttachmentFormat() != FormatText {
		t.Fatalf("default format = %q, want txt", p.AttachmentFormat())
	}
	if err := p.ChooseFormat("pdf"); !errors.Is(err, ErrInvalidProposal) {
		t.Fatalf("unknown format: %v", err)
	}
	if err := p.ChooseFormat(FormatDocx); err != nil || p.AttachmentFormat() != FormatDocx {
		t.Fatalf("ChooseFormat(docx) = %v, format %q", err, p.AttachmentFormat())
	}
	if err := p.Validate(goodFacts(t), t0); err != nil {
		t.Fatal(err)
	}
	if err := p.ChooseFormat(FormatText); !errors.Is(err, ErrInvalidTransition) || p.AttachmentFormat() != FormatDocx {
		t.Fatalf("after validation: %v, format %q", err, p.AttachmentFormat())
	}

	for _, out := range []string{outExport, outDeadline} {
		q := mustNew(t, out)
		if q.AttachmentFormat() != "" {
			t.Fatalf("%s has a format", q.Kind())
		}
		if err := q.ChooseFormat(FormatDocx); !errors.Is(err, ErrInvalidProposal) {
			t.Fatalf("%s: ChooseFormat = %v", q.Kind(), err)
		}
	}
}

func TestEmailCommandAttachesTheChosenFormat(t *testing.T) {
	for _, f := range []Format{FormatText, FormatDocx} {
		p := mustNew(t, outEmail)
		if err := p.ChooseFormat(f); err != nil {
			t.Fatal(err)
		}
		if err := p.Validate(goodFacts(t), t0); err != nil {
			t.Fatal(err)
		}
		if err := p.Confirm(t0.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
		cmd, err := p.EmailCommand()
		if err != nil {
			t.Fatal(err)
		}
		want, err := ExportRefOf(p.AssignmentID(), f)
		if err != nil || cmd.Attachment() != want {
			t.Fatalf("%s: attachment = %v, want %v (%v)", f, cmd.Attachment(), want, err)
		}
	}
}

func TestExportRefOf(t *testing.T) {
	txt, err := ExportRefOf("a1", FormatText)
	if err != nil || txt.Path() != "a1/homework.md" {
		t.Fatalf("txt = %v, %v", txt, err)
	}
	if ref, err := ExportRef("a1"); err != nil || ref != txt {
		t.Fatalf("ExportRef = %v, %v; want the text export", ref, err)
	}
	docx, err := ExportRefOf("a1", FormatDocx)
	if err != nil || docx.Path() != "a1/homework.docx" || docx.Kind() != txt.Kind() {
		t.Fatalf("docx = %v, %v", docx, err)
	}
	if _, err := ExportRefOf("a1", ""); !errors.Is(err, ErrInvalidProposal) {
		t.Fatalf("no format: %v", err)
	}
	if _, err := ExportRefOf("../x", FormatDocx); err == nil {
		t.Fatal("an unsafe id was accepted")
	}
}

// The format the dialog showed is part of what the user confirmed: another one makes the send STALE.
func TestRecheckComparesTheShownFormat(t *testing.T) {
	cases := []struct {
		chosen, shown Format
		reason        RejectReason
	}{
		{FormatText, FormatText, ""},
		{FormatDocx, FormatDocx, ""},
		{FormatDocx, FormatText, ReasonFormatChanged},
		{FormatText, FormatDocx, ReasonFormatChanged},
		{FormatDocx, "", ReasonFormatChanged},
	}
	for _, tc := range cases {
		p := mustNew(t, outEmail)
		if err := p.ChooseFormat(tc.chosen); err != nil {
			t.Fatal(err)
		}
		if err := p.Validate(goodFacts(t), t0); err != nil {
			t.Fatal(err)
		}
		if err := p.Confirm(t0.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
		f := goodFacts(t)
		f.ShownFormat = tc.shown
		err := p.Recheck(f, t0.Add(time.Hour))
		if tc.reason == "" {
			if err != nil {
				t.Fatalf("%s shown %s: %v", tc.chosen, tc.shown, err)
			}
			continue
		}
		if !errors.Is(err, ErrStale) || p.Reason() != tc.reason {
			t.Fatalf("%s shown %q: err = %v, reason %q", tc.chosen, tc.shown, err, p.Reason())
		}
		// A stale send with this reason is stored and restored.
		st := p.State()
		st.Version = 1
		if _, err := Restore(st); err != nil {
			t.Fatalf("Restore of the stale send: %v", err)
		}
	}
	// The other kinds have no attachment to compare.
	p := inStatus(t, outExport, StatusConfirmed)
	f := goodFacts(t)
	f.ShownFormat = FormatDocx
	if err := p.Recheck(f, t0.Add(time.Hour)); err != nil {
		t.Fatalf("export: %v", err)
	}
}

func TestRestoreAttachmentFormat(t *testing.T) {
	email := inStatus(t, outEmail, StatusValidated).State()
	email.Version = 1

	legacy := email
	legacy.AttachmentFormat = ""
	if p, err := Restore(legacy); err != nil || p.AttachmentFormat() != FormatText {
		t.Fatalf("a send stored before TASK-094: %v, format %q", err, p.AttachmentFormat())
	}
	docx := email
	docx.AttachmentFormat = FormatDocx
	if p, err := Restore(docx); err != nil || p.AttachmentFormat() != FormatDocx || p.State().AttachmentFormat != FormatDocx {
		t.Fatalf("docx: %v", err)
	}
	bad := email
	bad.AttachmentFormat = "exe"
	if _, err := Restore(bad); !errors.Is(err, ErrInvalidProposal) {
		t.Fatalf("unknown format: %v", err)
	}
	export := inStatus(t, outExport, StatusValidated).State()
	export.Version = 1
	export.AttachmentFormat = FormatText
	if _, err := Restore(export); !errors.Is(err, ErrInvalidProposal) {
		t.Fatalf("export with a format: %v", err)
	}
}
