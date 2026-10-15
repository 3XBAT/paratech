package connections

import (
	"context"
	"time"

	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/settings/settings"
)

// SettingsRepository stores the settings singleton. Implemented by the sqlite adapter.
type SettingsRepository interface {
	// Load returns the stored settings, or the defaults with Version() == 0 while nothing is stored.
	Load(ctx context.Context) (*settings.Settings, error)

	// Save is the version compare-and-swap of ADR-0004(d): a stale version yields shared.ErrVersionConflict. On
	// success the adapter calls Persisted on the aggregate.
	Save(ctx context.Context, s *settings.Settings) error
}

// Clock supplies the change time of a setting (ARCH-24).
type Clock interface {
	Now() time.Time
}

// SecretWriter is the write side of a credential store (the wincred adapter in the app; ARCH-34). There is no Get: no
// secret travels from the store into the core. Errors never contain the secret.
type SecretWriter interface {
	// Set stores or replaces the secret under name; an empty or too large secret yields shared.ErrInvalidSecret.
	Set(ctx context.Context, name, secret string) error

	// Exists reports whether a secret is stored under name, without revealing it.
	Exists(ctx context.Context, name string) (bool, error)

	// Delete removes the secret under name; a missing secret is not an error.
	Delete(ctx context.Context, name string) error
}

// MailChecker opens a session with the mail server exactly as a send does (connect, EHLO, STARTTLS, AUTH) and ends it
// with QUIT, without a message. Implemented by the smtp adapter through a bridge in the app.
type MailChecker interface {
	// CheckMail returns nil when the connection and the login work, otherwise one of the check errors of this package
	// (ErrMailNotConfigured ... ErrMailRefused) or an unclassified error.
	CheckMail(ctx context.Context) error
}

// QuotaSource reads the tester's remaining quota from the Hub (PRD section 9.2, AC-12). Implemented by the Hub client
// through a bridge in the app, with the URL from the settings and the key from the hub credential store.
type QuotaSource interface {
	// Remaining returns the Hub's snapshot, otherwise one of the quota results of this package (ErrHubNotConfigured
	// ... ErrHubRefused, ErrSecretsUnsupported, ErrSecretsUnavailable) or an unclassified error. Errors never contain
	// the key.
	Remaining(ctx context.Context) (values.QuotaSnapshot, error)
}
