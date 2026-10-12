package settings

import (
	"fmt"
	"time"

	"paratech/desktop/internal/domain/shared"
)

// Defaults of a fresh installation (PRD sections 6.1, 7.4 and 15).
const (
	DefaultRecordingPromptsEnabled    = true // AC-02: prompts are on until the user turns them off
	DefaultProcessOnlyOnACPower       = true // AC-21: heavy jobs wait for AC power by default
	DefaultNotifyOnProcessingComplete = true // PRD section 15
)

// Settings is the typed set of user settings of the installation (PRD sections 6.1, 7.4 and 15). It is a singleton:
// there is exactly one per installation.
type Settings struct {
	version                    int64 // persisted version for optimistic concurrency; 0 = never stored (ADR-0004)
	recordingPromptsEnabled    bool
	processOnlyOnACPower       bool
	notifyOnProcessingComplete bool
	defaultSttModel            shared.SttModelID // empty: no default selected (reference by ID, ARCH-13)
	updatedAt                  time.Time         // zero: never changed by the user
}

// New returns the defaults of a fresh installation, never stored (Version() == 0).
func New() *Settings {
	return &Settings{
		recordingPromptsEnabled:    DefaultRecordingPromptsEnabled,
		processOnlyOnACPower:       DefaultProcessOnlyOnACPower,
		notifyOnProcessingComplete: DefaultNotifyOnProcessingComplete,
	}
}

// State is the stored form of the settings, for Restore.
type State struct {
	Version                    int64
	RecordingPromptsEnabled    bool
	ProcessOnlyOnACPower       bool
	NotifyOnProcessingComplete bool
	DefaultSttModel            shared.SttModelID // empty: none selected
	UpdatedAt                  time.Time         // zero: stored defaults that the user never changed
}

// Restore rebuilds stored settings without side effects; it only checks that the state is consistent.
func Restore(s State) (*Settings, error) {
	if s.Version < 1 {
		return nil, fmt.Errorf("%w: stored version %d", ErrInvalidSettings, s.Version)
	}
	if s.DefaultSttModel != "" {
		if err := s.DefaultSttModel.Validate(); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalidSettings, err)
		}
	}

	return &Settings{
		version:                    s.Version,
		recordingPromptsEnabled:    s.RecordingPromptsEnabled,
		processOnlyOnACPower:       s.ProcessOnlyOnACPower,
		notifyOnProcessingComplete: s.NotifyOnProcessingComplete,
		defaultSttModel:            s.DefaultSttModel,
		updatedAt:                  s.UpdatedAt,
	}, nil
}

// RecordingPromptsEnabled reports whether the automatic "lesson is starting" popup is on (AC-02).
func (s *Settings) RecordingPromptsEnabled() bool { return s.recordingPromptsEnabled }

// ProcessOnlyOnACPower reports whether heavy local jobs wait for AC power (PRD section 7.4, AC-21, AC-22).
func (s *Settings) ProcessOnlyOnACPower() bool { return s.processOnlyOnACPower }

// NotifyOnProcessingComplete reports whether finished processing notifies the user (PRD section 15).
func (s *Settings) NotifyOnProcessingComplete() bool { return s.notifyOnProcessingComplete }

// DefaultSttModel returns the selected default STT model, or the empty ID if none is selected (AC-07).
func (s *Settings) DefaultSttModel() shared.SttModelID { return s.defaultSttModel }

// UpdatedAt returns when the user last changed a setting; zero means never (defaults).
func (s *Settings) UpdatedAt() time.Time { return s.updatedAt }

// SetRecordingPromptsEnabled turns the automatic recording prompts on or off (PRD section 6.1, AC-02).
func (s *Settings) SetRecordingPromptsEnabled(enabled bool, now time.Time) error {
	return change(s, &s.recordingPromptsEnabled, enabled, now)
}

// SetProcessOnlyOnACPower turns the AC-power restriction of heavy jobs on or off (PRD section 7.4, AC-22).
func (s *Settings) SetProcessOnlyOnACPower(enabled bool, now time.Time) error {
	return change(s, &s.processOnlyOnACPower, enabled, now)
}

// SetNotifyOnProcessingComplete turns the notification about finished processing on or off (PRD section 15).
func (s *Settings) SetNotifyOnProcessingComplete(enabled bool, now time.Time) error {
	return change(s, &s.notifyOnProcessingComplete, enabled, now)
}

// SelectDefaultSttModel makes an installed model the default (PRD section 8, AC-07). Whether the model is installed is
// checked by the use case and by the foreign key of the store; the aggregate only checks the identifier.
func (s *Settings) SelectDefaultSttModel(id shared.SttModelID, now time.Time) error {
	if err := id.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidSettings, err)
	}
	return change(s, &s.defaultSttModel, id, now)
}

// ClearDefaultSttModel removes the default model, for example before that model is removed (ADR-0004(g)).
func (s *Settings) ClearDefaultSttModel(now time.Time) error {
	return change(s, &s.defaultSttModel, "", now)
}

// change sets one field. Setting the current value is a no-op, so repeating a request does not move UpdatedAt.
func change[T comparable](s *Settings, field *T, value T, now time.Time) error {
	if now.IsZero() {
		return fmt.Errorf("%w: no change time", ErrInvalidSettings)
	}
	if *field == value {
		return nil
	}

	*field = value
	s.updatedAt = now
	return nil
}

// Version returns the version this instance was last loaded or saved with: 0 for an aggregate that was never
// stored, otherwise the stored version, which is at least 1 (optimistic concurrency, ADR-0004). The repository
// compares it with the stored one in Save.
func (s *Settings) Version() int64 { return s.version }

// Persisted records the version the store assigned after a successful Save. It is called by the repository adapter
// only, after the write succeeded, so that saving the same instance again compares against its own earlier write
// (ADR-0004). It holds no persistence logic and is not a state change of the business entity: use cases and
// handlers never call it.
func (s *Settings) Persisted(version int64) { s.version = version }
