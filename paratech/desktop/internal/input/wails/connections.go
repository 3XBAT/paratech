package wails

import (
	"context"
	"errors"
	"log/slog"

	"paratech/desktop/internal/cases/connections"
	"paratech/desktop/internal/domain/settings/settings"
)

// ErrConnectionsUnavailable is what the page sees when the section cannot be read or saved for a reason it cannot fix.
// The cause is logged, never the password.
var ErrConnectionsUnavailable = errors.New("connections settings are unavailable")

// Secret statuses as the page reads them.
const (
	secretSet         = "set"
	secretUnset       = "unset"
	secretUnsupported = "unsupported"
	secretUnavailable = "unavailable"
)

// Operation names of the log.
const (
	opGetConnections = "ui: get connections"
	opSaveMail       = "ui: save mail settings"
	opSaveHubURL     = "ui: save hub url"
	opMailStore      = "ui: set mail password"
	opMailRemove     = "ui: remove mail password"
	opCheckMail      = "ui: check mail"
)

// Fixed texts of the page (Russian UI).
const (
	msgFieldInvalid = "Недопустимое значение."
	msgMailStored   = "Пароль сохранён в Диспетчере учётных данных Windows."
	msgMailRemoved  = "Пароль удалён."
	msgMailRefused  = "Пароль не может быть пустым или длиннее 2560 байт."
	msgStoreMissing = "Диспетчер учётных данных Windows недоступен в этой системе: пароль хранить негде."
	msgStoreDown    = "Диспетчер учётных данных Windows не ответил. Попробуйте ещё раз."
	msgCheckOK      = "Соединение и вход прошли успешно. Письмо не отправлялось."
)

// fieldMessages explain the rule of each field of the settings aggregate.
var fieldMessages = map[connections.Field]string{
	connections.FieldHubURL:       "Адрес Hub: https://… (http только для localhost), без логина, запроса и #.",
	connections.FieldRecipient:    "Адрес получателя: обычный адрес вида name@example.com.",
	connections.FieldSMTPHost:     "Сервер: имя хоста без схемы и порта, например smtp.example.com.",
	connections.FieldSMTPPort:     "Порт: 1–65535 или 0 (по умолчанию для режима).",
	connections.FieldSMTPSecurity: "Режим TLS: STARTTLS или TLS.",
	connections.FieldSMTPLogin:    "Логин: 1–254 символа без управляющих; для отправки нужен адрес почты.",
}

// checkMessages are the answers to "Проверить почту" other than success, in the order they are matched.
var checkMessages = []struct {
	err error
	msg string
}{
	{connections.ErrCheckBusy, "Проверка уже идёт."},
	{connections.ErrMailNotConfigured, "Почта не настроена: укажите сервер и логин-адрес почты и сохраните."},
	{connections.ErrMailPasswordMissing, "Пароль почты не задан."},
	{connections.ErrSecretsUnsupported, "Диспетчер учётных данных Windows недоступен в этой системе: проверить почту нельзя."},
	{connections.ErrSecretsUnavailable, "Не удалось прочитать пароль из Диспетчера учётных данных Windows."},
	{connections.ErrMailUnreachable, "Сервер не отвечает: проверьте адрес, порт, режим TLS и подключение к интернету."},
	{connections.ErrMailCertificate, "Сертификат сервера не прошёл проверку: соединение небезопасно."},
	{connections.ErrMailTLSOrAuthUnsupported, "Сервер не поддерживает обязательный TLS или подходящий способ входа: проверьте порт и режим TLS."},
	{connections.ErrMailLoginRejected, "Сервер отклонил логин или пароль. Некоторым почтовым службам нужен пароль приложения."},
	{connections.ErrMailTemporary, "Сервер временно недоступен. Попробуйте позже."},
	{connections.ErrMailRefused, "Сервер отказал в соединении: проверьте адрес, порт и режим TLS."},
}

// ConnectionsUseCase is the use case of the "Connections" section (ARCH-42).
type ConnectionsUseCase interface {
	Get(ctx context.Context) (connections.View, error)
	SaveMail(ctx context.Context, in connections.MailInput) (connections.View, error)
	SaveHubURL(ctx context.Context, url string) (connections.View, error)
	SetMailPassword(ctx context.Context, password string) (connections.SecretStatus, error)
	RemoveMailPassword(ctx context.Context) (connections.SecretStatus, error)
	CheckMail(ctx context.Context) error
}

// Connections is the JSON form of the section. It never holds a secret: MailPassword is a status.
type Connections struct {
	HubURL         string `json:"hubUrl"`
	Recipient      string `json:"recipient"`
	SMTPHost       string `json:"smtpHost"`
	SMTPPort       int    `json:"smtpPort"`     // 0: the default of the mode
	SMTPSecurity   string `json:"smtpSecurity"` // "starttls" or "tls"
	SMTPLogin      string `json:"smtpLogin"`
	MailConfigured bool   `json:"mailConfigured"`
	MailPassword   string `json:"mailPassword"` // "set", "unset", "unsupported" or "unavailable"
}

// MailSettingsInput is the mail account the page submits; empty strings remove a value.
type MailSettingsInput struct {
	Recipient    string `json:"recipient"`
	SMTPHost     string `json:"smtpHost"`
	SMTPPort     int    `json:"smtpPort"`
	SMTPSecurity string `json:"smtpSecurity"`
	SMTPLogin    string `json:"smtpLogin"`
}

