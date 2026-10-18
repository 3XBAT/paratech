package speechcomponent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"

	"paratech/desktop/internal/domain/transcription/values"
)

// Operation names, used in error messages.
const (
	opDetect  = "speechcomponent: detect"
	opInstall = "speechcomponent: install"
)

// Service finds, installs and activates the speech component. One install runs at a time, in the background; the
// pages poll Status. It is safe for concurrent use.
type Service struct {
	spec      values.SpeechComponent
	source    ArchiveSource
	space     DiskSpace
	files     ComponentFiles
	activator Activator
	log       *slog.Logger

	// ready is closed after the startup cleanups of the component folder: no install writes before them.
	ready     chan struct{}
	readyOnce sync.Once

	ctx    context.Context // ends at Close
	cancel context.CancelFunc
	work   sync.WaitGroup // the install goroutine; Add only under mu while not closed

	activity sync.Mutex // serialises looking at the folders and activating

	mu      sync.Mutex
	closed  bool
	found   values.ComponentState // of the last look at the folders; zero before Detect
	active  bool
	lastErr error // why the last install or activation failed; nil after a success or a cancel
	run     *installRun
	stage   Stage
	done    int64
}

// installRun is one background install.
type installRun struct {
	stop      context.CancelFunc
	cancelled bool
}

// New checks the ports.
func New(d Deps) (*Service, error) {
	if d.Source == nil || d.Space == nil || d.Files == nil || d.Activator == nil || len(d.Spec.Files()) == 0 {
		return nil, fmt.Errorf("%w: the component and every port are required", ErrInvalidConfig)
	}
	log := d.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Service{
		spec: d.Spec, source: d.Source, space: d.Space, files: d.Files, activator: d.Activator, log: log,
		ready: make(chan struct{}), ctx: ctx, cancel: cancel,
	}, nil
}

// MarkReady allows installs; the app calls it after the startup cleanups of the component folder.
func (s *Service) MarkReady() {
	s.readyOnce.Do(func() { close(s.ready) })
}

// Detect looks at the folders in values.ComponentFolders order and activates speech recognition from the first one
// whose files verify. The app calls it once at start. A folder that cannot be read counts as damaged; the error is
// returned for the log, the status still says what was found.
func (s *Service) Detect(ctx context.Context) (Status, error) {
	err := s.lookAndActivate(ctx)
	if err != nil {
		err = fmt.Errorf("%s: %w", opDetect, err)
	}
	return s.Status(), err
}

// lookAndActivate assesses the folders and activates the first ready one unless speech recognition is active.
func (s *Service) lookAndActivate(ctx context.Context) error {
	s.activity.Lock()
	defer s.activity.Unlock()

	folders := values.ComponentFolders()
	states := make([]values.ComponentState, len(folders))
	var errs []error
	for i, f := range folders {
		found, err := s.files.Inspect(ctx, f, s.spec)
		if err != nil {
			states[i] = values.ComponentDamaged
			errs = append(errs, fmt.Errorf("%s folder: %w", f, err))
			continue
		}
		states[i] = s.spec.Assess(found)
		s.log.Info("speech component folder checked", "folder", f.String(), "state", states[i].String())
	}
	state, index := values.CombineFolders(states)

	s.mu.Lock()
	s.found = state
	active := s.active
	s.mu.Unlock()
	if active || state != values.ComponentReady {
		return errors.Join(errs...)
	}

	if err := s.activator.Activate(ctx, folders[index]); err != nil {
		err = fmt.Errorf("%w: %w", ErrActivationFailed, err)
		s.mu.Lock()
		s.lastErr = err
		s.mu.Unlock()
		return errors.Join(append(errs, err)...)
	}
	s.mu.Lock()
	s.active, s.lastErr = true, nil
	s.mu.Unlock()
	s.log.Info("speech recognition activated", "folder", folders[index].String(), "version", s.spec.Version())
	return errors.Join(errs...)
}

// Status returns what the pages show.
func (s *Service) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.run != nil:
		return Status{State: StateInstalling, Stage: s.stage, DoneBytes: s.done, TotalBytes: s.spec.ArchiveSize()}
	case s.active:
		return Status{State: StateReady}
	case s.found == values.ComponentReady:
		// Verified files that are not active: the activation failed or was cut short. This wins over a later install
		// failure, since reinstalling does not fix an activation.
		err := s.lastErr
		if !errors.Is(err, ErrActivationFailed) {
			err = ErrActivationFailed
		}
		return Status{State: StateFailed, Err: err}
	case s.lastErr != nil:
		return Status{State: StateFailed, Err: s.lastErr}
	case s.found == values.ComponentDamaged:
		return Status{State: StateDamaged}
	}
	return Status{State: StateMissing}
}

