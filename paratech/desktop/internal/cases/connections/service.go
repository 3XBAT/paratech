package connections

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"paratech/desktop/internal/domain/settings/settings"
	"paratech/desktop/internal/domain/shared"
)

// conflictAttempts bounds the reload-and-retry loop on a stale settings version (another writer saved in between).
const conflictAttempts = 3

// Operation names, for error texts and logs.
const (
	opGet            = "connections: get"
	opSaveMail       = "connections: save mail"
	opSaveHubURL     = "connections: save hub url"
	opSetMailSecret  = "connections: set mail password"
	opDropMailSecret = "connections: remove mail password"
	opMailStatus     = "connections: mail password status"
	opCheckMail      = "connections: check mail"
)

// secretSlot is one secret of the section: its store and its entry. TASK-039 adds the Hub key as a second slot.
type secretSlot struct {
	store SecretWriter
	name  string
}

// Service reads and changes the "Connections" section. Every save is one load, the setters of the aggregate and one
// compare-and-swap save of the settings singleton (ARCH-23).
type Service struct {
	settings SettingsRepository
	clock    Clock
	mail     secretSlot
	check    MailChecker
	log      *slog.Logger

	checkTimeout time.Duration
	checking     atomic.Bool // one mail check at a time
}

// New checks the ports and the configuration.
func New(d Deps, cfg Config) (*Service, error) {
	if d.Settings == nil || d.Clock == nil || d.MailSecret == nil || d.MailCheck == nil {
		return nil, fmt.Errorf("%w: every port is required", ErrInvalidConfig)
	}
	if cfg.MailSecretName == "" {
		cfg.MailSecretName = DefaultMailSecretName
	}
	if cfg.CheckTimeout == 0 {
		cfg.CheckTimeout = DefaultCheckTimeout
	}
	if cfg.CheckTimeout < 0 {
		return nil, fmt.Errorf("%w: negative check timeout", ErrInvalidConfig)
	}
	log := d.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Service{
		settings: d.Settings, clock: d.Clock, mail: secretSlot{store: d.MailSecret, name: cfg.MailSecretName},
		check: d.MailCheck, log: log, checkTimeout: cfg.CheckTimeout,
	}, nil
}

// Get returns the section: the stored settings (the defaults while nothing is stored) and whether the password is set.
func (s *Service) Get(ctx context.Context) (View, error) {
	st, err := s.settings.Load(ctx)
	if err != nil {
		return View{}, fmt.Errorf("%s: %w", opGet, err)
	}
	return s.view(ctx, st), nil
}

// SaveMail sets the mail account. Every field is checked by the settings aggregate; when any is refused, the error is
// an *InvalidFieldsError naming all of them and nothing is saved.
func (s *Service) SaveMail(ctx context.Context, in MailInput) (View, error) {
	return s.change(ctx, opSaveMail, func(st *settings.Settings, now time.Time) []Field {
		return refused(
			check(FieldRecipient, st.SetHomeworkRecipient(in.Recipient, now)),
			check(FieldSMTPHost, st.SetSMTPHost(in.Host, now)),
			check(FieldSMTPPort, st.SetSMTPPort(in.Port, now)),
			check(FieldSMTPSecurity, st.SetSMTPSecurity(in.Security, now)),
			check(FieldSMTPLogin, st.SetSMTPLogin(in.Login, now)),
		)
	})
}

// SaveHubURL sets the Hub address; empty removes it. A refused address is an *InvalidFieldsError.
func (s *Service) SaveHubURL(ctx context.Context, url string) (View, error) {
	return s.change(ctx, opSaveHubURL, func(st *settings.Settings, now time.Time) []Field {
		return refused(check(FieldHubURL, st.SetHubURL(url, now)))
	})
}

// SetMailPassword stores or replaces the SMTP password in the credential store, never in SQLite (hard rule 4). It
// returns the status after the call: errors are ErrInvalidSecret, ErrSecretsUnsupported or ErrSecretsUnavailable.
func (s *Service) SetMailPassword(ctx context.Context, password string) (SecretStatus, error) {
	return s.setSecret(ctx, opSetMailSecret, s.mail, password)
}

// RemoveMailPassword deletes the stored SMTP password; a missing one is not an error.
func (s *Service) RemoveMailPassword(ctx context.Context) (SecretStatus, error) {
	return s.removeSecret(ctx, opDropMailSecret, s.mail)
}

