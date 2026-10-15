package sqlite

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/shared"
)

// Model answers of the three kinds; the due time is in range of asgT0.
const (
	propEmail  = `{"kind":"send_homework_email","assignment_id":"a1"}`
	propExport = `{"kind":"export_homework","assignment_id":"a1","body":"Exercises 1-5\non page 42."}`
	propEvent  = `{"kind":"create_deadline_event","assignment_id":"a1","title":"Maths homework","due":"2026-10-20T18:00:00Z"}`
)

// propFacts are trusted facts under which every kind validates.
func propFacts(t *testing.T) proposal.Facts {
	t.Helper()
	r, err := proposal.NewRecipient("teacher@school.example")
	if err != nil {
		t.Fatal(err)
	}
	d, err := proposal.NewDigest(strings.Repeat("ab", 32), 42)
	if err != nil {
		t.Fatal(err)
	}
	return proposal.Facts{AssignmentExists: true, AssignmentTitle: "Rivers", Recipient: r, Attachment: d}
}

// newProposal parses answer and brings it to want through the domain methods, one minute per step.
func newProposal(t *testing.T, id proposal.ID, answer string, want proposal.Status) *proposal.Proposal {
	t.Helper()
	at := asgT0
	step := func() time.Time { at = at.Add(time.Minute); return at }

	p, err := proposal.New(id, values.NewUntrustedOutput(answer), at)
	if err != nil {
		t.Fatal(err)
	}
	facts := propFacts(t)
	if want == proposal.StatusRejected {
		facts.AssignmentExists = false
	}
	if err := p.Validate(facts, step()); err != nil {
		t.Fatal(err)
	}

	switch want {
	case proposal.StatusDeclined:
		err = p.Decline(step())
	case proposal.StatusConfirmed:
		err = p.Confirm(step())
	case proposal.StatusExecuted:
		if err = p.Confirm(step()); err == nil {
			err = p.MarkExecuted(step())
		}
	case proposal.StatusStale:
		if err = p.Confirm(step()); err == nil {
			if rerr := p.Recheck(proposal.Facts{}, step()); !errors.Is(rerr, proposal.ErrStale) {
				err = rerr
			}
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	if p.Status() != want {
		t.Fatalf("test bug: proposal is %s, want %s", p.Status(), want)
	}
	return p
}

func TestProposalSaveGet(t *testing.T) {
	cases := []struct {
		name   string
		answer string
		status proposal.Status
	}{
		{"validated email", propEmail, proposal.StatusValidated},
		{"validated export", propExport, proposal.StatusValidated},
		{"validated event", propEvent, proposal.StatusValidated},
		{"rejected", propEvent, proposal.StatusRejected},
		{"declined", propExport, proposal.StatusDeclined},
		{"confirmed email", propEmail, proposal.StatusConfirmed},
		{"executed export", propExport, proposal.StatusExecuted},
		{"stale email", propEmail, proposal.StatusStale},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			repo := NewProposalRepository(openTest(t, nil))
			p := newProposal(t, "p1", tc.answer, tc.status)
			if err := repo.Save(ctx, p); err != nil || p.Version() != 1 {
				t.Fatalf("Save = %v, version %d; want nil, 1", err, p.Version())
			}

			got, err := repo.Get(ctx, "p1")
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got.State(), p.State()) {
				t.Fatalf("reloaded proposal:\n got %+v\nwant %+v", got.State(), p.State())
			}

			// Saving the same instance again works (ADR-0004(d)).
			if err := repo.Save(ctx, got); err != nil || got.Version() != 2 {
				t.Fatalf("second save = %v, version %d", err, got.Version())
			}
		})
	}
}

