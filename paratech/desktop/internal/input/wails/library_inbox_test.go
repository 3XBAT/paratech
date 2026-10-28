package wails

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"paratech/desktop/internal/cases/library"
	"paratech/desktop/internal/domain/library/services/placement"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
)

// PlacementHint of the plain fake: no hint, or its error.
func (f *fakeLibrary) PlacementHint(_ context.Context, id shared.MaterialID) (library.Placement, error) {
	f.note("PlacementHint %s", id)
	return library.Placement{}, f.err
}

// hintLibrary is the fake with a hint to return.
type hintLibrary struct {
	*fakeLibrary
	hint library.Placement
}

func (h *hintLibrary) PlacementHint(ctx context.Context, id shared.MaterialID) (library.Placement, error) {
	if _, err := h.fakeLibrary.PlacementHint(ctx, id); err != nil {
		return library.Placement{}, err
	}
	return h.hint, nil
}

func folderName(t *testing.T, sys subject.System) subject.FolderName {
	t.Helper()
	n, ok := subject.SystemFolderName(sys)
	if !ok {
		t.Fatalf("no system folder %q", sys)
	}
	return n
}

func TestLibraryPlacementHint(t *testing.T) {
	ctx := context.Background()
	sub := testSubject(t)
	labs, _ := sub.SystemFolder(subject.SystemLabs)
	tests := []struct {
		name string
		hint library.Placement
		want LibraryPlacementHint
	}{
		{"existing", library.Placement{
			Kind: placement.KindExisting, Subject: sub, Folder: labs, FolderKind: subject.SystemLabs,
			FolderName: folderName(t, subject.SystemLabs),
		}, LibraryPlacementHint{
			Kind: "existing", SubjectID: "s1", SubjectName: "Физика", FolderID: "s1-labs", FolderName: "Лабораторные",
			FolderKind: "labs",
		}},
		{"create", library.Placement{
			Kind: placement.KindCreate, FolderKind: subject.SystemSeminars, FolderName: folderName(t, subject.SystemSeminars),
			CreateName: "Сети",
		}, LibraryPlacementHint{Kind: "create", FolderName: "Семинары", FolderKind: "seminars", CreateName: "Сети"}},
		{"none", library.Placement{}, LibraryPlacementHint{Kind: "none"}},
		{"existing without a subject is none", library.Placement{Kind: placement.KindExisting}, LibraryPlacementHint{Kind: "none"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, uc, _ := newLibraryBinding(t, "", &memFiles{})
			s.uc = &hintLibrary{fakeLibrary: uc, hint: tt.hint}
			got, err := s.PlacementHint(ctx, "m1")
			if err != nil || got != tt.want {
				t.Fatalf("PlacementHint = %+v, %v; want %+v", got, err, tt.want)
			}
			if len(uc.calls) != 1 || uc.calls[0] != "PlacementHint m1" {
				t.Fatalf("calls = %q", uc.calls)
			}
		})
	}
}

