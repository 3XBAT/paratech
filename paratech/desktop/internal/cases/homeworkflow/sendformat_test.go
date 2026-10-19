package homeworkflow

import (
	"context"
	"errors"
	"strings"
	"testing"

	"paratech/desktop/internal/domain/coursework/proposal"
)

// withDocx writes the Word export of a1 next to the text export of withHomework.
func (e *env) withDocx(t *testing.T, content string) {
	t.Helper()
	ref, err := proposal.ExportRefOf("a1", proposal.FormatDocx)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.files.Write(context.Background(), ref, strings.NewReader(content)); err != nil {
		t.Fatal(err)
	}
}

// TASK-094: the send attaches the format the user chose; the dialog shows that file, and the confirmed command
// carries it.
func TestSendAttachesTheChosenFormat(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t).withHomework(t)
	e.withDocx(t, "PK docx bytes")

	v, err := e.svc.ProposeSend(ctx, "a1", proposal.FormatDocx)
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != proposal.StatusValidated || v.AttachmentName != "homework.docx" || v.AttachmentSize != int64(len("PK docx bytes")) ||
		v.AttachmentFormat != proposal.FormatDocx {
		t.Fatalf("view = %+v", v)
	}
	if _, err := e.svc.ConfirmSend(ctx, v.ID, v.Subject, proposal.FormatDocx); err != nil {
		t.Fatal(err)
	}
	calls := e.mailer.Calls()
	want, _ := proposal.ExportRefOf("a1", proposal.FormatDocx)
	if len(calls) != 1 || calls[0].Attachment() != want || calls[0].AttachmentDigest().Size() != v.AttachmentSize {
		t.Fatalf("mailer calls = %+v", calls)
	}
}

// Without a Word export the docx send is rejected like a missing text export; an unknown format is an error.
func TestSendDocxNeedsTheWordExport(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t).withHomework(t)
	v, err := e.svc.ProposeSend(ctx, "a1", proposal.FormatDocx)
	if err != nil || v.Status != proposal.StatusRejected || v.Reason != proposal.ReasonNoArtifact {
		t.Fatalf("view = %+v, %v", v, err)
	}
	if _, err := e.svc.ProposeSend(ctx, "a1", "pdf"); !errors.Is(err, proposal.ErrInvalidProposal) {
		t.Fatalf("unknown format: %v", err)
	}
	if e.sideEffects() != 0 {
		t.Fatal("a side effect before any confirmation")
	}
}

// A confirmation for another format than the one prepared sends nothing: the proposal goes STALE (hard rule 3).
func TestConfirmSendWithAnotherFormatIsStale(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t).withHomework(t)
	e.withDocx(t, "PK docx bytes")
	v, err := e.svc.ProposeSend(ctx, "a1", proposal.FormatText)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.ConfirmSend(ctx, v.ID, v.Subject, proposal.FormatDocx); !errors.Is(err, proposal.ErrStale) {
		t.Fatalf("ConfirmSend = %v, want ErrStale", err)
	}
	if e.sideEffects() != 0 {
		t.Fatal("an email went out for a format the user did not see")
	}
	got, err := e.svc.Get(ctx, v.ID)
	if err != nil || got.Status != proposal.StatusStale || got.Reason != proposal.ReasonFormatChanged {
		t.Fatalf("proposal = %+v, %v", got, err)
	}
}

// A changed Word export after the confirmation is caught like a changed text export.
func TestDocxChangedAfterConfirmationIsStale(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t).withHomework(t)
	e.withDocx(t, "PK docx bytes")
	v, err := e.svc.ProposeSend(ctx, "a1", proposal.FormatDocx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Confirm(ctx, v.ID); err != nil {
		t.Fatal(err)
	}
	e.withDocx(t, "PK other bytes")
	if _, err := e.svc.Execute(ctx, v.ID); !errors.Is(err, proposal.ErrStale) || e.sideEffects() != 0 {
		t.Fatalf("Execute = %v with %d side effects", err, e.sideEffects())
	}
}
