package modelcatalog

import (
	"context"
	"errors"
	"fmt"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/services/modelselection"
	"paratech/desktop/internal/domain/transcription/sttmodel"
	"paratech/desktop/internal/domain/transcription/values"
)

// Operation names, used in error messages.
const (
	opListAvailable = "modelcatalog: list available models"
	opListInstalled = "modelcatalog: list installed models"
	opRegister      = "modelcatalog: register model"
	opSetDefault    = "modelcatalog: set default model"
	opDelete        = "modelcatalog: delete model"
	opResolve       = "modelcatalog: resolve model for job"
	opCurrentModel  = "modelcatalog: current model"
	opSourcePage    = "modelcatalog: model source page"
)

// conflictAttempts bounds every reload-and-retry loop: version conflicts on the settings and a default that is selected
// again while a removal clears it.
const conflictAttempts = 3

// Service manages local STT models (PRD sections 8 and 8.1): the catalog, installed models, the default model, the
// model an STT job pins and safe removal. Every write runs in its own Tx.Do and changes one aggregate (ARCH-23).
type Service struct {
	models   ModelRepository
	pins     ModelPins
	settings SettingsRepository
	files    ArtifactStore
	catalog  CatalogSource
	clock    Clock
	ids      IDGenerator
	tx       Tx
	sel      modelselection.Selector
}

// New builds the Service.
func New(d Deps) (*Service, error) {
	if d.Models == nil || d.Pins == nil || d.Settings == nil || d.Files == nil || d.Catalog == nil ||
		d.Clock == nil || d.IDs == nil || d.Tx == nil {
		return nil, fmt.Errorf("%w: every port is required", ErrInvalidConfig)
	}

	return &Service{
		models: d.Models, pins: d.Pins, settings: d.Settings, files: d.Files, catalog: d.Catalog,
		clock: d.Clock, ids: d.IDs, tx: d.Tx, sel: modelselection.New(),
	}, nil
}

// ListAvailable returns the catalog in manifest order with the installed model of each entry, the recommendation for
// machine and the slow-hardware warning (PRD sections 8 and 8.1). Pass the zero values.Machine when the hardware is
// unknown. An entry counts as installed only through a model installed from it (its CatalogID), never through an
// imported model, which was not verified against the entry.
func (s *Service) ListAvailable(ctx context.Context, machine values.Machine) ([]AvailableModel, error) {
	cat, err := s.loadCatalog(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", opListAvailable, err)
	}
	installed, err := s.models.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", opListAvailable, err)
	}

	installedIDs := make(map[values.CatalogID]shared.SttModelID, len(installed))
	for _, m := range installed {
		if m.CatalogID() != "" {
			installedIDs[m.CatalogID()] = m.ID()
		}
	}

	entries := cat.Entries()
	recommended, haveRecommendation := s.sel.Recommend(entries, machine)
	out := make([]AvailableModel, 0, len(entries))
	for _, e := range entries {
		out = append(out, AvailableModel{
			Entry:       e,
			InstalledID: installedIDs[e.ID()],
			Recommended: haveRecommendation && e.ID() == recommended.ID(),
			LikelySlow:  s.sel.LikelySlow(e, machine),
		})
	}
	return out, nil
}

// ListInstalled returns the installed models ordered by name and version, with the default marked (PRD section 8,
// AC-07).
func (s *Service) ListInstalled(ctx context.Context) ([]InstalledModel, error) {
	models, err := s.models.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", opListInstalled, err)
	}
	st, err := s.settings.Load(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", opListInstalled, err)
	}

	out := make([]InstalledModel, 0, len(models))
	for _, m := range models {
		out = append(out, installedModel(m, m.ID() == st.DefaultSttModel()))
	}
	return out, nil
}

