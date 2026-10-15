package jobrunner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/jobs/services/backoff"
	"paratech/desktop/internal/domain/jobs/services/powerpolicy"
	"paratech/desktop/internal/domain/shared"
)

// changeAttempts bounds the reload-and-retry loop of a user change (cancel, run now) that keeps meeting version conflicts.
const changeAttempts = 3

// Bounds of the final save of an attempt: a transient storage error (for example a busy database) is retried with a
// doubling delay, so the job does not stay RUNNING until the next start.
const (
	finalSaveAttempts  = 4
	finalSaveRetryBase = 200 * time.Millisecond
)

// permilleScale turns the permille of the Jitter port into a fraction.
const permilleScale = 1000

// Outcomes of one attempt, for the log.
const (
	outcomeCompleted   = "completed"
	outcomeCancelled   = "cancelled"
	outcomeFailed      = "failed"
	outcomeRetry       = "retry"
	outcomeInterrupted = "interrupted"
	outcomeDropped     = "dropped"
	outcomeLeftRunning = "left_running"
	outcomeWaiting     = "waiting"
)

// Service runs persistent jobs on a pool of workers (PRD section 7). Handlers are registered per job type before
// Start. A worker claims a due job, runs its handler under a timeout and moves the job through the aggregate:
//
//   - success: COMPLETED;
//   - permanent error, or no attempt left: FAILED;
//   - any other error, including a timeout: RETRY_WAIT with bounded exponential backoff, back to QUEUED when due,
//     for every Recovery policy (see Registration.Reconciler for what that asks of side-effect handlers);
//   - user cancellation: the handler context is cancelled and the job becomes CANCELLED;
//   - shutdown during an attempt: a RecoveryRequeue job is requeued like a stale RUNNING job at startup recovery;
//     any other job stays RUNNING and Recover handles it at the next start, as after a crash (hard rule 7);
//   - AC power lost during a Heavy attempt with "only on AC power" on: the next progress report saves the checkpoint
//     and returns powerpolicy.ErrPowerLost, and the job becomes WAITING_FOR_POWER with the attempt given back;
//   - an error that wraps shared.ErrNetworkUnavailable or shared.ErrQuotaExceeded (backoff.WaitStatus): the job becomes
//     WAITING_FOR_NETWORK or WAITING_FOR_QUOTA with the attempt given back, and is requeued after Config.Rechecks.
//
// Power-aware processing (PRD section 7.4, AC-21, AC-22) follows powerpolicy: on battery with the setting on, Heavy
// jobs are not claimed and due QUEUED ones are parked in WAITING_FOR_POWER; when AC power returns or the setting is
// turned off they go back to QUEUED. Light jobs are never affected, and ForceRun starts one job regardless. The power
// source comes from PowerMonitor events, the setting is re-read every poll interval. At most Config.MaxHeavy Heavy
// jobs run at once; meanwhile the other workers take only light jobs.
//
// An error that no handler classified counts as transient, so it stops at the attempt cap (AC-10).
// The runner is the only writer of a claimed job (ADR-0004); a version conflict on Save means someone else changed
// the job, and the runner then drops its own outcome.
type Service struct {
	jobs     JobRepository
	clock    Clock
	sleeper  Sleeper
	jitter   Jitter
	power    PowerMonitor
	settings SettingsReader
	observer Observer
	log      *slog.Logger

	cfg    Config
	policy backoff.Policy

	loopCtx  context.Context // ends on Stop: workers stop claiming
	stopLoop context.CancelFunc
	runCtx   context.Context // parent of the handler contexts; ends on a forced stop
	stopRuns context.CancelCauseFunc

	lifecycle sync.Mutex // held by Start and Recover: recovery never meets a job of a live worker

	mu       sync.Mutex
	handlers map[job.Type]Registration
	started  bool
	running  map[shared.JobID]*run
	stopped  chan struct{} // closed when every worker has exited
	unwatch  func() bool
	wg       sync.WaitGroup

	powerPolicy powerpolicy.Policy // set by Start, read-only afterwards
	lightOnly   job.ClaimFilter    // set by Start: the light types only; zero when every type is heavy
	heavySlots  chan struct{}      // one token per heavy job that may run now (Config.MaxHeavy)
	enforceMu   sync.Mutex         // serialises the power policy runs of the keeper and of power events
	subscribed  atomic.Bool        // power events arrive; otherwise the keeper polls the monitor

	condMu          sync.Mutex
	cond            powerpolicy.Conditions
	settingsFailing bool // the last settings read failed; logged once per failure streak
}

