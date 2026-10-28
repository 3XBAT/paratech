package tests

// The purge of a deleted assignment after the restore window (TASK-274, ADR-0029 addendum 2) end to end on a SQLite
// file and the Storage Manager: the agent adds an assignment, it gets every kind of link, the student deletes it, three
// days pass; «Вернуть» is expired, and the purge leaves no row naming it anywhere but the agent journal and the waiting
// Google removal. A deleted assignment still inside its window and a live one keep everything.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/filestore"
	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

// purgeLinks stores a link of every kind of assignment id through SQL: a deadline proposal, a SENT and a FAILED email
// attaching the text export and a copy of a work's file, a draft with two versions, a work with a round; and the two
// files below exports.
func purgeLinks(t *testing.T, db *sqlite.DB, files *filestore.Store, id shared.AssignmentID) {
	t.Helper()
	ctx := context.Background()
	s := string(id)
	digest := strings.Repeat("c", 64)
	for _, st := range []struct {
		q    string
		args []any
	}{
		{`INSERT INTO proposals (id, kind, status, assignment_id, title, due_at, created_at, updated_at)
		  VALUES (?, 'create_deadline_event', 'PROPOSED', ?, 'Срок', 1, 1, 1)`, []any{"p-" + s, s}},
		{`INSERT INTO deliveries (id, assignment_id, recipient, subject, artifact_ref, message_id, idempotency_key, status,
		    attempts, confirmed_at, sent_at, version, created_at, updated_at, attachment_sha256, attachment_size)
		  VALUES (?, ?, 'teacher@example.test', 'ДЗ', ?, ?, ?, 'SENT', 1, 1, 1, 1, 1, 1, ?, 2)`,
			[]any{"d1-" + s, s, "exports/" + s + "/homework.md", "<d1-" + s + "@paratech.invalid>", "k1-" + s, digest}},
		{`INSERT INTO deliveries (id, assignment_id, recipient, subject, artifact_ref, message_id, idempotency_key, status,
		    attempts, confirmed_at, version, created_at, updated_at, attachment_sha256, attachment_size)
		  VALUES (?, ?, 'teacher@example.test', 'ДЗ', ?, ?, ?, 'FAILED', 1, 1, 1, 1, 1, ?, 2)`,
			[]any{"d2-" + s, s, "exports/sent/p-" + s + "/работа.docx", "<d2-" + s + "@paratech.invalid>", "k2-" + s, digest}},
		{`INSERT INTO homework_drafts (assignment_id, version) VALUES (?, 1)`, []any{s}},
		{`INSERT INTO homework_draft_revisions (assignment_id, number, origin, title, body, created_at)
		  VALUES (?, 1, 'edited', 'Черновик', 'Текст', 1), (?, 2, 'edited', 'Черновик', 'Текст 2', 1)`, []any{s, s}},
		{`INSERT INTO works (id, assignment_id, status, round, attempt, version, created_at, updated_at)
		  VALUES (?, ?, 'done', 1, 1, 1, 1, 1)`, []any{"w-" + s, s}},
		{`INSERT INTO work_rounds (work_id, round, answer) VALUES (?, 1, '{}')`, []any{"w-" + s}},
	} {
		if _, err := db.DBTX(ctx).ExecContext(ctx, st.q, st.args...); err != nil {
			t.Fatalf("seed %s: %v", s, err)
		}
	}
	for _, path := range []string{s + "/homework.md", "sent/p-" + s + "/работа.docx"} {
		ref, err := shared.NewArtifactRef(shared.ArtifactExports, path)
		if err != nil {
			t.Fatal(err)
		}
		if err := files.Write(ctx, ref, strings.NewReader("ДЗ")); err != nil {
			t.Fatal(err)
		}
	}
}

