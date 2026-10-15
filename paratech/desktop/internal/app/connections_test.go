package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"paratech/desktop/internal/adapters/smtp"
	"paratech/desktop/internal/adapters/wincred"
	"paratech/desktop/internal/cases/connections"
	"paratech/desktop/internal/input/wails"
	"paratech/desktop/testenv"
)

type checkFunc func(ctx context.Context) error

func (f checkFunc) Check(ctx context.Context) error { return f(ctx) }

func TestMailCheckMapsTheSessionClasses(t *testing.T) {
	unsupported := fmt.Errorf("%w: %w", smtp.ErrStoreUnavailable, errors.ErrUnsupported)
	tests := []struct {
		from, want error
	}{
		{smtp.ErrNotConfigured, connections.ErrMailNotConfigured},
		{smtp.ErrAccountInvalid, connections.ErrMailNotConfigured},
		{smtp.ErrNoPassword, connections.ErrMailPasswordMissing},
		{unsupported, connections.ErrSecretsUnsupported},
		{smtp.ErrStoreUnavailable, connections.ErrSecretsUnavailable},
		{smtp.ErrUnreachable, connections.ErrMailUnreachable},
		{smtp.ErrCertificate, connections.ErrMailCertificate},
		{smtp.ErrSetupUnsupported, connections.ErrMailTLSOrAuthUnsupported},
		{smtp.ErrLoginRejected, connections.ErrMailLoginRejected},
		{smtp.ErrServerTemporary, connections.ErrMailTemporary},
		{smtp.ErrServerRefused, connections.ErrMailRefused},
	}
	for _, tt := range tests {
		t.Run(tt.from.Error(), func(t *testing.T) {
			from := fmt.Errorf("smtp: fixed text: %w", tt.from)
			err := mailCheck{checkFunc(func(context.Context) error { return from })}.CheckMail(context.Background())
			if !errors.Is(err, tt.want) || !errors.Is(err, tt.from) {
				t.Fatalf("CheckMail = %v, want %v", err, tt.want)
			}
		})
	}

	if err := (mailCheck{checkFunc(func(context.Context) error { return nil })}).CheckMail(context.Background()); err != nil {
		t.Fatalf("CheckMail ok = %v", err)
	}
	other := fmt.Errorf("smtp: mail settings cannot be read: %w", smtp.ErrSettingsUnavailable)
	if err := (mailCheck{checkFunc(func(context.Context) error { return other })}).CheckMail(context.Background()); err != other {
		t.Fatalf("CheckMail = %v, want the cause unchanged", err)
	}
}

// The page writes the password where the smtp adapter reads it.
func TestConnectionsPasswordEntryIsTheAdapters(t *testing.T) {
	if connections.DefaultMailSecretName != smtp.DefaultCredentialName {
		t.Fatalf("connections writes %q, smtp reads %q", connections.DefaultMailSecretName, smtp.DefaultCredentialName)
	}
}

