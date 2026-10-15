package connections

import (
	"errors"
	"fmt"
	"strings"

	"paratech/desktop/internal/domain/settings/settings"
	"paratech/desktop/internal/domain/shared"
)

// Errors of the use case (ARCH-18).
var (
	// ErrInvalidConfig is returned by New when a port is missing or the configuration is unusable.
	ErrInvalidConfig = errors.New("invalid connections configuration")

	// ErrInvalidSecret means the store refused the value: empty or too large. It wraps shared.ErrInvalidSecret.
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