// mentions counts, table by table, the rows whose any column holds id (JSON payloads too).
func mentions(t *testing.T, db *sqlite.DB, id shared.AssignmentID) map[string]int {
	t.Helper()
	ctx := context.Background()
	q := db.DBTX(ctx)
	rows, err := q.QueryContext(ctx,
		`SELECT m.name, p.name FROM sqlite_master m JOIN pragma_table_info(m.name) p
		 WHERE m.type = 'table' AND m.name NOT LIKE 'sqlite_%' AND m.name <> 'goose_db_version' ORDER BY m.name, p.cid`)
	if err != nil {
		t.Fatal(err)
	}
	cols := map[string][]string{}
	for rows.Next() {
		var table, col string
		if err := rows.Scan(&table, &col); err != nil {
			t.Fatal(err)
		}
		cols[table] = append(cols[table], col)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if len(cols) < 20 {
		t.Fatalf("only %d tables found", len(cols))
	}

	out := map[string]int{}
	for table, cs := range cols {
		var conds []string
		for _, c := range cs {
			conds = append(conds, fmt.Sprintf(`instr(CAST("%s" AS TEXT), ?1) > 0`, c))
		}
		var n int
		// Static names from sqlite_master of the test's own database; the value is bound.
		query := fmt.Sprintf(`SELECT count(*) FROM "%s" WHERE %s`, table, strings.Join(conds, " OR "))
		if err := q.QueryRowContext(ctx, query, string(id)).Scan(&n); err != nil {
			t.Fatalf("%s: %v", table, err)
		}
		if n > 0 {
			out[table] = n
		}
	}
	return out
}

func TestPurgeOfADeletedAssignmentOnSQLite(t *testing.T) {
	e := newAssignmentEnv(t)
	ctx := t.Context()
	files, err := filestore.New(filepath.Join(t.TempDir(), "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = files.Close() })

	// The agent adds the assignment: a journal line names its ID.
	e.hub.Then(testenv.AgentCall{ID: "c1", Tool: "create_assignment", Args: `{"title":"Эссе","due":"2026-10-30"}`}).Then(endCall("e1"))
	e.ask(t, "запиши эссе к 30 октября")
	gone := addedAssignmentID("th1", 1, "c1")
	recent, err := e.homework.CreateAssignment(ctx, homeworkflow.NewAssignment{Title: "Реферат"})
	if err != nil {
		t.Fatal(err)
	}
	live, err := e.homework.CreateAssignment(ctx, homeworkflow.NewAssignment{Title: "Лабораторная"})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []shared.AssignmentID{gone, recent.ID, live.ID} {
		purgeLinks(t, e.db, files, id)
	}

	del := func(id shared.AssignmentID) int64 {
		t.Helper()
		v, err := e.homework.Assignment(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		d, err := e.homework.DeleteAssignment(ctx, id, v.Version)
		if err != nil {
			t.Fatalf("DeleteAssignment %s: %v", id, err)
		}
		return d.Version
	}
	deletedVersion := del(gone)
	// The deadline event removal still waits for the calendar sync (no sign-in here): it stays.
	if err := e.db.Do(ctx, func(ctx context.Context) error {
		return sqlite.NewDeadlineEventRemovals(e.db).QueueDeadlineRemoval(ctx, gone, e.clock.Now())
	}); err != nil {
		t.Fatal(err)
	}
	e.clock.Advance(2 * 24 * time.Hour)
	del(recent.ID)
	e.clock.Advance(24*time.Hour + time.Minute) // gone: 3 days and a minute ago; recent: a day ago

	if _, err := e.homework.RestoreAssignment(ctx, gone, deletedVersion); !errors.Is(err, assignment.ErrRestoreExpired) {
		t.Fatalf("«Вернуть» after the window: err = %v, want ErrRestoreExpired", err)
	}

	before := map[shared.AssignmentID]map[string]int{recent.ID: mentions(t, e.db, recent.ID), live.ID: mentions(t, e.db, live.ID)}
	linked := mentions(t, e.db, gone)
	for _, table := range []string{"assignments", "proposals", "deliveries", "homework_drafts", "homework_draft_revisions",
		"works", "work_rounds", "deadline_event_removals", "agent_actions"} {
		if linked[table] == 0 {
			t.Fatalf("fixture: no %s row names the assignment before the purge: %v", table, linked)
		}
	}
	purger, err := homeworkflow.NewPurger(homeworkflow.PurgeDeps{
		Assignments: sqlite.NewAssignmentRepository(e.db), Proposals: sqlite.NewProposalRepository(e.db),
		Deliveries: sqlite.NewDeliveryRepository(e.db), Drafts: sqlite.NewDraftRepository(e.db),
		Works: sqlite.NewWorkRepository(e.db), Files: files, Clock: e.clock, Tx: e.db,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if n, err := purger.PurgeDeleted(ctx); err != nil || n != 1 {
		t.Fatalf("PurgeDeleted = %d, %v; want 1, nil", n, err)
	}

	// Nothing names it any more but the agent's own tables (the journal line and the thread) and the waiting removal.
	left := mentions(t, e.db, gone)
	for table, n := range left {
		if !strings.HasPrefix(table, "agent_") && table != "deadline_event_removals" {
			t.Errorf("%s: %d rows still name the purged assignment", table, n)
		}
	}
	if left["deadline_event_removals"] != 1 {
		t.Errorf("the waiting deadline removal is gone: %v", left)
	}
	if left["agent_actions"] == 0 {
		t.Errorf("the journal line is gone: %v", left)
	}
	for _, path := range []string{string(gone) + "/homework.md", "sent/p-" + string(gone) + "/работа.docx"} {
		ref, err := shared.NewArtifactRef(shared.ArtifactExports, path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := files.Size(ctx, ref); !errors.Is(err, shared.ErrNotFound) {
			t.Errorf("file %s: err = %v, want deleted", path, err)
		}
	}

	// The others are untouched, with their files.
	for id, want := range before {
		got := mentions(t, e.db, id)
		for table, n := range want {
			if got[table] != n {
				t.Errorf("%s: %s = %d rows, was %d", id, table, got[table], n)
			}
		}
		ref, err := proposal.ExportRef(id)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := files.Size(ctx, ref); err != nil {
			t.Errorf("%s: export file: %v", id, err)
		}
	}

	// A purged assignment is not found; the Undo of the agent's creation brings nothing back.
	if _, err := e.homework.RestoreAssignment(ctx, gone, deletedVersion); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("«Вернуть» after the purge: err = %v, want ErrNotFound", err)
	}
	entry, err := e.journal.Find(ctx, "th1", 1, "c1")
	if err != nil {
		t.Fatalf("journal line after the purge: %v", err)
	}
	if u, err := e.journal.Undo(ctx, entry.ID); err != nil || u.Status != action.StatusUndone {
		t.Fatalf("Undo after the purge = %+v, %v; want UNDONE with nothing applied", u, err)
	}
	if n := mentions(t, e.db, gone)["assignments"]; n != 0 {
		t.Fatal("the Undo brought the purged assignment back")
	}

	// A second run finds nothing.
	if n, err := purger.PurgeDeleted(ctx); err != nil || n != 0 {
		t.Fatalf("second run = %d, %v", n, err)
	}
}
