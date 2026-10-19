package sttspeed

import (
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/services/speedestimate"
	"paratech/desktop/internal/domain/transcription/values"
)

// Deps are the ports the Service needs; all are required.
type Deps struct {
	Models   ModelLister
	Catalog  CatalogSource
	Speeds   SpeedReader
	Settings SettingsReader
}

// ModelEstimates are the estimates of one model: in the current mode and in every mode.
type ModelEstimates struct {
	Current speedestimate.Estimate
	Modes   []speedestimate.Estimate // in the order of shared.RecognitionLoads
}

// Overview is what the models and settings pages show.
type Overview struct {
	Load      shared.RecognitionLoad
	Catalog   map[values.CatalogID]ModelEstimates
	Installed map[shared.SttModelID]ModelEstimates

	// InUse is the model a new STT job pins (modelselection), with its name; empty when none is installed or the
	// choice is open.
	InUse     shared.SttModelID
	InUseName values.ModelName
}
