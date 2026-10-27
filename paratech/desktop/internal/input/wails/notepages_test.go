package wails

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"paratech/desktop/internal/cases/library"
	"paratech/desktop/internal/cases/notes"
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/note"
	"paratech/desktop/internal/domain/shared"
)

// fakePages answers the photo pages it holds by id; err, when set, is returned instead.
type fakePages struct {
	pages map[shared.MaterialID]notes.Page
	err   error
	ids   []shared.MaterialID
}

func (f *fakePages) PageImage(_ context.Context, id shared.MaterialID) (notes.Page, error) {
	f.ids = append(f.ids, id)
	if f.err != nil {
		return notes.Page{}, f.err
	}
	p, ok := f.pages[id]
	if !ok {
		return notes.Page{}, fmt.Errorf("fake: %w", notes.ErrMaterialNotFound)
	}
	return p, nil
}

func testPhoto(t testing.TB, id shared.MaterialID, typ material.Type) *material.Material {
	t.Helper()
	m, err := material.New(material.Spec{ID: id, Digest: material.DigestOf([]byte(id)), Size: 4, Type: typ, Name: "стр.jpg"}, libT0)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestMediaServesNotePages(t *testing.T) {
	h, audio, nextCalls := newMediaFixture([]byte("RIFF"))
	src := &fakePages{pages: map[shared.MaterialID]notes.Page{
		"p1":  {Material: testPhoto(t, "p1", material.TypeJPEG), Data: []byte("\xff\xd8\xffJPEG")},
		"p2":  {Material: testPhoto(t, "p2", material.TypePNG), Data: []byte("\x89PNG")},
		"doc": {Material: testPart(t, "doc", "a.txt"), Data: []byte("<svg onload=x>")}, // never served
		"p3":  {Material: testPhoto(t, "other", material.TypePNG), Data: []byte("\x89PNG")},
	}}

	// Until the core is open nothing is found.
	if rec := serve(h, http.MethodGet, mediaNotePagePath+"p1", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("before setPages: %d", rec.Code)
	}
	h.setPages(src)

	for _, tt := range []struct{ id, ctype, body string }{
		{"p1", "image/jpeg", "\xff\xd8\xffJPEG"},
		{"p2", "image/png", "\x89PNG"},
	} {
		rec := serve(h, http.MethodGet, notePageURL(shared.MaterialID(tt.id)), nil)
		hdr := rec.Header()
		if rec.Code != http.StatusOK || rec.Body.String() != tt.body || hdr.Get("Content-Type") != tt.ctype ||
			hdr.Get("X-Content-Type-Options") != "nosniff" || hdr.Get("Cache-Control") != "no-store" ||
			hdr.Get("Content-Security-Policy") != notePageCSP || hdr.Get("Content-Length") != fmt.Sprint(len(tt.body)) {
			t.Fatalf("%s: %d %q %v", tt.id, rec.Code, rec.Body.String(), hdr)
		}
	}
	if rec := serve(h, http.MethodHead, mediaNotePagePath+"p1", nil); rec.Code != http.StatusOK || rec.Body.Len() != 0 ||
		rec.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("HEAD: %d %q", rec.Code, rec.Body.String())
	}

	tests := []struct {
		name, method, target string
		code                 int
	}{
		{"not a photo", http.MethodGet, mediaNotePagePath + "doc", http.StatusNotFound},
		{"another material answered", http.MethodGet, mediaNotePagePath + "p3", http.StatusNotFound},
		{"missing", http.MethodGet, mediaNotePagePath + "p9", http.StatusNotFound},
		{"a path", http.MethodGet, mediaNotePagePath + "..%2Fp1", http.StatusNotFound},
		{"a dot", http.MethodGet, mediaNotePagePath + "..", http.StatusNotFound},
		{"empty", http.MethodGet, mediaNotePagePath, http.StatusNotFound},
		{"post", http.MethodPost, mediaNotePagePath + "p1", http.StatusMethodNotAllowed},
	}
	for _, tt := range tests {
		rec := serve(h, tt.method, tt.target, nil)
		if rec.Code != tt.code || rec.Header().Get("Content-Type") == "image/jpeg" || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s: %d %v", tt.name, rec.Code, rec.Header())
		}
	}
	// "../p1" decoded is refused before the use case is asked; the other bad ids too.
	for _, id := range src.ids {
		if id == "../p1" || id == ".." || id == "" {
			t.Fatalf("the use case was asked for %q", id)
		}
	}

	src.err = errors.New("disk")
	if rec := serve(h, http.MethodGet, mediaNotePagePath+"p1", nil); rec.Code != http.StatusInternalServerError {
		t.Fatalf("failing use case: %d", rec.Code)
	}
	src.err = fmt.Errorf("w: %w", notes.ErrInvalidRequest)
	if rec := serve(h, http.MethodGet, mediaNotePagePath+"p1", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("invalid id: %d", rec.Code)
	}

	// The recordings and the assets are where they were.
	if rec := serve(h, http.MethodGet, mediaRecordingPath+"r1", nil); rec.Code != http.StatusOK || audio.opened != 1 {
		t.Fatalf("recording: %d", rec.Code)
	}
	if rec := serve(h, http.MethodGet, "/media/notepages", nil); rec.Body.String() != "asset" || *nextCalls != 1 {
		t.Fatalf("asset: %q", rec.Body.String())
	}
}

