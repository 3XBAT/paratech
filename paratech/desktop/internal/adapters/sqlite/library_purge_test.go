package sqlite

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/shared"
)

// purgeExtras adds to removalLibrary what the purge of s1 must and must not touch: lesson l1 «Физика» with recording
// r1, an assignment a1 and a summary made from m2, m2 linked to l1 with an inspection and a work, a send of a work that
// pinned the bytes of m1, a teacher of s1, and the ticket sets ts1 of s1 and ts2 of s2.
func purgeExtras(t *testing.T, db *DB) {
	t.Helper()
	for _, q := range []string{
		`INSERT INTO schedule_entries (id, title, starts_at, ends_at, timezone, subject, created_at, updated_at)
			VALUES ('l1', 'Лекция', 1000, 2000, 'Europe/Moscow', 'Физика', 1, 1)`,
		`INSERT INTO recordings (id, lesson_id, status, started_at, max_duration_ms, chunks_ref, lesson_subject, created_at, updated_at)
			VALUES ('r1', 'l1', 'FAILED', 1000, 10800000, 'recordings/r1', 'Физика', 1, 1)`,
		`INSERT INTO assignments (id, lesson_id, title, material_id, created_at, updated_at)
			VALUES ('a1', 'l1', 'Задачи 1-5', 'm2', 1, 1)`,
		`INSERT INTO summaries (id, body, prompt_version, material_id, created_at, updated_at)
			VALUES ('material-m2', 'Конспект', 1, 'm2', 1, 1)`,
		`INSERT INTO material_lessons (material_id, lesson_id, position) VALUES ('m2', 'l1', 0)`,
		`INSERT INTO material_inspections (material_id, text_sha256, status, attempt, created_at, updated_at)
			VALUES ('m2', '` + strings.Repeat("a", 64) + `', 'refused', 1, 1, 1)`,
		`INSERT INTO works (id, material_id, status, round, attempt, version, created_at, updated_at)
			VALUES ('w1', 'm2', 'done', 1, 1, 1, 1, 1)`,
		`INSERT INTO proposals (id, kind, status, assignment_id, work_id, attachment_sha256, attachment_size, created_at, updated_at)
			VALUES ('p1', 'send_work_email', 'VALIDATED', 'a1', 'w0', '` + string(material.DigestOf([]byte("content of m1"))) + `', 13, 1, 1)`,
		`INSERT INTO subject_teachers (subject_id, role, name, email) VALUES ('s1', 'lecture', 'Иванов И. И.', 'teacher@example.test')`,
		`INSERT INTO ticket_sets (id, subject_id, status, content, version, created_at, updated_at) VALUES ('ts1', 's1', 'done', '{}', 1, 1, 1)`,
		`INSERT INTO ticket_sets (id, subject_id, status, content, version, created_at, updated_at) VALUES ('ts2', 's2', 'done', '{}', 1, 1, 1)`,
	} {
		mustExec(t, db, q)
	}
}

