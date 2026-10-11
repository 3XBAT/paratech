package settings

import "paratech/desktop/internal/domain/shared"

// Settings is the typed set of user settings of the installation (PRD sections 6.1, 7.4 and 15).
// Skeleton: see the package comment.
type Settings struct {
	recordingPromptsEnabled    bool
	processOnlyOnACPower       bool
	notifyOnProcessingComplete bool
	defaultSttModel            shared.SttModelID // empty: no default selected (reference by ID, ARCH-13)
}

// RecordingPromptsEnabled reports whether the automatic "lesson is starting" popup is on (AC-02).
func (s *Settings) RecordingPromptsEnabled() bool { return s.recordingPromptsEnabled }

// ProcessOnlyOnACPower reports whether heavy local jobs wait for AC power (PRD section 7.4, AC-21, AC-22).
func (s *Settings) ProcessOnlyOnACPower() bool { return s.processOnlyOnACPower }

// NotifyOnProcessingComplete reports whether finished processing notifies the user (PRD section 15).
func (s *Settings) NotifyOnProcessingComplete() bool { return s.notifyOnProcessingComplete }

// DefaultSttModel returns the selected default STT model, or the empty ID if none is selected (AC-07).
func (s *Settings) DefaultSttModel() shared.SttModelID { return s.defaultSttModel }
