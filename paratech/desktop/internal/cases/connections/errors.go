package connections

import (
	"errors"
	"fmt"
	"strings"

	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/settings/settings"
	"paratech/desktop/internal/domain/shared"
)

// Errors of the use case (ARCH-18).
var (
	// ErrInvalidConfig is returned by New when a port is missing or the configuration is unusable.
	ErrInvalidConfig = errors.New("invalid connections configuration")

	// ErrInvalidSecret means the value was refused: empty, too large, or a Hub key that cannot be sent
	// (values.ParseHubAPIKey). It wraps shared.ErrInvalidSecret.
	ErrInvalidSecret = fmt.Errorf("connections: %w", shared.ErrInvalidSecret)

	// ErrSecretsUnsupported means there is no credential store on this system (any OS but Windows). It wraps
	// errors.ErrUnsupported.
	ErrSecretsUnsupported = fmt.Errorf("connections: credential store not supported: %w", errors.ErrUnsupported)

	// ErrSecretsUnavailable means the credential store failed.
	ErrSecretsUnavailable = errors.New("connections: credential store unavailable")
)

// Results of CheckMail other than nil (success). The MailChecker returns them; the page shows a fixed text for each.
var (
	ErrCheckBusy                = errors.New("connections: a mail check is already running")
	ErrMailNotConfigured        = errors.New("connections: mail server or login is not set")
	ErrMailPasswordMissing      = errors.New("connections: mail password is not set")
	ErrMailUnreachable          = errors.New("connections: mail server cannot be reached")
	ErrMailCertificate          = errors.New("connections: mail server certificate is not trusted")
	ErrMailTLSOrAuthUnsupported = errors.New("connections: mail server lacks the required TLS or login method")
	ErrMailLoginRejected        = errors.New("connections: mail server rejected the login")
	ErrMailTemporary            = errors.New("connections: mail server is temporarily unavailable")
	ErrMailRefused              = errors.New("connections: mail server refused the session")
)

// checkResults are the classified results of a check; anything else is wrapped as unclassified.
var checkResults = []error{
	ErrMailNotConfigured, ErrMailPasswordMissing, ErrSecretsUnsupported, ErrSecretsUnavailable, ErrMailUnreachable,
	ErrMailCertificate, ErrMailTLSOrAuthUnsupported, ErrMailLoginRejected, ErrMailTemporary, ErrMailRefused,
}

// Results of RefreshQuota other than nil (success). The QuotaSource returns the Hub ones; the page shows a fixed text
// for each. ErrSecretsUnsupported and ErrSecretsUnavailable are results too: the key could not be read.
var (
	ErrQuotaBusy        = errors.New("connections: a quota refresh is already running")
	ErrQuotaSuperseded  = errors.New("connections: the hub key or address changed during the refresh")
	ErrHubNotConfigured = errors.New("connections: hub address is not set")
	ErrHubKeyMissing    = errors.New("connections: hub api key is not set")
	ErrHubKeyRejected   = errors.New("connections: hub rejected the api key")
	ErrHubUnreachable   = errors.New("connections: hub cannot be reached")
	ErrHubUnavailable   = errors.New("connections: hub is temporarily unavailable")
	ErrHubRefused       = errors.New("connections: hub could not answer the quota request")
)

// quotaResults are the classified results of a refresh with what they mean for the shown quota; anything else is an
// unclassified failure.
var quotaResults = []struct {
	err     error
	outcome values.QuotaOutcome
}{
	{ErrHubNotConfigured, values.QuotaNotConfigured},
	{ErrHubKeyMissing, values.QuotaNoKey},
	{ErrHubKeyRejected, values.QuotaKeyRejected},
	{ErrHubUnreachable, values.QuotaUnreachable},
	{ErrHubUnavailable, values.QuotaUnreachable},
	{ErrHubRefused, values.QuotaFailed},
	{ErrSecretsUnsupported, values.QuotaFailed},
	{ErrSecretsUnavailable, values.QuotaFailed},
}

// Field names a setting of the page; the values are the codes the page uses.
type Field string

// Fields of the page.
const (
	FieldHubURL       Field = "hubUrl"
	FieldRecipient    Field = "recipient"
	FieldSMTPHost     Field = "smtpHost"
	FieldSMTPPort     Field = "smtpPort"
	FieldSMTPSecurity Field = "smtpSecurity"
	FieldSMTPLogin    Field = "smtpLogin"
)

// InvalidFieldsError lists every field the settings refused; nothing was saved. It wraps settings.ErrInvalidSettings.
// It names fields only, never the values.
type InvalidFieldsError struct {
	Fields []Field
}

func (e *InvalidFieldsError) Error() string {
	names := make([]string, len(e.Fields))
	for i, f := range e.Fields {
		names[i] = string(f)
	}
	return "connections: invalid " + strings.Join(names, ", ")
}

func (e *InvalidFieldsError) Unwrap() error { return settings.ErrInvalidSettings }
