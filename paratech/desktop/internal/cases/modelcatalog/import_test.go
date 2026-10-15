package modelcatalog

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/values"
)

// importFile is a file the user picked; size is what Open reports, content what reading yields.
type importFile struct {
	content []byte
	size    int64
}

// fakeImportSource serves picked files by path and counts the readers left open.
type fakeImportSource struct {
	mu     sync.Mutex
	files  map[string]importFile
	opens  int
	open   int
	onRead func() // runs before every read
}

func newFakeImportSource() *fakeImportSource {
	return &fakeImportSource{files: map[string]importFile{}}
}

func (s *fakeImportSource) put(path string, content []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[path] = importFile{content: content, size: int64(len(content))}
}

// putSized stores a file whose reported size differs from its content: it changes while it is read.
func (s *fakeImportSource) putSized(path string, content []byte, size int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[path] = importFile{content: content, size: size}
}

func (s *fakeImportSource) Open(ctx context.Context, path string) (io.ReadCloser, int64, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.files[path]
	if !ok {
		return nil, 0, fmt.Errorf("import file: %w", shared.ErrNotFound)
	}
	s.opens++
	s.open++
	return &importReader{s: s, r: hookedReader{r: bytes.NewReader(f.content), hook: s.onRead}}, f.size, nil
}

func (s *fakeImportSource) counts() (opens, open int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.opens, s.open
}

type importReader struct {
	s    *fakeImportSource
	r    io.Reader
	once sync.Once
}

func (r *importReader) Read(p []byte) (int, error) { return r.r.Read(p) }

func (r *importReader) Close() error {
	r.once.Do(func() {
		r.s.mu.Lock()
		r.s.open--
		r.s.mu.Unlock()
	})
	return nil
}

type importEnv struct {
	*env
	src   *fakeImportSource
	space *fakeSpace
	im    *Importer
}

const pickedPath = `C:\Users\student\Downloads\model.bin`

func newImportEnv(t *testing.T) *importEnv {
	t.Helper()
	e := newEnv(t)
	src := newFakeImportSource()
	space := &fakeSpace{free: 1 << 40}
	im, err := NewImporter(e.svc, ImportDeps{Source: src, Space: space})
	if err != nil {
		t.Fatal(err)
	}
	return &importEnv{env: e, src: src, space: space, im: im}
}

// requireClean checks that nothing is installed, no file is left in the store and every reader is closed.
func (e *importEnv) requireClean() {
	e.t.Helper()
	installed, err := e.svc.ListInstalled(context.Background())
	if err != nil {
		e.t.Fatal(err)
	}
	if len(installed) != 0 {
		e.t.Fatalf("installed = %+v, want none", installed)
	}
	e.w.mu.Lock()
	n := len(e.w.files)
	e.w.mu.Unlock()
	if n != 0 {
		e.t.Fatalf("%d files left in the store, want none", n)
	}
	e.requireClosed()
}

func (e *importEnv) requireClosed() {
	e.t.Helper()
	if _, open := e.src.counts(); open != 0 {
		e.t.Fatalf("%d import readers left open", open)
	}
}

// storedFiles returns the references of the files in the store.
func (e *importEnv) storedFiles() []shared.ArtifactRef {
	e.w.mu.Lock()
	defer e.w.mu.Unlock()
	out := make([]shared.ArtifactRef, 0, len(e.w.files))
	for ref := range e.w.files {
		out = append(out, ref)
	}
	return out
}

func TestNewImporterRequiresEveryPort(t *testing.T) {
	e := newEnv(t)
	src, space := newFakeImportSource(), &fakeSpace{}
	for name, tc := range map[string]struct {
		svc *Service
		d   ImportDeps
	}{
		"service": {nil, ImportDeps{Source: src, Space: space}},
		"source":  {e.svc, ImportDeps{Space: space}},
		"space":   {e.svc, ImportDeps{Source: src}},
	} {
		if _, err := NewImporter(tc.svc, tc.d); !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("without %s: %v, want ErrInvalidConfig", name, err)
		}
	}
}