// New validates the configuration and builds a stopped Service.
func New(deps Deps, cfg Config) (*Service, error) {
	switch {
	case deps.Jobs == nil || deps.Clock == nil || deps.Sleeper == nil:
		return nil, fmt.Errorf("%w: repository, clock and sleeper are required", ErrInvalidConfig)
	case cfg.Workers < 1:
		return nil, fmt.Errorf("%w: %d workers", ErrInvalidConfig, cfg.Workers)
	case cfg.PollInterval <= 0:
		return nil, fmt.Errorf("%w: poll interval %v", ErrInvalidConfig, cfg.PollInterval)
	case cfg.DefaultTimeout <= 0:
		return nil, fmt.Errorf("%w: default timeout %v", ErrInvalidConfig, cfg.DefaultTimeout)
	}
	if cfg.MaxHeavy == 0 {
		cfg.MaxHeavy = DefaultMaxHeavy
	}
	if cfg.MaxHeavy < 1 || cfg.MaxHeavy > cfg.Workers {
		return nil, fmt.Errorf("%w: %d heavy jobs at once with %d workers", ErrInvalidConfig, cfg.MaxHeavy, cfg.Workers)
	}
	if cfg.Rechecks == (backoff.Rechecks{}) {
		cfg.Rechecks = backoff.DefaultRechecks()
	}
	if err := cfg.Rechecks.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}
	policy, err := backoff.New(cfg.Backoff)
	if err != nil {
		return nil, err
	}
	log := deps.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	s := &Service{
		jobs: deps.Jobs, clock: deps.Clock, sleeper: deps.Sleeper, jitter: deps.Jitter, power: deps.Power, settings: deps.Settings,
		observer: deps.Observer, log: log,
		cfg: cfg, policy: policy,
		handlers: map[job.Type]Registration{}, running: map[shared.JobID]*run{},
		stopped: make(chan struct{}), heavySlots: make(chan struct{}, cfg.MaxHeavy),
	}
	s.loopCtx, s.stopLoop = context.WithCancel(context.Background())
	s.runCtx, s.stopRuns = context.WithCancelCause(context.Background())
	return s, nil
}

// Register binds a handler to a job type. It must be called before Start, once per type.
func (s *Service) Register(r Registration) error {
	if r.Handler == nil || r.Timeout < 0 {
		return fmt.Errorf("%w: handler for %q needs a handler and a non-negative timeout", ErrInvalidConfig, r.Type)
	}
	if !r.Recovery.valid() {
		return fmt.Errorf("%w: handler for %q needs a recovery policy", ErrInvalidConfig, r.Type)
	}
	if r.Reconciler != nil && r.Recovery != RecoveryReconcile {
		return fmt.Errorf("%w: reconciler for %q needs RecoveryReconcile", ErrInvalidConfig, r.Type)
	}
	if _, err := job.NewClaimFilter([]job.Type{r.Type}, nil, true); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return ErrAlreadyStarted
	}
	if _, dup := s.handlers[r.Type]; dup {
		return fmt.Errorf("%w: handler for %q is already registered", ErrInvalidConfig, r.Type)
	}
	s.handlers[r.Type] = r
	return nil
}