// StartInstall starts an install in the background; Status shows its progress. It refuses a second install
// (ErrInstallInProgress), an install while speech recognition works (ErrAlreadyActive) and one after Close.
func (s *Service) StartInstall() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.closed:
		return ErrClosed
	case s.run != nil:
		return ErrInstallInProgress
	case s.active:
		return ErrAlreadyActive
	}

	ctx, stop := context.WithCancel(s.ctx)
	r := &installRun{stop: stop}
	s.run, s.stage, s.done, s.lastErr = r, StageDownloading, 0, nil
	s.work.Add(1)
	go func() {
		defer s.work.Done()
		defer stop()
		s.finish(r, s.install(ctx))
	}()
	return nil
}

// CancelInstall stops the running install; the component stays as it was.
func (s *Service) CancelInstall() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.run != nil {
		s.run.cancelled = true
		s.run.stop()
	}
}

// finish records the outcome of r.
func (s *Service) finish(r *installRun, err error) {
	s.mu.Lock()
	s.run = nil
	switch {
	case err == nil:
	case r.cancelled:
		s.lastErr = nil
	default:
		s.lastErr = err
	}
	s.mu.Unlock()

	switch {
	case err == nil:
		s.log.Info("speech component installed", "version", s.spec.Version())
	case r.cancelled:
		s.log.Info("speech component install cancelled")
	default:
		s.log.Warn("speech component install failed", "err", err)
	}
}

// install waits for the startup cleanups, then downloads, verifies, unpacks and activates.
func (s *Service) install(ctx context.Context) error {
	select {
	case <-s.ready:
	case <-ctx.Done():
		return ctx.Err()
	}
	// The archive is not needed after the attempt, whatever its outcome; a cancelled ctx must not keep it.
	defer func() {
		if err := s.files.DiscardArchive(context.WithoutCancel(ctx)); err != nil {
			s.log.Warn("removing the speech component archive failed", "err", err)
		}
	}()

	if err := s.download(ctx); err != nil {
		return fmt.Errorf("%s: %w", opInstall, err)
	}

	s.setStage(StageUnpacking)
	if err := s.files.Unpack(ctx, s.spec); err != nil {
		return fmt.Errorf("%s: %w", opInstall, err)
	}

	// The files are in place now: a late cancel no longer helps, so the activation runs to its end.
	s.setStage(StageActivating)
	if err := s.lookAndActivate(context.WithoutCancel(ctx)); err != nil && !s.isActive() {
		return fmt.Errorf("%s: %w", opInstall, err)
	}
	if !s.isActive() {
		return fmt.Errorf("%s: %w", opInstall, ErrNotVerified)
	}
	return nil
}

// download checks the disk space, fetches the archive from the allowed hosts and verifies its size and SHA-256.
func (s *Service) download(ctx context.Context) error {
	free, err := s.space.FreeBytes(ctx)
	if err != nil {
		return err
	}
	if err := s.spec.CheckDiskSpace(free); err != nil {
		return err
	}
	allow := s.spec.DownloadHosts()
	if err := allow.Check(s.spec.SourceURL().String()); err != nil {
		return err
	}

	reply, body, err := s.source.Fetch(ctx, s.spec.SourceURL(), 0, allow)
	switch {
	case err == nil:
	case ctx.Err() != nil:
		return ctx.Err()
	case errors.Is(err, values.ErrHostNotAllowed) || errors.Is(err, values.ErrInvalidSourceURL):
		return err
	default:
		return fmt.Errorf("%w: %w", ErrDownloadFailed, err)
	}
	defer func() { _ = body.Close() }()
	if err := s.spec.CheckReply(reply); err != nil {
		return err
	}

	// One byte over the pinned size is enough to tell another file.
	pr := &progressReader{r: body, report: s.setDone}
	n, sum, err := s.files.SaveArchive(ctx, io.LimitReader(pr, s.spec.ArchiveSize()+1))
	switch {
	case err == nil:
	case ctx.Err() != nil:
		return ctx.Err()
	case pr.err != nil:
		return fmt.Errorf("%w: %w", ErrDownloadFailed, err)
	default:
		return err // the disk failed, not the transfer
	}

	s.setStage(StageVerifying)
	if n < s.spec.ArchiveSize() {
		return fmt.Errorf("%w: the source ended at %d of %d bytes", ErrDownloadFailed, n, s.spec.ArchiveSize())
	}
	return s.spec.VerifyArchive(sum, n)
}

func (s *Service) isActive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active
}

func (s *Service) setStage(st Stage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stage = st
}

func (s *Service) setDone(n int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.done = n
}

// Close refuses new installs, cancels the running one and waits for it until ctx ends. It is safe to repeat.
func (s *Service) Close(ctx context.Context) error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.cancel()

	done := make(chan struct{})
	go func() {
		s.work.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("speechcomponent: the install did not stop: %w", ctx.Err())
	}
}

// progressReader reports the bytes read so far and remembers a read error of the source.
type progressReader struct {
	r      io.Reader
	done   int64
	report func(done int64)
	err    error
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if n > 0 {
		p.done += int64(n)
		p.report(p.done)
	}
	if err != nil && !errors.Is(err, io.EOF) {
		p.err = err
	}
	return n, err
}