// TestSourcePage is AC 1 of TASK-028: the page for the browser, the source URL when the manifest names none.
func TestSourcePage(t *testing.T) {
	withPage := strings.Replace(catBase.json(), `"quantization"`, `"source_page": "https://Example.com/models/base", "quantization"`, 1)
	e := newEnv(t)
	e.w.manifest = []byte(`{"schema_version": 1, "models": [` + catTiny.json() + `, ` + withPage + `]}`)
	ctx := context.Background()

	cases := []struct {
		id   values.CatalogID
		want string
	}{
		{"base-q5_1", "https://example.com/models/base"},
		{"tiny-q5_1", catTiny.url()},
	}
	for _, tc := range cases {
		got, err := e.svc.SourcePage(ctx, tc.id)
		if err != nil || got.String() != tc.want {
			t.Errorf("SourcePage(%s) = %q, %v; want %q", tc.id, got, err, tc.want)
		}
	}

	if _, err := e.svc.SourcePage(ctx, "nope"); !errors.Is(err, ErrNotInCatalog) {
		t.Fatalf("unknown id: %v, want ErrNotInCatalog", err)
	}
	e.w.manifest = []byte(`{"schema_version": 1, "models": [` +
		strings.Replace(catBase.json(), `"quantization"`, `"source_page": "javascript:alert(1)", "quantization"`, 1) + `]}`)
	if _, err := e.svc.SourcePage(ctx, "base-q5_1"); !errors.Is(err, values.ErrInvalidCatalog) {
		t.Fatalf("manifest with a bad page: %v, want values.ErrInvalidCatalog", err)
	}
}

// TestImportCatalogInstallsVerifiedFile is AC 2 of TASK-028: the copy goes through the store under a fresh name and
// its SHA-256 matches the catalog.
func TestImportCatalogInstallsVerifiedFile(t *testing.T) {
	e := newImportEnv(t)
	e.src.put(pickedPath, catBase.content())

	m, err := e.im.ImportCatalog(context.Background(), values.CatalogID(catBase.id), pickedPath)
	if err != nil {
		t.Fatal(err)
	}
	if m.CatalogID != values.CatalogID(catBase.id) || m.SHA256 != catBase.sum() || m.SizeBytes != catBase.size {
		t.Fatalf("ImportCatalog = %+v", m)
	}

	files := e.storedFiles()
	if len(files) != 1 || files[0].Kind() != shared.ArtifactModels || !strings.HasPrefix(files[0].Path(), catBase.id+"-") ||
		!strings.HasSuffix(files[0].Path(), installedExt) {
		t.Fatalf("stored files = %v, want one fresh name below models/", files)
	}
	installed, err := e.svc.ListInstalled(context.Background())
	if err != nil || len(installed) != 1 || installed[0].ID != m.ID {
		t.Fatalf("ListInstalled = %+v, %v", installed, err)
	}
	e.requireClosed()
}

// TestImportCatalogRejects is test step 2 of TASK-028 and PRD section 14 case 13: a refused file is not installed
// and its copy is gone.
func TestImportCatalogRejects(t *testing.T) {
	corrupt := catBase.content()
	corrupt[len(corrupt)/2] ^= 0xff

	cases := []struct {
		name      string
		id        values.CatalogID
		prepare   func(e *importEnv)
		want      error
		noOpen    bool // refused before the file is opened
		noCopyRun bool // refused before the copy
	}{
		{"wrong checksum", "base-q5_1", func(e *importEnv) { e.src.put(pickedPath, corrupt) },
			values.ErrChecksumMismatch, false, false},
		{"another catalog model's file", "base-q5_1", func(e *importEnv) { e.src.put(pickedPath, catTiny.content()) },
			values.ErrSizeMismatch, false, true},
		{"wrong size", "base-q5_1", func(e *importEnv) { e.src.put(pickedPath, make([]byte, catBase.size+1)) },
			values.ErrSizeMismatch, false, true},
		{"file grew while copied", "base-q5_1", func(e *importEnv) {
			e.src.putSized(pickedPath, make([]byte, catBase.size+1), catBase.size)
		}, values.ErrSizeMismatch, false, false},
		{"file shrank while copied", "base-q5_1", func(e *importEnv) {
			e.src.putSized(pickedPath, make([]byte, catBase.size-1), catBase.size)
		}, values.ErrSizeMismatch, false, false},
		{"unknown catalog id", "nope", func(e *importEnv) { e.src.put(pickedPath, catBase.content()) },
			ErrNotInCatalog, true, true},
		{"missing file", "base-q5_1", func(*importEnv) {}, shared.ErrNotFound, false, true},
		{"too little disk space", "base-q5_1", func(e *importEnv) {
			e.src.put(pickedPath, catBase.content())
			e.space.free = catBase.size
		}, values.ErrInsufficientSpace, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newImportEnv(t)
			tc.prepare(e)
			reads := 0
			e.src.onRead = func() { reads++ }

			_, err := e.im.ImportCatalog(context.Background(), tc.id, pickedPath)
			if !errors.Is(err, tc.want) {
				t.Fatalf("ImportCatalog = %v, want %v", err, tc.want)
			}
			if strings.Contains(err.Error(), pickedPath) {
				t.Fatalf("the error names the OS path: %v", err)
			}
			if opens, _ := e.src.counts(); tc.noOpen && opens != 0 {
				t.Fatalf("the file was opened %d times, want a refusal before", opens)
			}
			if tc.noCopyRun && reads != 0 {
				t.Fatalf("the file was read %d times, want a refusal before the copy", reads)
			}
			e.requireClean()
		})
	}
}

