package wails

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"paratech/desktop/internal/cases/library"
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/shared"
)

func (f *fakeLibrary) Material(_ context.Context, id shared.MaterialID) (*material.Material, error) {
	f.note("Material %s", id)
	return f.material, f.err
}

// memViews is the artifact store in memory, by the stored form of the reference.
type memViews struct {
	mu       sync.Mutex
	files    map[string][]byte
	writes   int
	sizeErr  error
	writeErr error
}

func newMemViews() *memViews { return &memViews{files: map[string][]byte{}} }

func (m *memViews) Size(_ context.Context, ref shared.ArtifactRef) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sizeErr != nil {
		return 0, m.sizeErr
	}
	data, ok := m.files[ref.String()]
	if !ok {
		return 0, fmt.Errorf("size %s: %w", ref, shared.ErrNotFound)
	}
	return int64(len(data)), nil
}

func (m *memViews) Open(_ context.Context, ref shared.ArtifactRef) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.files[ref.String()]
	if !ok {
		return nil, fmt.Errorf("open %s: %w", ref, shared.ErrNotFound)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (m *memViews) Write(_ context.Context, ref shared.ArtifactRef, content io.Reader) error {
	if m.writeErr != nil {
		return m.writeErr
	}
	data, err := io.ReadAll(content)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[ref.String()] = data
	m.writes++
	return nil
}

// fakeOpener records the references it was asked to open; nothing is launched.
type fakeOpener struct {
	refs []shared.ArtifactRef
	err  error
}

func (f *fakeOpener) OpenArtifact(_ context.Context, ref shared.ArtifactRef) error {
	f.refs = append(f.refs, ref)
	return f.err
}

var docxBytes = []byte("PK\x03\x04 a docx of the library")

// openEnv is the binding with a stored DOCX material m1 named «Реферат: история.docx».
type openEnv struct {
	svc    *LibraryService
	uc     *fakeLibrary
	views  *memViews
	opener *fakeOpener
	logs   *bytes.Buffer
	m      *material.Material
}

func newOpenEnv(t *testing.T) *openEnv {
	t.Helper()
	m, err := material.New(material.Spec{
		ID: "m1", Digest: material.DigestOf(docxBytes), Size: int64(len(docxBytes)), Type: material.TypeDOCX,
		Name: "Реферат: история.docx",
	}, libT0)
	if err != nil {
		t.Fatal(err)
	}
	blob, err := material.BlobRef(m.Digest())
	if err != nil {
		t.Fatal(err)
	}
	e := &openEnv{uc: &fakeLibrary{material: m}, views: newMemViews(), opener: &fakeOpener{}, logs: &bytes.Buffer{}, m: m}
	e.views.files[blob.String()] = docxBytes
	e.svc = NewLibraryService(e.uc, nil, nil, slog.New(slog.NewTextHandler(e.logs, nil))).withOpener(e.views, e.opener)
	return e
}

const wantView = "exports/library/m1/Реферат_ история.docx"

func TestOpenMaterialOpensACopyUnderItsName(t *testing.T) {
	e := newOpenEnv(t)
	ctx := context.Background()

	got := e.svc.OpenMaterial(ctx, "m1")
	if got != (LibraryOpen{Status: "opened", Message: "Файл открывается в программе по умолчанию."}) {
		t.Fatalf("OpenMaterial = %+v", got)
	}
	if len(e.opener.refs) != 1 || e.opener.refs[0].String() != wantView {
		t.Fatalf("opened %v, want %s", e.opener.refs, wantView)
	}
	if !bytes.Equal(e.views.files[wantView], docxBytes) || e.views.writes != 1 {
		t.Fatalf("copy %q after %d writes", e.views.files[wantView], e.views.writes)
	}

	// The copy is kept: the student's edits in it survive the next «Открыть».
	e.views.files[wantView] = []byte("edited")
	if got := e.svc.OpenMaterial(ctx, "m1"); got.Status != OpenStatusOpened {
		t.Fatalf("second OpenMaterial = %+v", got)
	}
	if string(e.views.files[wantView]) != "edited" || e.views.writes != 1 || len(e.opener.refs) != 2 {
		t.Fatalf("copy %q after %d writes, %d opens", e.views.files[wantView], e.views.writes, len(e.opener.refs))
	}
	if log := e.logs.String(); !strings.Contains(log, "material=m1") || !strings.Contains(log, "result=opened") {
		t.Fatalf("log %q", log)
	}
}

func TestOpenMaterialOutcomes(t *testing.T) {
	const (
		notFound = "Файл не найден: возможно, его удалили."
		refused  = "Этот файл нельзя открыть отсюда."
		failed   = "Не удалось открыть файл. Попробуйте ещё раз."
	)
	tests := []struct {
		name   string
		id     string
		edit   func(t *testing.T, e *openEnv)
		status string
		text   string
		code   string
	}{
		{name: "traversal id", id: `..\..\Windows\notepad.exe`, status: "refused", text: refused, code: "invalid_id"},
		{name: "path as id", id: `C:\Users\student\secret.docx`, status: "refused", text: refused, code: "invalid_id"},
		{name: "empty id", id: "", status: "refused", text: refused, code: "invalid_id"},
		{name: "no material", status: "not_found", text: notFound, code: "no_material", edit: func(_ *testing.T, e *openEnv) {
			e.uc.err = fmt.Errorf("%w: %w", library.ErrMaterialNotFound, shared.ErrNotFound)
		}},
		{name: "removed material", status: "not_found", text: notFound, code: "removed", edit: func(t *testing.T, e *openEnv) {
			if _, err := e.m.Remove(libT0); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "library unavailable", status: "failed", text: failed, code: "lookup", edit: func(_ *testing.T, e *openEnv) {
			e.uc.err = library.ErrUnavailable
		}},
		{name: "stored file gone", status: "not_found", text: notFound, code: "no_file", edit: func(_ *testing.T, e *openEnv) {
			clear(e.views.files)
		}},
		{name: "store fails", status: "failed", text: failed, code: "copy", edit: func(_ *testing.T, e *openEnv) {
			e.views.sizeErr = shared.ErrStoragePermission
		}},
		{name: "copy fails", status: "failed", text: failed, code: "copy", edit: func(_ *testing.T, e *openEnv) {
			e.views.writeErr = shared.ErrStorageFull
		}},
		{name: "opener refuses", status: "refused", text: refused, code: "refused", edit: func(_ *testing.T, e *openEnv) {
			e.opener.err = fmt.Errorf("%w: outside the root", shared.ErrInvalidArtifactRef)
		}},
		{name: "copy gone before the start", status: "not_found", text: notFound, code: "no_copy", edit: func(_ *testing.T, e *openEnv) {
			e.opener.err = fmt.Errorf("fileopen: %w", shared.ErrNotFound)
		}},
		{name: "launcher fails", status: "failed", text: failed, code: "launch", edit: func(_ *testing.T, e *openEnv) {
			e.opener.err = errors.New(`start explorer C:\Users\student\AppData\Local\paratech\secret.docx: boom`)
		}},
		{name: "not wired", status: "failed", text: failed, code: "not_wired", edit: func(_ *testing.T, e *openEnv) {
			e.svc.opener = nil
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newOpenEnv(t)
			if tt.edit != nil {
				tt.edit(t, e)
			}
			id := tt.id
			if id == "" && tt.code != "invalid_id" {
				id = "m1"
			}
			got := e.svc.OpenMaterial(context.Background(), id)
			if got != (LibraryOpen{Status: tt.status, Message: tt.text}) {
				t.Fatalf("OpenMaterial = %+v, want %s «%s»", got, tt.status, tt.text)
			}
			log := e.logs.String()
			if !strings.Contains(log, "result="+tt.code) {
				t.Fatalf("log %q lacks the code %s", log, tt.code)
			}
			for _, leak := range []string{"Реферат", "secret", "Users", "boom", "exports", "materials"} {
				if strings.Contains(log, leak) {
					t.Fatalf("log %q names %q", log, leak)
				}
			}
		})
	}
}

// A refused ID never reaches the library or the launcher.
func TestOpenMaterialRefusedIDTouchesNothing(t *testing.T) {
	e := newOpenEnv(t)
	e.svc.OpenMaterial(context.Background(), "../m1")
	if len(e.uc.calls) != 0 || len(e.opener.refs) != 0 || e.views.writes != 0 {
		t.Fatalf("calls %v, opens %v, writes %d", e.uc.calls, e.opener.refs, e.views.writes)
	}
}

func TestOpenMaterialExtensionByType(t *testing.T) {
	for _, typ := range material.Types() {
		e := newOpenEnv(t)
		m, err := material.New(material.Spec{
			ID: "m1", Digest: e.m.Digest(), Size: e.m.Size(), Type: typ, Name: "file.bin",
		}, libT0)
		if err != nil {
			t.Fatal(err)
		}
		e.uc.material = m
		if got := e.svc.OpenMaterial(context.Background(), "m1"); got.Status != OpenStatusOpened {
			t.Fatalf("%s: %+v", typ, got)
		}
		if want := "exports/library/m1/file." + string(typ); e.opener.refs[0].String() != want {
			t.Fatalf("%s: opened %s, want %s", typ, e.opener.refs[0], want)
		}
	}
}

func TestOpenName(t *testing.T) {
	tests := []struct{ in, want string }{
		{in: "Реферат: история.docx", want: "Реферат_ история"},
		{in: "slides (final).pptx", want: "slides (final)"},
		{in: "a,b;c\"d%e.docx", want: "a_b_c_d_e"},
		{in: "  many   spaces\t.txt", want: "many spaces"},
		{in: "con.docx", want: "con"}, // the reference refuses it; viewRef falls back
		{in: "...docx", want: "файл"},
		{in: "отчёт.v2.final.docx", want: "отчёт_v2_final"},
		{in: ".docx", want: "файл"},
		{in: "", want: "файл"},
		{in: "%%%.docx", want: "файл"},
		{in: strings.Repeat("я", 100) + ".docx", want: strings.Repeat("я", maxOpenName)},
	}
	for _, tt := range tests {
		if got := openName(tt.in); got != tt.want {
			t.Errorf("openName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestViewRefFallsBackForAReservedName(t *testing.T) {
	m, err := material.New(material.Spec{
		ID: "m1", Digest: material.DigestOf(docxBytes), Size: 1, Type: material.TypeDOCX, Name: "CON.docx",
	}, libT0)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := viewRef(m)
	if err != nil || ref.String() != "exports/library/m1/файл.docx" {
		t.Fatalf("viewRef = %s, %v", ref, err)
	}
}

func TestOpenServicesNeedsTheOpener(t *testing.T) {
	for name, edit := range map[string]func(*Core){
		"views":  func(c *Core) { c.MaterialViews = nil },
		"opener": func(c *Core) { c.Opener = nil },
	} {
		opts := validOptions()
		opts.OpenCore = func(context.Context) (Core, error) {
			c := validCore()
			edit(&c)
			return c, nil
		}
		s, err := New(opts)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.OpenServices(context.Background()); err == nil {
			t.Errorf("no %s: OpenServices succeeded", name)
		}
	}
}
