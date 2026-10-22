package wails

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/cases/library"
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/services/subjectmatch"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/library/values"
	"paratech/desktop/internal/domain/shared"
)

var libT0 = time.Date(2026, time.October, 22, 10, 0, 0, 0, time.UTC)

// fakeLibrary returns what it is set to and records the last call.
type fakeLibrary struct {
	err      error
	subjects []*subject.Subject
	material *material.Material
	existing bool
	suggest  []subjectmatch.Suggestion
	calls    []string
	upload   library.Upload
	created  library.NewSubject
}

func (f *fakeLibrary) note(format string, args ...any) {
	f.calls = append(f.calls, fmt.Sprintf(format, args...))
}

func (f *fakeLibrary) Subjects(context.Context) ([]*subject.Subject, error) { return f.subjects, f.err }

func (f *fakeLibrary) CreateSubject(_ context.Context, req library.NewSubject) (*subject.Subject, error) {
	f.created = req
	if f.err != nil {
		return nil, f.err
	}
	return f.subjects[0], nil
}

func (f *fakeLibrary) AddAlias(_ context.Context, id shared.SubjectID, alias string) (*subject.Subject, error) {
	f.note("AddAlias %s %s", id, alias)
	if f.err != nil {
		return nil, f.err
	}
	return f.subjects[0], nil
}

func (f *fakeLibrary) CreateFolder(_ context.Context, id shared.SubjectID, name string) (subject.Folder, error) {
	f.note("CreateFolder %s %s", id, name)
	if f.err != nil {
		return subject.Folder{}, f.err
	}
	return f.subjects[0].Folders()[0], nil
}

func (f *fakeLibrary) RemoveFolder(_ context.Context, id shared.SubjectID, folder shared.FolderID) error {
	f.note("RemoveFolder %s %s", id, folder)
	return f.err
}

func (f *fakeLibrary) RemoveSubject(_ context.Context, id shared.SubjectID) error {
	f.note("RemoveSubject %s", id)
	return f.err
}

func (f *fakeLibrary) Materials(_ context.Context, sub shared.SubjectID) ([]*material.Material, error) {
	f.note("Materials %q", sub)
	if f.err != nil {
		return nil, f.err
	}
	return []*material.Material{f.material}, nil
}

func (f *fakeLibrary) Import(_ context.Context, up library.Upload) (library.Imported, error) {
	f.upload = up
	if f.err != nil {
		return library.Imported{}, f.err
	}
	return library.Imported{Material: f.material, Existing: f.existing}, nil
}

func (f *fakeLibrary) result(call string) (*material.Material, error) {
	f.note("%s", call)
	if f.err != nil {
		return nil, f.err
	}
	return f.material, nil
}

func (f *fakeLibrary) Place(_ context.Context, id shared.MaterialID, sub shared.SubjectID, folder shared.FolderID) (*material.Material, error) {
	return f.result(fmt.Sprintf("Place %s %s %q", id, sub, folder))
}

func (f *fakeLibrary) MoveToInbox(_ context.Context, id shared.MaterialID) (*material.Material, error) {
	return f.result("MoveToInbox " + string(id))
}

func (f *fakeLibrary) Link(_ context.Context, id shared.MaterialID, l shared.LessonID) (*material.Material, error) {
	return f.result(fmt.Sprintf("Link %s %s", id, l))
}

func (f *fakeLibrary) Unlink(_ context.Context, id shared.MaterialID, l shared.LessonID) (*material.Material, error) {
	return f.result(fmt.Sprintf("Unlink %s %s", id, l))
}

func (f *fakeLibrary) RemoveMaterial(_ context.Context, id shared.MaterialID) (*material.Material, error) {
	return f.result("RemoveMaterial " + string(id))
}

func (f *fakeLibrary) RestoreMaterial(_ context.Context, id shared.MaterialID) (*material.Material, error) {
	return f.result("RestoreMaterial " + string(id))
}

