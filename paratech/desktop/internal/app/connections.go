package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"paratech/desktop/internal/adapters/smtp"
	"paratech/desktop/internal/cases/connections"
)

// wireConnections builds the "Connections" section of the settings page (TASK-062) on the settings, the mail credential
// store (the one the smtp adapter reads) and the mailer, whose Check runs the session setup of a send.
func (c *core) wireConnections(e env, repo connections.SettingsRepository, mailSecrets secretStore, mailer mailChecker,
	log *slog.Logger) error {
	svc, err := connections.New(connections.Deps{
		Settings: repo, Clock: e.clock, MailSecret: mailSecrets, MailCheck: mailCheck{mailer}, Log: log,
	}, connections.Config{MailSecretName: smtp.DefaultCredentialName})
	if err != nil {
		return fmt.Errorf("app: connections: %w", err)
	}
	c.ui.Connections = svc
	return nil
}

// mailChecker is smtp.Mailer.Check.
type mailChecker interface {
	Check(ctx context.Context) error
}

// mailCheck is the connections.MailChecker of the smtp adapter: mapping only, from the adapter's session classes to the
// check results of the use case. The adapter's texts are fixed and carry no secret, so the cause stays wrapped.
type mailCheck struct{ mailer mailChecker }

func (m mailCheck) CheckMail(ctx context.Context) error {
	err := m.mailer.Check(ctx)
	var result error
	switch {
	case err == nil:
		return nil
	case errors.Is(err, smtp.ErrNotConfigured), errors.Is(err, smtp.ErrAccountInvalid):
		result = connections.ErrMailNotConfigured
	case errors.Is(err, smtp.ErrNoPassword):
		result = connections.ErrMailPasswordMissing
	case errors.Is(err, smtp.ErrStoreUnavailable) && errors.Is(err, errors.ErrUnsupported):
		result = connections.ErrSecretsUnsupported
	case errors.Is(err, smtp.ErrStoreUnavailable):
		result = connections.ErrSecretsUnavailable
	case errors.Is(err, smtp.ErrUnreachable):
		result = connections.ErrMailUnreachable
	case errors.Is(err, smtp.ErrCertificate):
		result = connections.ErrMailCertificate
	case errors.Is(err, smtp.ErrSetupUnsupported):
		result = connections.ErrMailTLSOrAuthUnsupported
	case errors.Is(err, smtp.ErrLoginRejected):
		result = connections.ErrMailLoginRejected
	case errors.Is(err, smtp.ErrServerTemporary):
		result = connections.ErrMailTemporary
	case errors.Is(err, smtp.ErrServerRefused):
		result = connections.ErrMailRefused
	default:
		return err // e.g. the settings could not be read: not a result of the check
	}
	return fmt.Errorf("%w: %w", result, err)
}
