package preferences

import (
	"paratech/desktop/internal/domain/settings/settings"
	"paratech/desktop/internal/domain/shared"
)

// Toggles are the settings the settings page shows and changes.
type Toggles struct {
	// RecordingPromptsEnabled: the "lesson is starting" popup is shown automatically (AC-02).
	RecordingPromptsEnabled bool
	// ProcessOnlyOnACPower: heavy local jobs wait for AC power (AC-21, AC-22).
	ProcessOnlyOnACPower bool
	// RecognitionLoad: how hard speech recognition may load the computer; applies from the next run (TASK-084).
	RecognitionLoad shared.RecognitionLoad
	// FreeWorkNotice: the agent shows the free-work card before a work on a topic (TASK-188).
	FreeWorkNotice bool
	// NoteRecognition: who recognises the text of photo pages of notes (TASK-294); off by default.
	NoteRecognition settings.NoteRecognition
}

// LanguageStatus is whether the photo recognition engine has the Russian recognizer.
type LanguageStatus string

// Language statuses.
const (
	LanguageAvailable LanguageStatus = "available" // Russian is installed
	LanguageMissing   LanguageStatus = "missing"   // the engine works, Russian is not installed: the page says how to add it
	LanguageUnknown   LanguageStatus = "unknown"   // not known (another OS, no OCR, a failure): the page says nothing
)
