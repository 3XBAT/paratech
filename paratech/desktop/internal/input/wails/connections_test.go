package wails

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"paratech/desktop/internal/cases/connections"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/settings/settings"
)

// fakeConnections is the use case behind the binding.
type fakeConnections struct {
	view      connections.View
	err       error // of Get, SaveMail and SaveHubURL
	status    connections.SecretStatus
	secretErr error
	checkErr  error

	reading  values.QuotaReading
	quotaErr error

	mail      connections.MailInput
	hubURL    string
	passwords []string
	keys      []string
}

func (f *fakeConnections) Get(context.Context) (connections.View, error) { return f.view, f.err }

func (f *fakeConnections) SaveMail(_ context.Context, in connections.MailInput) (connections.View, error) {
	f.mail = in
	return f.view, f.err
}

func (f *fakeConnections) SaveHubURL(_ context.Context, url string) (connections.View, error) {
	f.hubURL = url
	return f.view, f.err
}

func (f *fakeConnections) SetMailPassword(_ context.Context, p string) (connections.SecretStatus, error) {
	f.passwords = append(f.passwords, p)
	return f.status, f.secretErr
}

func (f *fakeConnections) RemoveMailPassword(context.Context) (connections.SecretStatus, error) {
	return f.status, f.secretErr
}

func (f *fakeConnections) CheckMail(context.Context) error { return f.checkErr }

func (f *fakeConnections) SetHubKey(_ context.Context, key string) (connections.SecretStatus, error) {
	f.keys = append(f.keys, key)
	return f.status, f.secretErr
}

func (f *fakeConnections) RemoveHubKey(context.Context) (connections.SecretStatus, error) {
	return f.status, f.secretErr
}

func (f *fakeConnections) RefreshQuota(context.Context) (values.QuotaReading, error) {
	return f.reading, f.quotaErr
}

