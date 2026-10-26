package sqlite

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/shared"
)

// workSendFacts are trusted facts under which a send of a work validates: its .pptx is filed in «ДЗ».
func workSendFacts(t *testing.T) proposal.Facts {
	t.Helper()
	f := propFacts(t)
	ref, err := shared.NewArtifactRef(shared.ArtifactMaterials, f.Attachment.SHA256())
	if err != nil {
		t.Fatal(err)
	}
	f.AttachmentRef, f.Filed = ref, true
	return f
}

// TASK-180: the send of a work keeps its work and its format; the columns refuse a work on another kind and a send of
// a work without one.
func TestProposalWorkSendRoundTrip(t *testing.T) {
	ctx := context.Background()
	db := openTest(t, nil)
	repo := NewProposalRepository(db)

	p, err := proposal.NewWorkSend("p1", "a1", "w1", proposal.FormatPptx, asgT0)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Validate(workSendFacts(t), asgT0); err != nil || p.Status() != proposal.StatusValidated {
		t.Fatalf("Validate = %v, %s", err, p.Status())
	}
	if err := repo.Save(ctx, p); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(ctx, "p1")
	if err != nil || !reflect.DeepEqual(got.State(), p.State()) || got.WorkID() != "w1" || got.AttachmentFormat() != proposal.FormatPptx {
		t.Fatalf("reloaded %+v, %v", got, err)
	}
	if err := got.Confirm(asgT0.Add(1)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, got); err != nil {
		t.Fatal(err)
	}
	if again, err := repo.Get(ctx, "p1"); err != nil || again.WorkID() != "w1" || again.Status() != proposal.StatusConfirmed {
		t.Fatalf("after an update: %+v, %v", again, err)
	}
	waiting, err := repo.ListByStatus(ctx, proposal.StatusConfirmed, 10)
	if err != nil || len(waiting) != 1 || waiting[0].Kind() != proposal.KindSendWork {
		t.Fatalf("ListByStatus = %v, %v", waiting, err)
	}

	for _, q := range []string{
		"UPDATE proposals SET work_id = '' WHERE id = 'p1'",
		"UPDATE proposals SET attachment_format = 'exe' WHERE id = 'p1'",
		"UPDATE proposals SET work_id = '" + strings.Repeat("w", 129) + "' WHERE id = 'p1'",
	} {
		if _, err := db.sql.ExecContext(ctx, q); !errors.Is(MapError(ctx, err), ErrConstraint) {
			t.Errorf("%s: %v, want a constraint violation", q, err)
		}
	}
	if err := repo.Save(ctx, newProposal(t, "p2", propEmail, proposal.StatusValidated)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.ExecContext(ctx, "UPDATE proposals SET work_id = 'w1' WHERE id = 'p2'"); !errors.Is(MapError(ctx, err), ErrConstraint) {
		t.Fatalf("a work on a homework send: %v, want a constraint violation", err)
	}
}

// Migration 32 rebuilds proposals: the rows stored before survive as they were, and going down drops only the sends
// of a work.
func TestMigrationToVersion32KeepsProposals(t *testing.T) {
	t.Parallel() // uses a database of its own
	ctx := context.Background()
	db := openTest(t, func(o *Options) { o.Migrate = false })
	p, err := db.provider()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.UpTo(ctx, 31); err != nil {
		t.Fatalf("up to 31: %v", err)
	}
	const cols = `(id, kind, status, assignment_id, title, body, due_at, reason, recipient, attachment_sha256,
		attachment_size, assignment_title, attachment_format, confirmed_at, executed_at, version, created_at, updated_at)`
	sha := strings.Repeat("ab", 32)
	mustExec(t, db, "INSERT INTO proposals "+cols+` VALUES
		('p1', 'send_homework_email', 'EXECUTED', 'a1', '', '', NULL, '', 'teacher@school.example', '`+sha+`', 42, '', 'docx', 5, 6, 3, 1, 6),
		('p2', 'send_homework_email', 'VALIDATED', 'a1', '', '', NULL, '', 'teacher@school.example', '`+sha+`', 42, '', '', NULL, NULL, 1, 1, 2),
		('p3', 'create_deadline_event', 'REJECTED', 'a2', 'Rivers', '', 1760983200000, 'unknown_assignment', '', '', 0, '', '', NULL, NULL, 1, 1, 2)`)
	before := proposalRows(t, db)

	if _, err := p.UpTo(ctx, 32); err != nil {
		t.Fatalf("up to 32: %v", err)
	}
	if after := proposalRows(t, db); !reflect.DeepEqual(after, before) {
		t.Fatalf("rows changed by the migration:\nbefore %v\nafter  %v", before, after)
	}
	repo := NewProposalRepository(db)
	for id, want := range map[proposal.ID]proposal.Format{"p1": proposal.FormatDocx, "p2": proposal.FormatText, "p3": ""} {
		got, err := repo.Get(ctx, id)
		if err != nil || got.AttachmentFormat() != want || got.WorkID() != "" {
			t.Fatalf("%s after the migration: %+v, %v", id, got, err)
		}
	}

	w, err := proposal.NewWorkSend("p4", "a1", "w1", proposal.FormatDocx, asgT0)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Validate(workSendFacts(t), asgT0); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, w); err != nil {
		t.Fatal(err)
	}
	if _, err := p.DownTo(ctx, 31); err != nil {
		t.Fatalf("down to 31: %v", err)
	}
	if after := proposalRows(t, db); !reflect.DeepEqual(after, before) {
		t.Fatalf("rows after going down:\nbefore %v\nafter  %v", before, after)
	}
}

// proposalRows is every stored proposal column the migration copies, in id order.
func proposalRows(t *testing.T, db *DB) []string {
	t.Helper()
	rows, err := db.sql.QueryContext(context.Background(), `SELECT id || '|' || kind || '|' || status || '|' || assignment_id || '|' ||
		title || '|' || body || '|' || COALESCE(due_at, 'null') || '|' || reason || '|' || recipient || '|' || attachment_sha256 || '|' ||
		attachment_size || '|' || assignment_title || '|' || attachment_format || '|' || COALESCE(confirmed_at, 'null') || '|' ||
		COALESCE(executed_at, 'null') || '|' || version || '|' || created_at || '|' || updated_at FROM proposals ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}