// Start launches the workers; it waits for a running Recover. It first reads the setting and the power source, so it
// returns once those reads do; meanwhile Register is refused, Cancel works, and Stop or cancelling ctx ends the reads
// and then waits for Start. Cancelling ctx is a forced stop: handlers are interrupted and their jobs handled by the
// Recovery policy of their type. For a graceful stop call Stop.
func (s *Service) Start(ctx context.Context) error {
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	if err := s.begin(ctx); err != nil {
		return err
	}

	// Without s.mu: these are DB reads and syscalls. The power source and the setting are known before the first
	// claim, so no heavy job starts on battery by accident. loopCtx ends on Stop and, through unwatch, with ctx.
	events := s.initPower(s.loopCtx)

	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil {
		// unwatch stops the loop asynchronously; stop it now so no worker claims a job.
		s.stopLoop()
		s.stopRuns(errShutdown)
	}
	s.wg.Add(s.cfg.Workers + 1)
	for range s.cfg.Workers {
		go s.worker()
	}
	go s.keeper()
	if events != nil {
		s.wg.Add(1)
		go s.watchPower(events)
	}
	go func() {
		s.wg.Wait()
		close(s.stopped)
	}()
	return nil
}

// begin checks the lifecycle state and marks the Service started. started and unwatch are set together under s.mu,
// so a Stop sees both or neither; once started, s.stopped is always closed, because Start always launches the workers.
func (s *Service) begin(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return ErrAlreadyStarted
	}
	if s.loopCtx.Err() != nil {
		return ErrStopped
	}
	if len(s.handlers) == 0 {
		return ErrNoHandlers
	}
	var types, heavy, light []job.Type
	for t, reg := range s.handlers {
		types = append(types, t)
		if reg.Heavy {
			heavy = append(heavy, t)
		} else {
			light = append(light, t)
		}
	}
	sort.Slice(types, func(i, j int) bool { return types[i] < types[j] })
	sort.Slice(heavy, func(i, j int) bool { return heavy[i] < heavy[j] })
	sort.Slice(light, func(i, j int) bool { return light[i] < light[j] })
	policy, err := powerpolicy.New(types, heavy)
	if err != nil {
		return err
	}
	var lightOnly job.ClaimFilter
	if len(light) > 0 {
		if lightOnly, err = job.NewClaimFilter(light, nil, true); err != nil {
			return err
		}
	}

	s.powerPolicy, s.lightOnly, s.started = policy, lightOnly, true
	s.unwatch = context.AfterFunc(ctx, func() {
		s.stopLoop()
		s.stopRuns(errShutdown)
	})
	return nil
}

// Stop stops claiming and waits for the running handlers to finish. If ctx ends first, the handlers are interrupted,
// Stop still waits for them to return, and it reports ctx's error. Handlers must honour their context, otherwise Stop
// does not return. It is safe to call more than once and before Start.
//
// An interrupted RecoveryRequeue job is requeued. Any other interrupted job stays RUNNING, exactly as after a crash,
// and Cancel refuses it (see Cancel). A stopped Service cannot start again: app wiring that starts a new runner in the
// same process after Stop must call Recover on it before Start, or those jobs stay RUNNING.
func (s *Service) Stop(ctx context.Context) error {
	s.mu.Lock()
	started, unwatch := s.started, s.unwatch
	s.mu.Unlock()

	s.stopLoop()
	if !started {
		s.stopRuns(errShutdown)
		return nil
	}
	var err error
	select {
	case <-s.stopped:
	case <-ctx.Done():
		err = ctx.Err()
		s.stopRuns(errShutdown)
		<-s.stopped
	}
	if unwatch != nil {
		unwatch()
	}
	s.stopRuns(errShutdown)
	return err
}

