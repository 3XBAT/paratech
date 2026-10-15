package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"paratech/desktop/internal/adapters/hubclient"
	"paratech/desktop/internal/adapters/wincred"
	"paratech/desktop/internal/cases/connections"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/input/wails"
	"paratech/desktop/testenv"
)

type remainingFunc func(ctx context.Context) (values.QuotaSnapshot, error)

func (f remainingFunc) GetRemaining(ctx context.Context) (values.QuotaSnapshot, error) { return f(ctx) }

func TestHubQuotaMapsTheClientClasses(t *testing.T) {
	// A missing URL or key also matches the network sentinel (it parks LLM jobs): it must still say "not configured".
	notConfigured := func(why error) error {
		return fmt.Errorf("hub: %w (%w, %w)", hubclient.ErrNotConfigured, why, shared.ErrNetworkUnavailable)
	}
	tests := []struct {
		name      string
		from      error
		want      error
		unchanged bool
	}{
		{"no key", notConfigured(hubclient.ErrMissingAPIKey), connections.ErrHubKeyMissing, false},
		{"no url", notConfigured(hubclient.ErrNoURL), connections.ErrHubNotConfigured, false},
		{"refused url", notConfigured(hubclient.ErrInvalidConfig), connections.ErrHubNotConfigured, false},
		{"rejected key", hubclient.ErrInvalidAPIKey, connections.ErrHubKeyRejected, false},
		{"malformed stored key", hubclient.ErrMalformedAPIKey, connections.ErrHubKeyRejected, false},
		{"store unsupported", fmt.Errorf("%w: %w", hubclient.ErrCredentials, wincred.ErrUnsupported), connections.ErrSecretsUnsupported, false},
		{"store failed", fmt.Errorf("%w: %w", hubclient.ErrCredentials, shared.ErrStorage), connections.ErrSecretsUnavailable, false},
		{"network", shared.ErrNetworkUnavailable, connections.ErrHubUnreachable, false},
		{"timeout", hubclient.ErrTimeout, connections.ErrHubUnreachable, false},
		{"unavailable", hubclient.ErrUnavailable, connections.ErrHubUnavailable, false},
		{"rate limited", hubclient.ErrRateLimited, connections.ErrHubUnavailable, false},
		{"busy", hubclient.ErrBusy, connections.ErrHubUnavailable, false},
		{"rejected request", hubclient.ErrRejected, connections.ErrHubRefused, false},
		{"bad response", hubclient.ErrBadResponse, connections.ErrHubRefused, false},
		{"settings unreadable", hubclient.ErrSettings, nil, true},
		{"cancelled", context.Canceled, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			from := fmt.Errorf("hubclient: get remaining: %w", tt.from)
			_, err := hubQuota{remainingFunc(func(context.Context) (values.QuotaSnapshot, error) { return values.QuotaSnapshot{}, from })}.
				Remaining(context.Background())
			if tt.unchanged {
				if err != from {
					t.Fatalf("Remaining = %v, want the cause unchanged", err)
				}
				return
			}
			if !errors.Is(err, tt.want) || !errors.Is(err, tt.from) {
				t.Fatalf("Remaining = %v, want %v", err, tt.want)
			}
		})
	}
}

// The page writes the key where the Hub client reads it, under the same rule the client sends it by.
func TestConnectionsHubKeyEntryIsTheClients(t *testing.T) {
	if connections.DefaultHubSecretName != hubclient.DefaultCredentialName {
		t.Fatalf("connections writes %q, hubclient reads %q", connections.DefaultHubSecretName, hubclient.DefaultCredentialName)
	}
	if values.MaxHubAPIKeyLen != wincred.MaxSecretBytes {
		t.Fatalf("a key may be %d bytes, the store keeps %d", values.MaxHubAPIKeyLen, wincred.MaxSecretBytes)
	}
}

