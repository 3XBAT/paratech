package library

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/services/subjectmatch"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
)

func docx(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, n := range []string{"[Content_Types].xml", "word/document.xml"} {
		if _, err := zw.Create(n); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func createSubject(t *testing.T, e *env, name string, aliases ...string) *subject.Subject {
	t.Helper()
	s, err := e.svc.CreateSubject(context.Background(), NewSubject{Name: name, Aliases: aliases})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func importText(t *testing.T, e *env, text string) *material.Material {
	t.Helper()
	res, err := e.svc.Import(context.Background(), Upload{Name: "notes.txt", Data: []byte(text)})
	if err != nil {
		t.Fatal(err)
	}
	return res.Material
}

func TestNewRequiresPorts(t *testing.T) {
	if _, err := New(Deps{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("New(empty) = %v", err)
	}
}

func TestCreateSubject(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	e.clock.now = time.Date(2027, time.March, 1, 12, 0, 0, 0, time.UTC)
	s, err := e.svc.CreateSubject(ctx, NewSubject{Name: " Физика ", Aliases: []string{"Общая физика"}})
	if err != nil {
		t.Fatal(err)
	}
	if s.Name().String() != "Физика" || s.Semester().Code() != "2027-spring" || len(s.Folders()) != len(subject.Systems()) {
		t.Fatalf("subject %+v", s.State())
	}
	if s.Version() != 1 {
		t.Fatalf("not saved: version %d", s.Version())
	}
	if got, err := e.svc.CreateSubject(ctx, NewSubject{Name: "Химия", Semester: "2026-autumn"}); err != nil || got.Semester().Code() != "2026-autumn" {
		t.Fatalf("CreateSubject with a semester = %v, %v", got, err)
	}

	tests := []struct {
		name string
		req  NewSubject
		want error
	}{
		{"empty name", NewSubject{Name: " "}, ErrInvalidName},
		{"bad semester", NewSubject{Name: "Биология", Semester: "summer"}, ErrInvalidRequest},
		{"name of another subject", NewSubject{Name: "ФИЗИКА"}, ErrNameTaken},
		{"name that is an alias", NewSubject{Name: "общая физика"}, ErrNameTaken},
		{"alias that is a name", NewSubject{Name: "Биология", Aliases: []string{"химия"}}, ErrNameTaken},
	}
	for _, tt := range tests {
		if _, err := e.svc.CreateSubject(ctx, tt.req); !errors.Is(err, tt.want) {
			t.Errorf("%s: CreateSubject = %v, want %v", tt.name, err, tt.want)
		}
	}
	list, err := e.svc.Subjects(ctx)
	if err != nil || len(list) != 2 {
		t.Fatalf("Subjects = %v, %v", list, err)
	}
}

func TestCreateSubjectRaceIsNameTaken(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	e.subjects.beforeSave = func() { createSubject(t, e, "физика") } // another window was faster
	if _, err := e.svc.CreateSubject(ctx, NewSubject{Name: "Физика"}); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("CreateSubject = %v, want ErrNameTaken", err)
	}
}

func TestAddAlias(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	phys := createSubject(t, e, "Физика")
	createSubject(t, e, "Химия")
	got, err := e.svc.AddAlias(ctx, phys.ID(), "Физ-ра")
	if err != nil || len(got.Aliases()) != 1 {
		t.Fatalf("AddAlias = %v, %v", got, err)
	}
	if _, err := e.svc.AddAlias(ctx, phys.ID(), "ХИМИЯ"); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("AddAlias(name of another) = %v", err)
	}
	if _, err := e.svc.AddAlias(ctx, "nope", "x"); !errors.Is(err, ErrSubjectNotFound) {
		t.Fatalf("AddAlias(unknown) = %v", err)
	}
	if _, err := e.svc.AddAlias(ctx, "a b", "x"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("AddAlias(bad id) = %v", err)
	}
}

func TestFolders(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	phys := createSubject(t, e, "Физика")
	f, err := e.svc.CreateFolder(ctx, phys.ID(), "  Конспекты ")
	if err != nil || f.Name().String() != "Конспекты" || f.IsSystem() {
		t.Fatalf("CreateFolder = %+v, %v", f, err)
	}

	tests := []struct {
		name   string
		folder string
		want   error
	}{
		{"same name", "конспекты", ErrFolderTaken},
		{"system name", "ЛЕКЦИИ", ErrFolderTaken},
		{"forbidden character", "a:b", ErrInvalidFolderName},
		{"device name", "NUL", ErrInvalidFolderName},
		{"trailing dot", "Папка.", ErrInvalidFolderName},
	}
	for _, tt := range tests {
		if _, err := e.svc.CreateFolder(ctx, phys.ID(), tt.folder); !errors.Is(err, tt.want) {
			t.Errorf("%s: CreateFolder = %v, want %v", tt.name, err, tt.want)
		}
	}
	if _, err := e.svc.CreateFolder(ctx, "nope", "x"); !errors.Is(err, ErrSubjectNotFound) {
		t.Fatalf("CreateFolder(unknown subject) = %v", err)
	}

	// A folder with a file stays; the system folders always stay; an empty own folder goes.
	m := importText(t, e, "x")
	if _, err := e.svc.Place(ctx, m.ID(), phys.ID(), f.ID()); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.RemoveFolder(ctx, phys.ID(), f.ID()); !errors.Is(err, ErrNotEmpty) {
		t.Fatalf("RemoveFolder(with a file) = %v", err)
	}
	if _, err := e.svc.RemoveMaterial(ctx, m.ID()); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.RemoveFolder(ctx, phys.ID(), f.ID()); !errors.Is(err, ErrNotEmpty) {
		t.Fatalf("RemoveFolder(with a removed file) = %v", err)
	}
	if err := e.svc.RemoveFolder(ctx, phys.ID(), subject.SystemFolderID(phys.ID(), subject.SystemTickets)); !errors.Is(err, ErrSystemFolder) {
		t.Fatalf("RemoveFolder(system) = %v", err)
	}
	if _, err := e.svc.RestoreMaterial(ctx, m.ID()); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.MoveToInbox(ctx, m.ID()); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.RemoveFolder(ctx, phys.ID(), f.ID()); err != nil {
		t.Fatalf("RemoveFolder(empty) = %v", err)
	}
	if err := e.svc.RemoveFolder(ctx, phys.ID(), f.ID()); !errors.Is(err, ErrFolderNotFound) {
		t.Fatalf("RemoveFolder again = %v", err)
	}
	if err := e.svc.RemoveFolder(ctx, phys.ID(), "a/b"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("RemoveFolder(bad id) = %v", err)
	}
}

// A file filed in the folder between the count and the save: the store refuses, the student sees «not empty».
func TestRemoveFolderRace(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	phys := createSubject(t, e, "Физика")
	f, err := e.svc.CreateFolder(ctx, phys.ID(), "Своя")
	if err != nil {
		t.Fatal(err)
	}
	e.subjects.holding[f.ID()] = true
	if err := e.svc.RemoveFolder(ctx, phys.ID(), f.ID()); !errors.Is(err, ErrNotEmpty) {
		t.Fatalf("RemoveFolder = %v, want ErrNotEmpty", err)
	}
}

func TestRemoveSubject(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	phys := createSubject(t, e, "Физика")
	m := importText(t, e, "x")
	if _, err := e.svc.Place(ctx, m.ID(), phys.ID(), ""); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.RemoveSubject(ctx, phys.ID()); !errors.Is(err, ErrNotEmpty) {
		t.Fatalf("RemoveSubject(with a file) = %v", err)
	}
	if _, err := e.svc.MoveToInbox(ctx, m.ID()); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.CreateFolder(ctx, phys.ID(), "Своя"); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.RemoveSubject(ctx, phys.ID()); err != nil {
		t.Fatalf("RemoveSubject(empty, with an own folder) = %v", err)
	}
	if err := e.svc.RemoveSubject(ctx, phys.ID()); !errors.Is(err, ErrSubjectNotFound) {
		t.Fatalf("RemoveSubject again = %v", err)
	}
}

func TestImportDetectsAndDeduplicates(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	first, err := e.svc.Import(ctx, Upload{Name: "Лекция 1.docx", Data: docx(t)})
	if err != nil || first.Existing {
		t.Fatalf("Import = %+v, %v", first, err)
	}
	m := first.Material
	if m.Type() != material.TypeDOCX || !m.InInbox() || m.Name() != "Лекция 1.docx" || m.TextStatus() != material.TextPending {
		t.Fatalf("material %+v", m.State())
	}
	ref, _ := material.BlobRef(m.Digest())
	if got := e.blobs.files[ref.String()]; !bytes.Equal(got, docx(t)) {
		t.Fatalf("blob %s not stored", ref)
	}

	// The same bytes under another name: the material that exists, no second blob write.
	again, err := e.svc.Import(ctx, Upload{Name: "copy.docx", Data: docx(t)})
	if err != nil || !again.Existing || again.Material.ID() != m.ID() || again.Material.Name() != "Лекция 1.docx" || e.blobs.writes != 1 {
		t.Fatalf("Import again = %+v, %v; %d writes", again, err, e.blobs.writes)
	}

	// A placed file uploaded again stays where it is.
	phys := createSubject(t, e, "Физика")
	if _, err := e.svc.Place(ctx, m.ID(), phys.ID(), ""); err != nil {
		t.Fatal(err)
	}
	if res, err := e.svc.Import(ctx, Upload{Data: docx(t)}); err != nil || res.Material.InInbox() {
		t.Fatalf("Import of a placed file = %+v, %v", res, err)
	}

	// A removed file uploaded again comes back into «Входящие».
	if _, err := e.svc.RemoveMaterial(ctx, m.ID()); err != nil {
		t.Fatal(err)
	}
	res, err := e.svc.Import(ctx, Upload{Data: docx(t)})
	if err != nil || !res.Existing || !res.Material.Active() || !res.Material.InInbox() {
		t.Fatalf("Import of a removed file = %+v, %v", res, err)
	}

	// A lost blob is written again.
	delete(e.blobs.files, ref.String())
	if _, err := e.svc.Import(ctx, Upload{Data: docx(t)}); err != nil || e.blobs.files[ref.String()] == nil {
		t.Fatalf("Import with a lost blob: %v", err)
	}

	pdf, err := e.svc.Import(ctx, Upload{Name: "scan.pdf", Data: []byte("%PDF-1.4 ...")})
	if err != nil || pdf.Material.TextStatus() != material.TextPDFNotExtracted {
		t.Fatalf("Import(pdf) = %+v, %v", pdf, err)
	}
}

func TestImportRejects(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	tests := []struct {
		name string
		data []byte
		want error
	}{
		{"empty", nil, ErrEmptyFile},
		{"executable", []byte("MZ\x90\x00\x03\x00"), ErrUnsupportedFile},
		{"other zip", []byte("PK\x03\x04junk"), ErrUnsupportedFile},
	}
	for _, tt := range tests {
		if _, err := e.svc.Import(ctx, Upload{Name: "x.docx", Data: tt.data}); !errors.Is(err, tt.want) {
			t.Errorf("%s: Import = %v, want %v", tt.name, err, tt.want)
		}
	}
	if e.blobs.writes != 0 {
		t.Fatalf("%d blobs written for refused uploads", e.blobs.writes)
	}

	e.blobs.err = errors.New("disk full")
	if _, err := e.svc.Import(ctx, Upload{Data: []byte("text")}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Import with a failing store = %v", err)
	}
}

// Two uploads of the same bytes at once: the second save meets the first row and returns it.
func TestImportConcurrentDuplicate(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	e.materials.beforeSave = func() { importText(t, e, "same") }
	res, err := e.svc.Import(ctx, Upload{Data: []byte("same")})
	if err != nil || !res.Existing || len(e.materials.rows) != 1 {
		t.Fatalf("Import = %+v, %v; %d rows", res, err, len(e.materials.rows))
	}
}

// The save that brings a removed file back is retried when it loses and reports a failing store; it never reports
// success it did not have.
func TestImportOfARemovedFileSaveErrors(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	m := importText(t, e, "x")
	if _, err := e.svc.RemoveMaterial(ctx, m.ID()); err != nil {
		t.Fatal(err)
	}

	e.materials.saveErrs = []error{shared.ErrVersionConflict}
	res, err := e.svc.Import(ctx, Upload{Data: []byte("x")})
	if err != nil || !res.Existing || !res.Material.Active() {
		t.Fatalf("Import after a lost save = %+v, %v", res, err)
	}

	if _, err := e.svc.RemoveMaterial(ctx, m.ID()); err != nil {
		t.Fatal(err)
	}
	disk := errors.New("disk")
	e.materials.saveErrs = []error{disk}
	if _, err := e.svc.Import(ctx, Upload{Data: []byte("x")}); !errors.Is(err, ErrUnavailable) || !errors.Is(err, disk) {
		t.Fatalf("Import with a failing save = %v", err)
	}
	if got, _ := e.materials.Get(ctx, m.ID()); got.Active() {
		t.Fatal("the failed save was kept")
	}
}

func TestPlace(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	phys, chem := createSubject(t, e, "Физика"), createSubject(t, e, "Химия")
	m := importText(t, e, "x")

	got, err := e.svc.Place(ctx, m.ID(), phys.ID(), "")
	if err != nil || got.Folder() != subject.SystemFolderID(phys.ID(), subject.SystemMaterials) {
		t.Fatalf("Place(default folder) = %+v, %v", got, err)
	}
	lectures := subject.SystemFolderID(phys.ID(), subject.SystemLectures)
	if got, err := e.svc.Place(ctx, m.ID(), phys.ID(), lectures); err != nil || got.Folder() != lectures {
		t.Fatalf("Place(lectures) = %+v, %v", got, err)
	}

	tests := []struct {
		name   string
		id     shared.MaterialID
		sub    shared.SubjectID
		folder shared.FolderID
		want   error
	}{
		{"folder of another subject", m.ID(), phys.ID(), subject.SystemFolderID(chem.ID(), subject.SystemLectures), ErrFolderNotFound},
		{"unknown subject", m.ID(), "nope", "", ErrSubjectNotFound},
		{"unknown material", "nope", phys.ID(), "", ErrMaterialNotFound},
		{"bad subject id", m.ID(), "", "", ErrInvalidRequest},
		{"bad folder id", m.ID(), phys.ID(), "a/b", ErrInvalidRequest},
		{"bad material id", "a b", phys.ID(), "", ErrInvalidRequest},
	}
	for _, tt := range tests {
		if _, err := e.svc.Place(ctx, tt.id, tt.sub, tt.folder); !errors.Is(err, tt.want) {
			t.Errorf("%s: Place = %v, want %v", tt.name, err, tt.want)
		}
	}

	if _, err := e.svc.RemoveMaterial(ctx, m.ID()); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Place(ctx, m.ID(), chem.ID(), ""); !errors.Is(err, ErrMaterialRemoved) {
		t.Fatalf("Place(removed) = %v", err)
	}
}

// A save that loses against another change is redone on the fresh state; a store that keeps losing is ErrBusy.
func TestMaterialRetries(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	phys := createSubject(t, e, "Физика")
	m := importText(t, e, "x")

	e.materials.beforeSave = func() {
		if _, err := e.svc.Link(ctx, m.ID(), "l2"); err != nil { // the concurrent change
			t.Error(err)
		}
	}
	got, err := e.svc.Place(ctx, m.ID(), phys.ID(), "")
	if err != nil || got.InInbox() || !reflect.DeepEqual(got.Lessons(), []shared.LessonID{"l2"}) {
		t.Fatalf("Place after a concurrent link = %+v, %v", got, err)
	}

	e.materials.saveErrs = []error{shared.ErrVersionConflict, shared.ErrVersionConflict, shared.ErrVersionConflict}
	if _, err := e.svc.MoveToInbox(ctx, m.ID()); !errors.Is(err, ErrBusy) {
		t.Fatalf("MoveToInbox while always losing = %v", err)
	}
	e.materials.saveErrs = []error{errors.New("disk")}
	if _, err := e.svc.MoveToInbox(ctx, m.ID()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("MoveToInbox with a failing store = %v", err)
	}
}

func TestLinkUnlink(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	m := importText(t, e, "x")
	got, err := e.svc.Link(ctx, m.ID(), "l1")
	if err != nil || !reflect.DeepEqual(got.Lessons(), []shared.LessonID{"l1"}) {
		t.Fatalf("Link = %+v, %v", got, err)
	}
	if _, err := e.svc.Link(ctx, m.ID(), "gone"); !errors.Is(err, ErrLessonNotFound) {
		t.Fatalf("Link(unknown lesson) = %v", err)
	}
	if _, err := e.svc.Link(ctx, m.ID(), "a b"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("Link(bad id) = %v", err)
	}
	if got, err := e.svc.Unlink(ctx, m.ID(), "l1"); err != nil || len(got.Lessons()) != 0 {
		t.Fatalf("Unlink = %+v, %v", got, err)
	}
	if _, err := e.svc.Unlink(ctx, m.ID(), ""); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("Unlink(empty) = %v", err)
	}
}

func TestRemoveRestoreMaterial(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	phys := createSubject(t, e, "Физика")
	m := importText(t, e, "x")
	if _, err := e.svc.Place(ctx, m.ID(), phys.ID(), ""); err != nil {
		t.Fatal(err)
	}
	removed, err := e.svc.RemoveMaterial(ctx, m.ID())
	if err != nil || removed.Active() {
		t.Fatalf("RemoveMaterial = %+v, %v", removed, err)
	}
	if list, err := e.svc.Materials(ctx, phys.ID()); err != nil || len(list) != 1 || list[0].Active() {
		t.Fatalf("Materials after removal = %v, %v (a removed file is listed with its status)", list, err)
	}
	restored, err := e.svc.RestoreMaterial(ctx, m.ID())
	if err != nil || !restored.Active() || restored.Subject() != phys.ID() {
		t.Fatalf("RestoreMaterial = %+v, %v", restored, err)
	}
	if _, err := e.svc.RestoreMaterial(ctx, "nope"); !errors.Is(err, ErrMaterialNotFound) {
		t.Fatalf("RestoreMaterial(unknown) = %v", err)
	}
	if _, err := e.svc.Materials(ctx, "a b"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("Materials(bad id) = %v", err)
	}
	if inbox, err := e.svc.Materials(ctx, ""); err != nil || len(inbox) != 0 {
		t.Fatalf("inbox = %v, %v", inbox, err)
	}
}

// Suggestions never create a subject; a string becomes known once the student creates the subject or an alias.
func TestSuggestAndResolve(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	phys := createSubject(t, e, "Физика")
	e.setStrings([]subjectmatch.Candidate{
		{Name: "физика", Lessons: 8},
		{Name: "Тёплые сети", Lessons: 3, Recordings: 1},
		{Name: "ТЕПЛЫЕ СЕТИ", Lessons: 1},
		{Name: "Матан", Lessons: 2},
	})
	got, err := e.svc.SuggestSubjects(ctx)
	want := []subjectmatch.Suggestion{{Name: "Тёплые сети", Lessons: 4, Recordings: 1}, {Name: "Матан", Lessons: 2}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("SuggestSubjects = %+v, %v; want %+v", got, err, want)
	}
	if list, _ := e.svc.Subjects(ctx); len(list) != 1 {
		t.Fatalf("SuggestSubjects created subjects: %d", len(list))
	}

	createSubject(t, e, "Теплоснабжение", "Тёплые сети")
	if _, err := e.svc.AddAlias(ctx, phys.ID(), "Матан"); err != nil { // a student's mistake, but allowed
		t.Fatal(err)
	}
	if got, err := e.svc.SuggestSubjects(ctx); err != nil || len(got) != 0 {
		t.Fatalf("SuggestSubjects after = %+v, %v", got, err)
	}

	res, err := e.svc.ResolveSubjects(ctx, []string{"ФИЗИКА", "теплые сети", "Химия", ""})
	if err != nil || len(res) != 2 || res["ФИЗИКА"].ID != phys.ID() || res["ФИЗИКА"].Name != "Физика" ||
		res["теплые сети"].Name != "Теплоснабжение" {
		t.Fatalf("ResolveSubjects = %+v, %v", res, err)
	}
	if res, err := e.svc.ResolveSubjects(ctx, nil); err != nil || len(res) != 0 {
		t.Fatalf("ResolveSubjects(nil) = %+v, %v", res, err)
	}

	e.strings.err = errors.New("db")
	if _, err := e.svc.SuggestSubjects(ctx); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("SuggestSubjects with a failing store = %v", err)
	}
	e.subjects.err = errors.New("db")
	if _, err := e.svc.ResolveSubjects(ctx, []string{"x"}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("ResolveSubjects with a failing store = %v", err)
	}
}
