package localhub

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"

	"paratech/desktop/internal/domain/localhub/services/diagnosis"
	"paratech/desktop/internal/domain/localhub/values"
)

// Operation names, used in error messages.
const (
	opEnsure       = "localhub: ensure"
	opStatus       = "localhub: status"
	opSettings     = "localhub: settings"
	opSaveSettings = "localhub: save settings"
	opStoreSlot    = "localhub: write slot"
	opCheckSlot    = "localhub: check slot"
)

// The readiness wait after a start (ADR-0036 section 4).
const (
	pollInterval = 500 * time.Millisecond
	readyTimeout = 20 * time.Second
)

// Service decides whether to start the local Hub, starts it and keeps the last status. It is safe for concurrent
// use; the recheck cadence belongs to the wiring.
type Service struct {
	probe    HubProbe
	launcher HubLauncher
	secrets  SecretSlots
	settings SettingsFile
	url      HubURL
	clock    Clock
	log      *slog.Logger

	mu     sync.Mutex
	last   Status
	known  bool    // last holds a status
	flight *flight // the running check; nil when none

	// acks counts the owner's acknowledgements of a recorded exit (Recheck, a saved setting or secret); spent is the
	// count the latest spawn consumed. While they differ, a recorded exit does not stand in the way of one new start.
	acks, spent uint64
}

// flight is one check that concurrent callers share. It runs detached from the callers' contexts.
type flight struct {
	done   chan struct{}
	joined int // callers that waited for it, for tests
	st     Status
	err    error
}

// New checks the ports.
func New(d Deps) (*Service, error) {
	if d.Probe == nil || d.Launcher == nil || d.Secrets == nil || d.Settings == nil || d.URL == nil || d.Clock == nil {
		return nil, fmt.Errorf("%w: every port is required", ErrInvalidConfig)
	}
	log := d.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Service{
		probe: d.Probe, launcher: d.Launcher, secrets: d.Secrets, settings: d.Settings, url: d.URL, clock: d.Clock, log: log,
	}, nil
}

// Ensure is the automatic check (the start of the app and the periodic recheck): it makes the local Hub run if it
// can (ADR-0036 section 4). A remote «Адрес сервера» gives StatusRemote and nothing else; a Hub that answers is left
// alone; otherwise, when every precondition holds, the seal keys are generated if missing, hub.exe is started and its
// answer awaited for up to 20 s. A recorded exit of the Hub this desktop started blocks a start until the owner
// acknowledges it (Recheck, SaveSettings, SetSecret), so an ending Hub is never respawned in a loop.
//
// The returned status is also kept for Status. Calls made while one runs share it: the Hub is started at most once.
// The shared run does not stop when a caller's ctx ends; that caller gets the last kept status and ctx's error.
func (s *Service) Ensure(ctx context.Context) (Status, error) { return s.run(ctx, false) }

// Recheck is Ensure for «Проверить снова»: the owner's request also acknowledges a recorded exit, so a Hub that ended
// may be started once more.
func (s *Service) Recheck(ctx context.Context) (Status, error) { return s.run(ctx, true) }

// run joins the running check or begins one, and waits for it or for ctx.
func (s *Service) run(ctx context.Context, ack bool) (Status, error) {
	if err := ctx.Err(); err != nil {
		return s.cached(), fmt.Errorf("%s: %w", opEnsure, err)
	}

	s.mu.Lock()
	if ack {
		s.acks++
	}
	f := s.flight
	if f == nil {
		f = &flight{done: make(chan struct{})}
		s.flight = f
		go s.fly(context.WithoutCancel(ctx), f)
	}
	f.joined++
	s.mu.Unlock()

	select {
	case <-f.done:
		return f.st, f.err
	case <-ctx.Done():
		return s.cached(), fmt.Errorf("%s: %w", opEnsure, ctx.Err())
	}
}

// fly runs the check of f; the readiness wait bounds it by the Clock (readyTimeout).
func (s *Service) fly(ctx context.Context, f *flight) {
	st, err := s.ensure(ctx)
	if err != nil {
		err = fmt.Errorf("%s: %w", opEnsure, err)
	}

	s.mu.Lock()
	s.flight = nil
	f.st, f.err = st, err
	s.mu.Unlock()
	close(f.done)
}

// acknowledge lets a recorded exit be started over once (see Service.acks).
func (s *Service) acknowledge() {
	s.mu.Lock()
	s.acks++
	s.mu.Unlock()
}

// pending returns the acknowledgement count and whether one is not yet spent by a spawn.
func (s *Service) pending() (uint64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.acks, s.acks != s.spent
}

