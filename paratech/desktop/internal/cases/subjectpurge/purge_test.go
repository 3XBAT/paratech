package subjectpurge

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
)

// due is the clock at which s1 (removed at t0) is due and s3 (removed an hour later) is not.
var due = t0.Add(subject.RestoreWindow)

// library is s1 «Физика» removed at t0, s2 «Химия» active, s3 «История» removed an hour later; the notes n1 (s1: m1),
// n3 (s2: m4), n4 (s2: m5) and n5 (s3: m8); m1..m5 and m8 filed under s1 (m3 and m5 removed), m6 in «Входящие», m7
// under s3; a send of a work pinned the bytes of m2; the ticket sets ts1 of s1 and ts2 of s2.
func library(t *testing.T) *store {
	t.Helper()
	st := newStore()
	st.addSubject(t, "s1", "Физика", t0)
	st.addSubject(t, "s2", "Химия", time.Time{})
	st.addSubject(t, "s3", "История", t0.Add(time.Hour))
	for _, m := range []struct {
		id      shared.MaterialID
		sub     shared.SubjectID
		removed bool
	}{
		{"m1", "s1", false}, {"m2", "s1", false}, {"m3", "s1", true}, {"m4", "s1", false}, {"m5", "s1", true},
		{"m6", "", false}, {"m7", "s3", false}, {"m8", "s1", false},
	} {
		d := st.addMaterial(t, m.id, m.sub, m.removed)
		if m.id == "m2" {
			st.pinned[d] = true
		}
	}
	st.notes["n1"] = fakeNote{sub: "s1", parts: []shared.MaterialID{"m1"}}
	st.notes["n3"] = fakeNote{sub: "s2", parts: []shared.MaterialID{"m4"}}
	st.notes["n4"] = fakeNote{sub: "s2", parts: []shared.MaterialID{"m5"}}
	st.notes["n5"] = fakeNote{sub: "s3", parts: []shared.MaterialID{"m8", "m7"}}
	st.ticketSets["ts1"] = "s1"
	st.ticketSets["ts2"] = "s2"
	return st
}

// checkPurged checks the library after s1 was purged.
func checkPurged(t *testing.T, st *store) {
	t.Helper()
	st.mu.Lock()
	defer st.mu.Unlock()
	if got := sortedKeys(st.subjects); !slices.Equal(got, []shared.SubjectID{"s2", "s3"}) {
		t.Errorf("subjects = %v, want s2 s3", got)
	}
	if got := sortedKeys(st.notes); !slices.Equal(got, []shared.NoteID{"n3", "n4", "n5"}) {
		t.Errorf("notes = %v, want n3 n4 n5", got)
	}
	if got := sortedKeys(st.ticketSets); !slices.Equal(got, []shared.TicketSetID{"ts2"}) {
		t.Errorf("ticket sets = %v, want ts2", got)
	}
	if got := sortedKeys(st.materials); !slices.Equal(got, []shared.MaterialID{"m4", "m5", "m6", "m7", "m8"}) {
		t.Errorf("materials = %v, want m4 m5 m6 m7 m8", got)
	}
	// The files a note of another subject holds are in «Входящие» with their status.
	for id, active := range map[shared.MaterialID]bool{"m4": true, "m5": false, "m8": true} {
		m := st.materials[id]
		if m.Subject != "" || m.Folder != "" || (m.Status == material.StatusActive) != active {
			t.Errorf("%s: subject %q folder %q status %s, want in the inbox, active %v", id, m.Subject, m.Folder, m.Status, active)
		}
	}
	if m := st.materials["m7"]; m.Subject != "s3" {
		t.Errorf("m7 of s3 (not due) moved to %q", m.Subject)
	}
	// The bytes: gone for m1 and m3, kept for m2 (pinned by a send) and every material that stays.
	for id, kept := range map[string]bool{"m1": false, "m2": true, "m3": false, "m4": true, "m5": true, "m6": true, "m7": true, "m8": true} {
		for _, ref := range refsOf(t, material.DigestOf([]byte("content of "+id))) {
			if st.files[ref] != kept {
				t.Errorf("%s: file %s kept = %v, want %v", id, ref, st.files[ref], kept)
			}
		}
	}
}

func TestPurgeErasesARemovedSubjectPastItsWindow(t *testing.T) {
	st := library(t)
	n, err := newPurger(t, st, due).Purge(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("Purge = %d, %v; want 1, nil", n, err)
	}
	checkPurged(t, st)

	// A second run finds nothing more to do.
	before := st.snapshot()
	if n, err := newPurger(t, st, due.Add(30*time.Minute)).Purge(context.Background()); err != nil || n != 0 {
		t.Fatalf("second Purge = %d, %v; want 0, nil", n, err)
	}
	if after := st.snapshot(); after != before {
		t.Fatalf("the second run changed the library:\n%s\n%s", before, after)
	}
}

// A moment before the end of the window nothing is touched, and an active subject never is.
func TestPurgeLeavesSubjectsInsideTheWindow(t *testing.T) {
	st := library(t)
	before := st.snapshot()
	if n, err := newPurger(t, st, due.Add(-time.Second)).Purge(context.Background()); err != nil || n != 0 {
		t.Fatalf("Purge = %d, %v; want 0, nil", n, err)
	}
	if after := st.snapshot(); after != before {
		t.Fatalf("a subject inside its window was touched:\n%s\n%s", before, after)
	}
}

