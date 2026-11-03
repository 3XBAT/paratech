package app

// The purge of removed subjects past the restore window (TASK-308, ADR-0027) on the wired core: a startup cleanup,
// then a loop once a day.

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/library/values"
	"paratech/desktop/internal/domain/shared"
)

// The start erases a subject removed longer than the window with its file; one removed an hour ago and an active one
// stay with theirs. The daily loop is wired.
func TestStartupPurgesRemovedSubjects(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	a := newTestApp(t, dir)
	if _, err := a.openCore(ctx); err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(a.core.cleanups, func(c cleanup) bool { return c.name == subjectPurgeStep }) {
		t.Fatal("no startup cleanup of the subject purge")
	}
	if !slices.ContainsFunc(a.core.loops(), func(l loop) bool { return l.name == "subject purge" && l.run != nil }) {
		t.Fatal("no daily subject purge loop")
	}

	now := time.Now()
	subjects, materials := sqlite.NewSubjectRepository(a.core.db), sqlite.NewMaterialRepository(a.core.db)
	for _, tc := range []struct {
		id        shared.SubjectID
		name      string
		removedAt time.Time
	}{
		{"s-old", "Физика", now.Add(-subject.RestoreWindow - time.Hour)},
		{"s-recent", "Химия", now.Add(-time.Hour)},
		{"s-live", "История", time.Time{}},
	} {
		created := now.Add(-subject.RestoreWindow - 2*time.Hour)
		s, err := subject.New(subject.Spec{ID: tc.id, Name: tc.name, Semester: values.SemesterOf(created)}, created)
		if err != nil {
			t.Fatal(err)
		}
		if err := subjects.Save(ctx, s); err != nil {
			t.Fatal(err)
		}
		data := []byte("Конспект по предмету " + string(tc.id))
		m, err := material.New(material.Spec{
			ID: shared.MaterialID("m-" + string(tc.id)), Digest: material.DigestOf(data), Size: int64(len(data)),
			Type: material.TypeTXT, Name: "конспект.txt",
		}, created)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := m.Place(tc.id, subject.SystemFolderID(tc.id, subject.SystemMaterials), created); err != nil {
			t.Fatal(err)
		}
		if err := materials.Save(ctx, m); err != nil {
			t.Fatal(err)
		}
		ref, err := material.BlobRef(m.Digest())
		if err != nil {
			t.Fatal(err)
		}
		if err := a.core.store.Write(ctx, ref, strings.NewReader(string(data))); err != nil {
			t.Fatal(err)
		}
		if !tc.removedAt.IsZero() {
			if _, err := s.Remove(tc.removedAt); err != nil {
				t.Fatal(err)
			}
			if err := subjects.Save(ctx, s); err != nil {
				t.Fatal(err)
			}
		}
	}

	life, ok := a.claimStart(false)
	if !ok {
		t.Fatal("no lifecycle to start")
	}
	if err := life.start(ctx); err != nil {
		t.Fatal(err)
	}

	for id, kept := range map[shared.SubjectID]bool{"s-old": false, "s-recent": true, "s-live": true} {
		if _, err := subjects.GetAny(ctx, id); errors.Is(err, shared.ErrNotFound) == kept {
			t.Errorf("%s: kept = %v, want %v (err %v)", id, !kept, kept, err)
		}
		m, err := materials.GetByDigest(ctx, material.DigestOf([]byte("Конспект по предмету "+string(id))))
		if (err == nil) != kept {
			t.Errorf("%s: material kept = %v, want %v (err %v)", id, err == nil, kept, err)
		}
		ref, err := material.BlobRef(material.DigestOf([]byte("Конспект по предмету " + string(id))))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := a.core.store.Size(ctx, ref); errors.Is(err, shared.ErrNotFound) == kept {
			t.Errorf("%s: file kept = %v, want %v (err %v, material %v)", id, !kept, kept, err, m)
		}
	}
}
