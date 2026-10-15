package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"paratech/desktop/internal/adapters/hubclient"
	"paratech/desktop/internal/adapters/smtp"
	"paratech/desktop/internal/cases/connections"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/shared"
)

// The bridges satisfy the ports of the use case, and the Hub client is what the quota bridge reads.
var (
	_ connections.MailChecker = mailCheck{}
	_ connections.QuotaSource = hubQuota{}
	_ quotaReader             = (*hubclient.Client)(nil)
)

// wireConnections builds the «Подключения» page (TASK-062, TASK-039) on the settings, the mail credential store (the
// one the smtp adapter reads), the mailer, whose Check runs the session setup of a send, the hub credential store (the
// one the Hub client reads) and the Hub client of the core, which reads the remaining quota.
func (c *core) wireConnections(e env, repo connections.SettingsRepository, mailSecrets secretStore, mailer mailChecker,
	hubSecrets secretStore, hub quotaReader, log *slog.Logger) error {
	svc, err := connections.New(connections.Deps{
		Settings: repo, Clock: e.clock, MailSecret: mailSecrets, MailCheck: mailCheck{mailer}, HubSecret: hubSecrets,
		Quota: hubQuota{hub}, Log: log,
	}, connections.Config{MailSecretName: smtp.DefaultCredentialName, HubSecretName: hubclient.DefaultCredentialName})
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

// quotaReader is hubclient.Client.GetRemaining.
type quotaReader interface {
	GetRemaining(ctx context.Context) (values.QuotaSnapshot, error)
}

// hubQuota is the connections.QuotaSource of the Hub client: mapping only, from the client's classes to the quota
// results of the use case. The client's texts are fixed and carry no key, so the cause stays wrapped. A missing URL or
// key also matches shared.ErrNetworkUnavailable (it parks LLM jobs), so those are matched first.
type hubQuota struct{ hub quotaReader }

func (q hubQuota) Remaining(ctx context.Context) (values.QuotaSnapshot, error) {
	s, err := q.hub.GetRemaining(ctx)
	var result error
	switch {
	case err == nil:
		return s, nil
	case errors.Is(err, hubclient.ErrMissingAPIKey):
		result = connections.ErrHubKeyMissing
	case errors.Is(err, hubclient.ErrNoURL), errors.Is(err, hubclient.ErrInvalidConfig):
		result = connections.ErrHubNotConfigured
	case errors.Is(err, hubclient.ErrInvalidAPIKey), errors.Is(err, hubclient.ErrMalformedAPIKey):
		result = connections.ErrHubKeyRejected
	case errors.Is(err, hubclient.ErrCredentials) && errors.Is(err, errors.ErrUnsupported):
		result = connections.ErrSecretsUnsupported
	case errors.Is(err, hubclient.ErrCredentials):
		result = connections.ErrSecretsUnavailable
	case errors.Is(err, shared.ErrNetworkUnavailable), errors.Is(err, hubclient.ErrTimeout):
		result = connections.ErrHubUnreachable
	case errors.Is(err, hubclient.ErrUnavailable), errors.Is(err, hubclient.ErrRateLimited), errors.Is(err, hubclient.ErrBusy):
		result = connections.ErrHubUnavailable
	case errors.Is(err, hubclient.ErrRejected), errors.Is(err, hubclient.ErrBadResponse):
		result = connections.ErrHubRefused
	default:
		return s, err // e.g. the settings could not be read or the call was cancelled: not a result of the Hub
	}
	return values.QuotaSnapshot{}, fmt.Errorf("%w: %w", result, err)
}