// Test step 3 of TASK-062 (AC-19, hard rule 4), through the bindings of the page: the password goes into the mail
// credential store under smtp-password, the check logs in with it over TLS and sends nothing, and afterwards the
// password is in no answer of the page, not in the log, the database files or any row.
func TestConnectionsPageKeepsThePasswordSecret(t *testing.T) {
	ctx := context.Background()
	const password = "conn-ac19-marker-3c9e5a1f"
	mail := testenv.NewFakeSMTP(t, testenv.SMTPBehavior{User: testMailUser, Password: password})

	dir := t.TempDir()
	a, f := newTestAppWith(t, dir)
	a.env.mailRoots = mail.RootCAs()
	var logs syncBuffer
	a.log = slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	page := wails.NewConnectionsService(startCore(t, a).Connections, a.log)

	var answers []any
	answer := func(res any, err error) any {
		t.Helper()
		answers = append(answers, res)
		if err != nil {
			answers = append(answers, err.Error())
			t.Fatalf("binding error: %v", err)
		}
		return res
	}

	if got := answer(page.SetMailPassword(ctx, password)).(wails.SecretResult); got.Status != "set" {
		t.Fatalf("SetMailPassword = %+v", got)
	}
	mailStore, hubStore := f.secrets[mailSecretsService], f.secrets[hubSecretsService]
	if stored, err := mailStore.Get(ctx, "smtp-password"); err != nil || stored != password || mailStore.Len() != 1 || hubStore.Len() != 0 {
		t.Fatalf("the mail store holds %d secrets (%v), the hub store %d", mailStore.Len(), err, hubStore.Len())
	}

	saved := answer(page.SaveMailSettings(ctx, wails.MailSettingsInput{
		Recipient: "teacher@school.example", SMTPHost: mail.Host(), SMTPPort: mail.Port(), SMTPSecurity: "starttls",
		SMTPLogin: testMailUser,
	})).(wails.SaveResult)
	if !saved.Saved || !saved.Connections.MailConfigured || saved.Connections.MailPassword != "set" {
		t.Fatalf("SaveMailSettings = %+v", saved)
	}
	if got := answer(page.GetConnections(ctx)).(wails.Connections); got.MailPassword != "set" || got.SMTPLogin != testMailUser {
		t.Fatalf("GetConnections = %+v", got)
	}

	if got := answer(page.CheckMail(ctx)).(wails.CheckResult); !got.OK {
		t.Fatalf("CheckMail = %+v", got)
	}
	logins := mail.Logins()
	if len(logins) != 1 || logins[0].Password != password || !logins[0].TLS {
		t.Fatalf("logins %d: the stored password was not used over TLS", len(logins))
	}
	if cmds := mail.Commands(); len(mail.Messages()) != 0 || slices.Contains(cmds, "MAIL") || slices.Contains(cmds, "DATA") ||
		cmds[len(cmds)-1] != "QUIT" {
		t.Fatalf("the check sent a message or did not quit: %v", cmds)
	}

	// A refused login and invalid input, including the password typed into the wrong fields.
	mail.SetBehavior(testenv.SMTPBehavior{User: testMailUser, Password: "other-" + password})
	if got := answer(page.CheckMail(ctx)).(wails.CheckResult); got.OK || !strings.Contains(got.Message, "отклонил логин") {
		t.Fatalf("CheckMail with a rejected login = %+v", got)
	}
	invalid := answer(page.SaveMailSettings(ctx, wails.MailSettingsInput{
		Recipient: password, SMTPHost: password + "/", SMTPPort: -1, SMTPSecurity: password, SMTPLogin: password + "\x00",
	})).(wails.SaveResult)
	if invalid.Saved || len(invalid.Errors) != 5 {
		t.Fatalf("SaveMailSettings with invalid values = %+v", invalid)
	}
	if hub := answer(page.SaveHubURL(ctx, "https://u:"+password+"@hub.example.org")).(wails.SaveResult); hub.Saved {
		t.Fatalf("SaveHubURL with credentials = %+v", hub)
	}
	if got := answer(page.SetMailPassword(ctx, "")).(wails.SecretResult); got.Status != "set" || got.Message == "" {
		t.Fatalf("SetMailPassword(empty) = %+v", got)
	}

	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	for _, res := range answers {
		raw, err := json.Marshal(res)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), password) {
			t.Fatalf("a page answer contains the password: %s", raw)
		}
	}
	// Every migration version and the saved settings row.
	dbPath := filepath.Join(dir, dbFile)
	assertNoSecretsIn(t, dbPath, logs.String(), settingsScanFloor(t, dbPath, "teacher@school.example"), password)
}

// settingsScanFloor checks that the settings row holds the saved recipient and returns the rows a full scan must see:
// the migration versions plus that row.
func settingsScanFloor(t *testing.T, dbPath, recipient string) int {
	t.Helper()
	raw, err := sql.Open("sqlite", "file:"+filepath.ToSlash(dbPath)+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = raw.Close() }()
	var versions, settings int
	if err := raw.QueryRow(`SELECT count(*) FROM goose_db_version`).Scan(&versions); err != nil || versions == 0 {
		t.Fatalf("migration versions: %d, %v", versions, err)
	}
	if err := raw.QueryRow(`SELECT count(*) FROM settings WHERE homework_recipient = ?`, recipient).Scan(&settings); err != nil ||
		settings != 1 {
		t.Fatalf("settings rows with the saved recipient: %d, %v", settings, err)
	}
	return versions + 1
}

// Off Windows the credential store refuses every call (wincred.ErrUnsupported): the page says so instead of failing,
// and the rest of the section still saves.
func TestConnectionsPageWithoutCredentialStore(t *testing.T) {
	ctx := context.Background()
	mail := testenv.NewFakeSMTP(t, testenv.SMTPBehavior{User: testMailUser, Password: testMailPassword})
	a, f := newTestAppWith(t, t.TempDir())
	a.env.mailRoots = mail.RootCAs()
	f.secrets[mailSecretsService].Fail(wincred.ErrUnsupported)
	page := wails.NewConnectionsService(startCore(t, a).Connections, a.log)

	got, err := page.GetConnections(ctx)
	if err != nil || got.MailPassword != "unsupported" {
		t.Fatalf("GetConnections = %+v, %v", got, err)
	}
	secret, err := page.SetMailPassword(ctx, testMailPassword)
	if err != nil || secret.Status != "unsupported" || !strings.Contains(secret.Message, "недоступен в этой системе") {
		t.Fatalf("SetMailPassword = %+v, %v", secret, err)
	}
	saved, err := page.SaveMailSettings(ctx, wails.MailSettingsInput{
		Recipient: "teacher@school.example", SMTPHost: mail.Host(), SMTPPort: mail.Port(), SMTPSecurity: "starttls",
		SMTPLogin: testMailUser,
	})
	if err != nil || !saved.Saved || saved.Connections.MailPassword != "unsupported" {
		t.Fatalf("SaveMailSettings = %+v, %v", saved, err)
	}
	check, err := page.CheckMail(ctx)
	if err != nil || check.OK || !strings.Contains(check.Message, "недоступен в этой системе") {
		t.Fatalf("CheckMail = %+v, %v", check, err)
	}
	if mail.Sessions() != 0 {
		t.Fatal("the check contacted the server without a password")
	}
}
