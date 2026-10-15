package connections

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"paratech/desktop/internal/domain/coursework/values"
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
	opSetHubKey      = "connections: set hub api key"
	opDropHubKey     = "connections: remove hub api key"
	opHubKeyStatus   = "connections: hub api key status"
	opRefreshQuota   = "connections: refresh quota"
)

// secretSlot is one secret of the section: its store and its entry.
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
	hub      secretSlot
	quota    QuotaSource
	log      *slog.Logger

	checkTimeout   time.Duration
	checking       atomic.Bool // one mail check at a time
	refreshTimeout time.Duration
	refreshing     atomic.Bool // one quota refresh at a time

	mu      sync.Mutex
	reading values.QuotaReading // in memory only: shown, never used to decide
	account uint64              // changes with the Hub key or address; a refresh of an older one is discarded
}

// New checks the ports and the configuration.
func New(d Deps, cfg Config) (*Service, error) {
	if d.Settings == nil || d.Clock == nil || d.MailSecret == nil || d.MailCheck == nil || d.HubSecret == nil || d.Quota == nil {
		return nil, fmt.Errorf("%w: every port is required", ErrInvalidConfig)
	}
	if cfg.MailSecretName == "" {
		cfg.MailSecretName = DefaultMailSecretName
	}
	if cfg.HubSecretName == "" {
		cfg.HubSecretName = DefaultHubSecretName
	}
	if cfg.CheckTimeout == 0 {
		cfg.CheckTimeout = DefaultCheckTimeout
	}
	if cfg.RefreshTimeout == 0 {
		cfg.RefreshTimeout = DefaultRefreshTimeout
	}
	if cfg.CheckTimeout < 0 || cfg.RefreshTimeout < 0 {
		return nil, fmt.Errorf("%w: negative timeout", ErrInvalidConfig)
	}
	log := d.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Service{
		settings: d.Settings, clock: d.Clock, mail: secretSlot{store: d.MailSecret, name: cfg.MailSecretName},
		check: d.MailCheck, hub: secretSlot{store: d.HubSecret, name: cfg.HubSecretName}, quota: d.Quota, log: log,
		checkTimeout: cfg.CheckTimeout, refreshTimeout: cfg.RefreshTimeout,
	}, nil
}

// Get returns the section: the stored settings (the defaults while nothing is stored), whether the password and the
// Hub key are set, and the quota of the last refresh. It does not call the Hub.
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

// SaveHubURL sets the Hub address; empty removes it. A refused address is an *InvalidFieldsError. A new address
// forgets the quota shown: it was the answer of another Hub.
func (s *Service) SaveHubURL(ctx context.Context, url string) (View, error) {
	var moved bool
	v, err := s.change(ctx, opSaveHubURL, func(st *settings.Settings, now time.Time) []Field {
		before := st.HubURL()
		bad := refused(check(FieldHubURL, st.SetHubURL(url, now)))
		moved = st.HubURL() != before
		return bad
	})
	if err == nil && moved {
		v.Quota = s.forgetQuota()
	}
	return v, err
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

// SetHubKey stores or replaces the Hub API key in the credential store, never in SQLite (hard rule 4; PRD section
// 12). The key must be sendable (values.ParseHubAPIKey: surrounding blanks of a paste are dropped); otherwise nothing
// changes and the error is ErrInvalidSecret. It returns the status after the call, never the key, and forgets the
// quota shown, which belonged to the old key.
func (s *Service) SetHubKey(ctx context.Context, key string) (SecretStatus, error) {
	parsed, err := values.ParseHubAPIKey(key)
	if err != nil {
		return s.status(ctx, opSetHubKey, s.hub), fmt.Errorf("%s: %w", opSetHubKey, ErrInvalidSecret)
	}
	defer s.forgetQuota()
	return s.setSecret(ctx, opSetHubKey, s.hub, parsed)
}

// RemoveHubKey deletes the stored Hub API key; a missing one is not an error. The quota shown is forgotten.
func (s *Service) RemoveHubKey(ctx context.Context) (SecretStatus, error) {
	defer s.forgetQuota()
	return s.removeSecret(ctx, opDropHubKey, s.hub)
}

// RefreshQuota asks the Hub for the remaining quota (AC-12) and returns what the page shows afterwards. The error is
// nil on success, ErrQuotaBusy while another refresh runs, ErrQuotaSuperseded when the key or the address changed
// meanwhile (the answer is discarded), a quota result of this package, or an unclassified failure. A done ctx changes
// nothing. The last snapshot is kept in memory only, as values.QuotaReading decides.
func (s *Service) RefreshQuota(ctx context.Context) (values.QuotaReading, error) {
	if !s.refreshing.CompareAndSwap(false, true) {
		return s.quotaReading(), ErrQuotaBusy
	}
	defer s.refreshing.Store(false)
	if err := ctx.Err(); err != nil {
		return s.quotaReading(), fmt.Errorf("%s: %w", opRefreshQuota, err)
	}

	account := s.quotaAccount()
	rctx, cancel := context.WithTimeout(ctx, s.refreshTimeout)
	snap, err := s.quota.Remaining(rctx)
	cancel()
	if err != nil && ctx.Err() != nil {
		return s.quotaReading(), fmt.Errorf("%s: %w", opRefreshQuota, ctx.Err())
	}
	outcome, result := quotaResult(err)
	now := s.clock.Now()

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.account != account {
		return s.reading, ErrQuotaSuperseded
	}
	if outcome == values.QuotaCurrent {
		next, err := s.reading.Read(snap, now)
		if err == nil {
			s.reading = next
			return s.reading, nil
		}
		outcome, result = values.QuotaFailed, fmt.Errorf("%s: %w", opRefreshQuota, err)
	}
	s.reading = s.reading.Failed(outcome, now)
	return s.reading, result
}

// quotaResult classifies an answer of the QuotaSource. The bound of the refresh counts as a Hub that did not answer.
func quotaResult(err error) (values.QuotaOutcome, error) {
	if err == nil {
		return values.QuotaCurrent, nil
	}
	for _, known := range quotaResults {
		if errors.Is(err, known.err) {
			return known.outcome, err
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return values.QuotaUnreachable, fmt.Errorf("%w: %w", ErrHubUnreachable, err)
	}
	return values.QuotaFailed, fmt.Errorf("%s: %w", opRefreshQuota, err)
}

func (s *Service) quotaReading() values.QuotaReading {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reading
}

func (s *Service) quotaAccount() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.account
}

// forgetQuota drops the shown quota when the key or the address changes, and makes a refresh still running for the
// old one discard its answer.
func (s *Service) forgetQuota() values.QuotaReading {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.account++
	s.reading = values.QuotaReading{}
	return s.reading
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
		HubKey:            s.status(ctx, opHubKeyStatus, s.hub),
		Quota:             s.quotaReading(),
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
