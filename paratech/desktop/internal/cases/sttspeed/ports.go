package sttspeed

import (
	"context"

	"paratech/desktop/internal/domain/settings/settings"
	"paratech/desktop/internal/domain/transcription/sttmodel"
	"paratech/desktop/internal/domain/transcription/values"
)

// ModelLister lists the installed models (PRD section 8). Implemented by the sqlite adapter.
type ModelLister interface {
	// List returns every installed model (PRD section 8).
	List(ctx context.Context) ([]*sttmodel.Model, error)
}

// CatalogSource returns the model catalog manifest, which names the family of each catalog model (PRD section 8.1).
// The app wires the embedded manifest.
type CatalogSource interface {
	// Manifest returns the JSON manifest (PRD section 8.1).
	Manifest(ctx context.Context) ([]byte, error)
}

// SpeedReader reads the recognition speeds measured on this computer (PRD section 8; TASK-084). Implemented by the
// sqlite adapter.
type SpeedReader interface {
	// Speeds returns every valid stored speed (PRD section 8).
	Speeds(ctx context.Context) (map[values.SpeedKey]values.MeasuredSpeed, error)
}

// SettingsReader reads the load mode and the default model (PRD section 8). Implemented by the sqlite settings
// repository.
type SettingsReader interface {
	// Load returns the stored settings, or the defaults while nothing is stored (PRD section 8).
	Load(ctx context.Context) (*settings.Settings, error)
}
