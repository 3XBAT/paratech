package settings

import (
	"fmt"
	"strings"
	"time"

	"paratech/desktop/internal/domain/shared"
)

// Defaults of a fresh installation (PRD sections 6.1 and 7.4).
const (
	DefaultRecordingPromptsEnabled = true // AC-02: prompts are on until the user turns them off
	DefaultProcessOnlyOnACPower    = true // AC-21: heavy jobs wait for AC power by default
	DefaultFreeWorkNotice          = true // TASK-188: the free-work card comes before a work on a topic
)

// Settings is the typed set of user settings of the installation (PRD sections 6.1 and 7.4). It is a singleton:
// there is exactly one per installation.
type Settings struct {
	version                 int64 // persisted version for optimistic concurrency; 0 = never stored (ADR-0004)
	recordingPromptsEnabled bool
	processOnlyOnACPower    bool
	defaultSttModel         shared.SttModelID // empty: no default selected (reference by ID, ARCH-13)

	// Mail and Hub setup (PRD sections 10 and 12). None of it is a secret: the SMTP password and the Hub API key live
	// in the credential store (hard rule 4). Empty means not set.
	homeworkRecipient string
	smtpHost          string
	smtpPort          int // 0: the default port of the security mode
	smtpSecurity      MailSecurity
	smtpLogin         string
	hubURL            string

	// recordingSource is the app the user last recorded (TASK-059), preselected in the source picker; empty: the
	// whole computer. An app name, never a process (it is remembered across restarts).
	recordingSource shared.AppName

	// recognitionLoad is how hard speech recognition may load the computer (TASK-084).
	recognitionLoad shared.RecognitionLoad

	// profile is the student profile of the title page of a homework document (TASK-094); not a secret.
	profile Profile

	// freeWorkNotice: the agent shows «Такого задания нет — могу сделать как свободную работу» before a work on a
	// topic (TASK-188); off after «Больше не показывать», back on from «Настройки».
	freeWorkNotice bool

	updatedAt time.Time // zero: never changed by the user
}

// New returns the defaults of a fresh installation, never stored (Version() == 0).
func New() *Settings {
	return &Settings{
		recordingPromptsEnabled: DefaultRecordingPromptsEnabled,
		processOnlyOnACPower:    DefaultProcessOnlyOnACPower,
		smtpSecurity:            DefaultMailSecurity,
		recognitionLoad:         shared.DefaultRecognitionLoad,
		freeWorkNotice:          DefaultFreeWorkNotice,
	}
}

// State is the stored form of the settings, for Restore.
type State struct {
	Version                 int64
	RecordingPromptsEnabled bool
	ProcessOnlyOnACPower    bool
	DefaultSttModel         shared.SttModelID      // empty: none selected
	HomeworkRecipient       string                 // empty: none
	SMTPHost                string                 // empty: none
	SMTPPort                int                    // 0: the default of the mode
	SMTPSecurity            MailSecurity           // empty: DefaultMailSecurity
	SMTPLogin               string                 // empty: none
	HubURL                  string                 // empty: none
	RecordingSource         shared.AppName         // empty: the whole computer
	RecognitionLoad         shared.RecognitionLoad // empty or unknown: shared.DefaultRecognitionLoad
	Profile                 ProfileFields          // the student profile; empty fields: not set
	FreeWorkNotice          bool                   // the free-work card before a work on a topic
	UpdatedAt               time.Time              // zero: stored defaults that the user never changed
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
	// A tampered row must not send the API key or the password somewhere the setters would refuse.
	if err := checkMail(s); err != nil {
		return nil, err
	}
	if !s.RecordingSource.IsZero() {
		if err := s.RecordingSource.Validate(); err != nil {
			return nil, fmt.Errorf("%w: recording source: %w", ErrInvalidSettings, err)
		}
	}
	// A broken stored profile (an older or tampered row) is dropped instead of failing the load: it only fills a title page.
	profile, err := NewProfile(s.Profile)
	if err != nil {
		profile = Profile{}
	}
	security := s.SMTPSecurity
	if security == "" {
		security = DefaultMailSecurity
	}

	return &Settings{
		version:                 s.Version,
		recordingPromptsEnabled: s.RecordingPromptsEnabled,
		processOnlyOnACPower:    s.ProcessOnlyOnACPower,
		defaultSttModel:         s.DefaultSttModel,
		homeworkRecipient:       s.HomeworkRecipient,
		smtpHost:                s.SMTPHost,
		smtpPort:                s.SMTPPort,
		smtpSecurity:            security,
		smtpLogin:               s.SMTPLogin,
		hubURL:                  s.HubURL,
		recordingSource:         s.RecordingSource,
		// An unknown stored mode (an older or tampered row) falls back to the gentlest instead of failing the load.
		recognitionLoad: s.RecognitionLoad.OrDefault(),
		profile:         profile,
		freeWorkNotice:  s.FreeWorkNotice,
		updatedAt:       s.UpdatedAt,
	}, nil
}

// RecordingPromptsEnabled reports whether the automatic "lesson is starting" popup is on (AC-02).
func (s *Settings) RecordingPromptsEnabled() bool { return s.recordingPromptsEnabled }

// ProcessOnlyOnACPower reports whether heavy local jobs wait for AC power (PRD section 7.4, AC-21, AC-22).
func (s *Settings) ProcessOnlyOnACPower() bool { return s.processOnlyOnACPower }

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

// HomeworkRecipient returns the address homework is emailed to (PRD section 10); empty when none is set.
func (s *Settings) HomeworkRecipient() string { return s.homeworkRecipient }