func TestConnectionsJSONNames(t *testing.T) {
	c := connectionsJSON(connections.View{
		HubURL: "https://hub.example.org", HomeworkRecipient: "t@school.example", SMTPHost: "smtp.example.org", SMTPPort: 465,
		SMTPSecurity: settings.MailSecurityTLS, SMTPLogin: "s@example.org", MailConfigured: true, MailPassword: connections.SecretSet,
		HubKey: connections.SecretUnset,
	})
	raw, err := json.Marshal(SaveResult{Saved: true, Connections: &c, Errors: []FieldError{{Field: "smtpPort", Message: "m"}}})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"saved":true,"connections":{"hubUrl":"https://hub.example.org","hubKey":"unset",` +
		`"quota":{"status":"unchecked","stale":false,"checkedAt":"","values":null},"recipient":"t@school.example",` +
		`"smtpHost":"smtp.example.org","smtpPort":465,"smtpSecurity":"tls","smtpLogin":"s@example.org",` +
		`"mailConfigured":true,"mailPassword":"set"},"errors":[{"field":"smtpPort","message":"m"}]}`
	if string(raw) != want {
		t.Fatalf("JSON = %s\nwant   %s", raw, want)
	}

	for _, tt := range []struct {
		v    any
		want string
	}{
		{MailSettingsInput{Recipient: "r", SMTPHost: "h", SMTPPort: 1, SMTPSecurity: "starttls", SMTPLogin: "l"},
			`{"recipient":"r","smtpHost":"h","smtpPort":1,"smtpSecurity":"starttls","smtpLogin":"l"}`},
		{SecretResult{Status: "unset", Message: "m"}, `{"status":"unset","message":"m"}`},
		{CheckResult{OK: true, Message: "m"}, `{"ok":true,"message":"m"}`},
		{QuotaResult{OK: true, Message: "m", Quota: Quota{Status: "current", CheckedAt: "t", Values: &QuotaValues{
			Unit: "tokens", Remaining: 1, Reserved: 2, Limit: 3, PeriodEnd: "p", FetchedAt: "f",
		}}}, `{"ok":true,"message":"m","quota":{"status":"current","stale":false,"checkedAt":"t","values":{"unit":"tokens",` +
			`"remaining":1,"reserved":2,"limit":3,"periodEnd":"p","fetchedAt":"f"}}}`},
	} {
		raw, err := json.Marshal(tt.v)
		if err != nil || string(raw) != tt.want {
			t.Errorf("JSON = %s (%v), want %s", raw, err, tt.want)
		}
	}
}

func TestSecretStatusNames(t *testing.T) {
	for status, want := range map[connections.SecretStatus]string{
		connections.SecretSet: "set", connections.SecretUnset: "unset", connections.SecretUnsupported: "unsupported",
		connections.SecretUnavailable: "unavailable", connections.SecretStatus(99): "unavailable",
	} {
		if got := secretStatus(status); got != want {
			t.Errorf("secretStatus(%d) = %q, want %q", status, got, want)
		}
	}
}

func TestSaveMailSettingsPassesTheInput(t *testing.T) {
	f := &fakeConnections{view: connections.View{SMTPHost: "smtp.example.org", SMTPSecurity: settings.MailSecurityTLS}}
	s := NewConnectionsService(f, discard())
	res, err := s.SaveMailSettings(context.Background(), MailSettingsInput{
		Recipient: "t@school.example", SMTPHost: "smtp.example.org", SMTPPort: 465, SMTPSecurity: "tls", SMTPLogin: "s@example.org",
	})
	if err != nil || !res.Saved || res.Connections == nil || res.Connections.SMTPHost != "smtp.example.org" || len(res.Errors) != 0 {
		t.Fatalf("SaveMailSettings = %+v, %v", res, err)
	}
	want := connections.MailInput{
		Recipient: "t@school.example", Host: "smtp.example.org", Port: 465, Security: settings.MailSecurityTLS, Login: "s@example.org",
	}
	if f.mail != want {
		t.Fatalf("use case got %+v, want %+v", f.mail, want)
	}

	if res, err := s.SaveHubURL(context.Background(), "https://hub.example.org"); err != nil || !res.Saved || f.hubURL != "https://hub.example.org" {
		t.Fatalf("SaveHubURL = %+v, %v (use case got %q)", res, err, f.hubURL)
	}
}

// Refused fields are a normal answer with a fixed text per field, never the value.
func TestSaveReportsFieldErrors(t *testing.T) {
	all := []connections.Field{
		connections.FieldRecipient, connections.FieldSMTPHost, connections.FieldSMTPPort, connections.FieldSMTPSecurity,
		connections.FieldSMTPLogin, connections.FieldHubURL,
	}
	f := &fakeConnections{err: fmt.Errorf("connections: save mail: %w", &connections.InvalidFieldsError{Fields: all})}
	s := NewConnectionsService(f, discard())
	res, err := s.SaveMailSettings(context.Background(), MailSettingsInput{SMTPHost: "VALUEMARKER"})
	if err != nil || res.Saved || res.Connections != nil || len(res.Errors) != len(all) {
		t.Fatalf("SaveMailSettings = %+v, %v", res, err)
	}
	seen := map[string]bool{}
	for i, e := range res.Errors {
		if e.Field != string(all[i]) || e.Message == "" || e.Message == msgFieldInvalid || seen[e.Message] {
			t.Errorf("error %d = %+v: want field %q with its own text", i, e, all[i])
		}
		seen[e.Message] = true
	}
	raw, _ := json.Marshal(res)
	if strings.Contains(string(raw), "VALUEMARKER") {
		t.Fatal("the answer quotes the value")
	}

	f.err = &connections.InvalidFieldsError{Fields: []connections.Field{"other"}}
	res, err = s.SaveHubURL(context.Background(), "x")
	if err != nil || len(res.Errors) != 1 || res.Errors[0].Message != msgFieldInvalid {
		t.Fatalf("unknown field: %+v, %v", res, err)
	}
}

func TestPasswordResults(t *testing.T) {
	tests := []struct {
		name   string
		status connections.SecretStatus
		err    error
		want   SecretResult
	}{
		{"saved", connections.SecretSet, nil, SecretResult{Status: "set", Message: msgMailStored}},
		{"invalid", connections.SecretUnset, fmt.Errorf("op: %w", connections.ErrInvalidSecret),
			SecretResult{Status: "unset", Message: msgMailRefused}},
		{"unsupported", connections.SecretUnsupported, fmt.Errorf("op: %w", connections.ErrSecretsUnsupported),
			SecretResult{Status: "unsupported", Message: msgStoreMissing}},
		{"unavailable", connections.SecretUnavailable, fmt.Errorf("op: %w: locked", connections.ErrSecretsUnavailable),
			SecretResult{Status: "unavailable", Message: msgStoreDown}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewConnectionsService(&fakeConnections{status: tt.status, secretErr: tt.err}, discard())
			got, err := s.SetMailPassword(context.Background(), "pw")
			if err != nil || got != tt.want {
				t.Fatalf("SetMailPassword = %+v, %v; want %+v", got, err, tt.want)
			}
		})
	}

	s := NewConnectionsService(&fakeConnections{status: connections.SecretUnset}, discard())
	if got, err := s.RemoveMailPassword(context.Background()); err != nil || got != (SecretResult{Status: "unset", Message: msgMailRemoved}) {
		t.Fatalf("RemoveMailPassword = %+v, %v", got, err)
	}
}

func TestCheckMailMessages(t *testing.T) {
	s := NewConnectionsService(&fakeConnections{}, discard())
	if got, err := s.CheckMail(context.Background()); err != nil || got != (CheckResult{OK: true, Message: msgCheckOK}) {
		t.Fatalf("CheckMail ok = %+v, %v", got, err)
	}

	results := []error{
		connections.ErrCheckBusy, connections.ErrMailNotConfigured, connections.ErrMailPasswordMissing,
		connections.ErrSecretsUnsupported, connections.ErrSecretsUnavailable, connections.ErrMailUnreachable,
		connections.ErrMailCertificate, connections.ErrMailTLSOrAuthUnsupported, connections.ErrMailLoginRejected,
		connections.ErrMailTemporary, connections.ErrMailRefused,
	}
	seen := map[string]bool{msgCheckOK: true}
	for _, r := range results {
		s := NewConnectionsService(&fakeConnections{checkErr: fmt.Errorf("bridge: %w", r)}, discard())
		got, err := s.CheckMail(context.Background())
		if err != nil || got.OK || got.Message == "" || seen[got.Message] {
			t.Errorf("%v: CheckMail = %+v, %v; want its own text", r, got, err)
		}
		seen[got.Message] = true
	}
}

// Unexpected failures reach the page as one fixed error; the cause is logged, the password never.
func TestConnectionsHideErrorDetails(t *testing.T) {
	const password = "binding-test-password-4e1d"
	var logs bytes.Buffer
	cause := errors.New(`open C:\Users\student\db: locked`)
	f := &fakeConnections{err: cause, secretErr: cause, checkErr: cause}
	s := NewConnectionsService(f, slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))

	ctx := context.Background()
	calls := map[string]func() (any, error){
		"GetConnections":   func() (any, error) { return s.GetConnections(ctx) },
		"SaveMailSettings": func() (any, error) { return s.SaveMailSettings(ctx, MailSettingsInput{}) },
		"SaveHubURL":       func() (any, error) { return s.SaveHubURL(ctx, "") },
		"SetMailPassword":  func() (any, error) { return s.SetMailPassword(ctx, password) },
		"RemoveMailPassword": func() (any, error) {
			return s.RemoveMailPassword(ctx)
		},
		"CheckMail":    func() (any, error) { return s.CheckMail(ctx) },
		"SetHubKey":    func() (any, error) { return s.SetHubKey(ctx, password) },
		"RemoveHubKey": func() (any, error) { return s.RemoveHubKey(ctx) },
	}
	for name, call := range calls {
		res, err := call()
		if !errors.Is(err, ErrConnectionsUnavailable) || strings.Contains(err.Error(), "student") {
			t.Errorf("%s: err = %v, want only ErrConnectionsUnavailable", name, err)
		}
		if raw, _ := json.Marshal(res); strings.Contains(string(raw), "student") {
			t.Errorf("%s: answer %s carries the cause", name, raw)
		}
	}
	if !strings.Contains(logs.String(), "locked") {
		t.Errorf("the cause must be logged, got %q", logs.String())
	}
	if strings.Contains(logs.String(), password) {
		t.Fatal("the log contains the password")
	}
	if len(f.passwords) != 1 || f.passwords[0] != password {
		t.Fatal("the password did not reach the use case unchanged")
	}
}
