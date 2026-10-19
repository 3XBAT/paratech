package sqlite

import (
	"context"
	"errors"
	"testing"

	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/coursework/values"
)

// TASK-094: the attachment format a send pinned is stored; a send stored before reads as the text export, and the
// column refuses an unknown format.
func TestProposalAttachmentFormatRoundTrip(t *testing.T) {
	ctx := context.Background()
	db := openTest(t, nil)
	repo := NewProposalRepository(db)

	p, err := proposal.New("p1", values.NewUntrustedOutput(propEmail), asgT0)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.ChooseFormat(proposal.FormatDocx); err != nil {
		t.Fatal(err)
	}
	if err := p.Validate(propFacts(t), asgT0); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, p); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(ctx, "p1")
	if err != nil || got.AttachmentFormat() != proposal.FormatDocx {
		t.Fatalf("reloaded format = %q, %v", got.AttachmentFormat(), err)
	}
	if err := got.Confirm(asgT0.Add(1)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, got); err != nil {
		t.Fatal(err)
	}
	if again, err := repo.Get(ctx, "p1"); err != nil || again.AttachmentFormat() != proposal.FormatDocx {
		t.Fatalf("after an update: %q, %v", again.AttachmentFormat(), err)
	}

	mustExec(t, db, "UPDATE proposals SET attachment_format = '' WHERE id = 'p1'")
	if legacy, err := repo.Get(ctx, "p1"); err != nil || legacy.AttachmentFormat() != proposal.FormatText {
		t.Fatalf("a send stored before: %q, %v", legacy.AttachmentFormat(), err)
	}
	if _, err := db.sql.ExecContext(ctx, "UPDATE proposals SET attachment_format = 'exe' WHERE id = 'p1'"); !errors.Is(MapError(ctx, err), ErrConstraint) {
		t.Fatalf("unknown format = %v, want a constraint violation", err)
	}

	export := newProposal(t, "p2", propExport, proposal.StatusValidated)
	if err := repo.Save(ctx, export); err != nil {
		t.Fatal(err)
	}
	if e, err := repo.Get(ctx, "p2"); err != nil || e.AttachmentFormat() != "" {
		t.Fatalf("export format = %q, %v", e.AttachmentFormat(), err)
	}
}