// Register installs a model file (PRD section 8.1, AC-07, AC-23). The file must already be stored below models/.
// Register reads its size and computes its SHA-256 itself (streaming, outside any transaction, honoring ctx), so no
// caller-supplied digest is trusted.
//
//   - A catalog model must match its entry's size and digest, otherwise values.ErrSizeMismatch or
//     values.ErrChecksumMismatch.
//   - An imported model stores the computed digest, which must be the one the user confirmed
//     (values.ErrConfirmationRequired, values.ErrConfirmationMismatch); the name and version of a catalog entry yield
//     values.ErrReservedIdentity.
//   - A second model with the same name and version yields ErrAlreadyInstalled; a file that is, contains or lies inside
//     the file of another installed model yields sttmodel.ErrFileInUse (both wrap shared.ErrDuplicate).
//
// Nothing is installed on error and the file is left in place: the caller owns it until Register succeeds. Give every
// install a file name of its own (for example with a fresh suffix): Delete removes the file after the row, so a file
// written at the name of a model that is being removed can be lost.
func (s *Service) Register(ctx context.Context, r Registration) (InstalledModel, error) {
	size, err := s.files.Size(ctx, r.File)
	if err != nil {
		return InstalledModel{}, fmt.Errorf("%s: %w", opRegister, err)
	}

	spec, err := s.registrationSpec(ctx, r, size)
	if err != nil {
		return InstalledModel{}, fmt.Errorf("%s: %w", opRegister, err)
	}
	m, err := sttmodel.New(spec, s.clock.Now())
	if err != nil {
		return InstalledModel{}, fmt.Errorf("%s: %w", opRegister, err)
	}
	if r.CatalogID == "" {
		if err := r.Confirmation.Verify(m.SHA256()); err != nil {
			return InstalledModel{}, fmt.Errorf("%s: %w", opRegister, err)
		}
	}

	// The file check and the insert share one write transaction, so two registrations cannot claim one file.
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		installed, err := s.models.List(ctx)
		if err != nil {
			return err
		}
		if err := m.CheckFileFree(installed); err != nil {
			return err
		}
		return s.models.Save(ctx, m)
	})
	switch {
	case errors.Is(err, sttmodel.ErrFileInUse):
		return InstalledModel{}, fmt.Errorf("%s: %w", opRegister, err)
	case errors.Is(err, shared.ErrDuplicate):
		return InstalledModel{}, fmt.Errorf("%s: %s %s: %w", opRegister, m.Name(), m.ModelVersion(), ErrAlreadyInstalled)
	case err != nil:
		return InstalledModel{}, fmt.Errorf("%s: %w", opRegister, err)
	}
	return installedModel(m, false), nil
}

// registrationSpec checks the registration against the catalog and hashes the file; a catalog file of the wrong size
// is refused before it is read.
func (s *Service) registrationSpec(ctx context.Context, r Registration, size int64) (sttmodel.Spec, error) {
	cat, err := s.loadCatalog(ctx)
	if err != nil {
		return sttmodel.Spec{}, err
	}

	if r.CatalogID == "" {
		if err := cat.CheckImportIdentity(r.Name, r.ModelVersion); err != nil {
			return sttmodel.Spec{}, err
		}
		sum, err := s.digestFile(ctx, r.File, size)
		if err != nil {
			return sttmodel.Spec{}, err
		}
		return sttmodel.Spec{
			ID: s.ids.NewSttModelID(), Name: r.Name, ModelVersion: r.ModelVersion, File: r.File, SizeBytes: size,
			SHA256: sum, Quantization: r.Quantization, Capabilities: r.Capabilities,
		}, nil
	}

	if r.Name != "" || r.ModelVersion != "" || r.Quantization != "" || len(r.Capabilities) > 0 || !r.Confirmation.IsZero() {
		return sttmodel.Spec{}, fmt.Errorf("%w: a catalog model takes its identity from the catalog", ErrInvalidRegistration)
	}
	entry, ok := cat.Entry(r.CatalogID)
	if !ok {
		return sttmodel.Spec{}, fmt.Errorf("%q: %w", r.CatalogID, ErrNotInCatalog)
	}
	if err := entry.VerifySize(size); err != nil {
		return sttmodel.Spec{}, err
	}

	sum, err := s.digestFile(ctx, r.File, size)
	if err != nil {
		return sttmodel.Spec{}, err
	}
	if err := entry.Verify(sum, size); err != nil {
		return sttmodel.Spec{}, err
	}
	return sttmodel.CatalogSpec(s.ids.NewSttModelID(), entry, r.File), nil
}

// SourcePage returns the page of a catalog model to open in the default browser, so the user can download the file
// by hand and import it (PRD section 8.1: open source page). It is the manifest's https source_page, or the source
// URL when there is none; the caller opens it, the use case never does. An unknown id yields ErrNotInCatalog.
func (s *Service) SourcePage(ctx context.Context, id values.CatalogID) (values.SourceURL, error) {
	entry, err := s.catalogEntry(ctx, id)
	if err != nil {
		return values.SourceURL{}, fmt.Errorf("%s %s: %w", opSourcePage, id, err)
	}
	return entry.SourcePage(), nil
}

// SetDefault makes an installed model the default (PRD section 8, AC-07). It changes only the settings aggregate, in
// one transaction that also checks that the model is installed; a model that is not installed yields
// modelselection.ErrModelNotInstalled.
func (s *Service) SetDefault(ctx context.Context, id shared.SttModelID) error {
	if err := id.Validate(); err != nil {
		return fmt.Errorf("%s: %w", opSetDefault, err)
	}

	err := retryVersionConflicts(func() error {
		return s.tx.Do(ctx, func(ctx context.Context) error {
			if _, err := s.models.Get(ctx, id); err != nil {
				return err
			}
			st, err := s.settings.Load(ctx)
			if err != nil {
				return err
			}
			if err := st.SelectDefaultSttModel(id, s.clock.Now()); err != nil {
				return err
			}
			return s.settings.Save(ctx, st)
		})
	})
	if isMissingModel(err) {
		return fmt.Errorf("%s %s: %w", opSetDefault, id, modelselection.ErrModelNotInstalled)
	}
	if err != nil {
		return fmt.Errorf("%s %s: %w", opSetDefault, id, err)
	}
	return nil
}

