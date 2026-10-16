package settings

import (
	"net/url"
	"strings"

	"paratech/desktop/internal/domain/shared"
)

// Summary is what a support bundle may tell about the settings (TASK-058, AC-4): an allowlist of switches, numbers,
// modes and server names. E-mail addresses keep only their domain, the Hub URL only its scheme, host and port; the
// secrets are not in the aggregate at all (hard rule 4).
type Summary struct {
	RecordingPromptsEnabled bool
	ProcessOnlyOnACPower    bool
	DefaultSttModel         shared.SttModelID
	RecordingSource         shared.AppName // the remembered app; empty: the whole computer

	HomeworkRecipient string // "***@domain" or empty
	SMTPHost          string
	SMTPPort          int
	SMTPSecurity      MailSecurity
	SMTPLogin         string // "***@domain" for an address, "***" for another login, empty when none
	MailConfigured    bool
	HubURL            string // "https://host[:port]" or empty
}

// masked stands for the hidden part of a value.
const masked = "***"

// SupportSummary returns the allowlist summary of the settings for a support bundle.
func (s *Settings) SupportSummary() Summary {
	return Summary{
		RecordingPromptsEnabled: s.recordingPromptsEnabled,
		ProcessOnlyOnACPower:    s.processOnlyOnACPower,
		DefaultSttModel:         s.defaultSttModel,
		RecordingSource:         s.recordingSource,
		HomeworkRecipient:       maskAddress(s.homeworkRecipient),
		SMTPHost:                s.smtpHost,
		SMTPPort:                s.smtpPort,
		SMTPSecurity:            s.smtpSecurity,
		SMTPLogin:               maskAddress(s.smtpLogin),
		MailConfigured:          s.MailConfigured(),
		HubURL:                  hubOrigin(s.hubURL),
	}
}

// maskAddress keeps the domain of an address: "***@example.org"; any other non-empty value is "***".
func maskAddress(v string) string {
	if v == "" {
		return ""
	}
	if isAddress(v) {
		_, domain, _ := strings.Cut(v, "@")
		return masked + "@" + domain
	}
	return masked
}

// hubOrigin is the scheme, host and port of a stored Hub URL (the path may name a tenant); "***" if it does not parse.
func hubOrigin(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return masked
	}
	return u.Scheme + "://" + u.Host
}