// CheckMail opens a session with the mail server as a send does and closes it without a message. It returns nil on
// success, ErrCheckBusy while another check runs, a check result of this package, or an unclassified error.
func (s *Service) CheckMail(ctx context.Context) error {
	if !s.checking.CompareAndSwap(false, true) {
		return ErrCheckBusy
	}
	defer s.checking.Store(false)

	ctx, cancel := context.WithTimeout(ctx, s.checkTimeout)
	defer cancel()
	err := s.check.CheckMail(ctx)
	if err == nil {
		return nil
	}
	for _, known := range checkResults {
		if errors.Is(err, known) {
			return err
		}
	}
	return fmt.Errorf("%s: %w", opCheckMail, err)
}

// change reloads the settings and applies fn again while the save meets a stale version (ADR-0004(d)).
func (s *Service) change(ctx context.Context, op string, fn func(*settings.Settings, time.Time) []Field) (View, error) {
	var err error
	for range conflictAttempts {
		var st *settings.Settings
		st, err = s.settings.Load(ctx)
		if err != nil {
			break
		}
		if bad := fn(st, s.clock.Now()); len(bad) > 0 {
			return View{}, fmt.Errorf("%s: %w", op, &InvalidFieldsError{Fields: bad})
		}
		if err = s.settings.Save(ctx, st); err == nil {
			return s.view(ctx, st), nil
		}
		if !errors.Is(err, shared.ErrVersionConflict) {
			break
		}
	}
	return View{}, fmt.Errorf("%s: %w", op, err)
}

// fieldCheck is the outcome of one setter.
type fieldCheck struct {
	field Field
	ok    bool
}

func check(f Field, err error) fieldCheck { return fieldCheck{field: f, ok: err == nil} }

// refused returns the fields whose setter failed, in the order given.
func refused(checks ...fieldCheck) []Field {
	var out []Field
	for _, c := range checks {
		if !c.ok {
			out = append(out, c.field)
		}
	}
	return out
}

func (s *Service) view(ctx context.Context, st *settings.Settings) View {
	return View{
		HubURL:            st.HubURL(),
		HomeworkRecipient: st.HomeworkRecipient(),
		SMTPHost:          st.SMTPHost(),
		SMTPPort:          st.SMTPPort(),
		SMTPSecurity:      st.SMTPSecurity(),
		SMTPLogin:         st.SMTPLogin(),
		MailConfigured:    st.MailConfigured(),
		MailPassword:      s.status(ctx, opMailStatus, s.mail),
	}
}

// status asks the store whether the secret exists. A failure is a status, not an error: the rest of the section stays
// usable.
func (s *Service) status(ctx context.Context, op string, slot secretSlot) SecretStatus {
	ok, err := slot.store.Exists(ctx, slot.name)
	if err != nil {
		status, cause := storeFailure(err)
		s.log.Warn(op, "err", cause)
		return status
	}
	if ok {
		return SecretSet
	}
	return SecretUnset
}

func (s *Service) setSecret(ctx context.Context, op string, slot secretSlot, secret string) (SecretStatus, error) {
	if err := slot.store.Set(ctx, slot.name, secret); err != nil {
		status, cause := storeFailure(err)
		if errors.Is(cause, ErrInvalidSecret) {
			status = s.status(ctx, op, slot) // nothing changed: report what is stored
		}
		return status, fmt.Errorf("%s: %w", op, cause)
	}
	return SecretSet, nil
}

func (s *Service) removeSecret(ctx context.Context, op string, slot secretSlot) (SecretStatus, error) {
	if err := slot.store.Delete(ctx, slot.name); err != nil {
		status, cause := storeFailure(err)
		return status, fmt.Errorf("%s: %w", op, cause)
	}
	return SecretUnset, nil
}

// storeFailure classifies an error of the credential store. The store's errors carry no secret.
func storeFailure(err error) (SecretStatus, error) {
	switch {
	case errors.Is(err, errors.ErrUnsupported):
		return SecretUnsupported, ErrSecretsUnsupported
	case errors.Is(err, shared.ErrInvalidSecret):
		return SecretUnavailable, ErrInvalidSecret
	}
	return SecretUnavailable, fmt.Errorf("%w: %w", ErrSecretsUnavailable, err)
}
