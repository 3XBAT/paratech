package library

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"paratech/desktop/internal/domain/library/services/placement"
	"paratech/desktop/internal/domain/library/services/schedulesubjects"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
)

func importNamed(t *testing.T, e *env, name, text string) shared.MaterialID {
	t.Helper()
	res, err := e.svc.Import(context.Background(), Upload{Name: name, Data: []byte(text)})
	if err != nil {
		t.Fatal(err)
	}
	return res.Material.ID()
}

func TestImportTo(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	nets, phys := createSubject(t, e, "Сети"), createSubject(t, e, "Физика")
	labs := subject.SystemFolderID(nets.ID(), subject.SystemLabs)
	materials := subject.SystemFolderID(nets.ID(), subject.SystemMaterials)

	res, err := e.svc.Import(ctx, Upload{Name: "лр1.txt", Data: []byte("one"), Subject: nets.ID(), Folder: labs})
	if err != nil || res.Existing || res.Material.Subject() != nets.ID() || res.Material.Folder() != labs {
		t.Fatalf("Import(to labs) = %+v, %v", res, err)
	}
	if got, err := e.svc.Material(ctx, res.Material.ID()); err != nil || got.Folder() != labs {
		t.Fatalf("stored = %+v, %v", got, err)
	}
	res, err = e.svc.Import(ctx, Upload{Name: "two.txt", Data: []byte("two"), Subject: nets.ID()})
	if err != nil || res.Material.Folder() != materials {
		t.Fatalf("Import(to the subject) = %+v, %v; want «Материалы»", res, err)
	}

	// The same bytes, filed already: they stay where they are.
	res, err = e.svc.Import(ctx, Upload{Name: "again.txt", Data: []byte("one"), Subject: phys.ID()})
	if err != nil || !res.Existing || res.Material.Subject() != nets.ID() || res.Material.Folder() != labs {
		t.Fatalf("Import(existing, placed) = %+v, %v; want it left in «Сети/Лабораторные»", res, err)
	}

	// The same bytes in «Входящие», and removed: placed into the target.
	inbox := importNamed(t, e, "inbox.txt", "three")
	res, err = e.svc.Import(ctx, Upload{Data: []byte("three"), Subject: phys.ID()})
	if err != nil || !res.Existing || res.Material.ID() != inbox || res.Material.Subject() != phys.ID() {
		t.Fatalf("Import(existing, inbox) = %+v, %v", res, err)
	}
	if _, err := e.svc.RemoveMaterial(ctx, inbox); err != nil {
		t.Fatal(err)
	}
	res, err = e.svc.Import(ctx, Upload{Data: []byte("three"), Subject: nets.ID(), Folder: labs})
	if err != nil || !res.Existing || !res.Material.Active() || res.Material.Folder() != labs {
		t.Fatalf("Import(existing, removed) = %+v, %v", res, err)
	}
}

func TestImportToRefusesBeforeStoring(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	nets, phys := createSubject(t, e, "Сети"), createSubject(t, e, "Физика")
	tests := []struct {
		name   string
		sub    shared.SubjectID
		folder shared.FolderID
		want   error
	}{
		{"unknown subject", "nope", "", ErrSubjectNotFound},
		{"unknown folder", nets.ID(), "nope", ErrFolderNotFound},
		{"folder of another subject", nets.ID(), subject.SystemFolderID(phys.ID(), subject.SystemLabs), ErrFolderNotFound},
		{"folder without a subject", "", subject.SystemFolderID(nets.ID(), subject.SystemLabs), ErrInvalidRequest},
		{"bad subject id", "a b", "", ErrInvalidRequest},
		{"bad folder id", nets.ID(), "a/b", ErrInvalidRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := e.svc.Import(ctx, Upload{Name: "x.txt", Data: []byte(tt.name), Subject: tt.sub, Folder: tt.folder})
			if !errors.Is(err, tt.want) {
				t.Fatalf("Import = %v, want %v", err, tt.want)
			}
			if e.blobs.writes != 0 || len(e.materials.rows) != 0 {
				t.Fatalf("stored %d files, %d materials", e.blobs.writes, len(e.materials.rows))
			}
		})
	}
}

