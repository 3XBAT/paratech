package tests

// The purge of a deleted assignment racing «Вернуть» on a SQLite file (TASK-274 review): whichever moment the
// assignment stops being due (a restore committed after the listing, or found by any step of the purge transaction),
// it keeps every row and every file.

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/filestore"
	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/delivery/delivery"
	"paratech/desktop/internal/domain/shared"
)

// purgeAt is the purge's own clock: the homework service keeps the test clock, so «Вернуть» still works (a clock set
// back, or a restore that read its time just before the window ended).
type purgeAt struct{ now time.Time }

func (c purgeAt) Now() time.Time { return c.now }

// purgeHooks runs fn before the purge step named step (after it for "list").
type purgeHooks struct {
	step string
	fn   func(ctx context.Context)
}

func (h *purgeHooks) at(ctx context.Context, step string) {
	if h.step == step {
		h.fn(ctx)
	}
}

type hookedAssignments struct {
	*sqlite.AssignmentRepository
	h *purgeHooks
}

func (r hookedAssignments) ListDeletedBefore(ctx context.Context, cutoff, afterAt time.Time, afterID shared.AssignmentID,
	limit int) ([]*assignment.Assignment, error) {
	list, err := r.AssignmentRepository.ListDeletedBefore(ctx, cutoff, afterAt, afterID, limit)
	r.h.at(ctx, "list")
	return list, err
}

func (r hookedAssignments) StillDeletedBefore(ctx context.Context, id shared.AssignmentID, cutoff time.Time) (bool, error) {
	r.h.at(ctx, "re-check")
	return r.AssignmentRepository.StillDeletedBefore(ctx, id, cutoff)
}

func (r hookedAssignments) PurgeDeleted(ctx context.Context, id shared.AssignmentID, cutoff time.Time) (bool, error) {
	r.h.at(ctx, "assignment")
	return r.AssignmentRepository.PurgeDeleted(ctx, id, cutoff)
}

type hookedProposals struct {
	*sqlite.ProposalRepository
	h *purgeHooks
}

func (r hookedProposals) DeleteOfAssignment(ctx context.Context, a shared.AssignmentID, cutoff time.Time) (int, error) {
	r.h.at(ctx, "proposals")
	return r.ProposalRepository.DeleteOfAssignment(ctx, a, cutoff)
}

type hookedDeliveries struct {
	*sqlite.DeliveryRepository
	h *purgeHooks
}

func (r hookedDeliveries) ListByAssignment(ctx context.Context, a shared.AssignmentID, limit int) ([]*delivery.Delivery, error) {
	r.h.at(ctx, "list deliveries")
	return r.DeliveryRepository.ListByAssignment(ctx, a, limit)
}

func (r hookedDeliveries) DeleteFinalOfAssignment(ctx context.Context, a shared.AssignmentID, cutoff time.Time) (int, error) {
	r.h.at(ctx, "deliveries")
	return r.DeliveryRepository.DeleteFinalOfAssignment(ctx, a, cutoff)
}

type hookedDrafts struct {
	*sqlite.DraftRepository
	h *purgeHooks
}

func (r hookedDrafts) DeleteOfAssignment(ctx context.Context, a shared.AssignmentID, cutoff time.Time) (int, error) {
	r.h.at(ctx, "draft")
	return r.DraftRepository.DeleteOfAssignment(ctx, a, cutoff)
}

type hookedWorks struct {
	*sqlite.WorkRepository
	h *purgeHooks
}

func (r hookedWorks) DeleteOfAssignment(ctx context.Context, a shared.AssignmentID, cutoff time.Time) (int, error) {
	r.h.at(ctx, "works")
	return r.WorkRepository.DeleteOfAssignment(ctx, a, cutoff)
}

func TestPurgeKeepsEverythingOfAnAssignmentNoLongerDue(t *testing.T) {
	for _, step := range []string{"list", "re-check", "list deliveries", "proposals", "deliveries", "draft", "works", "assignment"} {
		t.Run(step, func(t *testing.T) {
			e := newAssignmentEnv(t)
			ctx := t.Context()
			files, err := filestore.New(filepath.Join(t.TempDir(), "artifacts"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = files.Close() })

			v, err := e.homework.CreateAssignment(ctx, homeworkflow.NewAssignment{Title: "Эссе"})
			if err != nil {
				t.Fatal(err)
			}
			purgeLinks(t, e.db, files, v.ID)
			d, err := e.homework.DeleteAssignment(ctx, v.ID, v.Version)
			if err != nil {
				t.Fatal(err)
			}
			before := mentions(t, e.db, v.ID)

			h := &purgeHooks{step: step}
			restored := false
			if step == "list" {
				// «Вернуть» commits between the listing and the purge transaction.
				h.fn = func(context.Context) {
					if _, err := e.homework.RestoreAssignment(context.Background(), v.ID, d.Version); err != nil {
						t.Errorf("RestoreAssignment: %v", err)
					}
					restored = true
				}
			} else {
				// Inside the transaction (a restore cannot get in: the write lock): the step's own guard sees it.
				h.fn = func(ctx context.Context) {
					if _, err := e.db.DBTX(ctx).ExecContext(ctx, "UPDATE assignments SET deleted_at = NULL WHERE id = ?", string(v.ID)); err != nil {
						t.Errorf("restore in the transaction: %v", err)
					}
				}
			}
			purger, err := homeworkflow.NewPurger(homeworkflow.PurgeDeps{
				Assignments: hookedAssignments{sqlite.NewAssignmentRepository(e.db), h},
				Proposals:   hookedProposals{sqlite.NewProposalRepository(e.db), h},
				Deliveries:  hookedDeliveries{sqlite.NewDeliveryRepository(e.db), h},
				Drafts:      hookedDrafts{sqlite.NewDraftRepository(e.db), h},
				Works:       hookedWorks{sqlite.NewWorkRepository(e.db), h},
				Files:       files, Clock: purgeAt{e.clock.Now().Add(assignment.RestoreWindow + time.Minute)}, Tx: e.db,
				Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
			})
			if err != nil {
				t.Fatal(err)
			}

			if n, err := purger.PurgeDeleted(ctx); err != nil || n != 0 {
				t.Fatalf("PurgeDeleted = %d, %v; want 0, nil", n, err)
			}
			after := mentions(t, e.db, v.ID)
			for table, n := range before {
				if after[table] != n {
					t.Errorf("%s: %d rows, was %d", table, after[table], n)
				}
			}
			for _, path := range []string{string(v.ID) + "/homework.md", "sent/p-" + string(v.ID) + "/работа.docx"} {
				ref, err := shared.NewArtifactRef(shared.ArtifactExports, path)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := files.Size(ctx, ref); err != nil {
					t.Errorf("file %s: %v, want kept", path, err)
				}
			}

			// The committed restore stays; the one inside the purge transaction went with its rollback.
			var live bool
			if err := e.db.DBTX(ctx).QueryRowContext(ctx, "SELECT deleted_at IS NULL FROM assignments WHERE id = ?",
				string(v.ID)).Scan(&live); err != nil {
				t.Fatal(err)
			}
			if live != restored {
				t.Fatalf("live = %v, want %v", live, restored)
			}
		})
	}
}
