package sttmodel

import (
	"errors"
	"fmt"
	"time"

	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/values"
)

// Model is an installed speech-to-text model (PRD section 8): one verified file below models/ with its identity
// (name and model version, unique among installed models), size, SHA-256 and capabilities. An installed model does
// not change; an update is a separate install of another version.
type Model struct {
	version int64 // persisted version for optimistic concurrency; 0 = never stored (ADR-0004)
	id      shared.SttModelID

	name         values.ModelName
	modelVersion values.ModelVersion
	catalogID    values.CatalogID // empty: imported outside the catalog after explicit confirmation (AC-23)
	file         shared.ArtifactRef
	size         int64
	sha256       values.SHA256
	quantization values.Quantization
	capabilities []values.Capability
	installedAt  time.Time
}

// Spec is what the installer of a model provides to New. The file must already be verified against SHA256.
type Spec struct {
	ID           shared.SttModelID
	Name         values.ModelName
	ModelVersion values.ModelVersion
	CatalogID    values.CatalogID // empty: imported outside the catalog
	File         shared.ArtifactRef
	SizeBytes    int64
	SHA256       values.SHA256
	Quantization values.Quantization
	Capabilities []values.Capability
}

// CatalogSpec builds the Spec of a catalog model: identity, size, digest and capabilities come from the entry, so an
// installed catalog model always matches its manifest entry.
func CatalogSpec(id shared.SttModelID, e values.CatalogEntry, file shared.ArtifactRef) Spec {
	return Spec{
		ID: id, Name: e.Name(), ModelVersion: e.Version(), CatalogID: e.ID(), File: file, SizeBytes: e.SizeBytes(),
		SHA256: e.SHA256(), Quantization: e.Quantization(), Capabilities: e.Capabilities(),
	}
}

// New creates a model that was just installed, never stored (Version() == 0).
func New(spec Spec, installedAt time.Time) (*Model, error) {
	return build(spec, installedAt, 0)
}

// State is the stored form of a model, for Restore.
type State struct {
	Spec
	Version     int64
	InstalledAt time.Time
}

// Restore rebuilds a stored model without side effects; it checks the same rules as New.
func Restore(s State) (*Model, error) {
	if s.Version < 1 {
		return nil, fmt.Errorf("%w: stored version %d", ErrInvalidModel, s.Version)
	}
	return build(s.Spec, s.InstalledAt, s.Version)
}

func build(s Spec, installedAt time.Time, version int64) (*Model, error) {
	if err := validate(s, installedAt); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidModel, err)
	}

	return &Model{
		version: version, id: s.ID, name: s.Name, modelVersion: s.ModelVersion, catalogID: s.CatalogID,
		file: s.File, size: s.SizeBytes, sha256: s.SHA256, quantization: s.Quantization,
		capabilities: append([]values.Capability(nil), s.Capabilities...), installedAt: installedAt,
	}, nil
}

func validate(s Spec, installedAt time.Time) error {
	if s.CatalogID != "" {
		if err := s.CatalogID.Validate(); err != nil {
			return err
		}
	}
	switch {
	case installedAt.IsZero():
		return errors.New("no installation time")
	case s.File.IsZero() || s.File.Kind() != shared.ArtifactModels:
		return fmt.Errorf("file %q is not below %s/", s.File, shared.ArtifactModels)
	}
	for _, err := range []error{
		s.ID.Validate(), s.Name.Validate(), s.ModelVersion.Validate(), values.ValidateSize(s.SizeBytes),
		s.SHA256.Validate(), s.Quantization.Validate(), values.ValidateCapabilities(s.Capabilities),
	} {
		if err != nil {
			return err
		}
	}
	return nil
}

// ID returns the model identifier.
func (m *Model) ID() shared.SttModelID { return m.id }

// Name returns the model name.
func (m *Model) Name() values.ModelName { return m.name }

// ModelVersion returns the model's own version (not the aggregate Version).
func (m *Model) ModelVersion() values.ModelVersion { return m.modelVersion }

// CatalogID returns the catalog entry the model was installed from, empty for an imported model.
func (m *Model) CatalogID() values.CatalogID { return m.catalogID }

// File returns the model file below models/.
func (m *Model) File() shared.ArtifactRef { return m.file }

// SizeBytes returns the size of the model file.
func (m *Model) SizeBytes() int64 { return m.size }

// SHA256 returns the verified digest of the model file.
func (m *Model) SHA256() values.SHA256 { return m.sha256 }

// Quantization returns the weight format, empty if unknown.
func (m *Model) Quantization() values.Quantization { return m.quantization }

// Capabilities returns a copy of the capability labels.
func (m *Model) Capabilities() []values.Capability {
	return append([]values.Capability(nil), m.capabilities...)
}

// InstalledAt returns when the model was installed.
func (m *Model) InstalledAt() time.Time { return m.installedAt }

// CheckRemoval decides whether the model may be removed (PRD section 8: removal only when no active job depends on
// it; section 14 case 11). pinnedByActiveJob is the answer of the pin lookup for the unfinished job statuses.
func (m *Model) CheckRemoval(pinnedByActiveJob bool) error {
	if pinnedByActiveJob {
		return fmt.Errorf("%w: %s %s", ErrPinned, m.name, m.modelVersion)
	}
	return nil
}

// Version returns the version this instance was last loaded or saved with: 0 for an aggregate that was never
// stored, otherwise the stored version, which is at least 1 (optimistic concurrency, ADR-0004). The repository
// compares it with the stored one in Save.
func (m *Model) Version() int64 { return m.version }

// Persisted records the version the store assigned after a successful Save. It is called by the repository adapter
// only, after the write succeeded, so that saving the same instance again compares against its own earlier write
// (ADR-0004). It holds no persistence logic and is not a state change of the business entity: use cases and
// handlers never call it.
func (m *Model) Persisted(version int64) { m.version = version }