// Cancel cancels a job at the user's request. A running job gets its handler context cancelled and becomes
// CANCELLED when the handler returns; if the handler finishes successfully anyway, the job is COMPLETED. A queued or
// waiting job is cancelled directly. ErrNotCancellable refuses:
//
//   - a finished job;
//   - a running job of an Uncancellable type;
//   - a RUNNING job that no handler of this runner runs: left by a crash or a Stop, of an unregistered type, or
//     claimed a moment ago and not started yet. Only Recover may move a stale job, so a side effect is never hidden
//     behind CANCELLED; a just-claimed job can be cancelled again once its handler runs.
//
// A run whose attempt is over is judged by the stored state: RUNNING until its final save, then the outcome.
//
// There is a narrow window as an attempt ends (its handler returned, or the shutdown ended its context): Cancel still
// returns nil, but the outcome of the attempt stands (for example COMPLETED, or requeued at shutdown).
func (s *Service) Cancel(ctx context.Context, id shared.JobID) error {
	var err error
	for range changeAttempts {
		// Looked up on every pass: after a conflict the job may have been claimed by a worker.
		s.mu.Lock()
		r := s.running[id]
		s.mu.Unlock()
		// A run that has ended its attempt only waits for its final save and cleanup: cancelling its context does
		// nothing, so the stored state decides (a finished job is not cancellable, a RETRY_WAIT one is).
		if r != nil && !r.ended.Load() {
			return r.cancelByUser()
		}

		var j *job.Job
		if j, err = s.jobs.Get(ctx, id); err != nil {
			return err
		}
		if j.Status() == job.StatusRunning {
			return fmt.Errorf("%w: %s is RUNNING but not cancellable now: its attempt is ending or another process runs it; startup recovery handles the latter", ErrNotCancellable, id)
		}
		if err = j.Cancel(s.clock.Now()); err != nil {
			return fmt.Errorf("%w: %w", ErrNotCancellable, err)
		}
		if err = s.jobs.Save(ctx, j); !errors.Is(err, shared.ErrVersionConflict) {
			if err == nil {
				s.jobFinished(ctx, j.Snapshot())
			}
			return err
		}
	}
	return err
}

// worker claims and runs jobs until the loop context ends.
func (s *Service) worker() {
	defer s.wg.Done()
	for s.loopCtx.Err() == nil {
		worked, err := s.step()
		if err != nil && s.loopCtx.Err() == nil {
			s.log.Error("job claim failed", slog.Any("error", err))
		}
		if !worked || err != nil {
			_ = s.sleeper.Sleep(s.loopCtx, s.cfg.PollInterval)
		}
	}
}

// keeper moves RETRY_WAIT jobs whose backoff has elapsed, and network and quota waits that are due for a recheck, back
// to QUEUED and applies the power policy every poll.
func (s *Service) keeper() {
	defer s.wg.Done()
	for s.loopCtx.Err() == nil {
		if err := s.promoteDue(s.loopCtx); err != nil && s.loopCtx.Err() == nil {
			s.log.Error("promoting due retries failed", slog.Any("error", err))
		}
		s.enforcePower(s.loopCtx)
		_ = s.sleeper.Sleep(s.loopCtx, s.cfg.PollInterval)
	}
}

func (s *Service) promoteDue(ctx context.Context) error {
	waiting, err := s.jobs.ListByStatus(ctx, job.StatusRetryWait, job.StatusWaitingForNetwork, job.StatusWaitingForQuota)
	if err != nil {
		return err
	}
	now := s.clock.Now()
	for _, j := range waiting {
		// A retry is due by its backoff time (Requeue checks it); a network or quota wait by the recheck interval.
		if j.Status() != job.StatusRetryWait && !s.cfg.Rechecks.Due(j.Status(), j.UpdatedAt(), now) {
			continue
		}
		if err := j.Requeue(now); err != nil {
			if errors.Is(err, job.ErrNotDue) {
				continue
			}
			s.log.Error("requeue of a due retry refused", slog.String("job", j.ID().String()), slog.Any("error", err))
			continue
		}
		// A conflict means the job was cancelled or requeued meanwhile; nothing to do.
		if err := s.jobs.Save(ctx, j); err != nil && !errors.Is(err, shared.ErrConflict) {
			return err
		}
	}
	return nil
}