// SMTPHost returns the host name of the SMTP server; empty when none is set.
func (s *Settings) SMTPHost() string { return s.smtpHost }

// SMTPPort returns the SMTP port; 0 means the default port of SMTPSecurity.
func (s *Settings) SMTPPort() int { return s.smtpPort }

// SMTPSecurity returns how the SMTP connection is encrypted.
func (s *Settings) SMTPSecurity() MailSecurity { return s.smtpSecurity }

// SMTPLogin returns the SMTP user name; empty when none is set. The password is in the credential store.
func (s *Settings) SMTPLogin() string { return s.smtpLogin }

// HubURL returns the base URL of the LLM Hub (PRD section 12); empty when none is set.
func (s *Settings) HubURL() string { return s.hubURL }

// MailSender returns the From address of the homework email: the login, when it is a plain address. Empty otherwise,
// and then mail is not configured (MailConfigured).
func (s *Settings) MailSender() string {
	if isAddress(s.smtpLogin) {
		return s.smtpLogin
	}
	return ""
}

// MailConfigured reports whether homework can be emailed: a server, a recipient and a login that is an address
// (it is also the sender) are set. Whether the password is stored is up to the credential store.
func (s *Settings) MailConfigured() bool {
	return s.SMTPAccountConfigured() && s.homeworkRecipient != ""
}

// SMTPAccountConfigured reports whether an email can be sent at all: a server and a login that is an address. The
// recipient is not needed: a delivery keeps the recipient frozen at confirmation, so clearing the setting later does
// not break a pending send.
func (s *Settings) SMTPAccountConfigured() bool {
	return s.smtpHost != "" && s.MailSender() != ""
}

// SetHomeworkRecipient sets the address homework is emailed to (PRD section 10); empty removes it. Only a plain
// address is accepted.
func (s *Settings) SetHomeworkRecipient(address string, now time.Time) error {
	address = strings.TrimSpace(address)
	if address != "" {
		if err := checkAddress(address); err != nil {
			return err
		}
	}
	return change(s, &s.homeworkRecipient, address, now)
}

// SetSMTPHost sets the SMTP server, a plain host name or IPv4 address; empty removes it.
func (s *Settings) SetSMTPHost(host string, now time.Time) error {
	host = strings.TrimSpace(host)
	if host != "" {
		if err := checkHost(host); err != nil {
			return err
		}
	}
	return change(s, &s.smtpHost, host, now)
}

// SetSMTPPort sets the SMTP port, 1-65535, or 0 for the default port of the security mode.
func (s *Settings) SetSMTPPort(port int, now time.Time) error {
	if err := checkPort(port); err != nil {
		return err
	}
	return change(s, &s.smtpPort, port, now)
}

// SetSMTPSecurity sets how the SMTP connection is encrypted.
func (s *Settings) SetSMTPSecurity(m MailSecurity, now time.Time) error {
	if !m.Valid() {
		return fmt.Errorf("%w: unknown smtp security %q", ErrInvalidSettings, m)
	}
	return change(s, &s.smtpSecurity, m, now)
}

// SetSMTPLogin sets the SMTP user name; empty removes it. Mail is configured only when it is a plain address.
func (s *Settings) SetSMTPLogin(login string, now time.Time) error {
	login = strings.TrimSpace(login)
	if login != "" {
		if err := checkLogin(login); err != nil {
			return err
		}
	}
	return change(s, &s.smtpLogin, login, now)
}

// SetHubURL sets the base URL of the LLM Hub: https, or http to a loopback host; empty removes it.
func (s *Settings) SetHubURL(raw string, now time.Time) error {
	raw = strings.TrimSpace(raw)
	if raw != "" {
		if err := checkHubURL(raw); err != nil {
			return err
		}
	}
	return change(s, &s.hubURL, raw, now)
}

// RecordingSource returns the app the user last recorded, preselected in the source picker; empty means the whole
// computer (TASK-059).
func (s *Settings) RecordingSource() shared.AppName { return s.recordingSource }

// SetRecordingSource remembers app as the source of the next recordings; empty means the whole computer. Only a
// normalized executable name is accepted (shared.AppName).
func (s *Settings) SetRecordingSource(app shared.AppName, now time.Time) error {
	if !app.IsZero() {
		if err := app.Validate(); err != nil {
			return fmt.Errorf("%w: recording source: %w", ErrInvalidSettings, err)
		}
	}
	return change(s, &s.recordingSource, app, now)
}

// RecognitionLoad returns how hard speech recognition may load the computer (TASK-084).
func (s *Settings) RecognitionLoad() shared.RecognitionLoad { return s.recognitionLoad }

// SetRecognitionLoad sets the load mode of speech recognition; it applies from the next recognition run.
func (s *Settings) SetRecognitionLoad(l shared.RecognitionLoad, now time.Time) error {
	if !l.Valid() {
		return fmt.Errorf("%w: unknown recognition load %q", ErrInvalidSettings, l)
	}
	return change(s, &s.recognitionLoad, l, now)
}

// FreeWorkNotice reports whether the agent shows the free-work card before a work on a topic (TASK-188).
func (s *Settings) FreeWorkNotice() bool { return s.freeWorkNotice }

// SetFreeWorkNotice turns the free-work card on or off: off is «Больше не показывать» on the card, on is the toggle
// in «Настройки».
func (s *Settings) SetFreeWorkNotice(on bool, now time.Time) error {
	return change(s, &s.freeWorkNotice, on, now)
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
