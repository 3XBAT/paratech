package app

// Bridges from the settings aggregate to the adapters and use cases that read the mail and Hub setup at call time, so
// a change applies without a restart. Mapping only: what "configured" means is decided by settings.Settings.

import (
	"context"
	"fmt"

	"paratech/desktop/internal/adapters/smtp"
	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/settings/settings"
)

// settingsLoader reads the settings singleton (sqlite.SettingsRepository).
type settingsLoader interface {
	Load(ctx context.Context) (*settings.Settings, error)
}

// hubSettings is the hubclient.URLSource of the settings.
type hubSettings struct{ settings settingsLoader }

func (h hubSettings) HubURL(ctx context.Context) (string, error) {
	s, err := h.settings.Load(ctx)
	if err != nil {
		return "", err
	}
	return s.HubURL(), nil
}

// mailSettings is the smtp.AccountSource of the settings: the zero account while no SMTP account is configured. The
// recipient setting is not needed: a delivery sends to the recipient frozen at confirmation.
type mailSettings struct{ settings settingsLoader }

func (m mailSettings) MailAccount(ctx context.Context) (smtp.Account, error) {
	s, err := m.settings.Load(ctx)
	if err != nil || !s.SMTPAccountConfigured() {
		return smtp.Account{}, err
	}
	security, ok := map[settings.MailSecurity]smtp.Security{
		settings.MailSecurityStartTLS: smtp.SecurityStartTLS,
		settings.MailSecurityTLS:      smtp.SecurityImplicitTLS,
	}[s.SMTPSecurity()]
	if !ok {
		return smtp.Account{}, fmt.Errorf("app: unknown smtp security %q", s.SMTPSecurity())
	}
	return smtp.Account{
		Host: s.SMTPHost(), Port: s.SMTPPort(), Security: security, Username: s.SMTPLogin(), From: s.MailSender(),
	}, nil
}

// recipientSettings is the homeworkflow.RecipientSource of the settings, for the proposals service once that is wired
// (ADR-0009 "Not wired yet").
type recipientSettings struct{ settings settingsLoader }

func (r recipientSettings) HomeworkRecipient(ctx context.Context) (proposal.Recipient, error) {
	s, err := r.settings.Load(ctx)
	if err != nil || s.HomeworkRecipient() == "" {
		return proposal.Recipient{}, err
	}
	return proposal.NewRecipient(s.HomeworkRecipient())
}