// step claims one due job and runs it to the end. worked is false when nothing was eligible.
//
// A heavy slot is reserved before the claim, so two workers cannot both take the last one; without a free slot only
// light types are claimed. The slot is kept until the attempt has ended (execute returns after the final save, and
// a handler panic is caught inside it), and given back at once when the claim brought a light job or nothing.
func (s *Service) step() (worked bool, err error) {
	cond := s.conditions()
	filter := s.powerPolicy.Filter(cond)
	slot := s.reserveHeavy()
	defer func() {
		if slot {
			s.releaseHeavy()
		}
	}()
	if !slot {
		if s.lightOnly.IsZero() {
			return false, nil
		}
		filter = s.lightOnly
	}

	j, ok, err := s.jobs.Claim(s.loopCtx, s.clock.Now(), filter)
	if err != nil || !ok {
		return false, err
	}
	if slot && s.powerPolicy.ClassOf(j.Type()) != powerpolicy.ClassHeavy {
		s.releaseHeavy()
		slot = false
	}
	// forced uses cond from before the claim: a Run-now job claimed on AC is not forced and stops at a checkpoint if
	// power drops later (PRD 7.4); only a start that needed the override is protected (AC-22).
	s.execute(j, s.powerPolicy.StartedByOverride(j.Type(), cond))
	return true, nil
}

// reserveHeavy takes a heavy slot if one is free.
func (s *Service) reserveHeavy() bool {
	select {
	case s.heavySlots <- struct{}{}:
		return true
	default:
		return false
	}
}

func (s *Service) releaseHeavy() { <-s.heavySlots }

// run is one attempt of a claimed job.
type run struct {
	svc           *Service
	typ           job.Type
	uncancellable bool
	recovery      Recovery
	forced        bool // started by the "Run now" override: power loss does not stop this attempt
	cancel        context.CancelCauseFunc
	ended         atomic.Bool // the attempt is over and being finished: cancelling the context changes nothing

	mu       sync.Mutex // guards job, finished, lost and stopping: reports may come from several goroutines
	job      *job.Job
	finished bool
	lost     error
	stopping bool // a report returned powerpolicy.ErrPowerLost: the attempt ends in WAITING_FOR_POWER
}

// cancelByUser ends the handler context with the user cancellation, unless the type is not safe to interrupt.
func (r *run) cancelByUser() error {
	if r.uncancellable {
		return fmt.Errorf("%w: %s handler is not safe to interrupt", ErrNotCancellable, r.typ)
	}
	r.cancel(errCancelled)
	return nil
}

// report is the progress callback handed to the handler.
func (r *run) report(ctx context.Context, p job.Progress) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.finished {
		return ErrJobFinished
	}
	if r.lost != nil {
		return r.lost
	}
	if err := r.job.ReportProgress(p, r.svc.clock.Now()); err != nil {
		return err
	}
	if err := r.svc.jobs.Save(ctx, r.job); err != nil {
		if errors.Is(err, shared.ErrConflict) || errors.Is(err, shared.ErrNotFound) {
			r.lost = err
			r.cancel(errLost)
		}
		return err
	}

	// Checked after the save, so the checkpoint the job resumes from is stored. Once stopping, it stays stopping.
	if r.stopping || r.svc.mustWaitForPower(r) {
		r.stopping = true
		return powerpolicy.ErrPowerLost
	}
	return nil
}

