package localhub

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/domain/localhub/values"
)

// fakeProbe returns before until the launcher started the Hub, then the after facts in turn (the last one repeats).
type fakeProbe struct {
	mu      sync.Mutex
	before  values.Facts
	after   []values.Facts
	started bool
	calls   int
	err     error
	targets []values.Target
}

func (p *fakeProbe) Facts(_ context.Context, t values.Target) (values.Facts, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	p.targets = append(p.targets, t)
	if p.err != nil {
		return values.Facts{}, p.err
	}
	if !p.started || len(p.after) == 0 {
		return p.before, nil
	}
	f := p.after[0]
	if len(p.after) > 1 {
		p.after = p.after[1:]
	}
	return f, nil
}

func (p *fakeProbe) set(f values.Facts) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.before = f
}

func (p *fakeProbe) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

type fakeLauncher struct {
	mu       sync.Mutex
	probe    *fakeProbe
	starts   int
	settings []values.ServerSettings
	targets  []values.Target
	err      error
	entered  chan struct{} // receives once per Start when not nil
	release  chan struct{} // Start blocks on it when not nil
}

func (l *fakeLauncher) Start(_ context.Context, t values.Target, s values.ServerSettings) error {
	l.mu.Lock()
	l.starts++
	l.settings = append(l.settings, s)
	l.targets = append(l.targets, t)
	err, entered, release := l.err, l.entered, l.release
	l.mu.Unlock()
	if entered != nil {
		entered <- struct{}{}
	}
	if release != nil {
		<-release
	}
	if err != nil {
		return err
	}
	l.probe.mu.Lock()
	l.probe.started = true
	l.probe.mu.Unlock()
	return nil
}

func (l *fakeLauncher) Stop(context.Context, values.Target) error { return errors.New("not used") }

func (l *fakeLauncher) IssueKey(context.Context) error { return errors.New("not used") }

func (l *fakeLauncher) startCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.starts
}

type fakeSecrets struct {
	mu        sync.Mutex
	values    map[values.SecretName]string
	generated []values.SecretName
	isSetErr  error
	setErr    func(value string) error
	genErr    error
}

func newSecrets(names ...values.SecretName) *fakeSecrets {
	s := &fakeSecrets{values: map[values.SecretName]string{}}
	for _, n := range names {
		s.values[n] = "fake-" + string(n)
	}
	return s
}

func (s *fakeSecrets) IsSet(_ context.Context, n values.SecretName) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.isSetErr != nil {
		return false, s.isSetErr
	}
	_, ok := s.values[n]
	return ok, nil
}

func (s *fakeSecrets) Set(_ context.Context, n values.SecretName, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.setErr != nil {
		return s.setErr(value)
	}
	s.values[n] = value
	return nil
}

func (s *fakeSecrets) Generate(_ context.Context, n values.SecretName) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.genErr != nil {
		return s.genErr
	}
	s.generated = append(s.generated, n)
	s.values[n] = "1:generated"
	return nil
}

func (s *fakeSecrets) generatedCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.generated)
}

type fakeSettings struct {
	mu      sync.Mutex
	v       values.ServerSettings
	saves   int
	loadErr error
}

func (f *fakeSettings) Load(context.Context) (values.ServerSettings, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.v, f.loadErr
}

func (f *fakeSettings) Save(_ context.Context, v values.ServerSettings) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.v = v
	f.saves++
	return nil
}

type fakeURL struct {
	raw string
	err error
}

func (u fakeURL) Current(context.Context) (string, error) { return u.raw, u.err }

// fakeClock advances by the slept duration at once.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	sleeps int
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sleeps++
	c.now = c.now.Add(d)
	return nil
}

// world is a Service with its fakes; by default every precondition holds and nothing answers.
type world struct {
	svc      *Service
	probe    *fakeProbe
	launcher *fakeLauncher
	secrets  *fakeSecrets
	settings *fakeSettings
	clock    *fakeClock
	logs     *syncBuffer
}

const localURL = "http://127.0.0.1:8080"

func readyFacts() values.Facts {
	return values.Facts{HubExe: true, DockerEngine: true, DatabasePort: true}
}

func answering() values.Facts {
	f := readyFacts()
	f.Healthz, f.Readyz, f.Listener = true, true, values.ListenerOurs
	return f
}

func newWorld(t *testing.T, url string) *world {
	t.Helper()
	w := &world{
		probe:    &fakeProbe{before: readyFacts()},
		secrets:  newSecrets(values.SecretProviderKey, values.SecretDatabaseURL),
		settings: &fakeSettings{},
		clock:    &fakeClock{now: time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC)},
		logs:     &syncBuffer{},
	}
	w.launcher = &fakeLauncher{probe: w.probe}
	svc, err := New(Deps{
		Probe: w.probe, Launcher: w.launcher, Secrets: w.secrets, Settings: w.settings, URL: fakeURL{raw: url},
		Clock: w.clock, Log: slog.New(slog.NewTextHandler(w.logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})
	if err != nil {
		t.Fatal(err)
	}
	w.svc = svc
	return w
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}