// Status returns the last status. Before the first Ensure it probes once without starting anything.
func (s *Service) Status(ctx context.Context) (Status, error) {
	s.mu.Lock()
	st, known := s.last, s.known
	s.mu.Unlock()
	if known {
		return st, nil
	}

	t, remote, err := s.target(ctx)
	if err != nil {
		return Status{}, fmt.Errorf("%s: %w", opStatus, err)
	}
	if remote != nil {
		return *remote, nil
	}
	_, ack := s.pending()
	d, err := s.diagnose(ctx, t, ack)
	if err != nil {
		return Status{}, fmt.Errorf("%s: %w", opStatus, err)
	}
	return localStatus(t, d), nil
}

// Settings returns the saved non-secret settings for the form.
func (s *Service) Settings(ctx context.Context) (Settings, error) {
	v, err := s.settings.Load(ctx)
	if err != nil {
		return Settings{}, fmt.Errorf("%s: %w", opSettings, err)
	}
	return Settings{BaseURL: v.BaseURL(), Auth: v.Auth(), QuotaLimit: v.QuotaLimit()}, nil
}

// SaveSettings validates and saves the non-secret settings; a running Hub picks them up at its next start. Saving
// acknowledges a recorded exit, so the next Ensure or Recheck may start the Hub with the new settings.
func (s *Service) SaveSettings(ctx context.Context, in Settings) error {
	v, err := values.NewServerSettings(in.BaseURL, in.Auth, in.QuotaLimit)
	if err != nil {
		return fmt.Errorf("%s: %w", opSaveSettings, err)
	}
	if err := s.settings.Save(ctx, v); err != nil {
		return fmt.Errorf("%s: %w", opSaveSettings, err)
	}
	s.acknowledge()
	s.log.Info("local hub settings saved")
	return nil
}

// SetSecret stores value, trimmed of surrounding whitespace, in the slot name: provider-key or database-url only
// (values.ParseTypedSecret). The value is handed to the store and not kept; no error or log line carries it. Like
// SaveSettings, a stored value acknowledges a recorded exit.
func (s *Service) SetSecret(ctx context.Context, name, value string) error {
	n, err := values.ParseTypedSecret(name)
	if err != nil {
		return fmt.Errorf("%s: %w", opStoreSlot, err)
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("%s %s: %w", opStoreSlot, n, ErrEmptySecret)
	}
	if err := s.secrets.Set(ctx, n, value); err != nil {
		if strings.Contains(err.Error(), value) {
			err = ErrSecretStore
		}
		return fmt.Errorf("%s %s: %w", opStoreSlot, n, err)
	}
	s.acknowledge()
	s.log.Info("local hub secret saved", "name", n.String())
	return nil
}

// SecretSet reports whether the slot name (provider-key or database-url) holds a value.
func (s *Service) SecretSet(ctx context.Context, name string) (bool, error) {
	n, err := values.ParseTypedSecret(name)
	if err != nil {
		return false, fmt.Errorf("%s: %w", opCheckSlot, err)
	}
	ok, err := s.secrets.IsSet(ctx, n)
	if err != nil {
		return false, fmt.Errorf("%s %s: %w", opCheckSlot, n, err)
	}
	return ok, nil
}

// ensure is one run of Ensure.
func (s *Service) ensure(ctx context.Context) (Status, error) {
	t, remote, err := s.target(ctx)
	if err != nil {
		return s.cached(), err
	}
	if remote != nil {
		return s.store(*remote), nil
	}

	acks, ack := s.pending()
	d, err := s.diagnose(ctx, t, ack)
	if err != nil {
		return s.cached(), err
	}
	if !d.CanStart() {
		return s.store(localStatus(t, d)), nil
	}
	return s.start(ctx, t, acks)
}