func TestPageContentType(t *testing.T) {
	for _, typ := range material.Types() {
		ctype, ok := pageContentType(typ)
		if ok != typ.Image() || (ok && ctype != "image/"+string(typ)) {
			t.Errorf("%s: %q, %v", typ, ctype, ok)
		}
	}
	if _, ok := pageContentType("svg"); ok {
		t.Fatal("svg has a content type")
	}
}

// A photo part has the address of its page by id; the note tells the pages without text.
func TestNoteViewPhotoPages(t *testing.T) {
	n := testNote(t, "n1", note.AnchorUndated, "p1", "m2", "p3")
	gone := testPhoto(t, "p3", material.TypePNG)
	if _, err := gone.Remove(libT0); err != nil {
		t.Fatal(err)
	}
	v := noteViewOf(notes.View{
		Note: n, Anchor: n.Anchor(), NoTextPages: 1,
		Parts: []*material.Material{testPhoto(t, "p1", material.TypeJPEG), testPart(t, "m2", "a.txt"), gone},
	})
	if v.NoTextPages != 1 || v.Parts[0].PageURL != "/media/notepage/p1" || v.Parts[0].TextStatus != "no_text" ||
		v.Parts[0].Type != "jpeg" || v.Parts[1].PageURL != "" || v.Parts[2].PageURL != "" || !v.Parts[2].Removed {
		t.Fatalf("view %+v", v)
	}
	assertNoNotePath(t, v)
}

func TestNotesPhotoErrors(t *testing.T) {
	tests := []struct{ err, want error }{
		{library.ErrImageTooLarge, ErrNotesImageTooLarge},
		{library.ErrUnsupportedImage, ErrNotesImageFormat},
		{library.ErrImageUnreadable, ErrNotesImageUnreadable},
	}
	ctx := context.Background()
	for _, tt := range tests {
		s, uc, _, _ := newNotesBinding(t, nil)
		uc.err = fmt.Errorf("wrapped: %w", tt.err)
		if _, err := s.Paste(ctx, recTarget, "x"); !errors.Is(err, tt.want) {
			t.Errorf("%v = %v, want %v", tt.err, err, tt.want)
		}
		if !fileError(tt.err) {
			t.Errorf("%v does not refuse one file only", tt.err)
		}
	}
}

func TestNoteDroppedPathError(t *testing.T) {
	tests := []struct {
		path string
		want error
	}{
		{dropDir + `\стр1.jpg`, nil},
		{dropDir + `\стр2.JPEG`, nil},
		{dropDir + `\стр3.png`, nil},
		{dropDir + `\лекция.docx`, nil},
		{dropDir + `\IMG_0001.HEIC`, ErrNotesImageFormat},
		{dropDir + `\схема.svg`, ErrNotesImageFormat},
		{dropDir + `\anim.gif`, ErrNotesImageFormat},
		{dropDir + `\x.webp`, ErrNotesImageFormat},
		{dropDir + `\setup.exe`, ErrLibraryUnsupported},
		{dropDir + `\стр.jpg.lnk`, ErrLibraryUnsupported},
		{`\\server\share\стр.jpg`, ErrLibraryDroppedPath},
		{dropDir + `\..\стр.jpg`, ErrLibraryDroppedPath},
		{dropDir + `\стр.jpg:stream`, ErrLibraryDroppedPath},
	}
	for _, tt := range tests {
		if err := noteDroppedPathError(tt.path); !errors.Is(err, tt.want) || (tt.want == nil && err != nil) {
			t.Errorf("%s: %v, want %v", tt.path, err, tt.want)
		}
	}
	// A drop into «Входящие» still takes documents only.
	if err := droppedPathError(dropDir + `\стр1.jpg`); !errors.Is(err, ErrLibraryUnsupported) {
		t.Fatalf("library drop of a photo: %v", err)
	}
}

func TestNotesDropPhoto(t *testing.T) {
	ctx := context.Background()
	jpg, heic := dropDir+`\стр1.jpg`, dropDir+`\стр2.heic`
	files := map[string][]byte{jpg: []byte("\xff\xd8\xff"), heic: []byte("heic")}
	attrs := map[string]string{dropNoteKind: "recording", dropNoteSubject: "s1", dropNoteRecording: "r1"}
	lib, _, _, _ := newDropBinding(t, files)
	nts, uc, _, _ := newNotesBinding(t, files)
	em := &fakeEmitter{}
	routeDrop(ctx, lib, nts, attrs, []string{jpg, heic}, em)

	d := notesDropped(t, em)
	if len(d.Files) != 2 || d.Files[0].Error != "" || d.Files[1].Name != "стр2.heic" || d.Files[1].Error != ErrNotesImageFormat.Error() {
		t.Fatalf("drop %+v", d.Files)
	}
	if len(uc.adds) != 1 || uc.adds[0].Uploads[0].Name != "стр1.jpg" {
		t.Fatalf("adds %+v", uc.adds)
	}
}
