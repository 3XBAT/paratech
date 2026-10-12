package settings

import "paratech/desktop/internal/domain/shared"

// Settings is the typed set of user settings of the installation (PRD sections 6.1, 7.4 and 15).
// Skeleton: see the package comment.
type Settings struct {
	version                    int64 // persisted version for optimistic concurrency; 0 = never stored (ADR-0004)
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

// Version returns the version this instance was last loaded or saved with: 0 for an aggregate that was never
// stored, otherwise the stored version, which is at least 1 (optimistic concurrency, ADR-0004). The repository
// compares it with the stored one in Save.
func (s *Settings) Version() int64 { return s.version }

// Persisted records the version the store assigned after a successful Save. It is called by the repository adapter
// only, after the write succeeded, so that saving the same instance again compares against its own earlier write
// (ADR-0004). It holds no persistence logic and is not a state change of the business entity: use cases and
// handlers never call it.
func (s *Settings) Persisted(version int64) { s.version = version }