// TASK-039 (AC-12, PRD section 12, hard rule 4), through the bindings of the page with a debug log: the key goes into
// the hub credential store under api-key, the quota is read from the Hub with it, and afterwards the key is in no
// answer of the page, not in the log, the database files or any row.
func TestConnectionsPageKeepsTheHubKeySecret(t *testing.T) {
	ctx := context.Background()
	const key = "la_test_ac12_marker_7e3b9d1c"
	hub := testenv.NewFakeHub()
	hub.SetRemaining(600, 400, 1000)
	hubURL, hubHTTP := testenv.StartFakeHub(t, hub)

	dir := t.TempDir()
	a, f := newTestAppWith(t, dir)
	a.env.hubHTTP = hubHTTP
	var logs syncBuffer
	a.log = slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	page := wails.NewConnectionsService(startCore(t, a).Connections, a.log)

	var answers []any
	answer := func(res any, err error) any {
		t.Helper()
		answers = append(answers, res)
		if err != nil {
			t.Fatalf("binding error: %v", err)
		}
		return res
	}
	refresh := func(status string) wails.QuotaResult {
		t.Helper()
		got := answer(page.RefreshQuota(ctx)).(wails.QuotaResult)
		if got.Quota.Status != status || got.Message == "" {
			t.Fatalf("RefreshQuota = %+v, want status %q", got, status)
		}
		return got
	}

	// Nothing configured: the page is pointed to the address, then to the key.
	if got := refresh("notConfigured"); got.OK || !strings.Contains(got.Message, "Адрес Hub") || got.Quota.Values != nil {
		t.Fatalf("RefreshQuota without an address = %+v", got)
	}
	if saved := answer(page.SaveHubURL(ctx, hubURL)).(wails.SaveResult); !saved.Saved || saved.Connections.HubKey != "unset" {
		t.Fatalf("SaveHubURL = %+v", saved)
	}
	refresh("noKey")

	if got := answer(page.SetHubKey(ctx, " "+key+"\r\n")).(wails.SecretResult); got.Status != "set" {
		t.Fatalf("SetHubKey = %+v", got)
	}
	hubStore, mailStore := f.secrets[hubSecretsService], f.secrets[mailSecretsService]
	if stored, err := hubStore.Get(ctx, "api-key"); err != nil || stored != key || hubStore.Len() != 1 || mailStore.Len() != 0 {
		t.Fatalf("the hub store holds %d secrets (%v), the mail store %d", hubStore.Len(), err, mailStore.Len())
	}

	got := refresh("current")
	if !got.OK || got.Quota.Stale || got.Quota.Values == nil || got.Quota.Values.Remaining != 600 ||
		got.Quota.Values.Reserved != 400 || got.Quota.Values.Limit != 1000 || got.Quota.Values.Unit != "tokens" ||
		got.Quota.Values.FetchedAt == "" || got.Quota.Values.PeriodEnd != "" {
		t.Fatalf("RefreshQuota = %+v (values %+v)", got, got.Quota.Values)
	}
	if auth := hub.QuotaAuthorizations(); auth[len(auth)-1] != "Bearer "+key {
		t.Fatal("the key from the store was not sent")
	}
	if c := answer(page.GetConnections(ctx)).(wails.Connections); c.HubKey != "set" || c.Quota.Status != "current" ||
		c.Quota.Values == nil || c.Quota.Values.Remaining != 600 {
		t.Fatalf("GetConnections = %+v", c)
	}

	// The Hub cannot answer: the last values stay, marked stale with their time.
	hub.QuotaDown(true)
	if got := refresh("offline"); got.OK || !got.Quota.Stale || got.Quota.Values == nil || got.Quota.Values.Remaining != 600 {
		t.Fatalf("RefreshQuota with the Hub down = %+v", got)
	}
	hub.QuotaDown(false)

	// A revoked key: the last values are not shown, the page asks for a valid key.
	hub.RejectKey(true)
	if got := refresh("keyRejected"); got.OK || got.Quota.Values != nil || !strings.Contains(got.Message, "отозван") {
		t.Fatalf("RefreshQuota with a revoked key = %+v", got)
	}

	// A refused value, the key typed into the address field, and the removal.
	if got := answer(page.SetHubKey(ctx, key+" x")).(wails.SecretResult); got.Status != "set" || got.Message == "" {
		t.Fatalf("SetHubKey(invalid) = %+v", got)
	}
	if saved := answer(page.SaveHubURL(ctx, "https://u:"+key+"@hub.example.org")).(wails.SaveResult); saved.Saved {
		t.Fatalf("SaveHubURL with the key = %+v", saved)
	}
	if got := answer(page.RemoveHubKey(ctx)).(wails.SecretResult); got.Status != "unset" || hubStore.Len() != 0 {
		t.Fatalf("RemoveHubKey = %+v (store %d)", got, hubStore.Len())
	}

	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	for _, res := range answers {
		raw, err := json.Marshal(res)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), key) {
			t.Fatalf("a page answer contains the key: %s", raw)
		}
	}
	dbPath := filepath.Join(dir, dbFile)
	assertNoSecretsIn(t, dbPath, logs.String(), hubURLScanFloor(t, dbPath, hubURL), key)
}

// hubURLScanFloor checks that the settings row holds the saved Hub address and returns the rows a full scan must see.
func hubURLScanFloor(t *testing.T, dbPath, hubURL string) int {
	t.Helper()
	raw, err := sql.Open("sqlite", "file:"+filepath.ToSlash(dbPath)+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = raw.Close() }()
	var versions, rows int
	if err := raw.QueryRow(`SELECT count(*) FROM goose_db_version`).Scan(&versions); err != nil || versions == 0 {
		t.Fatalf("migration versions: %d, %v", versions, err)
	}
	if err := raw.QueryRow(`SELECT count(*) FROM settings WHERE hub_url = ?`, hubURL).Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("settings rows with the saved address: %d, %v", rows, err)
	}
	return versions + 1
}

// Off Windows there is no store for the key (wincred.ErrUnsupported): the page says so instead of failing.
func TestConnectionsHubWithoutCredentialStore(t *testing.T) {
	ctx := context.Background()
	hub := testenv.NewFakeHub()
	hub.SetRemaining(600, 0, 1000)
	hubURL, hubHTTP := testenv.StartFakeHub(t, hub)
	a, f := newTestAppWith(t, t.TempDir())
	a.env.hubHTTP = hubHTTP
	f.secrets[hubSecretsService].Fail(wincred.ErrUnsupported)
	page := wails.NewConnectionsService(startCore(t, a).Connections, a.log)

	got, err := page.GetConnections(ctx)
	if err != nil || got.HubKey != "unsupported" || got.MailPassword != "unset" || got.Quota.Status != "unchecked" {
		t.Fatalf("GetConnections = %+v, %v", got, err)
	}
	secret, err := page.SetHubKey(ctx, testHubKey)
	if err != nil || secret.Status != "unsupported" || !strings.Contains(secret.Message, "ключ хранить негде") {
		t.Fatalf("SetHubKey = %+v, %v", secret, err)
	}
	if saved, err := page.SaveHubURL(ctx, hubURL); err != nil || !saved.Saved {
		t.Fatalf("SaveHubURL = %+v, %v", saved, err)
	}
	quota, err := page.RefreshQuota(ctx)
	if err != nil || quota.OK || quota.Quota.Status != "failed" || !strings.Contains(quota.Message, "недоступен в этой системе") {
		t.Fatalf("RefreshQuota = %+v, %v", quota, err)
	}
	if n := len(hub.QuotaAuthorizations()); n != 0 {
		t.Fatalf("the Hub was called %d times without a key", n)
	}
}