// The transitions after the first save reach the row: validated, confirmed, executed.
func TestProposalSaveFollowsTheStatus(t *testing.T) {
	ctx := context.Background()
	repo := NewProposalRepository(openTest(t, nil))
	p := newProposal(t, "p1", propEmail, proposal.StatusValidated)
	if err := repo.Save(ctx, p); err != nil {
		t.Fatal(err)
	}

	at := asgT0.Add(time.Hour)
	for _, apply := range []func(time.Time) error{p.Confirm, p.MarkExecuted} {
		at = at.Add(time.Minute)
		if err := apply(at); err != nil {
			t.Fatal(err)
		}
		if err := repo.Save(ctx, p); err != nil {
			t.Fatal(err)
		}
		got, err := repo.Get(ctx, "p1")
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got.State(), p.State()) {
			t.Fatalf("reloaded proposal:\n got %+v\nwant %+v", got.State(), p.State())
		}
	}
	if p.Version() != 3 {
		t.Fatalf("version %d after three saves", p.Version())
	}
}

func TestProposalGetMissing(t *testing.T) {
	repo := NewProposalRepository(openTest(t, nil))
	if _, err := repo.Get(context.Background(), "nope"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("Get of no proposal = %v, want ErrNotFound", err)
	}
}

func TestProposalSaveConflicts(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name   string
		setup  func(t *testing.T, db *DB, repo *ProposalRepository) *proposal.Proposal
		status proposal.Status // stored afterwards; "" for no row
	}{
		{"existing id", func(t *testing.T, _ *DB, repo *ProposalRepository) *proposal.Proposal {
			if err := repo.Save(ctx, newProposal(t, "p1", propEmail, proposal.StatusValidated)); err != nil {
				t.Fatal(err)
			}
			return newProposal(t, "p1", propEmail, proposal.StatusValidated)
		}, proposal.StatusValidated},
		{"stale version", func(t *testing.T, _ *DB, repo *ProposalRepository) *proposal.Proposal {
			if err := repo.Save(ctx, newProposal(t, "p1", propEmail, proposal.StatusValidated)); err != nil {
				t.Fatal(err)
			}
			winner, err := repo.Get(ctx, "p1")
			if err != nil {
				t.Fatal(err)
			}
			loser, err := repo.Get(ctx, "p1")
			if err != nil {
				t.Fatal(err)
			}
			if err := winner.Decline(asgT0.Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
			if err := repo.Save(ctx, winner); err != nil {
				t.Fatal(err)
			}
			if err := loser.Confirm(asgT0.Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
			return loser
		}, proposal.StatusDeclined},
		{"purged row", func(t *testing.T, db *DB, repo *ProposalRepository) *proposal.Proposal {
			p := newProposal(t, "p1", propEmail, proposal.StatusValidated)
			if err := repo.Save(ctx, p); err != nil {
				t.Fatal(err)
			}
			mustExec(t, db, "DELETE FROM proposals WHERE id = 'p1'")
			return p
		}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := openTest(t, nil)
			repo := NewProposalRepository(db)
			p := tc.setup(t, db, repo)
			before := p.Version()
			if err := repo.Save(ctx, p); !errors.Is(err, shared.ErrVersionConflict) {
				t.Fatalf("Save = %v, want ErrVersionConflict", err)
			}
			if p.Version() != before {
				t.Fatalf("a refused save moved the version to %d", p.Version())
			}

			got, err := repo.Get(ctx, "p1")
			switch {
			case tc.status == "":
				if !errors.Is(err, shared.ErrNotFound) {
					t.Fatalf("a purged proposal came back: %v", err)
				}
			case err != nil || got.Status() != tc.status:
				t.Fatalf("stored proposal %v, %v; want %s", got, err, tc.status)
			}
		})
	}
}