func (s *Service) execute(j *job.Job, forced bool) {
	s.mu.Lock()
	reg, ok := s.handlers[j.Type()]
	s.mu.Unlock()

	ctx, cancel := context.WithCancelCause(s.runCtx)
	r := &run{svc: s, typ: j.Type(), uncancellable: reg.Uncancellable, recovery: reg.Recovery, forced: forced, cancel: cancel, job: j}
	s.mu.Lock()
	s.running[j.ID()] = r
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		// The job may be requeued and claimed by another worker before this line; keep that run's entry.
		if s.running[j.ID()] == r {
			delete(s.running, j.ID())
		}
		s.mu.Unlock()
		cancel(errFinished)
	}()

	// A change by another writer between the claim and the registration above went to the database only; look again.
	if cur, err := s.jobs.Get(ctx, j.ID()); err == nil && (cur.Version() != j.Version() || cur.Status() != job.StatusRunning) {
		s.log.Info("claimed job was changed elsewhere", slog.String("job", j.ID().String()), slog.String("outcome", outcomeDropped))
		return
	}

	var herr error
	timeout := reg.Timeout
	if timeout == 0 {
		timeout = s.cfg.DefaultTimeout
	}
	timerDone := make(chan struct{})
	go func() {
		defer close(timerDone)
		if s.sleeper.Sleep(ctx, timeout) == nil {
			cancel(errTimeout)
		}
	}()
	if ok {
		herr = s.call(ctx, reg.Handler, r)
	} else {
		herr = backoff.Permanent(fmt.Errorf("no handler for job type %q", j.Type()))
	}
	var cause error
	if ctx.Err() != nil {
		cause = context.Cause(ctx)
	}
	cancel(errFinished)
	<-timerDone

	s.finish(r, herr, cause, timeout)
}

// call runs the handler and turns a panic into a permanent error: a bug that panicked once will panic again.
func (s *Service) call(ctx context.Context, h Handler, r *run) (err error) {
	snap := r.job.Snapshot()
	defer func() {
		if p := recover(); p != nil {
			// The value may carry user data; only the log gets it, the stored error gets just the type.
			s.log.Error("job handler panicked", slog.String("job", snap.ID().String()), slog.String("type", fmt.Sprintf("%T", p)),
				slog.Any("panic", p), slog.String("stack", string(debug.Stack())))
			err = backoff.Permanent(fmt.Errorf("handler panic: %T", p))
		}
	}()
	return h.Handle(ctx, snap, r.report)
}

// finish applies the outcome of the attempt through the aggregate and saves it.
func (s *Service) finish(r *run, herr, cause error, timeout time.Duration) {
	r.ended.Store(true)
	// Set when a terminal status was saved; the observer runs after r.mu is released (deferred calls run in reverse).
	var finished *job.Snapshot
	defer func() {
		if finished != nil {
			s.jobFinished(s.runCtx, *finished)
		}
	}()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.finished = true
	j := r.job
	id, attempt := j.ID().String(), j.Attempts()

	if r.lost != nil {
		s.log.Info("job was changed elsewhere, outcome dropped", slog.String("job", id), slog.String("outcome", outcomeDropped))
		return
	}
	// The save must happen even when the runner is shutting down.
	saveCtx := context.WithoutCancel(s.runCtx)
	waitPower := r.stopping && errors.Is(herr, powerpolicy.ErrPowerLost)
	outcome, err := s.transition(saveCtx, j, herr, cause, timeout, r.recovery, waitPower)
	if err != nil {
		s.log.Error("job transition refused", slog.String("job", id), slog.String("outcome", outcome), slog.Any("error", err))
		return
	}
	if outcome == outcomeLeftRunning {
		s.log.Info("job interrupted by shutdown, left RUNNING for recovery at the next start", slog.String("job", id),
			slog.String("type", string(j.Type())), slog.String("outcome", outcome))
		return
	}
	// The aggregate decides the final status: an exhausted job fails whatever the runner meant to do.
	if j.Status() == job.StatusFailed {
		outcome = outcomeFailed
	}

	switch err := s.saveFinal(saveCtx, j); {
	case err == nil:
		s.log.Info("job attempt finished", slog.String("job", id), slog.String("type", string(j.Type())),
			slog.Int("attempt", attempt), slog.String("outcome", outcome), slog.String("status", j.Status().String()))
		if j.Status().IsTerminal() {
			snap := j.Snapshot()
			finished = &snap
		}
	case errors.Is(err, shared.ErrConflict) || errors.Is(err, shared.ErrNotFound):
		s.log.Info("job was changed elsewhere, outcome dropped", slog.String("job", id), slog.String("outcome", outcomeDropped))
	default:
		s.log.Error("saving the job failed; it stays RUNNING until startup recovery", slog.String("job", id), slog.Any("error", err))
	}
}