// start generates the seal keys if missing, starts hub.exe and waits for its answer. A spawn spends the
// acknowledgements up to acks: from then on the recorded exit is the new process's and is seen again.
func (s *Service) start(ctx context.Context, t values.Target, acks uint64) (Status, error) {
	if err := s.ensureSealKeys(ctx); err != nil {
		return s.store(localStatus(t, values.Exited(values.ExitNotStarted))), err
	}
	settings, err := s.settings.Load(ctx)
	if err != nil {
		return s.store(localStatus(t, values.NotRunning(values.ReasonConfigError))), fmt.Errorf("load settings: %w", err)
	}

	s.store(localStatus(t, values.Starting()))
	s.log.Info("starting the local hub", "port", t.Port())
	if err := s.launcher.Start(ctx, t, settings); err != nil {
		return s.store(localStatus(t, values.Exited(values.ExitNotStarted))), fmt.Errorf("start hub: %w", err)
	}
	s.mu.Lock()
	s.spent = acks
	s.mu.Unlock()

	deadline := s.clock.Now().Add(readyTimeout)
	last := values.Starting()
	for s.clock.Now().Before(deadline) {
		if err := s.clock.Sleep(ctx, pollInterval); err != nil {
			return s.cached(), err
		}
		f, err := s.facts(ctx, t)
		if err != nil {
			if ctx.Err() != nil {
				return s.cached(), ctx.Err()
			}
			s.log.Warn("probing the starting local hub failed", "err", err)
			continue
		}
		last = diagnosis.Diagnose(f)
		if f.Healthz || f.Exited {
			return s.store(localStatus(t, last)), nil
		}
	}

	// Alive but silent after 20 s: still starting, unless the facts name something in the way.
	if last.CanStart() {
		last = values.Starting()
	}
	s.log.Warn("the local hub does not answer yet", "port", t.Port(), "status", string(last.Status()))
	return s.store(localStatus(t, last)), nil
}

// ensureSealKeys generates the seal keys once, before the first start.
func (s *Service) ensureSealKeys(ctx context.Context) error {
	ok, err := s.secrets.IsSet(ctx, values.SecretSealKeys)
	if err != nil {
		return fmt.Errorf("check seal keys: %w", err)
	}
	if ok {
		return nil
	}
	if err := s.secrets.Generate(ctx, values.SecretSealKeys); err != nil {
		return fmt.Errorf("generate seal keys: %w", err)
	}
	s.log.Info("local hub seal keys generated")
	return nil
}

// target reads «Адрес сервера»: a loopback Target, or the remote status when it is not local.
func (s *Service) target(ctx context.Context) (values.Target, *Status, error) {
	raw, err := s.url.Current(ctx)
	if err != nil {
		return values.Target{}, nil, fmt.Errorf("read server address: %w", err)
	}
	t, err := values.ParseTarget(raw)
	if errors.Is(err, values.ErrNotLocal) {
		d := values.Remote()
		return values.Target{}, &Status{State: d.Status(), URL: withoutUserInfo(raw)}, nil
	}
	if err != nil {
		return values.Target{}, nil, err
	}
	return t, nil, nil
}

// withoutUserInfo drops credentials from a remote address before it is shown. An unparsable one, or one without a
// scheme that may hide them ("user:pass@host" parses as an opaque URL), is not shown.
func withoutUserInfo(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || strings.Contains(u.Opaque, "@") {
		return ""
	}
	if u.User == nil {
		return raw
	}
	u.User = nil
	return u.String()
}

// diagnose probes t and applies the diagnosis rule; ack hides a recorded exit the owner acknowledged.
func (s *Service) diagnose(ctx context.Context, t values.Target, ack bool) (values.Diagnosis, error) {
	f, err := s.facts(ctx, t)
	if err != nil {
		return values.Diagnosis{}, err
	}
	if ack {
		f.Exited, f.ExitCode = false, 0
	}
	return diagnosis.Diagnose(f), nil
}

// facts probes t and sets the secret flags from the secret store.
func (s *Service) facts(ctx context.Context, t values.Target) (values.Facts, error) {
	f, err := s.probe.Facts(ctx, t)
	if err != nil {
		return values.Facts{}, fmt.Errorf("probe: %w", err)
	}
	if f.ProviderKeySet, err = s.secrets.IsSet(ctx, values.SecretProviderKey); err != nil {
		return values.Facts{}, fmt.Errorf("check %s: %w", values.SecretProviderKey, err)
	}
	if f.DatabaseURLSet, err = s.secrets.IsSet(ctx, values.SecretDatabaseURL); err != nil {
		return values.Facts{}, fmt.Errorf("check %s: %w", values.SecretDatabaseURL, err)
	}
	return f, nil
}

// store keeps st as the last status and logs a change.
func (s *Service) store(st Status) Status {
	s.mu.Lock()
	changed := !s.known || s.last != st
	s.last, s.known = st, true
	s.mu.Unlock()
	if changed {
		s.log.Info("local hub status", "status", string(st.State), "reason", string(st.Reason), "exit_code", st.ExitCode)
	}
	return st
}

// cached returns the last status; the zero Status before any.
func (s *Service) cached() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last
}

func localStatus(t values.Target, d values.Diagnosis) Status {
	return Status{
		State: d.Status(), Reason: d.Reason(), Next: d.NextStep(), ExitCode: d.ExitCode(), Port: t.Port(), URL: t.BaseURL(),
	}
}
