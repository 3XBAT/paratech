package hubclient

import (
	"context"
	"errors"
	"sync"
	"testing"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/jobs/services/backoff"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

// urlSetting is a URLSource the test changes between calls, like the settings in the app.
type urlSetting struct {
	mu  sync.Mutex
	url string
	err error
}

func (u *urlSetting) HubURL(context.Context) (string, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.url, u.err
}

func (u *urlSetting) set(url string, err error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.url, u.err = url, err
}

func newSettingsClient(t *testing.T, urls *urlSetting) (*Client, *testenv.MemCredentials, *fakeHub) {
	t.Helper()
	f := &fakeHub{answer: okAnswer}
	_, creds, srv := startHub(t, f)
	c, err := New(Config{URLs: urls, HTTPClient: srv.Client()}, creds)
	if err != nil {
		t.Fatal(err)
	}
	urls.set(srv.URL, nil)
	return c, creds, f
}

// The URL is read on every call: a Hub that appears, moves or goes away in the settings applies without a restart.
func TestURLFromSettingsAppliesWithoutRestart(t *testing.T) {
	urls := &urlSetting{}
	c, _, first := newSettingsClient(t, urls)
	if _, err := c.Complete(t.Context(), request(t)); err != nil {
		t.Fatal(err)
	}

	second := &fakeHub{answer: okAnswer}
	_, _, srv := startHub(t, second)
	urls.set(srv.URL, nil)
	if _, err := c.Complete(t.Context(), request(t)); err != nil {
		t.Fatal(err)
	}
	a, _ := first.calls()
	b, _ := second.calls()
	if len(a) != 1 || len(b) != 1 {
		t.Fatalf("requests: first hub %d, second hub %d; want 1 and 1", len(a), len(b))
	}
}

// Not configured (no URL, no key) parks the job in WAITING_FOR_NETWORK with a fixed text and sends nothing; an
// unreadable setting is a transient error, a stored URL that breaks the rule is permanent.
func TestNotConfiguredParks(t *testing.T) {
	cases := []struct {
		name   string
		setup  func(t *testing.T, u *urlSetting, m *testenv.MemCredentials)
		is     []error
		class  backoff.Class
		parked bool
	}{
		{"no url", func(_ *testing.T, u *urlSetting, _ *testenv.MemCredentials) { u.set("", nil) },
			[]error{ErrNotConfigured, ErrNoURL, shared.ErrNetworkUnavailable}, backoff.ClassTransient, true},
		{"no key", func(t *testing.T, _ *urlSetting, m *testenv.MemCredentials) {
			if err := m.Delete(t.Context(), DefaultCredentialName); err != nil {
				t.Fatal(err)
			}
		}, []error{ErrNotConfigured, ErrMissingAPIKey, shared.ErrNetworkUnavailable}, backoff.ClassTransient, true},
		{"settings unreadable", func(_ *testing.T, u *urlSetting, _ *testenv.MemCredentials) {
			u.set("", errors.New("database is busy"))
		}, []error{ErrSettings}, backoff.ClassTransient, false},
		{"stored url breaks the rule", func(_ *testing.T, u *urlSetting, _ *testenv.MemCredentials) {
			u.set("http://hub.example.com", nil)
		}, []error{ErrInvalidConfig}, backoff.ClassPermanent, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			urls := &urlSetting{}
			c, creds, f := newSettingsClient(t, urls)
			tc.setup(t, urls, creds)

			_, err := c.Complete(t.Context(), request(t))
			for _, w := range tc.is {
				if !errors.Is(err, w) {
					t.Fatalf("error %v does not match %v", err, w)
				}
			}
			if got := backoff.Classify(err); got != tc.class {
				t.Fatalf("class = %v, want %v", got, tc.class)
			}
			status, parked := backoff.WaitStatus(err)
			if parked != tc.parked || (parked && status != job.StatusWaitingForNetwork) {
				t.Fatalf("wait status = %v, %v; want parked %v", status, parked, tc.parked)
			}
			if tc.parked && err.Error() != "hub: not configured" {
				t.Fatalf("error text %q, want the fixed text", err.Error())
			}
			if reqs, _ := f.calls(); len(reqs) != 0 {
				t.Fatalf("hub saw %d requests, want none", len(reqs))
			}
		})
	}
}

func TestNewTakesOneURLSource(t *testing.T) {
	creds := testenv.NewMemCredentials()
	if _, err := New(Config{BaseURL: "https://hub.example.com", URLs: &urlSetting{}}, creds); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("both sources: %v, want ErrInvalidConfig", err)
	}
	if _, err := New(Config{URLs: &urlSetting{}}, creds); err != nil {
		t.Fatalf("a source without a url yet: %v", err)
	}
}