func TestImportCatalogAlreadyInstalled(t *testing.T) {
	e := newImportEnv(t)
	e.install(catBase)
	e.src.put(pickedPath, catBase.content())

	_, err := e.im.ImportCatalog(context.Background(), values.CatalogID(catBase.id), pickedPath)
	if !errors.Is(err, ErrAlreadyInstalled) {
		t.Fatalf("ImportCatalog = %v, want ErrAlreadyInstalled", err)
	}
	if opens, _ := e.src.counts(); opens != 0 {
		t.Fatal("the file was opened for a model that is installed")
	}
}

// TestImportCustomWithConfirmation is test step 3 of TASK-028 (AC-23): the digest shown by Inspect, confirmed,
// installs the model.
func TestImportCustomWithConfirmation(t *testing.T) {
	e := newImportEnv(t)
	ctx := context.Background()
	content := []byte("own model weights")
	e.src.put(pickedPath, content)

	preview, err := e.im.Inspect(ctx, pickedPath)
	if err != nil {
		t.Fatal(err)
	}
	if preview.SizeBytes != int64(len(content)) || preview.SHA256 != digest(content) {
		t.Fatalf("Inspect = %+v, want the size and digest of the file", preview)
	}
	if len(e.storedFiles()) != 0 {
		t.Fatal("Inspect stored a file")
	}
	confirmed, err := values.ConfirmImport(preview.SHA256)
	if err != nil {
		t.Fatal(err)
	}

	m, err := e.im.ImportCustom(ctx, CustomImport{
		Path: pickedPath, Name: "own", ModelVersion: "2", Quantization: "f16", Capabilities: []values.Capability{"en"},
		Confirmation: confirmed,
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.CatalogID != "" || m.Name != "own" || m.ModelVersion != "2" || m.SHA256 != preview.SHA256 ||
		m.SizeBytes != preview.SizeBytes || m.Quantization != "f16" || len(m.Capabilities) != 1 {
		t.Fatalf("ImportCustom = %+v", m)
	}
	files := e.storedFiles()
	if len(files) != 1 || !strings.HasPrefix(files[0].Path(), importPrefix) {
		t.Fatalf("stored files = %v, want one fresh import name", files)
	}
	e.requireClosed()
}

// TestImportCustomRejects is test step 3 of TASK-028: no confirmation, a confirmation of other bytes and other rule
// breaks install nothing and leave no copy.
func TestImportCustomRejects(t *testing.T) {
	content := []byte("own model weights")
	swapped := []byte("other model weights")

	cases := []struct {
		name    string
		prepare func(e *importEnv) CustomImport
		want    error
		noOpen  bool
	}{
		{"no confirmation", func(e *importEnv) CustomImport {
			e.src.put(pickedPath, content)
			return CustomImport{Path: pickedPath, Name: "own", ModelVersion: "1"}
		}, values.ErrConfirmationRequired, true},
		{"confirmed another digest", func(e *importEnv) CustomImport {
			e.src.put(pickedPath, content)
			return CustomImport{Path: pickedPath, Name: "own", ModelVersion: "1", Confirmation: confirm(e.t, swapped)}
		}, values.ErrConfirmationMismatch, false},
		{"file swapped after it was shown", func(e *importEnv) CustomImport {
			e.src.put(pickedPath, content)
			preview, err := e.im.Inspect(context.Background(), pickedPath)
			if err != nil {
				e.t.Fatal(err)
			}
			confirmed, err := values.ConfirmImport(preview.SHA256)
			if err != nil {
				e.t.Fatal(err)
			}
			e.src.put(pickedPath, swapped)
			return CustomImport{Path: pickedPath, Name: "own", ModelVersion: "1", Confirmation: confirmed}
		}, values.ErrChecksumMismatch, false},
		{"catalog identity", func(e *importEnv) CustomImport {
			e.src.put(pickedPath, catBase.content())
			return CustomImport{Path: pickedPath, Name: "base", ModelVersion: "1", Confirmation: confirm(e.t, catBase.content())}
		}, values.ErrReservedIdentity, true},
		{"invalid name", func(e *importEnv) CustomImport {
			e.src.put(pickedPath, content)
			return CustomImport{Path: pickedPath, Name: "../own", ModelVersion: "1", Confirmation: confirm(e.t, content)}
		}, values.ErrInvalidModelName, true},
		{"too large", func(e *importEnv) CustomImport {
			e.src.putSized(pickedPath, content, values.MaxImportSizeBytes+1)
			return CustomImport{Path: pickedPath, Name: "own", ModelVersion: "1", Confirmation: confirm(e.t, content)}
		}, values.ErrImportTooLarge, false},
		{"too little disk space", func(e *importEnv) CustomImport {
			e.src.put(pickedPath, content)
			e.space.free = values.DiskSpaceMargin
			return CustomImport{Path: pickedPath, Name: "own", ModelVersion: "1", Confirmation: confirm(e.t, content)}
		}, values.ErrInsufficientSpace, false},
		{"file grew while copied", func(e *importEnv) CustomImport {
			e.src.putSized(pickedPath, content, int64(len(content)-1))
			return CustomImport{Path: pickedPath, Name: "own", ModelVersion: "1", Confirmation: confirm(e.t, content)}
		}, values.ErrSizeMismatch, false},
		{"missing file", func(e *importEnv) CustomImport {
			return CustomImport{Path: pickedPath, Name: "own", ModelVersion: "1", Confirmation: confirm(e.t, content)}
		}, shared.ErrNotFound, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newImportEnv(t)
			req := tc.prepare(e)
			opensBefore, _ := e.src.counts()

			_, err := e.im.ImportCustom(context.Background(), req)
			if !errors.Is(err, tc.want) {
				t.Fatalf("ImportCustom = %v, want %v", err, tc.want)
			}
			if strings.Contains(err.Error(), pickedPath) {
				t.Fatalf("the error names the OS path: %v", err)
			}
			if opens, _ := e.src.counts(); tc.noOpen && opens != opensBefore {
				t.Fatal("the file was opened, want a refusal before")
			}
			e.requireClean()
		})
	}
}

func TestImportCustomDuplicateRemovesCopy(t *testing.T) {
	e := newImportEnv(t)
	ctx := context.Background()
	content := []byte("own model weights")
	e.src.put(pickedPath, content)
	req := CustomImport{Path: pickedPath, Name: "own", ModelVersion: "1", Confirmation: confirm(t, content)}

	if _, err := e.im.ImportCustom(ctx, req); err != nil {
		t.Fatal(err)
	}
	if _, err := e.im.ImportCustom(ctx, req); !errors.Is(err, ErrAlreadyInstalled) {
		t.Fatalf("second import: %v, want ErrAlreadyInstalled", err)
	}
	if files := e.storedFiles(); len(files) != 1 {
		t.Fatalf("stored files = %v, want only the first copy", files)
	}
}

func TestInspectRejects(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(s *fakeImportSource)
		want    error
	}{
		{"missing file", func(*fakeImportSource) {}, shared.ErrNotFound},
		{"empty file", func(s *fakeImportSource) { s.put(pickedPath, nil) }, values.ErrInvalidSize},
		{"too large", func(s *fakeImportSource) { s.putSized(pickedPath, []byte("x"), values.MaxImportSizeBytes+1) }, values.ErrImportTooLarge},
		{"changed while read", func(s *fakeImportSource) { s.putSized(pickedPath, []byte("xy"), 1) }, values.ErrSizeMismatch},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newImportEnv(t)
			tc.prepare(e.src)
			if _, err := e.im.Inspect(context.Background(), pickedPath); !errors.Is(err, tc.want) {
				t.Fatalf("Inspect = %v, want %v", err, tc.want)
			}
			e.requireClosed()
		})
	}
}