// Step by step as subjectpurge runs it: nothing goes before the cutoff passes the removal; then the notes, the ticket
// sets, the files (one held by a note of s2 refuses, one moved to the inbox), the subject row last. The lesson, the
// recording, the assignment and the summary stay.
func TestPurgeOfARemovedSubjectOnSQLite(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	removalLibrary(t, db)
	purgeExtras(t, db)
	subjects, materials, notes, sets := NewSubjectRepository(db), NewMaterialRepository(db), NewNoteRepository(db), NewSubjectTicketSets(db)
	removedAt := libT0.Add(time.Hour)
	setRemoved(t, subjects, removedAt)

	noteIDs, err := notes.ListIDsBySubject(ctx, "s1")
	if err != nil || !slices.Equal(noteIDs, []shared.NoteID{"n1", "n1b"}) {
		t.Fatalf("ListIDsBySubject = %v, %v", noteIDs, err)
	}
	setIDs, err := sets.ListOfSubject(ctx, "s1")
	if err != nil || !slices.Equal(setIDs, []shared.TicketSetID{"ts1"}) {
		t.Fatalf("ListOfSubject = %v, %v", setIDs, err)
	}
	filed, err := materials.ListFiledUnder(ctx, "s1")
	if err != nil || len(filed) != 3 {
		t.Fatalf("ListFiledUnder = %d, %v; want m1 m2 m3, also hidden", len(filed), err)
	}

	// A cutoff a moment before the removal: every delete refuses.
	early := removedAt.Add(-time.Millisecond)
	for name, purge := range map[string]func() (bool, error){
		"note":       func() (bool, error) { return notes.PurgeOfRemovedSubject(ctx, "n1", "s1", early) },
		"ticket set": func() (bool, error) { return sets.PurgeOfRemovedSubject(ctx, "ts1", "s1", early) },
		"material":   func() (bool, error) { return materials.PurgeFiledUnderRemoved(ctx, "m2", "s1", early) },
		"subject":    func() (bool, error) { return subjects.PurgeRemoved(ctx, "s1", early) },
	} {
		if done, err := purge(); err != nil || done {
			t.Fatalf("%s purge before the cutoff = %v, %v", name, done, err)
		}
	}

	cutoff := removedAt
	for _, id := range noteIDs {
		if done, err := notes.PurgeOfRemovedSubject(ctx, id, "s1", cutoff); err != nil || !done {
			t.Fatalf("purge note %s = %v, %v", id, done, err)
		}
	}
	if done, err := notes.PurgeOfRemovedSubject(ctx, "n2", "s1", cutoff); err != nil || done {
		t.Fatalf("purge of a note of another subject = %v, %v", done, err)
	}
	if done, err := sets.PurgeOfRemovedSubject(ctx, "ts1", "s1", cutoff); err != nil || !done {
		t.Fatalf("purge ticket set = %v, %v", done, err)
	}

	// m3 is a part of the note n2 of s2; m1 and m2 are held by no note now.
	for id, want := range map[shared.MaterialID]bool{"m1": false, "m2": false, "m3": true} {
		if held, err := materials.PartOfOtherNote(ctx, id, "s1"); err != nil || held != want {
			t.Fatalf("PartOfOtherNote(%s) = %v, %v; want %v", id, held, err, want)
		}
	}
	// The bytes of m1 are pinned by a send not yet carried out; those of m2 by nothing.
	m1, m2 := material.DigestOf([]byte("content of m1")), material.DigestOf([]byte("content of m2"))
	if used, err := materials.DigestInUse(ctx, m1, "m1"); err != nil || !used {
		t.Fatalf("DigestInUse(m1, pinned) = %v, %v", used, err)
	}
	if used, err := materials.DigestInUse(ctx, m2, "m2"); err != nil || used {
		t.Fatalf("DigestInUse(m2) = %v, %v", used, err)
	}
	if used, err := materials.DigestInUse(ctx, m2, "m1"); err != nil || !used {
		t.Fatalf("DigestInUse(m2 except another material) = %v, %v", used, err)
	}
	mustExec(t, db, `UPDATE proposals SET status = 'EXECUTED', confirmed_at = 2, executed_at = 2 WHERE id = 'p1'`)
	if used, err := materials.DigestInUse(ctx, m1, "m1"); err != nil || used {
		t.Fatalf("DigestInUse(m1, send carried out) = %v, %v", used, err)
	}

	// m3 refuses (held by n2), m2 goes with its links, inspection and work; its assignment and summary stay.
	if done, err := materials.PurgeFiledUnderRemoved(ctx, "m3", "s1", cutoff); err != nil || done {
		t.Fatalf("purge of a material held by a note = %v, %v", done, err)
	}
	if done, err := materials.PurgeFiledUnderRemoved(ctx, "m2", "s1", cutoff); err != nil || !done {
		t.Fatalf("purge m2 = %v, %v", done, err)
	}
	if done, err := materials.PurgeFiledUnderRemoved(ctx, "m4", "s1", cutoff); err != nil || done {
		t.Fatalf("purge of a material in the inbox = %v, %v", done, err)
	}

	// The subject row refuses while files are under it.
	if _, err := subjects.PurgeRemoved(ctx, "s1", cutoff); !errors.Is(err, shared.ErrConflict) {
		t.Fatalf("PurgeRemoved with files under it = %v, want ErrConflict", err)
	}
	m3, err := materials.GetByDigest(ctx, material.DigestOf([]byte("content of m3")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m3.MoveToInbox(libT0.Add(4 * 24 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := materials.Save(ctx, m3); err != nil {
		t.Fatal(err)
	}
	if done, err := materials.PurgeFiledUnderRemoved(ctx, "m1", "s1", cutoff); err != nil || !done {
		t.Fatalf("purge m1 = %v, %v", done, err)
	}
	if done, err := subjects.PurgeRemoved(ctx, "s1", cutoff); err != nil || !done {
		t.Fatalf("PurgeRemoved = %v, %v", done, err)
	}
	if done, err := subjects.PurgeRemoved(ctx, "s1", cutoff); err != nil || done {
		t.Fatalf("PurgeRemoved again = %v, %v; want nothing left", done, err)
	}

	for q, want := range map[string]int64{
		"SELECT count(*) FROM subjects WHERE id = 's1'":                                   0,
		"SELECT count(*) FROM subject_aliases WHERE subject_id = 's1'":                    0,
		"SELECT count(*) FROM subject_folders WHERE subject_id = 's1'":                    0,
		"SELECT count(*) FROM subject_teachers WHERE subject_id = 's1'":                   0,
		"SELECT count(*) FROM notes WHERE subject_id = 's1'":                              0,
		"SELECT count(*) FROM note_parts WHERE note_id IN ('n1', 'n1b')":                  0,
		"SELECT count(*) FROM materials WHERE subject_id = 's1' OR id IN ('m1', 'm2')":    0,
		"SELECT count(*) FROM material_lessons WHERE material_id = 'm2'":                  0,
		"SELECT count(*) FROM material_inspections WHERE material_id = 'm2'":              0,
		"SELECT count(*) FROM works WHERE id = 'w1'":                                      0,
		"SELECT count(*) FROM ticket_sets WHERE subject_id = 's1'":                        0,
		"SELECT count(*) FROM ticket_sets WHERE id = 'ts2'":                               1,
		"SELECT count(*) FROM materials WHERE id = 'm3' AND subject_id IS NULL":           1,
		"SELECT count(*) FROM notes WHERE id = 'n2'":                                      1,
		"SELECT count(*) FROM subjects WHERE id = 's2'":                                   1,
		"SELECT count(*) FROM schedule_entries WHERE id = 'l1' AND deleted_at IS NULL":    1,
		"SELECT count(*) FROM recordings WHERE id = 'r1'":                                 1,
		"SELECT count(*) FROM assignments WHERE id = 'a1' AND material_id IS NULL":        1,
		"SELECT count(*) FROM summaries WHERE id = 'material-m2' AND material_id IS NULL": 1,
		"SELECT count(*) FROM proposals WHERE id = 'p1'":                                  1,
	} {
		if n := scanInt(t, db.sql, q); n != want {
			t.Errorf("%s = %d, want %d", q, n, want)
		}
	}

	// The name is free again.
	if err := subjects.Save(ctx, newLibSubject(t, "s5", "Физика", "Физ")); err != nil {
		t.Fatalf("Save of the purged name = %v", err)
	}
}

// A subject restored before the purge reached its row keeps it, whatever the cutoff.
func TestPurgeKeepsARestoredSubjectOnSQLite(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	removalLibrary(t, db)
	subjects, notes := NewSubjectRepository(db), NewNoteRepository(db)
	setRemoved(t, subjects, libT0.Add(time.Hour))
	setRemoved(t, subjects, time.Time{})

	late := libT0.Add(30 * 24 * time.Hour)
	if done, err := notes.PurgeOfRemovedSubject(ctx, "n1", "s1", late); err != nil || done {
		t.Fatalf("purge a note of a restored subject = %v, %v", done, err)
	}
	if done, err := subjects.PurgeRemoved(ctx, "s1", late); err != nil || done {
		t.Fatalf("PurgeRemoved of a restored subject = %v, %v", done, err)
	}
	if done, err := subjects.PurgeRemoved(ctx, "s2", late); err != nil || done {
		t.Fatalf("PurgeRemoved of an active subject = %v, %v", done, err)
	}
	if n := scanInt(t, db.sql, "SELECT count(*) FROM subjects"); n != 2 {
		t.Fatalf("%d subjects left, want 2", n)
	}
}
