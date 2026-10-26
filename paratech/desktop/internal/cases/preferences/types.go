package preferences

import "paratech/desktop/internal/domain/shared"

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
}
