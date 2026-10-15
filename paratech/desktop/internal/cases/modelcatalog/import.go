package modelcatalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/values"
)

// Operation names of the import flow, used in error messages. The OS path of the file is not part of them.
const (
	opImportCatalog = "modelcatalog: import catalog model"
	opImportCustom  = "modelcatalog: import model"
	opInspectImport = "modelcatalog: inspect model file"
)

// importPrefix names the installed files of models imported outside the catalog.
const importPrefix = "import-"

// Importer installs model files the user downloaded by hand (PRD sections 8 and 8.1: open the source page, import
// the downloaded file; AC-23). The file is copied through the ArtifactStore into models/ under a fresh name and
// installed by Service.Register, which hashes the copy: a catalog file must match its entry, a file outside the
// catalog must have the digest the user confirmed. On any failure the copy is deleted and nothing is installed
// (PRD section 14 case 13).
//
// Outside the catalog the flow has two steps: Inspect hashes the picked file for the user to see, ImportCustom
// copies it again and installs it only when the copy has the confirmed digest. Nothing is staged between the steps,
// so an abandoned confirmation leaves nothing behind, and a file swapped after it was shown is refused. It is safe
// for concurrent use.
type Importer struct {
	svc    *Service
	source ImportSource
	space  DiskSpace
}

// NewImporter builds an Importer on top of the Service.
func NewImporter(svc *Service, d ImportDeps) (*Importer, error) {
	if svc == nil || d.Source == nil || d.Space == nil {
		return nil, fmt.Errorf("%w: the service and every import port are required", ErrInvalidConfig)
	}
	return &Importer{svc: svc, source: d.Source, space: d.Space}, nil
}

// ImportCatalog installs the file at path as the catalog model id (PRD section 8.1, AC-23).
//
//   - An unknown id is ErrNotInCatalog, a model installed from the entry already ErrAlreadyInstalled.
//   - A file of another size is values.ErrSizeMismatch before anything is copied; too little free disk space a
//     *values.InsufficientSpaceError.
//   - A copy that does not have the catalog SHA-256 is values.ErrChecksumMismatch; it is deleted.
func (im *Importer) ImportCatalog(ctx context.Context, id values.CatalogID, path string) (InstalledModel, error) {
	m, err := im.importCatalog(ctx, id, path)
	if err != nil {
		return InstalledModel{}, fmt.Errorf("%s %s: %w", opImportCatalog, id, err)
	}
	return m, nil
}

func (im *Importer) importCatalog(ctx context.Context, id values.CatalogID, path string) (InstalledModel, error) {
	entry, err := im.svc.catalogEntry(ctx, id)
	if err != nil {
		return InstalledModel{}, err
	}
	if err := im.svc.checkNotInstalled(ctx, id); err != nil {
		return InstalledModel{}, err
	}

	src, size, err := im.source.Open(ctx, path)
	if err != nil {
		return InstalledModel{}, err
	}
	defer func() { _ = src.Close() }()

	if err := entry.VerifySize(size); err != nil {
		return InstalledModel{}, err
	}
	free, err := im.space.FreeBytes(ctx)
	if err != nil {
		return InstalledModel{}, err
	}
	if err := entry.CheckDiskSpace(0, free); err != nil {
		return InstalledModel{}, err
	}

	ref, err := shared.NewArtifactRef(shared.ArtifactModels,
		entry.ID().String()+"-"+string(im.svc.ids.NewSttModelID())+installedExt)
	if err != nil {
		return InstalledModel{}, err
	}
	return im.install(ctx, ref, src, size, Registration{CatalogID: entry.ID(), File: ref})
}

// Inspect reads the file at path and returns its size and SHA-256, which the user confirms before ImportCustom
// (PRD section 8, AC-23). A file larger than values.MaxImportSizeBytes is values.ErrImportTooLarge.
func (im *Importer) Inspect(ctx context.Context, path string) (ImportPreview, error) {
	p, err := im.inspect(ctx, path)
	if err != nil {
		return ImportPreview{}, fmt.Errorf("%s: %w", opInspectImport, err)
	}
	return p, nil
}

func (im *Importer) inspect(ctx context.Context, path string) (ImportPreview, error) {
	src, size, err := im.source.Open(ctx, path)
	if err != nil {
		return ImportPreview{}, err
	}
	defer func() { _ = src.Close() }()

	if err := values.CheckImportSize(size); err != nil {
		return ImportPreview{}, err
	}

	h := sha256.New()
	if _, err := io.Copy(h, exactReader(ctx, src, size)); err != nil {
		return ImportPreview{}, err
	}
	return ImportPreview{SizeBytes: size, SHA256: values.SHA256(hex.EncodeToString(h.Sum(nil)))}, nil
}

