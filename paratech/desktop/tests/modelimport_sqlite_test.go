package tests

// Scenario tests of the manual model import (TASK-028) on the real SQLite adapter, the real file store and the real
// local file reader; only the clock, the id generator and the catalog source are fakes (ARCH-61).

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"paratech/desktop/internal/adapters/localfile"
	"paratech/desktop/internal/cases/modelcatalog"
	"paratech/desktop/internal/domain/transcription/values"
	"paratech/desktop/testenv"
)

var _ modelcatalog.ImportSource = (*localfile.Reader)(nil)

// importEnv is the model manager with the importer; the user's downloads live outside the data root.
type importEnv struct {
	*catalogEnv
	dir       string
	downloads string
	im        *modelcatalog.Importer
}

func openImportEnv(t *testing.T, dir string, ids *seqIDs) *importEnv {
	t.Helper()
	ce := openCatalogEnv(t, dir, testenv.NewFakeClock(catalogT0), ids)
	im, err := modelcatalog.NewImporter(ce.svc, modelcatalog.ImportDeps{Source: localfile.New(), Space: ce.files})
	if err != nil {
		ce.close()
		t.Fatal(err)
	}
	downloads := filepath.Join(dir, "Downloads")
	if err := os.MkdirAll(downloads, 0o700); err != nil {
		ce.close()
		t.Fatal(err)
	}
	return &importEnv{catalogEnv: ce, dir: dir, downloads: downloads, im: im}
}

// download puts a file into the user's downloads, as a browser would, and returns its path.
func (e *importEnv) download(name string, content []byte) string {
	e.t.Helper()
	path := filepath.Join(e.downloads, name)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		e.t.Fatal(err)
	}
	return path
}

// modelFiles lists every file below the models directory of the data root.
func (e *importEnv) modelFiles() []string {
	e.t.Helper()
	var out []string
	err := filepath.WalkDir(filepath.Join(e.dir, "data", "models"), func(p string, d fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err == nil && !d.IsDir() {
			out = append(out, p)
		}
		return err
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return out
}

// TestModelImportRejectsTamperedFile is test step 2 of TASK-028 and PRD section 14 case 13: a downloaded file with a
// wrong checksum is refused, not installed and not kept; the right file then installs.
func TestModelImportRejectsTamperedFile(t *testing.T) {
	ctx := context.Background()
	e := openImportEnv(t, t.TempDir(), &seqIDs{})
	defer e.close()

	page, err := e.svc.SourcePage(ctx, values.CatalogID(fileBase.id))
	if err != nil || page.String() != "https://example.com/"+fileBase.id+".bin" {
		t.Fatalf("SourcePage = %q, %v", page, err)
	}

	tampered := bytes.Clone(fileBase.content)
	tampered[len(tampered)/2] ^= 0xff
	path := e.download("ggml-base.bin", tampered)

	_, err = e.im.ImportCatalog(ctx, values.CatalogID(fileBase.id), path)
	if !errors.Is(err, values.ErrChecksumMismatch) {
		t.Fatalf("ImportCatalog = %v, want values.ErrChecksumMismatch", err)
	}
	if strings.Contains(err.Error(), e.downloads) {
		t.Fatalf("the error names the OS path: %v", err)
	}
	installed, err := e.svc.ListInstalled(ctx)
	if err != nil || len(installed) != 0 {
		t.Fatalf("ListInstalled = %+v, %v; want none", installed, err)
	}
	available, err := e.svc.ListAvailable(ctx, values.Machine{})
	if err != nil || available[1].Entry.ID() != values.CatalogID(fileBase.id) || available[1].InstalledID != "" {
		t.Fatalf("ListAvailable = %+v, %v; want base not installed", available, err)
	}
	if files := e.modelFiles(); len(files) != 0 {
		t.Fatalf("model files left: %v", files)
	}

	path = e.download("ggml-base.bin", fileBase.content)
	m, err := e.im.ImportCatalog(ctx, values.CatalogID(fileBase.id), path)
	if err != nil {
		t.Fatalf("ImportCatalog of the right file: %v", err)
	}
	if m.SHA256 != fileBase.sum() || m.CatalogID != values.CatalogID(fileBase.id) {
		t.Fatalf("installed = %+v", m)
	}
	if files := e.modelFiles(); len(files) != 1 {
		t.Fatalf("model files = %v, want the one copy", files)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("the downloaded file is still held open: %v", err)
	}
}

// TestModelImportCustomNeedsConfirmation is test step 3 of TASK-028 (AC-23): a model outside the catalog is refused
// without confirmation and installed with the confirmed SHA-256; it survives a restart.
func TestModelImportCustomNeedsConfirmation(t *testing.T) {
	ctx := context.Background()
	dir, ids := t.TempDir(), &seqIDs{}
	e := openImportEnv(t, dir, ids)
	path := e.download("my-model.bin", fileOwn.content)
	req := modelcatalog.CustomImport{Path: path, Name: "own", ModelVersion: "1", Quantization: "q8_0"}

	if _, err := e.im.ImportCustom(ctx, req); !errors.Is(err, values.ErrConfirmationRequired) {
		t.Fatalf("ImportCustom without confirmation = %v, want values.ErrConfirmationRequired", err)
	}
	if files := e.modelFiles(); len(files) != 0 {
		t.Fatalf("model files after a refusal: %v", files)
	}

	preview, err := e.im.Inspect(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if preview.SHA256 != fileOwn.sum() || preview.SizeBytes != int64(len(fileOwn.content)) {
		t.Fatalf("Inspect = %+v, want the digest and size of the file", preview)
	}
	if req.Confirmation, err = values.ConfirmImport(preview.SHA256); err != nil {
		t.Fatal(err)
	}

	m, err := e.im.ImportCustom(ctx, req)
	if err != nil {
		t.Fatalf("ImportCustom with confirmation: %v", err)
	}
	if m.CatalogID != "" || m.SHA256 != fileOwn.sum() || m.Quantization != "q8_0" {
		t.Fatalf("installed = %+v", m)
	}
	e.close()

	e = openImportEnv(t, dir, ids)
	defer e.close()
	installed, err := e.svc.ListInstalled(ctx)
	if err != nil || len(installed) != 1 || installed[0].ID != m.ID || installed[0].SHA256 != fileOwn.sum() {
		t.Fatalf("after restart: %+v, %v", installed, err)
	}
}

// TestModelImportCustomRefusesSwappedFile: a file replaced after its digest was shown is not installed.
func TestModelImportCustomRefusesSwappedFile(t *testing.T) {
	ctx := context.Background()
	e := openImportEnv(t, t.TempDir(), &seqIDs{})
	defer e.close()

	path := e.download("my-model.bin", fileOwn.content)
	preview, err := e.im.Inspect(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	confirmed, err := values.ConfirmImport(preview.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	swapped := bytes.Clone(fileOwn.content)
	swapped[0] ^= 0xff
	e.download("my-model.bin", swapped)

	_, err = e.im.ImportCustom(ctx, modelcatalog.CustomImport{Path: path, Name: "own", ModelVersion: "1", Confirmation: confirmed})
	if !errors.Is(err, values.ErrConfirmationMismatch) {
		t.Fatalf("ImportCustom = %v, want values.ErrConfirmationMismatch", err)
	}
	installed, err := e.svc.ListInstalled(ctx)
	if err != nil || len(installed) != 0 {
		t.Fatalf("ListInstalled = %+v, %v; want none", installed, err)
	}
	if files := e.modelFiles(); len(files) != 0 {
		t.Fatalf("model files left: %v", files)
	}
}