// Delete removes an installed model and its file (PRD section 8: removal only when no active job depends on it;
// section 14 case 11). The steps follow ADR-0004(g):
//
//  1. the model is loaded and its aggregate decides on the pins of job.ActiveStatuses; a refusal
//     (sttmodel.ErrPinned) changes nothing;
//  2. a default pointing at the model is cleared through the settings aggregate, in its own transaction;
//  3. in one transaction the pins are checked again and the row is deleted;
//  4. after the commit the file is deleted through the artifact store, unless another row still uses it.
//
// The file is touched only after the row is gone, so a refusal never destroys anything: a job that pins the model
// between 1 and 3 makes step 3 return sttmodel.ErrPinned, and a default selected again between 2 and 3 makes the
// database refuse the delete; Delete then starts over, and after conflictAttempts rounds returns
// ErrDefaultReselected. Such a late refusal leaves only the default cleared.
//
// A model that is not installed wraps shared.ErrNotFound. When step 4 fails, the error is returned and the model
// stays uninstalled; a repeated Delete then finds no row and returns shared.ErrNotFound, leaving an orphan file below
// models/ that no row references. It is harmless garbage: nothing loads it, and a reinstall at the same name
// overwrites it. A row that a concurrent Delete removed after step 1 is not an error: the file is deleted again
// (idempotent), in case the other removal failed at step 4.
func (s *Service) Delete(ctx context.Context, id shared.SttModelID) error {
	for attempt := 1; ; attempt++ {
		err := s.deleteOnce(ctx, id)
		switch {
		case err == nil:
			return nil
		case !errors.Is(err, errDefaultReselected):
			return fmt.Errorf("%s %s: %w", opDelete, id, err)
		case attempt >= conflictAttempts:
			return fmt.Errorf("%s %s: %w", opDelete, id, ErrDefaultReselected)
		}
	}
}

func (s *Service) deleteOnce(ctx context.Context, id shared.SttModelID) error {
	// Step 1: load and pre-check.
	m, err := s.models.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := s.checkRemoval(ctx, m); err != nil {
		return err
	}

	// Step 2: clear the default (ADR-0004(g)).
	if err := s.clearDefault(ctx, id); err != nil {
		return err
	}

	// Step 3: re-check and delete the row.
	fileInUse := false
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		err := s.removeRow(ctx, m)
		if err != nil && !errors.Is(err, shared.ErrNotFound) {
			return err
		}

		rest, err := s.models.List(ctx)
		if err != nil {
			return err
		}
		fileInUse = m.CheckFileFree(rest) != nil
		return nil
	})
	if isForeignKeyConflict(err) {
		return errDefaultReselected
	}
	if err != nil {
		return err
	}

	// Step 4: delete the file after the commit.
	if fileInUse {
		return nil // defensive: rows registered before one file per model was enforced
	}
	return s.files.Delete(ctx, m.File())
}

// removeRow re-checks the pins and deletes the row; a row that is already gone wraps shared.ErrNotFound.
func (s *Service) removeRow(ctx context.Context, m *sttmodel.Model) error {
	if _, err := s.models.Get(ctx, m.ID()); err != nil {
		return err
	}
	if err := s.checkRemoval(ctx, m); err != nil {
		return err
	}
	return s.models.Delete(ctx, m.ID())
}

// checkRemoval asks the aggregate with the pins of the unfinished job statuses.
func (s *Service) checkRemoval(ctx context.Context, m *sttmodel.Model) error {
	pinned, err := s.pins.IsPinnedByJobInStatuses(ctx, m.ID(), job.ActiveStatuses())
	if err != nil {
		return err
	}
	return m.CheckRemoval(pinned)
}

// clearDefault clears the default through the settings aggregate when it points at the model being removed.
func (s *Service) clearDefault(ctx context.Context, id shared.SttModelID) error {
	return retryVersionConflicts(func() error {
		return s.tx.Do(ctx, func(ctx context.Context) error {
			st, err := s.settings.Load(ctx)
			if err != nil {
				return err
			}
			if !s.sel.ClearsDefault(st.DefaultSttModel(), id) {
				return nil
			}
			if err := st.ClearDefaultSttModel(s.clock.Now()); err != nil {
				return err
			}
			return s.settings.Save(ctx, st)
		})
	})
}