// A target that goes away after the file is stored: the file stays in «Входящие», returned with NotPlaced.
func TestImportToStoredNotPlaced(t *testing.T) {
	tests := []struct {
		name   string
		meddle func(e *env, sub shared.SubjectID)
	}{
		{"subject removed meanwhile", func(e *env, sub shared.SubjectID) {
			e.materials.beforeSave = func() { delete(e.subjects.rows, sub) } // after the check, before the file is saved
		}},
		{"busy", func(e *env, _ shared.SubjectID) {
			e.materials.beforeSave = func() { // the file is saved; every save of placing it conflicts
				e.materials.beforeSave = func() {
					e.materials.saveErrs = slices.Repeat([]error{shared.ErrVersionConflict}, saveAttempts)
				}
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, ctx := newEnv(t), context.Background()
			nets := createSubject(t, e, "Сети")
			tt.meddle(e, nets.ID())

			res, err := e.svc.Import(ctx, Upload{Name: "лр1.txt", Data: []byte("one"), Subject: nets.ID(),
				Folder: subject.SystemFolderID(nets.ID(), subject.SystemLabs)})
			if err != nil || !res.NotPlaced || res.Existing || res.Material == nil || !res.Material.InInbox() {
				t.Fatalf("Import = %+v, %v; want the stored file, not placed", res, err)
			}
			if got, err := e.svc.Material(ctx, res.Material.ID()); err != nil || !got.Active() || !got.InInbox() {
				t.Fatalf("stored = %+v, %v; want it in «Входящие»", got, err)
			}
		})
	}
}

func TestPlacementHint(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	nets := createSubject(t, e, "Сети")
	e.strings.rows = []schedulesubjects.Row{
		{Name: "Матанализ (лек.)", Lessons: schedulesubjects.Kinds{Lecture: 3}},
		{Name: "матанализ, сем.", Lessons: schedulesubjects.Kinds{Seminar: 1}},
		{Name: "Сети (лаб.)", Lessons: schedulesubjects.Kinds{Lab: 2}}, // spellings of an existing subject create nothing
	}

	lab := importNamed(t, e, "Сети_лр2.docx", "a")
	got, err := e.svc.PlacementHint(ctx, lab)
	if err != nil || got.Kind != placement.KindExisting || got.Subject.ID() != nets.ID() ||
		got.Folder.ID() != subject.SystemFolderID(nets.ID(), subject.SystemLabs) || got.FolderName.String() != "Лабораторные" {
		t.Fatalf("PlacementHint(lab) = %+v, %v", got, err)
	}

	// Named and aliased as the schedule-subjects card proposes it: the name without the mark.
	create := importNamed(t, e, "МАТАНАЛИЗ сем3.pdf", "b")
	got, err = e.svc.PlacementHint(ctx, create)
	if err != nil || got.Kind != placement.KindCreate || got.CreateName != "Матанализ" || got.Subject != nil ||
		!slices.Equal(got.CreateAliases, []string{"Матанализ (лек.)", "матанализ, сем."}) ||
		got.FolderKind != subject.SystemSeminars || got.FolderName.String() != "Семинары" {
		t.Fatalf("PlacementHint(create) = %+v, %v", got, err)
	}
	card, err := e.svc.ScheduleSubjects(ctx)
	if err != nil || len(card.Groups) == 0 || card.Groups[0].Name != got.CreateName {
		t.Fatalf("ScheduleSubjects = %+v, %v; want the group named %q", card, err, got.CreateName)
	}

	none := importNamed(t, e, "scan_0001.pdf", "c")
	if got, err := e.svc.PlacementHint(ctx, none); err != nil || got.Kind != placement.KindNone {
		t.Fatalf("PlacementHint(no match) = %+v, %v", got, err)
	}

	if _, err := e.svc.Place(ctx, lab, nets.ID(), ""); err != nil {
		t.Fatal(err)
	}
	if got, err := e.svc.PlacementHint(ctx, lab); err != nil || !reflect.DeepEqual(got, Placement{}) {
		t.Fatalf("PlacementHint(placed) = %+v, %v", got, err)
	}
	if _, err := e.svc.RemoveMaterial(ctx, create); err != nil {
		t.Fatal(err)
	}
	if got, err := e.svc.PlacementHint(ctx, create); err != nil || !reflect.DeepEqual(got, Placement{}) {
		t.Fatalf("PlacementHint(removed) = %+v, %v", got, err)
	}

	if _, err := e.svc.PlacementHint(ctx, "nope"); !errors.Is(err, ErrMaterialNotFound) {
		t.Fatalf("PlacementHint(missing) = %v", err)
	}
	if _, err := e.svc.PlacementHint(ctx, "a b"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("PlacementHint(bad id) = %v", err)
	}

	writes := e.blobs.writes
	e.strings.err = errors.New("disk")
	if _, err := e.svc.PlacementHint(ctx, none); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("PlacementHint(strings fail) = %v", err)
	}
	if e.blobs.writes != writes {
		t.Fatal("PlacementHint wrote a file")
	}
}

// A file of «Входящие» removed and restored comes back into «Входящие»; Place with no folder files it in «Материалы».
func TestInboxRemoveRestore(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	id := importNamed(t, e, "inbox.txt", "x")
	if m, err := e.svc.RemoveMaterial(ctx, id); err != nil || m.Active() || !m.InInbox() {
		t.Fatalf("RemoveMaterial = %+v, %v", m, err)
	}
	if list, err := e.svc.Materials(ctx, ""); err != nil || len(list) != 1 || list[0].Active() {
		t.Fatalf("inbox after removal = %v, %v", list, err)
	}
	if m, err := e.svc.RestoreMaterial(ctx, id); err != nil || !m.Active() || !m.InInbox() {
		t.Fatalf("RestoreMaterial = %+v, %v", m, err)
	}
}
