package wails

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"paratech/desktop/internal/domain/library/material"
)

// Windows paths on every OS: the check is string work and the fake reader never touches the disk.
const dropDir = `C:\Users\student\Документы`

// countingFiles records every path read.
type countingFiles struct {
	*memFiles
	read []string
}

func (c *countingFiles) ReadFile(ctx context.Context, path string, maxBytes int64) ([]byte, error) {
	c.read = append(c.read, path)
	return c.memFiles.ReadFile(ctx, path, maxBytes)
}

func newDropBinding(t *testing.T, files map[string][]byte) (*LibraryService, *fakeLibrary, *countingFiles, *bytes.Buffer) {
	t.Helper()
	cf := &countingFiles{memFiles: &memFiles{files: files}}
	s, uc, logs := newLibraryBinding(t, "", &memFiles{})
	s.files = cf
	return s, uc, cf, logs
}

// dropped returns the one LibraryDrop emitted.
func dropped(t *testing.T, em *fakeEmitter) LibraryDrop {
	t.Helper()
	if len(em.events) != 1 || em.events[0].name != LibraryDropEvent || len(em.events[0].data) != 1 {
		t.Fatalf("events = %+v, want one %s", em.events, LibraryDropEvent)
	}
	d, ok := em.events[0].data[0].(LibraryDrop)
	if !ok {
		t.Fatalf("payload %T, want LibraryDrop", em.events[0].data[0])
	}
	return d
}

// assertNoPath fails if the JSON of the payload has any part of the folder of the dropped files.
func assertNoPath(t *testing.T, d LibraryDrop, parts ...string) {
	t.Helper()
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range append([]string{"Users", "student", "Документы", `\\`, ":/"}, parts...) {
		if strings.Contains(string(b), p) {
			t.Fatalf("payload %s has %q of the path", b, p)
		}
	}
}

func TestLibraryDropImports(t *testing.T) {
	ctx := context.Background()
	good := dropDir + `\Лекция 1.DOCX`
	s, uc, files, logs := newDropBinding(t, map[string][]byte{good: []byte("bytes")})
	uc.existing = true
	em := &fakeEmitter{}

	s.importDropped(ctx, []string{good}, em)

	d := dropped(t, em)
	if d.Error != "" || len(d.Files) != 1 {
		t.Fatalf("drop = %+v", d)
	}
	f := d.Files[0]
	if f.Name != "Лекция 1.DOCX" || !f.Existing || f.Error != "" || f.Material == nil || f.Material.ID != "m1" ||
		f.Material.Type != "pdf" {
		t.Fatalf("file = %+v", f)
	}
	if uc.upload.Name != "Лекция 1.DOCX" || string(uc.upload.Data) != "bytes" || files.limit != material.MaxBytes {
		t.Fatalf("upload %q %q, limit %d: want the base name only", uc.upload.Name, uc.upload.Data, files.limit)
	}
	assertNoPath(t, d)
	if strings.Contains(logs.String(), "student") || strings.Contains(logs.String(), "Лекция") {
		t.Fatalf("the log has the path: %s", logs.String())
	}
}