func (f *fakeLibrary) SuggestSubjects(context.Context) ([]subjectmatch.Suggestion, error) {
	return f.suggest, f.err
}

// memFiles is the reader of the chosen file: content by path.
type memFiles struct {
	files map[string][]byte
	limit int64
	err   error
}

func (m *memFiles) ReadFile(_ context.Context, path string, maxBytes int64) ([]byte, error) {
	m.limit = maxBytes
	if m.err != nil {
		return nil, m.err
	}
	data, ok := m.files[path]
	if !ok {
		return nil, errors.New("not found")
	}
	return data, nil
}

func testSubject(t *testing.T) *subject.Subject {
	t.Helper()
	s, err := subject.New(subject.Spec{ID: "s1", Name: "Физика", Aliases: []string{"Общая физика"}, Semester: values.SemesterOf(libT0)}, libT0)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func testMaterial(t *testing.T, typ material.Type) *material.Material {
	t.Helper()
	m, err := material.New(material.Spec{ID: "m1", Digest: material.DigestOf([]byte("x")), Size: 1, Type: typ, Name: "scan.pdf"}, libT0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Link("l1", libT0); err != nil {
		t.Fatal(err)
	}
	return m
}

func newLibraryBinding(t *testing.T, picked string, files *memFiles) (*LibraryService, *fakeLibrary, *bytes.Buffer) {
	t.Helper()
	uc := &fakeLibrary{subjects: []*subject.Subject{testSubject(t)}, material: testMaterial(t, material.TypePDF)}
	logs := &bytes.Buffer{}
	picker := materialPickerFunc(func(context.Context) (string, error) { return picked, nil })
	return NewLibraryService(uc, picker, files, slog.New(slog.NewTextHandler(logs, nil))), uc, logs
}

func TestLibrarySubjectsAndFolders(t *testing.T) {
	s, uc, _ := newLibraryBinding(t, "", &memFiles{})
	ctx := context.Background()
	list, err := s.Subjects(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("Subjects = %+v, %v", list, err)
	}
	got := list[0]
	if got.ID != "s1" || got.Name != "Физика" || !reflect.DeepEqual(got.Aliases, []string{"Общая физика"}) ||
		got.Semester != "2026-autumn" || got.SemesterLabel != "Осень 2026" || len(got.Folders) != 6 ||
		got.Folders[0] != (LibraryFolder{ID: "s1-lectures", Name: "Лекции", System: "lectures"}) {
		t.Fatalf("subject %+v", got)
	}

	if _, err := s.CreateSubject(ctx, "Физика", []string{"физ"}, ""); err != nil {
		t.Fatal(err)
	}
	if want := (library.NewSubject{Name: "Физика", Aliases: []string{"физ"}}); !reflect.DeepEqual(uc.created, want) {
		t.Fatalf("CreateSubject passed %+v", uc.created)
	}
	if f, err := s.CreateFolder(ctx, "s1", "Своя"); err != nil || f.ID != "s1-lectures" {
		t.Fatalf("CreateFolder = %+v, %v", f, err)
	}
	if _, err := s.AddAlias(ctx, "s1", "Физ-ра"); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveFolder(ctx, "s1", "f1"); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveSubject(ctx, "s1"); err != nil {
		t.Fatal(err)
	}
	want := []string{"CreateFolder s1 Своя", "AddAlias s1 Физ-ра", "RemoveFolder s1 f1", "RemoveSubject s1"}
	if !reflect.DeepEqual(uc.calls, want) {
		t.Fatalf("calls = %q", uc.calls)
	}
}

func TestLibraryMaterials(t *testing.T) {
	s, uc, _ := newLibraryBinding(t, "", &memFiles{})
	ctx := context.Background()
	list, err := s.Materials(ctx, "")
	if err != nil || len(list) != 1 {
		t.Fatalf("Materials = %+v, %v", list, err)
	}
	want := LibraryMaterial{
		ID: "m1", Name: "scan.pdf", Type: "pdf", SizeBytes: 1, Inbox: true, LessonIDs: []string{"l1"},
		TextStatus: "pdf_not_extracted", TextNote: "текст PDF не извлечён", CreatedAt: "2026-10-22T10:00:00Z",
	}
	if !reflect.DeepEqual(list[0], want) {
		t.Fatalf("material %+v\nwant %+v", list[0], want)
	}

	calls := []func() (LibraryMaterial, error){
		func() (LibraryMaterial, error) { return s.Place(ctx, "m1", "s1", "") },
		func() (LibraryMaterial, error) { return s.MoveToInbox(ctx, "m1") },
		func() (LibraryMaterial, error) { return s.Link(ctx, "m1", "l2") },
		func() (LibraryMaterial, error) { return s.Unlink(ctx, "m1", "l2") },
		func() (LibraryMaterial, error) { return s.RemoveMaterial(ctx, "m1") },
		func() (LibraryMaterial, error) { return s.RestoreMaterial(ctx, "m1") },
	}
	for _, c := range calls {
		if got, err := c(); err != nil || got.ID != "m1" {
			t.Fatalf("call = %+v, %v", got, err)
		}
	}
	wantCalls := []string{`Materials ""`, `Place m1 s1 ""`, "MoveToInbox m1", "Link m1 l2", "Unlink m1 l2", "RemoveMaterial m1", "RestoreMaterial m1"}
	if !reflect.DeepEqual(uc.calls, wantCalls) {
		t.Fatalf("calls = %q", uc.calls)
	}

	uc.suggest = []subjectmatch.Suggestion{{Name: "История", Lessons: 3, Recordings: 1}}
	if got, err := s.Suggestions(ctx); err != nil || !reflect.DeepEqual(got, []LibrarySuggestion{{Name: "История", Lessons: 3, Recordings: 1}}) {
		t.Fatalf("Suggestions = %+v, %v", got, err)
	}
}

func TestLibraryImportFile(t *testing.T) {
	ctx := context.Background()
	const path = `C:\Users\student\Documents\Лекция 1.docx`

	files := &memFiles{files: map[string][]byte{path: []byte("bytes")}}
	s, uc, logs := newLibraryBinding(t, path, files)
	uc.existing = true
	res, err := s.ImportFile(ctx)
	if err != nil || res.Cancelled || !res.Existing || res.Material == nil || res.Material.ID != "m1" {
		t.Fatalf("ImportFile = %+v, %v", res, err)
	}
	if uc.upload.Name != "Лекция 1.docx" || string(uc.upload.Data) != "bytes" || files.limit != material.MaxBytes {
		t.Fatalf("upload %q %q, limit %d: want the base name only", uc.upload.Name, uc.upload.Data, files.limit)
	}

	cancelled, _, _ := newLibraryBinding(t, "", files)
	if res, err := cancelled.ImportFile(ctx); err != nil || !res.Cancelled || res.Material != nil {
		t.Fatalf("ImportFile cancelled = %+v, %v", res, err)
	}

	unreadable, _, _ := newLibraryBinding(t, path, &memFiles{err: errors.New("access denied")})
	if _, err := unreadable.ImportFile(ctx); !errors.Is(err, ErrLibraryFileUnreadable) {
		t.Fatalf("ImportFile unreadable = %v", err)
	}

	large, luc, _ := newLibraryBinding(t, path, &memFiles{files: map[string][]byte{path: make([]byte, material.MaxBytes+1)}})
	if _, err := large.ImportFile(ctx); !errors.Is(err, ErrLibraryFileTooLarge) || luc.upload.Data != nil {
		t.Fatalf("ImportFile too large = %v", err)
	}

	failing := &LibraryService{
		uc: uc, files: files, log: slog.New(slog.NewTextHandler(logs, nil)),
		picker: materialPickerFunc(func(context.Context) (string, error) { return "", errors.New("no window") }),
	}
	if _, err := failing.ImportFile(ctx); !errors.Is(err, ErrLibraryFileUnreadable) {
		t.Fatalf("ImportFile with a failing dialog = %v", err)
	}
	if strings.Contains(logs.String(), "student") || strings.Contains(logs.String(), "Лекция") {
		t.Fatalf("the log has the path or the file name: %s", logs.String())
	}
}

func TestLibraryImportDroppedFile(t *testing.T) {
	ctx := context.Background()
	// Windows paths on every OS: the check is string work and the fake reader never touches the disk.
	dir := `C:\Users\student\Документы`
	good := dir + `\Лекция 1.DOCX`
	files := &memFiles{files: map[string][]byte{good: []byte("bytes")}}
	s, uc, logs := newLibraryBinding(t, "", files)
	res, err := s.ImportDroppedFile(ctx, good)
	if err != nil || res.Cancelled || res.Material == nil || res.Material.ID != "m1" {
		t.Fatalf("ImportDroppedFile = %+v, %v", res, err)
	}
	if uc.upload.Name != "Лекция 1.DOCX" || string(uc.upload.Data) != "bytes" || files.limit != material.MaxBytes {
		t.Fatalf("upload %q %q, limit %d", uc.upload.Name, uc.upload.Data, files.limit)
	}

	tests := []struct {
		name string
		path string
		want error
	}{
		{"empty", "", ErrLibraryDroppedPath},
		{"relative", "Лекция.docx", ErrLibraryDroppedPath},
		{"relative up", `..\Лекция.docx`, ErrLibraryDroppedPath},
		{"traversal", dir + `\..\Лекция.docx`, ErrLibraryDroppedPath},
		{"traversal, slashes", "C:/Users/../Лекция.docx", ErrLibraryDroppedPath},
		{"dot segment", dir + `\.\Лекция.docx`, ErrLibraryDroppedPath},
		{"network share", `\\server\share\Лекция.docx`, ErrLibraryDroppedPath},
		{"network share, slashes", "//server/share/Лекция.docx", ErrLibraryDroppedPath},
		{"network share, mixed", `\/server/share/Лекция.docx`, ErrLibraryDroppedPath},
		{"network share, mixed other way", `/\server\share\Лекция.docx`, ErrLibraryDroppedPath},
		{"device path", `\\?\C:\Users\student\Лекция.docx`, ErrLibraryDroppedPath},
		{"device namespace", `\\.\C:\Users\student\Лекция.docx`, ErrLibraryDroppedPath},
		{"rooted without drive", `\Users\student\Лекция.docx`, ErrLibraryDroppedPath},
		{"rooted without drive, slash", "/server/Лекция.docx", ErrLibraryDroppedPath},
		{"drive-relative", `C:Лекция.docx`, ErrLibraryDroppedPath},
		{"non-letter drive", `1:\Лекция.docx`, ErrLibraryDroppedPath},
		{"non-ASCII drive", `Ж:\Лекция.docx`, ErrLibraryDroppedPath},
		{"alternate stream", dir + `\a.docx:hidden.txt`, ErrLibraryDroppedPath},
		{"nul byte", dir + "\\a\x00.docx", ErrLibraryDroppedPath},
		{"not UTF-8", dir + "\\a\xff.docx", ErrLibraryDroppedPath},
		{"too long", dir + `\` + strings.Repeat("a", maxDroppedPath) + ".docx", ErrLibraryDroppedPath},
		{"executable", dir + `\setup.exe`, ErrLibraryUnsupported},
		{"no extension", dir + `\README`, ErrLibraryUnsupported},
		{"dot in folder only", `C:\Users\a.docx\README`, ErrLibraryUnsupported},
		{"shortcut", dir + `\Лекция.docx.lnk`, ErrLibraryUnsupported},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := &memFiles{files: map[string][]byte{tt.path: []byte("bytes")}}
			s, uc, logs := newLibraryBinding(t, "", files)
			if _, err := s.ImportDroppedFile(ctx, tt.path); !errors.Is(err, tt.want) {
				t.Fatalf("ImportDroppedFile = %v, want %v", err, tt.want)
			}
			if files.limit != 0 || uc.upload.Data != nil {
				t.Fatalf("a refused path was read or imported")
			}
			if strings.Contains(logs.String(), "Лекция") || strings.Contains(logs.String(), "server") {
				t.Fatalf("the log has the path: %s", logs.String())
			}
		})
	}

	for _, ok := range []string{`c:\a.pdf`, "D:/Учёба/a.txt", `Z:\x/y\Слайды.PPTX`} {
		if err := droppedPathError(ok); err != nil {
			t.Fatalf("droppedPathError(%q) = %v, want nil", ok, err)
		}
	}

	// The reader's refusal (a directory, a device, a missing file) and the size limit hold as for the dialog.
	unreadable, _, _ := newLibraryBinding(t, "", &memFiles{err: errors.New("not a regular file")})
	if _, err := unreadable.ImportDroppedFile(ctx, good); !errors.Is(err, ErrLibraryFileUnreadable) {
		t.Fatalf("ImportDroppedFile unreadable = %v", err)
	}
	large, luc, _ := newLibraryBinding(t, "", &memFiles{files: map[string][]byte{good: make([]byte, material.MaxBytes+1)}})
	if _, err := large.ImportDroppedFile(ctx, good); !errors.Is(err, ErrLibraryFileTooLarge) || luc.upload.Data != nil {
		t.Fatalf("ImportDroppedFile too large = %v", err)
	}
	if strings.Contains(logs.String(), "Лекция") {
		t.Fatalf("the log has the file name: %s", logs.String())
	}
}

func TestLibraryErrors(t *testing.T) {
	tests := []struct {
		err  error
		want error
	}{
		{library.ErrInvalidRequest, ErrLibraryInvalidID},
		{library.ErrInvalidName, ErrLibraryName},
		{library.ErrInvalidFolderName, ErrLibraryFolderName},
		{library.ErrNameTaken, ErrLibraryNameTaken},
		{library.ErrFolderTaken, ErrLibraryFolderTaken},
		{library.ErrSubjectNotFound, ErrLibrarySubjectNotFound},
		{library.ErrFolderNotFound, ErrLibraryFolderNotFound},
		{library.ErrMaterialNotFound, ErrLibraryFileNotFound},
		{library.ErrLessonNotFound, ErrLibraryLessonNotFound},
		{library.ErrNotEmpty, ErrLibraryNotEmpty},
		{library.ErrSystemFolder, ErrLibrarySystemFolder},
		{library.ErrTooMany, ErrLibraryLimit},
		{library.ErrMaterialRemoved, ErrLibraryFileRemoved},
		{library.ErrEmptyFile, ErrLibraryEmptyFile},
		{library.ErrFileTooLarge, ErrLibraryFileTooLarge},
		{library.ErrUnsupportedFile, ErrLibraryUnsupported},
		{library.ErrBusy, ErrLibraryBusy},
		{fmt.Errorf("%w: disk", library.ErrUnavailable), ErrLibraryUnavailable},
		{errors.New("other"), ErrLibraryUnavailable},
	}
	ctx := context.Background()
	for _, tt := range tests {
		s, uc, _ := newLibraryBinding(t, "x.txt", &memFiles{files: map[string][]byte{"x.txt": []byte("x")}})
		uc.err = fmt.Errorf("wrapped: %w", tt.err)
		checks := map[string]error{}
		_, checks["Subjects"] = s.Subjects(ctx)
		_, checks["CreateSubject"] = s.CreateSubject(ctx, "x", nil, "")
		_, checks["Place"] = s.Place(ctx, "m1", "s1", "")
		_, checks["ImportFile"] = s.ImportFile(ctx)
		checks["RemoveSubject"] = s.RemoveSubject(ctx, "s1")
		_, checks["Suggestions"] = s.Suggestions(ctx)
		for name, err := range checks {
			if !errors.Is(err, tt.want) {
				t.Errorf("%s with %v = %v, want %v", name, tt.err, err, tt.want)
			}
		}
	}
}