// ResolveForJob returns the exact model an STT job pins (PRD section 8, AC-08): requested if set, otherwise the
// default, otherwise the only installed model (modelselection.Selector.Resolve). The reads run in one Tx.Do; call it
// with the context of the Tx.Do that enqueues the job, so that no removal can slip between the choice and the pin.
func (s *Service) ResolveForJob(ctx context.Context, requested shared.SttModelID) (PinnedModel, error) {
	var pinned PinnedModel
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		var err error
		pinned, err = s.resolve(ctx, requested)
		return err
	})
	if err != nil {
		return PinnedModel{}, fmt.Errorf("%s: %w", opResolve, err)
	}
	return pinned, nil
}

// CurrentModel returns the model a new STT job would pin, for display. It opens no transaction, so it does not take
// the write lock, and may be momentarily stale against a concurrent SetDefault or Delete; jobs use ResolveForJob.
func (s *Service) CurrentModel(ctx context.Context) (PinnedModel, error) {
	pinned, err := s.resolve(ctx, "")
	if err != nil {
		return PinnedModel{}, fmt.Errorf("%s: %w", opCurrentModel, err)
	}
	return pinned, nil
}

// resolve loads the settings and the installed models and asks the selector.
func (s *Service) resolve(ctx context.Context, requested shared.SttModelID) (PinnedModel, error) {
	st, err := s.settings.Load(ctx)
	if err != nil {
		return PinnedModel{}, err
	}
	installed, err := s.models.List(ctx)
	if err != nil {
		return PinnedModel{}, err
	}
	m, err := s.sel.Resolve(requested, st.DefaultSttModel(), installed)
	if err != nil {
		return PinnedModel{}, err
	}
	return PinnedModel{ID: m.ID(), Name: m.Name(), ModelVersion: m.ModelVersion()}, nil
}

// catalogEntry loads the catalog and returns the entry with the id.
func (s *Service) catalogEntry(ctx context.Context, id values.CatalogID) (values.CatalogEntry, error) {
	cat, err := s.loadCatalog(ctx)
	if err != nil {
		return values.CatalogEntry{}, err
	}
	entry, ok := cat.Entry(id)
	if !ok {
		return values.CatalogEntry{}, fmt.Errorf("%q: %w", id, ErrNotInCatalog)
	}
	return entry, nil
}

// checkNotInstalled refuses a second install of a catalog entry before gigabytes are copied; Register checks again.
func (s *Service) checkNotInstalled(ctx context.Context, id values.CatalogID) error {
	installed, err := s.models.List(ctx)
	if err != nil {
		return err
	}
	for _, m := range installed {
		if m.CatalogID() == id {
			return fmt.Errorf("%s %s: %w", m.Name(), m.ModelVersion(), ErrAlreadyInstalled)
		}
	}
	return nil
}

// checkIdentityFree refuses an import under the name and version of an installed model before gigabytes are copied;
// Register checks again.
func (s *Service) checkIdentityFree(ctx context.Context, name values.ModelName, version values.ModelVersion) error {
	installed, err := s.models.List(ctx)
	if err != nil {
		return err
	}
	for _, m := range installed {
		if m.Name() == name && m.ModelVersion() == version {
			return fmt.Errorf("%s %s: %w", name, version, ErrAlreadyInstalled)
		}
	}
	return nil
}

func (s *Service) loadCatalog(ctx context.Context) (values.Catalog, error) {
	data, err := s.catalog.Manifest(ctx)
	if err != nil {
		return values.Catalog{}, err
	}
	return values.ParseCatalog(data)
}

func installedModel(m *sttmodel.Model, isDefault bool) InstalledModel {
	return InstalledModel{
		ID: m.ID(), Name: m.Name(), ModelVersion: m.ModelVersion(), CatalogID: m.CatalogID(), SizeBytes: m.SizeBytes(),
		SHA256: m.SHA256(), Quantization: m.Quantization(), Capabilities: m.Capabilities(), InstalledAt: m.InstalledAt(),
		IsDefault: isDefault,
	}
}

// retryVersionConflicts reruns fn, which reloads what it changes, while it meets a stale version (ADR-0004(d)).
func retryVersionConflicts(fn func() error) error {
	var err error
	for range conflictAttempts {
		if err = fn(); !errors.Is(err, shared.ErrVersionConflict) {
			return err
		}
	}
	return err
}

// isForeignKeyConflict tells the plain conflict of a foreign key (ADR-0004(g)) from its narrower relatives.
func isForeignKeyConflict(err error) bool {
	return errors.Is(err, shared.ErrConflict) && !errors.Is(err, shared.ErrVersionConflict) &&
		!errors.Is(err, shared.ErrDuplicate) && !errors.Is(err, sttmodel.ErrPinned)
}

// isMissingModel reports a model that is gone: not found, or removed before the settings referencing it were saved.
func isMissingModel(err error) bool {
	return errors.Is(err, shared.ErrNotFound) || isForeignKeyConflict(err)
}
