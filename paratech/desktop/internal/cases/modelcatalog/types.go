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
// file must match the entry's size and digest. An imported model (empty CatalogID) names its identity itself and
// carries the user's confirmation of the digest it was shown (AC-23). Size and SHA-256 are always taken from the
// stored file by Register.
type Registration struct {
	CatalogID values.CatalogID
	File      shared.ArtifactRef

	// Imported models only.
	Name         values.ModelName
	ModelVersion values.ModelVersion
	Quantization values.Quantization
	Capabilities []values.Capability
	Confirmation values.ImportConfirmation
}

// PinnedModel is the exact model an STT job pins (AC-08).
type PinnedModel struct {
	ID           shared.SttModelID
	Name         values.ModelName
	ModelVersion values.ModelVersion
}

// DownloadDeps are the ports a Downloader needs on top of the Service; all are required.
type DownloadDeps struct {
	Source ModelSource
	Space  DiskSpace
}

// DownloadPhase is the stage of a download that a progress event reports.
type DownloadPhase string

// Phases of a download.
const (
	PhaseDownloading DownloadPhase = "downloading"
	PhaseVerifying   DownloadPhase = "verifying"
	PhaseInstalled   DownloadPhase = "installed"
	PhaseFailed      DownloadPhase = "failed"
)

// DownloadProgress is one progress event of a download (PRD section 8: progress). Subscribers may miss intermediate
// events but always get the latest one.
type DownloadProgress struct {
	CatalogID  values.CatalogID
	Phase      DownloadPhase
	DoneBytes  int64 // bytes of the file stored so far, resumed bytes included
	TotalBytes int64 // size of the file from the catalog
}

// DownloadPlan is what a download of a catalog model needs, for the disk space warning before it starts
// (PRD section 8).
type DownloadPlan struct {
	CatalogID      values.CatalogID
	TotalBytes     int64 // size of the model file
	HaveBytes      int64 // bytes already downloaded and kept for resume
	RequiredBytes  int64 // free space the download needs, margin included
	AvailableBytes int64 // free space on the disk of the data directory
	Enough         bool  // AvailableBytes >= RequiredBytes
}

// ImportDeps are the ports an Importer needs on top of the Service; all are required.
type ImportDeps struct {
	Source ImportSource
	Space  DiskSpace
}

// ImportPreview is what the user is shown before confirming the import of a model outside the catalog (AC-23).
type ImportPreview struct {
	SizeBytes int64
	SHA256    values.SHA256
}

// CustomImport is the import of a model file outside the catalog (PRD section 8, AC-23). Path is the OS path of the
// file the user picked; Confirmation carries the SHA-256 of the ImportPreview the user confirmed.
type CustomImport struct {
	Path         string
	Name         values.ModelName
	ModelVersion values.ModelVersion
	Quantization values.Quantization
	Capabilities []values.Capability
	Confirmation values.ImportConfirmation
}
