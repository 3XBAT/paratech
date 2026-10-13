package modelcatalog

import (
	"time"

	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/values"
)

// Deps are the ports the Service needs; all are required.
type Deps struct {
	Models   ModelRepository
	Pins     ModelPins
	Settings SettingsRepository
	Files    ArtifactStore
	Catalog  CatalogSource
	Clock    Clock
	IDs      IDGenerator
	Tx       Tx
}

// AvailableModel is one catalog entry as the model manager shows it (PRD section 8: available models, recommendation,
// slow-hardware warning).
type AvailableModel struct {
	Entry       values.CatalogEntry
	InstalledID shared.SttModelID // the model installed from this entry; empty if not installed
	Recommended bool              // the model suggested for this machine
	LikelySlow  bool              // the machine is below the recommended hardware
}

// InstalledModel is one installed model as the model manager shows it (PRD section 8, AC-07).
type InstalledModel struct {
	ID           shared.SttModelID
	Name         values.ModelName
	ModelVersion values.ModelVersion
	CatalogID    values.CatalogID // empty: imported outside the catalog
	SizeBytes    int64
	SHA256       values.SHA256
	Quantization values.Quantization
	Capabilities []values.Capability
	InstalledAt  time.Time
	IsDefault    bool
}

// Registration describes a model file to install (PRD section 8.1). The download (TASK-027) and import (TASK-028)
// flows call Register after they wrote the file below models/.
//
// A catalog model names only CatalogID and File: identity, size and capabilities come from the catalog entry, and the
// file must match the entry's size and digest. An imported model (empty CatalogID, after explicit user confirmation,
// AC-23) names its identity itself. Size and SHA-256 are always taken from the stored file by Register.
type Registration struct {
	CatalogID values.CatalogID
	File      shared.ArtifactRef

	// Imported models only.
	Name         values.ModelName
	ModelVersion values.ModelVersion
	Quantization values.Quantization
	Capabilities []values.Capability
}

// PinnedModel is the exact model an STT job pins (AC-08).
type PinnedModel struct {
	ID           shared.SttModelID
	Name         values.ModelName
	ModelVersion values.ModelVersion
}
