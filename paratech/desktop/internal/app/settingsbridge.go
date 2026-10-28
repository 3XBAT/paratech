package app

// Bridges from the settings aggregate to the adapters and use cases that read the mail and Hub setup at call time, so
// a change applies without a restart, and from the library's subject teachers to the recipient of a send. Mapping
// only: what "configured" means is decided by settings.Settings.

import (
	"context"
	"fmt"
	"strings"

	"paratech/desktop/internal/adapters/smtp"
	"paratech/desktop/internal/cases/library"
	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/settings/settings"
	"paratech/desktop/internal/domain/shared"
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

// subjectTeachers is the homeworkflow.RecipientSource of the proposals service (wireProposals, ADR-0030): the teachers
// of a library subject, of which a send freezes one at validation and checks it again before the send. The settings
// address is not read: it is never a recipient of these sends. Mapping only; the choice is proposal.ChooseRecipient.
type subjectTeachers struct{ lib *library.Service }

func (t subjectTeachers) Teachers(ctx context.Context, id shared.SubjectID) (proposal.Teachers, error) {
	if id == "" {
		return proposal.Teachers{}, nil
	}
	list, err := t.lib.Subjects(ctx)
	if err != nil {
		return proposal.Teachers{}, err
	}
	for _, s := range list {
		if s.ID() != id {
			continue
		}
		of := func(r subject.TeacherRole) proposal.Teacher {
			x := s.Teacher(r)
			return proposal.Teacher{Name: x.Name(), Email: x.Email()}
		}
		return proposal.Teachers{
			Subject: id, Lecture: of(subject.TeacherLecture), Practice: of(subject.TeacherPractice), Lab: of(subject.TeacherLab),
		}, nil
	}
	return proposal.Teachers{}, nil // removed since: no subject
}

func (t subjectTeachers) SubjectNamed(ctx context.Context, name string) (shared.SubjectID, error) {
	if strings.TrimSpace(name) == "" {
		return "", nil
	}
	m, err := t.lib.ResolveSubjects(ctx, []string{name})
	if err != nil {
		return "", err
	}
	return m[name].ID, nil
}
