package transcript

import (
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/values"
)

// Created is the fact that a transcript was stored: the trigger for the "processing complete" notification and for
// scheduling post-processing (PRD sections 7.1 and 15). It may be published more than once for the same transcript
// (a rerun after a crash), so consumers deduplicate by Transcript.
type Created struct {
	Transcript   shared.TranscriptID
	Recording    shared.RecordingID
	ModelName    values.ModelName
	ModelVersion values.ModelVersion
	Language     Language
}