func TestLibraryDropRefusesPaths(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name     string
		path     string
		want     error
		wantName string
	}{
		{"empty", "", ErrLibraryDroppedPath, ""},
		{"relative", "Лекция.docx", ErrLibraryDroppedPath, ""},
		{"relative up", `..\Лекция.docx`, ErrLibraryDroppedPath, ""},
		{"traversal", dropDir + `\..\Лекция.docx`, ErrLibraryDroppedPath, ""},
		{"traversal, slashes", "C:/Users/../Лекция.docx", ErrLibraryDroppedPath, ""},
		{"dot segment", dropDir + `\.\Лекция.docx`, ErrLibraryDroppedPath, ""},
		{"network share", `\\server\share\Лекция.docx`, ErrLibraryDroppedPath, ""},
		{"network share, slashes", "//server/share/Лекция.docx", ErrLibraryDroppedPath, ""},
		{"network share, mixed", `\/server/share/Лекция.docx`, ErrLibraryDroppedPath, ""},
		{"network share, mixed other way", `/\server\share\Лекция.docx`, ErrLibraryDroppedPath, ""},
		{"device path", `\\?\C:\Users\student\Лекция.docx`, ErrLibraryDroppedPath, ""},
		{"device namespace", `\\.\C:\Users\student\Лекция.docx`, ErrLibraryDroppedPath, ""},
		{"rooted without drive", `\Users\student\Лекция.docx`, ErrLibraryDroppedPath, ""},
		{"rooted without drive, slash", "/server/Лекция.docx", ErrLibraryDroppedPath, ""},
		{"drive-relative", `C:Лекция.docx`, ErrLibraryDroppedPath, ""},
		{"non-letter drive", `1:\Лекция.docx`, ErrLibraryDroppedPath, ""},
		{"non-ASCII drive", `Ж:\Лекция.docx`, ErrLibraryDroppedPath, ""},
		{"alternate stream", dropDir + `\a.docx:hidden.txt`, ErrLibraryDroppedPath, ""},
		{"nul byte", dropDir + "\\a\x00.docx", ErrLibraryDroppedPath, ""},
		{"not UTF-8", dropDir + "\\a\xff.docx", ErrLibraryDroppedPath, ""},
		{"too long", dropDir + `\` + strings.Repeat("a", maxDroppedPath) + ".docx", ErrLibraryDroppedPath, ""},
		{"executable", dropDir + `\setup.exe`, ErrLibraryUnsupported, "setup.exe"},
		{"no extension", dropDir + `\README`, ErrLibraryUnsupported, "README"},
		{"dot in folder only", `C:\Users\a.docx\README`, ErrLibraryUnsupported, "README"},
		{"shortcut", dropDir + `\Лекция.docx.lnk`, ErrLibraryUnsupported, "Лекция.docx.lnk"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, uc, files, logs := newDropBinding(t, map[string][]byte{tt.path: []byte("bytes")})
			em := &fakeEmitter{}

			s.importDropped(ctx, []string{tt.path}, em)

			d := dropped(t, em)
			want := LibraryDropFile{Name: tt.wantName, Error: tt.want.Error()}
			if d.Error != "" || !reflect.DeepEqual(d.Files, []LibraryDropFile{want}) {
				t.Fatalf("drop = %+v, want %+v", d, want)
			}
			if len(files.read) != 0 || uc.upload.Data != nil {
				t.Fatalf("a refused path was read or imported")
			}
			assertNoPath(t, d, "server", "hidden")
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
}

func TestLibraryDropEachFileOnItsOwn(t *testing.T) {
	ctx := context.Background()
	good := dropDir + `\a.pdf`
	unreadable := dropDir + `\gone.txt`
	large := dropDir + `\big.pptx`
	s, _, files, _ := newDropBinding(t, map[string][]byte{good: []byte("x"), large: make([]byte, material.MaxBytes+1)})
	em := &fakeEmitter{}

	s.importDropped(ctx, []string{`\\server\share\a.pdf`, unreadable, good, large, dropDir + `\x.exe`}, em)

	d := dropped(t, em)
	if d.Error != "" || len(d.Files) != 5 {
		t.Fatalf("drop = %+v", d)
	}
	wantErr := []string{
		ErrLibraryDroppedPath.Error(), ErrLibraryFileUnreadable.Error(), "", ErrLibraryFileTooLarge.Error(),
		ErrLibraryUnsupported.Error(),
	}
	wantName := []string{"", "gone.txt", "a.pdf", "big.pptx", "x.exe"}
	for i, f := range d.Files {
		if f.Error != wantErr[i] || f.Name != wantName[i] || (f.Material != nil) != (wantErr[i] == "") {
			t.Errorf("file %d = %+v, want error %q name %q", i, f, wantErr[i], wantName[i])
		}
	}
	if want := []string{unreadable, good, large}; !reflect.DeepEqual(files.read, want) {
		t.Fatalf("read %q, want %q", files.read, want)
	}
	assertNoPath(t, d, "server")
}

func TestLibraryDropTooManyFiles(t *testing.T) {
	paths := make([]string, maxDroppedFiles+1)
	for i := range paths {
		paths[i] = dropDir + `\a.pdf`
	}
	s, uc, files, _ := newDropBinding(t, map[string][]byte{paths[0]: []byte("x")})
	em := &fakeEmitter{}

	s.importDropped(context.Background(), paths, em)

	d := dropped(t, em)
	if d.Error != ErrLibraryTooManyDropped.Error() || len(d.Files) != 0 {
		t.Fatalf("drop = %+v", d)
	}
	if len(files.read) != 0 || uc.upload.Data != nil {
		t.Fatal("a drop of too many files was read")
	}

	// The cap itself still imports.
	em = &fakeEmitter{}
	s.importDropped(context.Background(), paths[:maxDroppedFiles], em)
	if d := dropped(t, em); d.Error != "" || len(d.Files) != maxDroppedFiles {
		t.Fatalf("drop at the cap = %+v", d)
	}
}

func TestLibraryDropNothing(t *testing.T) {
	s, _, _, _ := newDropBinding(t, nil)
	em := &fakeEmitter{}
	s.importDropped(context.Background(), nil, em)
	if len(em.events) != 0 {
		t.Fatalf("events = %+v, want none for an empty drop", em.events)
	}
}

func TestLibraryDropImportFailure(t *testing.T) {
	good := dropDir + `\a.pdf`
	s, uc, _, _ := newDropBinding(t, map[string][]byte{good: []byte("x")})
	uc.err = errors.New("disk")
	em := &fakeEmitter{}

	s.importDropped(context.Background(), []string{good}, em)

	want := []LibraryDropFile{{Name: "a.pdf", Error: ErrLibraryUnavailable.Error()}}
	if d := dropped(t, em); !reflect.DeepEqual(d.Files, want) {
		t.Fatalf("files = %+v, want %+v", d.Files, want)
	}
}
