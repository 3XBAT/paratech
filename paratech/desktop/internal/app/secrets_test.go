package app

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"paratech/desktop/internal/adapters/hubclient"
	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/delivery/delivery"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/testenv"
)

// Obviously fake secrets; the test looks for them everywhere they must not be.
const (
	testHubKey       = "la_test_hubkey_5d1e8b2f7a"
	testMailUser     = "student@example.org"
	testMailPassword = "fake-smtp-secret-9b4c2a7d"
)

// Test step 3 of TASK-057 (AC-19, hard rule 4): with the core as the app builds it, an LLM job authenticates to a Hub
// with the API key and an email is sent with the SMTP password, both read from the credential stores. Afterwards
// neither secret is in the database files (UTF-8 or UTF-16), in any row, or in the log.
func TestSecretsReachNeitherTheDatabaseNorTheLog(t *testing.T) {
	ctx := context.Background()
	hub := testenv.NewFakeHub()
	hub.Answer(values.OperationHomeworkDraft, `{"title":"Rivers","body":"Deltas form where rivers reach the sea."}`)
	hubURL, hubHTTP := testenv.StartFakeHub(t, hub)
	mail := testenv.NewFakeSMTP(t, testenv.SMTPBehavior{User: testMailUser, Password: testMailPassword})

	dir := t.TempDir()
	a, f := newTestAppWith(t, dir)
	a.env.hubHTTP, a.env.mailRoots = hubHTTP, mail.RootCAs()
	var logs syncBuffer
	a.log = slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	if err := f.secrets[hubSecretsService].Set(ctx, hubclient.DefaultCredentialName, testHubKey); err != nil {
		t.Fatal(err)
	}

	startCore(t, a)
	insertAssignment(t, dir, "a1", "Rivers")
	// The password goes in as the settings page will put it: through the use case into the mail store.
	if err := a.core.deliveries.SetMailPassword(ctx, testMailPassword); err != nil {
		t.Fatal(err)
	}
	configureMailAndHub(t, a, hubURL, mail.Host(), mail.Port())

	// The LLM job: a draft generation through the Hub client.
	res, err := a.core.drafts.Generate(ctx, "a1", "")
	if err != nil {
		t.Fatal(err)
	}
	jobs := sqlite.NewJobRepository(a.core.db)
	testenv.Eventually(t, "the generation completes", func() bool {
		j, err := jobs.Get(ctx, res.JobID)
		return err == nil && j.Status() == job.StatusCompleted
	}, nil)
	if auth := hub.Authorizations(); len(auth) != 1 || auth[0] != "Bearer "+testHubKey {
		t.Fatalf("hub authorizations %d: the key from the store was not used", len(auth))
	}

	// The email: the exported draft, sent over TLS with the password from the store.
	if _, err := a.core.drafts.ExportDraft(ctx, "a1", 1); err != nil {
		t.Fatal(err)
	}
	if err := a.core.deliveries.SendHomework(ctx, confirmedEmail(t, a, "a1", "teacher@school.example")); err != nil {
		t.Fatal(err)
	}
	deliveries := sqlite.NewDeliveryRepository(a.core.db)
	testenv.Eventually(t, "the email is sent", func() bool {
		d, err := deliveries.GetByIdempotencyKey(ctx, "proposal:p-a1")
		return err == nil && d.Status() == delivery.StatusSent
	}, nil)
	logins := mail.Logins()
	if len(logins) != 1 || logins[0].User != testMailUser || logins[0].Password != testMailPassword || !logins[0].TLS {
		t.Fatalf("smtp logins %d: the password from the store was not used over TLS", len(logins))
	}
	if msgs := mail.Messages(); len(msgs) != 1 || !bytes.Contains(msgs[0], []byte("From: <"+testMailUser+">")) {
		t.Fatalf("messages %d: the sender is not the login from the settings", len(msgs))
	}

	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	assertNoSecrets(t, filepath.Join(dir, dbFile), logs.String(), testHubKey, testMailPassword)
}