// The schema refuses rows the aggregate cannot have, and a row that passes the schema but not Restore is refused when
// loaded, so a tampered row cannot produce a confirmed command.
func TestProposalRowsAreChecked(t *testing.T) {
	ctx := context.Background()
	const insert = `INSERT INTO proposals (id, kind, status, assignment_id, reason, recipient, attachment_sha256,
		confirmed_at, executed_at, due_at, version, created_at, updated_at) VALUES `
	refused := []struct{ name, values string }{
		{"unknown kind", `('p1', 'send_email', 'VALIDATED', 'a1', '', '', '', NULL, NULL, NULL, 1, 0, 0)`},
		{"unknown status", `('p1', 'export_homework', 'DONE', 'a1', '', '', '', NULL, NULL, NULL, 1, 0, 0)`},
		{"no assignment", `('p1', 'export_homework', 'VALIDATED', '', '', '', '', NULL, NULL, NULL, 1, 0, 0)`},
		{"empty id", `('', 'export_homework', 'VALIDATED', 'a1', '', '', '', NULL, NULL, NULL, 1, 0, 0)`},
		{"rejected without a reason", `('p1', 'export_homework', 'REJECTED', 'a1', '', '', '', NULL, NULL, NULL, 1, 0, 0)`},
		{"validated with a reason", `('p1', 'export_homework', 'VALIDATED', 'a1', 'unknown_assignment', '', '', NULL, NULL, NULL, 1, 0, 0)`},
		{"confirmed without the time", `('p1', 'export_homework', 'CONFIRMED', 'a1', '', '', '', NULL, NULL, NULL, 1, 0, 0)`},
		{"validated with a confirmation", `('p1', 'export_homework', 'VALIDATED', 'a1', '', '', '', 5, NULL, NULL, 1, 0, 0)`},
		{"executed without the time", `('p1', 'export_homework', 'EXECUTED', 'a1', '', '', '', 5, NULL, NULL, 1, 0, 0)`},
		{"short digest", `('p1', 'send_homework_email', 'VALIDATED', 'a1', '', 'a@b.cd', 'abc', NULL, NULL, NULL, 1, 0, 0)`},
		{"deadline without a due", `('p1', 'create_deadline_event', 'PROPOSED', 'a1', '', '', '', NULL, NULL, NULL, 1, 0, 0)`},
		{"export with a due", `('p1', 'export_homework', 'PROPOSED', 'a1', '', '', '', NULL, NULL, 5, 1, 0, 0)`},
	}
	for _, tc := range refused {
		t.Run(tc.name, func(t *testing.T) {
			db := openTest(t, nil)
			_, err := db.sql.ExecContext(ctx, insert+tc.values)
			if !errors.Is(MapError(ctx, err), ErrConstraint) {
				t.Fatalf("insert = %v, want a constraint violation", err)
			}
		})
	}

	t.Run("confirmed email without its recipient", func(t *testing.T) {
		db := openTest(t, nil)
		repo := NewProposalRepository(db)
		if err := repo.Save(ctx, newProposal(t, "p1", propEmail, proposal.StatusConfirmed)); err != nil {
			t.Fatal(err)
		}
		mustExec(t, db, "UPDATE proposals SET recipient = '' WHERE id = 'p1'")
		if _, err := repo.Get(ctx, "p1"); !errors.Is(err, proposal.ErrInvalidProposal) {
			t.Fatalf("Get of a tampered row = %v, want ErrInvalidProposal", err)
		}
	})
}

// A proposal for an assignment that does not exist is stored REJECTED: there is no foreign key to refuse it, and
// deleting an assignment is not blocked by its proposals.
func TestProposalHasNoAssignmentForeignKey(t *testing.T) {
	ctx := context.Background()
	e := newCourseworkEnv(t)
	repo := NewProposalRepository(e.db)
	if err := repo.Save(ctx, newProposal(t, "p1", propEvent, proposal.StatusRejected)); err != nil {
		t.Fatalf("Save of a proposal of an unknown assignment = %v", err)
	}

	a := newExtracted(t, "a1", "t1", 0, time.Time{})
	if err := e.assignments.Save(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, newProposal(t, "p2", propExport, proposal.StatusExecuted)); err != nil {
		t.Fatal(err)
	}
	if err := e.assignments.Delete(ctx, a); err != nil {
		t.Fatalf("Delete of an assignment with proposals = %v", err)
	}
	if got, err := repo.Get(ctx, "p2"); err != nil || got.Status() != proposal.StatusExecuted {
		t.Fatalf("executed proposal after the delete: %v, %v", got, err)
	}
}