// ImportCustom installs a model file outside the catalog (PRD section 8, AC-23).
//
//   - Without a confirmation it is values.ErrConfirmationRequired before the file is opened.
//   - The name and version of a catalog entry are values.ErrReservedIdentity, a file larger than
//     values.MaxImportSizeBytes values.ErrImportTooLarge, too little free disk space a *values.InsufficientSpaceError.
//   - A copy whose SHA-256 is not the confirmed one is values.ErrConfirmationMismatch (also
//     values.ErrChecksumMismatch); it is deleted.
func (im *Importer) ImportCustom(ctx context.Context, req CustomImport) (InstalledModel, error) {
	m, err := im.importCustom(ctx, req)
	if err != nil {
		return InstalledModel{}, fmt.Errorf("%s %s %s: %w", opImportCustom, req.Name, req.ModelVersion, err)
	}
	return m, nil
}

func (im *Importer) importCustom(ctx context.Context, req CustomImport) (InstalledModel, error) {
	if err := req.Confirmation.Require(); err != nil {
		return InstalledModel{}, err
	}
	// Cheap refusals before gigabytes are copied; Register checks all of it again.
	for _, err := range []error{req.Name.Validate(), req.ModelVersion.Validate()} {
		if err != nil {
			return InstalledModel{}, err
		}
	}
	cat, err := im.svc.loadCatalog(ctx)
	if err != nil {
		return InstalledModel{}, err
	}
	if err := cat.CheckImportIdentity(req.Name, req.ModelVersion); err != nil {
		return InstalledModel{}, err
	}

	src, size, err := im.source.Open(ctx, req.Path)
	if err != nil {
		return InstalledModel{}, err
	}
	defer func() { _ = src.Close() }()

	if err := values.CheckImportSize(size); err != nil {
		return InstalledModel{}, err
	}
	free, err := im.space.FreeBytes(ctx)
	if err != nil {
		return InstalledModel{}, err
	}
	if err := values.CheckImportSpace(size, free); err != nil {
		return InstalledModel{}, err
	}

	ref, err := shared.NewArtifactRef(shared.ArtifactModels, importPrefix+string(im.svc.ids.NewSttModelID())+installedExt)
	if err != nil {
		return InstalledModel{}, err
	}
	return im.install(ctx, ref, src, size, Registration{
		File: ref, Name: req.Name, ModelVersion: req.ModelVersion, Quantization: req.Quantization,
		Capabilities: req.Capabilities, Confirmation: req.Confirmation,
	})
}

// install copies exactly size bytes of src to ref and registers the copy; the copy is deleted when either fails.
func (im *Importer) install(ctx context.Context, ref shared.ArtifactRef, src io.Reader, size int64, r Registration) (InstalledModel, error) {
	err := im.svc.files.Write(ctx, ref, exactReader(ctx, src, size))
	if err == nil {
		var m InstalledModel
		if m, err = im.svc.Register(ctx, r); err == nil {
			return m, nil
		}
	}
	// A cancelled ctx must not keep the copy from being removed.
	return InstalledModel{}, errors.Join(err, im.svc.files.Delete(context.WithoutCancel(ctx), ref))
}

// exactReader yields the size bytes of r and fails with values.ErrSizeMismatch when r has more or fewer: the file
// changed after its size was read. It never reads more than size+1 bytes and stops when ctx ends.
func exactReader(ctx context.Context, r io.Reader, size int64) io.Reader {
	return &boundedReader{r: io.LimitReader(ctxReader{ctx: ctx, r: r}, size+1), size: size}
}

type boundedReader struct {
	r    io.Reader
	size int64
	n    int64
}

func (b *boundedReader) Read(p []byte) (int, error) {
	n, err := b.r.Read(p)
	b.n += int64(n)
	switch {
	case b.n > b.size:
		return 0, fmt.Errorf("%w: the file grew beyond %d bytes while it was read", values.ErrSizeMismatch, b.size)
	case errors.Is(err, io.EOF) && b.n < b.size:
		return n, fmt.Errorf("%w: the file ended at %d of %d bytes", values.ErrSizeMismatch, b.n, b.size)
	}
	return n, err
}