// saveFinal saves the final state of an attempt, retrying other errors than a conflict or a missing job (those mean
// someone else owns the job now) a few times with a doubling delay. The version is unchanged after a failed save, so
// the same aggregate can be saved again.
func (s *Service) saveFinal(ctx context.Context, j *job.Job) error {
	delay := finalSaveRetryBase
	for attempt := 1; ; attempt++ {
		err := s.jobs.Save(ctx, j)
		if err == nil || errors.Is(err, shared.ErrConflict) || errors.Is(err, shared.ErrNotFound) || attempt == finalSaveAttempts {
			return err
		}

		s.log.Warn("saving the job failed, retrying", slog.String("job", j.ID().String()), slog.Int("save_attempt", attempt), slog.Any("error", err))
		if s.sleeper.Sleep(ctx, delay) != nil {
			return err
		}
		delay *= 2
	}
}

// transition picks the aggregate method for the result of the attempt. cause is why the handler context ended, if
// it did; waitPower means the handler stopped at a checkpoint because AC power was lost. An interrupted job that is
// not safe to requeue is not changed: outcomeLeftRunning.
func (s *Service) transition(ctx context.Context, j *job.Job, herr, cause error, timeout time.Duration, recovery Recovery,
	waitPower bool) (string, error) {
	now := s.clock.Now()
	wait, waits := backoff.WaitStatus(herr)
	switch {
	case herr == nil:
		return outcomeCompleted, j.Complete(now)
	case errors.Is(cause, errCancelled):
		return outcomeCancelled, j.Cancel(now)
	case waitPower:
		// The checkpoint is saved and waiting is not a failure: the aggregate gives the attempt back.
		return outcomeWaitingPower, j.WaitFor(job.StatusWaitingForPower, now)
	case cause == nil && waits:
		// No network or no quota (PRD sections 7.2 and 9.2): the job waits without using an attempt, and the keeper
		// tries it again after Config.Rechecks.
		return outcomeWaiting, j.WaitFor(wait, now)
	case backoff.Classify(herr) == backoff.ClassPermanent:
		return outcomeFailed, j.Fail(now, herr.Error())
	case cause != nil && !errors.Is(cause, errTimeout) && recovery != RecoveryRequeue:
		return outcomeLeftRunning, nil
	case cause != nil && !errors.Is(cause, errTimeout):
		return outcomeInterrupted, j.Requeue(now)
	}

	// Every policy retries here, RecoveryReconcile too, even when a side effect may have happened: the runner does
	// not check it. Such a handler MUST return backoff.Permanent or record the unknown outcome in its own aggregate
	// when the send outcome is ambiguous (Registration.Reconciler, TASK-044).
	msg := herr.Error()
	if errors.Is(cause, errTimeout) {
		msg = fmt.Sprintf("attempt timed out after %v: %s", timeout, msg)
	}
	delay, retry := s.policy.Next(j.Attempts(), s.jitterUnit(ctx))
	if !retry {
		return outcomeFailed, j.Fail(now, "attempts exhausted: "+msg)
	}
	return outcomeRetry, j.Retry(now, now.Add(delay), msg)
}

// jitterUnit returns a number in [0, 1) for the backoff; any problem with the source means no jitter.
func (s *Service) jitterUnit(ctx context.Context) float64 {
	if s.jitter == nil {
		return 0
	}
	n, err := s.jitter.Permille(ctx)
	if err != nil {
		s.log.Warn("jitter source failed, retrying without jitter", slog.Any("error", err))
		return 0
	}
	return float64(n) / permilleScale
}