// FieldError is a refused field with the rule it breaks.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// SaveResult answers a save. Saved false: nothing was stored, Errors lists every refused field and Connections is null.
type SaveResult struct {
	Saved       bool         `json:"saved"`
	Connections *Connections `json:"connections"`
	Errors      []FieldError `json:"errors"`
}

// SecretResult answers a change of the password: the status afterwards and a fixed text for the page.
type SecretResult struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

// CheckResult answers "Проверить почту".
type CheckResult struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

// ConnectionsService is bound to the UI: every exported method is callable from the page by its full name
// "<package path>.ConnectionsService.<Method>". Keep the method set to what the page may call.
type ConnectionsService struct {
	uc  ConnectionsUseCase
	log *slog.Logger
}

// NewConnectionsService returns the binding of the "Connections" section.
func NewConnectionsService(uc ConnectionsUseCase, log *slog.Logger) *ConnectionsService {
	return &ConnectionsService{uc: uc, log: log}
}

// GetConnections returns the section.
func (s *ConnectionsService) GetConnections(ctx context.Context) (Connections, error) {
	v, err := s.uc.Get(ctx)
	if err != nil {
		s.log.Error(opGetConnections, "err", err)
		return Connections{}, ErrConnectionsUnavailable
	}
	return connectionsJSON(v), nil
}

// SaveMailSettings stores the mail account, or lists every refused field.
func (s *ConnectionsService) SaveMailSettings(ctx context.Context, in MailSettingsInput) (SaveResult, error) {
	v, err := s.uc.SaveMail(ctx, connections.MailInput{
		Recipient: in.Recipient, Host: in.SMTPHost, Port: in.SMTPPort, Security: settings.MailSecurity(in.SMTPSecurity),
		Login: in.SMTPLogin,
	})
	return s.saved(opSaveMail, v, err)
}

// SaveHubURL stores the Hub address; empty removes it.
func (s *ConnectionsService) SaveHubURL(ctx context.Context, url string) (SaveResult, error) {
	v, err := s.uc.SaveHubURL(ctx, url)
	return s.saved(opSaveHubURL, v, err)
}

// SetMailPassword stores or replaces the mail password in Credential Manager. It is never returned or logged.
func (s *ConnectionsService) SetMailPassword(ctx context.Context, password string) (SecretResult, error) {
	status, err := s.uc.SetMailPassword(ctx, password)
	return s.secret(opMailStore, status, err, msgMailStored)
}

// RemoveMailPassword deletes the stored mail password.
func (s *ConnectionsService) RemoveMailPassword(ctx context.Context) (SecretResult, error) {
	status, err := s.uc.RemoveMailPassword(ctx)
	return s.secret(opMailRemove, status, err, msgMailRemoved)
}

// CheckMail connects and logs in to the mail server without sending a message.
func (s *ConnectionsService) CheckMail(ctx context.Context) (CheckResult, error) {
	err := s.uc.CheckMail(ctx)
	if err == nil {
		return CheckResult{OK: true, Message: msgCheckOK}, nil
	}
	for _, m := range checkMessages {
		if errors.Is(err, m.err) {
			s.log.Info(opCheckMail, "result", m.err)
			return CheckResult{Message: m.msg}, nil
		}
	}
	s.log.Error(opCheckMail, "err", err)
	return CheckResult{}, ErrConnectionsUnavailable
}

func (s *ConnectionsService) saved(op string, v connections.View, err error) (SaveResult, error) {
	var fields *connections.InvalidFieldsError
	switch {
	case errors.As(err, &fields):
		out := SaveResult{Errors: make([]FieldError, 0, len(fields.Fields))}
		for _, f := range fields.Fields {
			msg, ok := fieldMessages[f]
			if !ok {
				msg = msgFieldInvalid
			}
			out.Errors = append(out.Errors, FieldError{Field: string(f), Message: msg})
		}
		return out, nil
	case err != nil:
		s.log.Error(op, "err", err)
		return SaveResult{}, ErrConnectionsUnavailable
	}
	c := connectionsJSON(v)
	return SaveResult{Saved: true, Connections: &c, Errors: []FieldError{}}, nil
}

func (s *ConnectionsService) secret(op string, status connections.SecretStatus, err error, done string) (SecretResult, error) {
	out := SecretResult{Status: secretStatus(status)}
	switch {
	case err == nil:
		out.Message = done
	case errors.Is(err, connections.ErrInvalidSecret):
		out.Message = msgMailRefused
	case errors.Is(err, connections.ErrSecretsUnsupported):
		out.Message = msgStoreMissing
	case errors.Is(err, connections.ErrSecretsUnavailable):
		s.log.Warn(op, "err", err)
		out.Message = msgStoreDown
	default:
		s.log.Error(op, "err", err)
		return SecretResult{}, ErrConnectionsUnavailable
	}
	return out, nil
}

func connectionsJSON(v connections.View) Connections {
	return Connections{
		HubURL: v.HubURL, Recipient: v.HomeworkRecipient, SMTPHost: v.SMTPHost, SMTPPort: v.SMTPPort,
		SMTPSecurity: string(v.SMTPSecurity), SMTPLogin: v.SMTPLogin, MailConfigured: v.MailConfigured,
		MailPassword: secretStatus(v.MailPassword),
	}
}

func secretStatus(s connections.SecretStatus) string {
	switch s {
	case connections.SecretSet:
		return secretSet
	case connections.SecretUnset:
		return secretUnset
	case connections.SecretUnsupported:
		return secretUnsupported
	}
	return secretUnavailable
}