// TestImportHonorsCancellation: a cancelled copy or digest stops after the read in progress and leaves nothing.
func TestImportHonorsCancellation(t *testing.T) {
	big := make([]byte, 1<<20)
	cases := []struct {
		name string
		run  func(ctx context.Context, e *importEnv) error
	}{
		{"catalog import", func(ctx context.Context, e *importEnv) error {
			_, err := e.im.ImportCatalog(ctx, values.CatalogID(catBase.id), pickedPath)
			return err
		}},
		{"custom import", func(ctx context.Context, e *importEnv) error {
			_, err := e.im.ImportCustom(ctx, CustomImport{
				Path: pickedPath, Name: "own", ModelVersion: "1", Confirmation: confirm(e.t, big),
			})
			return err
		}},
		{"inspect", func(ctx context.Context, e *importEnv) error {
			_, err := e.im.Inspect(ctx, pickedPath)
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newImportEnv(t)
			if tc.name == "catalog import" {
				e.src.put(pickedPath, catBase.content())
			} else {
				e.src.put(pickedPath, big)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			reads := 0
			e.src.onRead = func() {
				reads++
				cancel()
			}

			if err := tc.run(ctx, e); !errors.Is(err, context.Canceled) {
				t.Fatalf("= %v, want context.Canceled", err)
			}
			if reads != 1 {
				t.Fatalf("%d reads, want the work to stop after the first", reads)
			}
			e.requireClean()
		})
	}
}
