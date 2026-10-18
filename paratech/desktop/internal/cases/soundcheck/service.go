package soundcheck

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/recording/values"
)

const (
	// DefaultDuration is how long a check captures.
	DefaultDuration = 15 * time.Second

	// maxDuration bounds Config.Duration.
	maxDuration = time.Minute

	// sessionSlack bounds a session past its duration in case the checker does not return by itself.
	sessionSlack = 30 * time.Second
)

// Deps are the ports of the Service; all but Log are required.
type Deps struct {
	Checker    LevelChecker
	Settings   SettingsReader
	Recordings Recordings
	Clock      Clock
	Log        *slog.Logger
}

// Config tunes the Service; zero fields take the defaults.
type Config struct {
	Duration time.Duration // of a check; DefaultDuration
}

// Service runs the sound check: one session at a time, kept after it ends so the page can read the verdict.
type Service struct {
	deps     Deps
	log      *slog.Logger
	duration time.Duration

	life    context.Context // ends with Close; a check never depends on the request that started it
	endLife context.CancelFunc
	wg      sync.WaitGroup

	mu     sync.Mutex
	cur    *session
	closed bool
}

// session is one check. Its fields but the constant ones are guarded by Service.mu.
type session struct {
	source  values.Source
	started time.Time
	cancel  context.CancelFunc
	done    chan struct{} // closed when it has ended

	running bool
	stopped bool // ended by Stop or Close
	check   values.LevelCheck
	elapsed time.Duration // frozen at the end
	verdict values.SoundCause
}

// New builds the Service. It starts nothing.
func New(deps Deps, cfg Config) (*Service, error) {
	if deps.Checker == nil || deps.Settings == nil || deps.Recordings == nil || deps.Clock == nil {
		return nil, fmt.Errorf("%w: every port is required", ErrInvalidConfig)
	}
	if cfg.Duration == 0 {
		cfg.Duration = DefaultDuration
	}
	if cfg.Duration < time.Second || cfg.Duration > maxDuration {
		return nil, fmt.Errorf("%w: duration %v outside 1 s..%v", ErrInvalidConfig, cfg.Duration, maxDuration)
	}
	log := deps.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	life, end := context.WithCancel(context.Background())
	return &Service{deps: deps, log: log, duration: cfg.Duration, life: life, endLife: end}, nil
}

// Start begins a check of the remembered source (the whole computer when the settings cannot be read) and returns
// its status at once. While a recording runs it captures nothing and the check is done with the verdict busy. A check
// that runs already is not started again: ErrBusy with its status. After Close: ErrClosed.
func (s *Service) Start(ctx context.Context) (Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.closed:
		return Status{}, ErrClosed
	case s.cur != nil && s.cur.running:
		return s.statusLocked(), ErrBusy
	}

	sess := &session{source: s.source(ctx), started: s.deps.Clock.Now(), done: make(chan struct{})}
	s.cur = sess
	if s.deps.Recordings.Recording() {
		sess.verdict = values.SoundBusy
		close(sess.done)
		s.log.Info("sound check refused: a recording runs")
		return s.statusLocked(), nil
	}

	ctx, cancel := context.WithTimeout(s.life, s.duration+sessionSlack)
	sess.cancel, sess.running = cancel, true
	s.wg.Add(1)
	go s.run(ctx, sess)
	s.log.Info("sound check started", "source", sess.source.String())
	return s.statusLocked(), nil
}

// run captures until the checker returns and then sets the verdict.
func (s *Service) run(ctx context.Context, sess *session) {
	defer s.wg.Done()
	defer sess.cancel()

	end, err := s.deps.Checker.CheckLevels(ctx, sess.source, s.duration, func(l values.SoundLevel) {
		s.mu.Lock()
		defer s.mu.Unlock()
		sess.check = sess.check.Hear(l)
	})

	s.mu.Lock()
	end = s.endOf(sess.stopped, end, err)
	sess.verdict = sess.check.Verdict(end)
	sess.elapsed = s.elapsedLocked(sess)
	sess.check.Level = 0
	sess.running = false
	verdict := sess.verdict
	s.mu.Unlock()
	close(sess.done)
	s.log.Info("sound check", "source", sess.source.String(), "end", string(end), "verdict", string(verdict))
}

// endOf is how the capture ended: the checker's answer, ok when the user or the shutdown stopped it, or the cause of
// its error.
func (s *Service) endOf(stopped bool, end values.SoundCause, err error) values.SoundCause {
	switch {
	case err == nil:
		return end
	case stopped:
		return values.SoundOK
	case errors.Is(err, recording.ErrSourceNotRunning):
		return values.SoundNotRunning
	case errors.Is(err, recording.ErrCaptureUnavailable):
		return values.SoundUnsupported
	}
	s.log.Error("sound check failed", "err", err)
	return values.SoundFailed
}

// Status returns the last check; StateIdle when none ran.
func (s *Service) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.statusLocked()
}

func (s *Service) statusLocked() Status {
	sess := s.cur
	if sess == nil {
		return Status{State: StateIdle, Duration: s.duration}
	}
	st := Status{Source: sess.source, Check: sess.check, Heard: sess.check.Heard(), Duration: s.duration}
	if sess.running {
		st.State, st.Elapsed = StateRunning, s.elapsedLocked(sess)
		return st
	}
	st.State, st.Elapsed, st.Verdict = StateDone, sess.elapsed, sess.verdict
	return st
}

func (s *Service) elapsedLocked(sess *session) time.Duration {
	return min(max(s.deps.Clock.Now().Sub(sess.started), 0), s.duration)
}

// Stop ends a running check early and waits until it has ended or ctx ends; then it returns the status. The verdict
// is ok when sound was heard, silence otherwise. Without a running check it only returns the status.
func (s *Service) Stop(ctx context.Context) Status {
	s.mu.Lock()
	sess := s.cur
	if sess == nil || !sess.running {
		defer s.mu.Unlock()
		return s.statusLocked()
	}
	sess.stopped = true
	sess.cancel()
	s.mu.Unlock()

	select {
	case <-sess.done:
	case <-ctx.Done():
	}
	return s.Status()
}

// Close ends a running check and waits for it; later Starts fail with ErrClosed. It is safe to call more than once.
func (s *Service) Close() {
	s.mu.Lock()
	s.closed = true
	if s.cur != nil && s.cur.running {
		s.cur.stopped = true
	}
	s.mu.Unlock()
	s.endLife()
	s.wg.Wait()
}

// source is the remembered source; the whole computer when the settings cannot be read.
func (s *Service) source(ctx context.Context) values.Source {
	st, err := s.deps.Settings.Load(ctx)
	if err != nil {
		s.log.Warn("sound check: the settings cannot be read; checking the whole computer", "err", err)
		return values.WholeComputer()
	}
	src, err := values.SourceOf(st.RecordingSource())
	if err != nil {
		return values.WholeComputer()
	}
	return src
}