func TestLibraryPlacementHintJSON(t *testing.T) {
	data, err := json.Marshal(LibraryPlacementHint{Kind: "none"})
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"kind":"none","subjectId":"","subjectName":"","folderId":"","folderName":"","folderKind":"","createName":""}`
	if string(data) != want {
		t.Fatalf("json = %s; want %s", data, want)
	}
}

func TestLibraryPlacementHintRefusals(t *testing.T) {
	ctx := context.Background()
	s, uc, logs := newLibraryBinding(t, "", &memFiles{})
	for _, id := range []string{"", "a b", `..\x`, strings.Repeat("x", 300)} {
		if _, err := s.PlacementHint(ctx, id); !errors.Is(err, ErrLibraryInvalidID) {
			t.Errorf("PlacementHint(%q) = %v", id, err)
		}
	}
	if len(uc.calls) != 0 {
		t.Fatalf("an invalid id reached the use case: %q", uc.calls)
	}

	uc.err = library.ErrMaterialNotFound
	if _, err := s.PlacementHint(ctx, "m9"); !errors.Is(err, ErrLibraryFileNotFound) {
		t.Fatalf("PlacementHint(missing) = %v", err)
	}
	uc.err = errors.New("disk")
	if _, err := s.PlacementHint(ctx, "m9"); !errors.Is(err, ErrLibraryUnavailable) {
		t.Fatalf("PlacementHint(failing) = %v", err)
	}
	if !strings.Contains(logs.String(), opLibraryHint) {
		t.Fatalf("log %s", logs.String())
	}
}

func TestLibraryImportFileTo(t *testing.T) {
	ctx := context.Background()
	const path = `C:\Users\student\Documents\Сети_лр2.docx`
	files := &memFiles{files: map[string][]byte{path: []byte("bytes")}}

	s, uc, logs := newLibraryBinding(t, path, files)
	res, err := s.ImportFileTo(ctx, "s1", "s1-labs")
	if err != nil || res.Cancelled || res.Material == nil || res.Material.ID != "m1" {
		t.Fatalf("ImportFileTo = %+v, %v", res, err)
	}
	want := library.Upload{Name: "Сети_лр2.docx", Data: []byte("bytes"), Subject: "s1", Folder: "s1-labs"}
	if uc.upload.Name != want.Name || string(uc.upload.Data) != "bytes" || uc.upload.Subject != want.Subject || uc.upload.Folder != want.Folder {
		t.Fatalf("upload %+v; want %+v", uc.upload, want)
	}
	if _, err := s.ImportFileTo(ctx, "s1", ""); err != nil || uc.upload.Folder != "" || uc.upload.Subject != "s1" {
		t.Fatalf("ImportFileTo(«Материалы») = %v, upload %+v", err, uc.upload)
	}

	// ImportFile keeps «Входящие».
	if _, err := s.ImportFile(ctx); err != nil || uc.upload.Subject != "" || uc.upload.Folder != "" {
		t.Fatalf("ImportFile = %v, upload %+v", err, uc.upload)
	}

	cancelled, cuc, _ := newLibraryBinding(t, "", files)
	if res, err := cancelled.ImportFileTo(ctx, "s1", ""); err != nil || !res.Cancelled || res.Material != nil || cuc.upload.Data != nil {
		t.Fatalf("ImportFileTo cancelled = %+v, %v", res, err)
	}

	for _, ids := range [][2]string{{"", ""}, {"a b", ""}, {"s1", "a/b"}, {"", "s1-labs"}} {
		picked := false
		bad := &LibraryService{
			uc: uc, files: files, log: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
			picker: materialPickerFunc(func(context.Context) (string, error) { picked = true; return path, nil }),
		}
		if _, err := bad.ImportFileTo(ctx, ids[0], ids[1]); !errors.Is(err, ErrLibraryInvalidID) || picked {
			t.Errorf("ImportFileTo(%q, %q) = %v, dialog shown %v", ids[0], ids[1], err, picked)
		}
	}

	for _, tt := range []struct{ err, want error }{
		{library.ErrSubjectNotFound, ErrLibrarySubjectNotFound},
		{library.ErrFolderNotFound, ErrLibraryFolderNotFound},
	} {
		uc.err = tt.err
		if _, err := s.ImportFileTo(ctx, "s1", "s1-labs"); !errors.Is(err, tt.want) {
			t.Errorf("ImportFileTo with %v = %v, want %v", tt.err, err, tt.want)
		}
	}
	if strings.Contains(logs.String(), "student") || strings.Contains(logs.String(), "Сети") {
		t.Fatalf("the log has the path or the file name: %s", logs.String())
	}
}

// The results carry no path: only the base name, as the material is named.
func TestLibraryImportFileToNoPath(t *testing.T) {
	const path = `C:\Users\student\Documents\x.docx`
	s, _, _ := newLibraryBinding(t, path, &memFiles{files: map[string][]byte{path: []byte("bytes")}})
	res, err := s.ImportFileTo(context.Background(), "s1", "")
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "student") || strings.Contains(string(data), `\\`) {
		t.Fatalf("result has a path: %s", data)
	}
}