// Before the user configures anything, the real adapters fail safely, as the stand-ins of TASK-056 did: an LLM job
// waits for the network without using attempts (its key stays usable), and an email is refused for sure (FAILED, not
// UNKNOWN), with nothing handed to a server.
func TestUnconfiguredHubAndMailFailSafely(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	a, _ := newTestAppWith(t, dir)
	startCore(t, a)
	insertAssignment(t, dir, "a1", "Rivers")

	res, err := a.core.drafts.Generate(ctx, "a1", "")
	if err != nil {
		t.Fatal(err)
	}
	jobs := sqlite.NewJobRepository(a.core.db)
	testenv.Eventually(t, "the generation waits for the network", func() bool {
		j, err := jobs.Get(ctx, res.JobID)
		return err == nil && j.Status() == job.StatusWaitingForNetwork && j.Attempts() == 0
	}, nil)

	if _, err := a.core.drafts.EditDraft(ctx, homeworkflow.DraftEdit{Assignment: "a1", Title: "Rivers", Body: "Deltas."}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.core.drafts.ExportDraft(ctx, "a1", 1); err != nil {
		t.Fatal(err)
	}
	if err := a.core.deliveries.SendHomework(ctx, confirmedEmail(t, a, "a1", "teacher@school.example")); err != nil {
		t.Fatal(err)
	}
	deliveries := sqlite.NewDeliveryRepository(a.core.db)
	var d *delivery.Delivery
	testenv.Eventually(t, "the email is refused", func() bool {
		d, err = deliveries.GetByIdempotencyKey(ctx, "proposal:p-a1")
		return err == nil && d.Status() == delivery.StatusFailed
	}, nil)
	if !strings.HasPrefix(d.LastError(), "smtp: mail is not configured") {
		t.Fatalf("last error %q, want the fixed not-configured text", d.LastError())
	}
}

// configureMailAndHub stores the non-secret setup as the settings page will.
func configureMailAndHub(t *testing.T, a *App, hubURL, smtpHost string, smtpPort int) {
	t.Helper()
	ctx := context.Background()
	repo := sqlite.NewSettingsRepository(a.core.db)
	s, err := repo.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	now := a.env.clock.Now()
	for _, err := range []error{
		s.SetHubURL(hubURL, now), s.SetSMTPHost(smtpHost, now), s.SetSMTPPort(smtpPort, now),
		s.SetSMTPLogin(testMailUser, now), s.SetHomeworkRecipient("teacher@school.example", now),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.Save(ctx, s); err != nil {
		t.Fatal(err)
	}
}

// assertNoSecrets scans every byte of the database files (with a write-ahead log or journal), every value of every
// row and the log for each secret, in UTF-8 and UTF-16.
func assertNoSecrets(t *testing.T, dbPath, log string, secrets ...string) {
	t.Helper()
	files, err := filepath.Glob(dbPath + "*")
	if err != nil || len(files) == 0 {
		t.Fatalf("database files: %v %v", files, err)
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range secrets {
			if bytes.Contains(data, []byte(s)) || bytes.Contains(data, utf16le(s)) {
				t.Fatalf("%s contains a secret", filepath.Base(f))
			}
		}
	}
	for _, s := range secrets {
		if strings.Contains(log, s) {
			t.Fatal("the log contains a secret")
		}
	}

	raw, err := sql.Open("sqlite", "file:"+filepath.ToSlash(dbPath)+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = raw.Close() }()
	var tables []string
	rows, err := raw.Query(`SELECT name FROM sqlite_master WHERE type = 'table'`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, n)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	scanned := 0
	for _, table := range tables {
		rows, err := raw.Query(`SELECT * FROM "` + table + `"`)
		if err != nil {
			t.Fatal(err)
		}
		cols, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				t.Fatal(err)
			}
			for i, v := range vals {
				text := fmt.Sprint(v)
				if b, ok := v.([]byte); ok {
					text = string(b)
				}
				for _, s := range secrets {
					if strings.Contains(text, s) {
						t.Fatalf("%s.%s contains a secret", table, cols[i])
					}
				}
			}
			scanned++
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
	}
	// At least the settings, the assignment, the draft and its version, the delivery and two jobs.
	if scanned < 7 {
		t.Fatalf("scanned only %d rows", scanned)
	}
}

func utf16le(s string) []byte {
	out := make([]byte, 0, 2*len(s))
	for _, r := range s {
		out = append(out, byte(r), byte(r>>8))
	}
	return out
}