// A failure at any step stops that subject; the rows it did not reach stay, and the next run finishes it.
func TestPurgeFailureMidwayIsFinishedByTheNextRun(t *testing.T) {
	m1Blob, err := material.BlobRef(material.DigestOf([]byte("content of m1")))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		op string
		// kept checks what the failed run left.
		kept func(t *testing.T, st *store)
	}{
		{"list subjects", nil},
		{"list notes", nil},
		{"note n1", func(t *testing.T, st *store) {
			if _, ok := st.notes["n1"]; !ok {
				t.Error("the note was deleted although its delete failed")
			}
		}},
		{"list ticket sets", nil},
		{"ticket set ts1", nil},
		{"list materials", nil},
		{"held m4", nil},
		{"in use m1", nil},
		{"blob " + m1Blob.String(), func(t *testing.T, st *store) {
			if _, ok := st.materials["m1"]; !ok {
				t.Error("the row of m1 was deleted although its file was not")
			}
			if _, ok := st.materials["m3"]; ok {
				t.Error("one failing file stopped the others")
			}
		}},
		{"material m3", nil},
		{"save m4", func(t *testing.T, st *store) {
			if st.materials["m4"].Subject != "s1" {
				t.Error("m4 moved although its save failed")
			}
		}},
		{"subject s1", nil},
	}
	for _, tt := range tests {
		t.Run(tt.op, func(t *testing.T) {
			st := library(t)
			st.failOnce(tt.op, errDisk)
			n, err := newPurger(t, st, due).Purge(context.Background())
			if !errors.Is(err, errDisk) || n != 0 {
				t.Fatalf("Purge with %q failing = %d, %v; want 0 and the failure", tt.op, n, err)
			}
			if _, ok := st.subjects["s1"]; !ok {
				t.Fatal("the subject row went although a step failed")
			}
			if tt.kept != nil {
				tt.kept(t, st)
			}

			if n, err := newPurger(t, st, due.Add(time.Minute)).Purge(context.Background()); err != nil || n != 1 {
				t.Fatalf("the next run = %d, %v; want 1, nil", n, err)
			}
			checkPurged(t, st)
		})
	}
}

// One subject that fails does not stop another.
func TestPurgeOneFailingSubjectDoesNotStopAnother(t *testing.T) {
	st := library(t)
	st.addSubject(t, "s0", "Алгебра", t0.Add(-time.Hour))
	st.addMaterial(t, "m0", "s0", false)
	st.failOnce("material m0", errDisk)

	n, err := newPurger(t, st, due).Purge(context.Background())
	if !errors.Is(err, errDisk) || n != 1 || !strings.Contains(err.Error(), "s0") {
		t.Fatalf("Purge = %d, %v; want s1 purged and the failure of s0", n, err)
	}
	if _, ok := st.subjects["s0"]; !ok {
		t.Fatal("s0 went although its file failed")
	}
	if _, ok := st.subjects["s1"]; ok {
		t.Fatal("s1 was not purged")
	}
}

// A subject the store no longer has due (restored when the clock was set back, before its notes were reached) keeps
// its rows: every delete re-checks it.
func TestPurgeStoreGuardKeepsARestoredSubject(t *testing.T) {
	st := newStore()
	st.addSubject(t, "s1", "Физика", t0)
	st.notes["n1"] = fakeNote{sub: "s1"}
	st.ticketSets["ts1"] = "s1"
	p := newPurger(t, st, due)

	list, err := fakeSubjects{st}.ListRemoved(context.Background())
	if err != nil || len(list) != 1 {
		t.Fatal(list, err)
	}
	restored := st.subjects["s1"]
	restored.RemovedAt = time.Time{}
	st.subjects["s1"] = restored

	done, err := p.purge(context.Background(), "s1", subject.PurgeCutoff(due))
	if err != nil || done {
		t.Fatalf("purge of a restored subject = %v, %v; want false, nil", done, err)
	}
	if len(st.subjects) != 1 || len(st.notes) != 1 || len(st.ticketSets) != 1 {
		t.Fatal("a restored subject lost rows")
	}
}

func TestNewRequiresEveryPort(t *testing.T) {
	st := newStore()
	full := Deps{
		Subjects: fakeSubjects{st}, Notes: fakeNotes{st}, TicketSets: fakeTicketSets{st}, Materials: fakeMaterials{st},
		Blobs: fakeBlobs{st}, Clock: &fakeClock{now: t0},
	}
	for name, drop := range map[string]func(*Deps){
		"subjects":    func(d *Deps) { d.Subjects = nil },
		"notes":       func(d *Deps) { d.Notes = nil },
		"ticket sets": func(d *Deps) { d.TicketSets = nil },
		"materials":   func(d *Deps) { d.Materials = nil },
		"blobs":       func(d *Deps) { d.Blobs = nil },
		"clock":       func(d *Deps) { d.Clock = nil },
	} {
		d := full
		drop(&d)
		if _, err := New(d); !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("New without %s = %v, want ErrInvalidConfig", name, err)
		}
	}
	if _, err := New(full); err != nil {
		t.Fatal(err)
	}
}
