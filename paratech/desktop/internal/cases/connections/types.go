package connections

import (
	"log/slog"
	"time"

	"paratech/desktop/internal/domain/settings/settings"
)

// Defaults of Config.
const (
	DefaultMailSecretName = "smtp-password" // the entry the smtp adapter reads (smtp.DefaultCredentialName)
	DefaultCheckTimeout   = 30 * time.Second
)

// Deps are the ports the Service needs. Log is optional.
type Deps struct {
	Settings   SettingsRepository
	Clock      Clock
	MailSecret SecretWriter // the store the smtp adapter reads the password from
	MailCheck  MailChecker
	Log        *slog.Logger
}

// Config holds the settings of the use case; zero fields take the defaults.
type Config struct {
	MailSecretName string        // entry of the SMTP password; "": DefaultMailSecretName
	CheckTimeout   time.Duration // bound of one mail check; 0: DefaultCheckTimeout
}

// SecretStatus is what the page may know about a secret: whether it is stored, never the value.
type SecretStatus int

// Secret statuses. The zero value never claims a stored secret.
const (
	SecretUnavailable SecretStatus = iota // the store failed, the answer is unknown
	SecretUnset
	SecretSet
	SecretUnsupported // no credential store on this system
)

// View is the "Connections" section as the page shows it. It never holds a secret. Empty strings are "not set".
type View struct {
	HubURL string

	HomeworkRecipient string
	SMTPHost          string
	SMTPPort          int // 0: the default port of SMTPSecurity
	SMTPSecurity      settings.MailSecurity
	SMTPLogin         string
	MailConfigured    bool // server, recipient and a login that is an address are set (settings.MailConfigured)

	MailPassword SecretStatus
}

// MailInput is the mail account as the page submits it. Empty strings remove a value.
type MailInput struct {
	Recipient string
	Host      string
	Port      int
	Security  settings.MailSecurity
	Login     string
}
