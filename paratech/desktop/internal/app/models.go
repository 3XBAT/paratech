package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"paratech/desktop/internal/cases/modelcatalog"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/values"
	"paratech/desktop/internal/input/wails"
)

// errManagerClosed refuses model work once the shutdown began.
var errManagerClosed = errors.New("app: the model manager is closed")

// modelManager is what the models page binds to (wails.ModelCatalogUseCase, ModelDownloads, ModelImports) over the
// model catalog use cases. On top of mapping it owns the work that outlives a call: one background download at a
// time, and the transcription pass after a model is installed or made the default, so that recordings waiting for a
// model get their STT job without a restart. close cancels and waits for all of it before the storage closes.
type modelManager struct {
	models    *modelcatalog.Service
	downloads *modelcatalog.Downloader
	imports   *modelcatalog.Importer
	pending   transcriptionLife
	log       *slog.Logger
	timeout   time.Duration // bounds the wait in close

	// ready is closed after the startup cleanups: no artifact is written before them (ADR-0009).
	ready     chan struct{}
	readyOnce sync.Once

	ctx    context.Context // ends at close
	cancel context.CancelFunc
	work   sync.WaitGroup // calls, the download and the pass; Add only under mu while not closed

	mu       sync.Mutex
	closed   bool
	run      *downloadRun // the running download, nil when none
	state    wails.DownloadState
	passing  bool // a transcription pass runs
	passMore bool // another pass was asked for while one ran
}

// downloadRun is one background download.
type downloadRun struct {
	id        values.CatalogID
	stop      context.CancelFunc
	cancelled bool
}

func newModelManager(c *core, pending transcriptionLife, log *slog.Logger, timeout time.Duration) *modelManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &modelManager{
		models: c.models, downloads: c.downloads, imports: c.imports, pending: pending, log: log, timeout: timeout,
		ready: make(chan struct{}), ctx: ctx, cancel: cancel,
	}
}

// markReady opens the writes; the lifecycle calls it after the startup cleanups (a cleanup step of its own).
func (m *modelManager) markReady(context.Context) (int, error) {
	m.readyOnce.Do(func() { close(m.ready) })
	return 0, nil
}

// ListAvailable implements wails.ModelCatalogUseCase.
func (m *modelManager) ListAvailable(ctx context.Context, machine values.Machine) ([]modelcatalog.AvailableModel, error) {
	return m.models.ListAvailable(ctx, machine)
}

// ListInstalled implements wails.ModelCatalogUseCase.
func (m *modelManager) ListInstalled(ctx context.Context) ([]modelcatalog.InstalledModel, error) {
	return m.models.ListInstalled(ctx)
}

// SourcePage implements wails.ModelCatalogUseCase.
func (m *modelManager) SourcePage(ctx context.Context, id values.CatalogID) (values.SourceURL, error) {
	return m.models.SourcePage(ctx, id)
}

// CurrentModel implements wails.ModelCatalogUseCase.
func (m *modelManager) CurrentModel(ctx context.Context) (modelcatalog.PinnedModel, error) {
	return m.models.CurrentModel(ctx)
}

// SetDefault makes the model the default, then runs the transcription pass: with several models and no default the
// waiting recordings had no model to pin.
func (m *modelManager) SetDefault(ctx context.Context, id shared.SttModelID) error {
	ctx, done, err := m.begin(ctx, false)
	if err != nil {
		return err
	}
	defer done()
	if err := m.models.SetDefault(ctx, id); err != nil {
		return err
	}
	m.passAfter("default model selected")
	return nil
}

// Delete implements wails.ModelCatalogUseCase.
func (m *modelManager) Delete(ctx context.Context, id shared.SttModelID) error {
	ctx, done, err := m.begin(ctx, true)
	if err != nil {
		return err
	}
	defer done()
	return m.models.Delete(ctx, id)
}

// Preflight implements wails.ModelDownloads.
func (m *modelManager) Preflight(ctx context.Context, id values.CatalogID) (modelcatalog.DownloadPlan, error) {
	ctx, done, err := m.begin(ctx, false)
	if err != nil {
		return modelcatalog.DownloadPlan{}, err
	}
	defer done()
	return m.downloads.Preflight(ctx, id)
}

// DiscardPartial implements wails.ModelDownloads.
func (m *modelManager) DiscardPartial(ctx context.Context, id values.CatalogID) error {
	ctx, done, err := m.begin(ctx, true)
	if err != nil {
		return err
	}
	defer done()
	return m.downloads.DiscardPartial(ctx, id)
}

// StartDownload starts the download of a catalog model on a goroutine of the manager; one at a time, a second one is
// modelcatalog.ErrDownloadInProgress. Its progress is DownloadState.
func (m *modelManager) StartDownload(id values.CatalogID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	switch {
	case m.closed:
		return errManagerClosed
	case m.run != nil:
		return fmt.Errorf("app: download %s: %w", m.run.id, modelcatalog.ErrDownloadInProgress)
	}

	ctx, stop := context.WithCancel(m.ctx)
	r := &downloadRun{id: id, stop: stop}
	m.run = r
	m.state = wails.DownloadState{CatalogID: id, Phase: modelcatalog.PhaseDownloading, Running: true}
	events, unsubscribe := m.downloads.Subscribe()
	m.work.Add(2)
	go func() {
		defer m.work.Done()
		for p := range events {
			m.progress(r, p)
		}
	}()
	go func() {
		defer m.work.Done()
		defer stop()
		_, err := m.download(ctx, id)
		unsubscribe()
		m.finish(r, err)
	}()
	return nil
}

// download waits for the startup cleanups, then downloads.
func (m *modelManager) download(ctx context.Context, id values.CatalogID) (modelcatalog.InstalledModel, error) {
	select {
	case <-m.ready:
	case <-ctx.Done():
		return modelcatalog.InstalledModel{}, fmt.Errorf("%w: %w", modelcatalog.ErrDownloadInterrupted, ctx.Err())
	}
	return m.downloads.Download(ctx, id)
}

// CancelDownload stops the running download; its bytes are kept for a resume.
func (m *modelManager) CancelDownload() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.run != nil {
		m.run.cancelled = true
		m.run.stop()
	}
}

// DownloadState returns the latest state of the current or last download.
func (m *modelManager) DownloadState() wails.DownloadState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state
}

// progress records an event of the running download r; events of a finished run are late and dropped.
func (m *modelManager) progress(r *downloadRun, p modelcatalog.DownloadProgress) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.run != r || p.CatalogID != r.id || p.Phase == modelcatalog.PhaseInstalled || p.Phase == modelcatalog.PhaseFailed {
		return // finish records the outcome
	}
	m.state.Phase, m.state.DoneBytes, m.state.TotalBytes = p.Phase, p.DoneBytes, p.TotalBytes
}

// finish records the outcome of r and, after an install, runs the transcription pass.
func (m *modelManager) finish(r *downloadRun, err error) {
	m.mu.Lock()
	m.run = nil
	st := m.state
	st.Running, st.Cancelled, st.Err = false, r.cancelled && err != nil, err
	if err == nil {
		st.Phase, st.DoneBytes = modelcatalog.PhaseInstalled, st.TotalBytes
	} else {
		st.Phase = modelcatalog.PhaseFailed
	}
	m.state = st
	m.mu.Unlock()

	switch {
	case err == nil:
		m.log.Info("model downloaded", "catalog_id", string(r.id))
		m.passAfter("model downloaded")
	case r.cancelled:
		m.log.Info("model download cancelled", "catalog_id", string(r.id))
	default:
		m.log.Warn("model download failed", "catalog_id", string(r.id), "err", err)
	}
}

// Inspect implements wails.ModelImports.
func (m *modelManager) Inspect(ctx context.Context, path string) (modelcatalog.ImportPreview, error) {
	ctx, done, err := m.begin(ctx, false)
	if err != nil {
		return modelcatalog.ImportPreview{}, err
	}
	defer done()
	return m.imports.Inspect(ctx, path)
}

// ImportCatalog installs the file as a catalog model, then runs the transcription pass.
func (m *modelManager) ImportCatalog(ctx context.Context, id values.CatalogID, path string) (modelcatalog.InstalledModel, error) {
	ctx, done, err := m.begin(ctx, true)
	if err != nil {
		return modelcatalog.InstalledModel{}, err
	}
	defer done()
	im, err := m.imports.ImportCatalog(ctx, id, path)
	if err != nil {
		return modelcatalog.InstalledModel{}, err
	}
	m.passAfter("catalog model imported")
	return im, nil
}

// ImportCustom installs a file outside the catalog, then runs the transcription pass.
func (m *modelManager) ImportCustom(ctx context.Context, req modelcatalog.CustomImport) (modelcatalog.InstalledModel, error) {
	ctx, done, err := m.begin(ctx, true)
	if err != nil {
		return modelcatalog.InstalledModel{}, err
	}
	defer done()
	im, err := m.imports.ImportCustom(ctx, req)
	if err != nil {
		return modelcatalog.InstalledModel{}, err
	}
	m.passAfter("model imported")
	return im, nil
}

// begin counts a call as work of the manager and returns its context, which also ends at close. A call that writes
// artifacts first waits for the startup cleanups.
func (m *modelManager) begin(ctx context.Context, writes bool) (context.Context, func(), error) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, nil, errManagerClosed
	}
	m.work.Add(1)
	m.mu.Unlock()

	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(m.ctx, cancel)
	done := func() {
		stop()
		cancel()
		m.work.Done()
	}
	if writes {
		select {
		case <-m.ready:
		case <-ctx.Done():
			done()
			return nil, nil, ctx.Err()
		}
	}
	return ctx, done, nil
}

// passAfter runs the transcription pass in the background. Passes never overlap: one asked for while another runs
// runs once after it, so a model installed during a pass is seen.
func (m *modelManager) passAfter(why string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	if m.passing {
		m.passMore = true
		return
	}
	m.passing = true
	m.work.Add(1)
	go m.passes(why)
}

func (m *modelManager) passes(why string) {
	defer m.work.Done()
	for {
		// Idempotent by the job key, like the startup sweep it may race.
		n, err := m.pending.EnqueuePending(m.ctx)
		switch {
		case err != nil && m.ctx.Err() == nil:
			m.log.Warn("enqueueing the transcription of waiting recordings failed", "after", why, "enqueued", n, "err", err)
		case n > 0:
			m.log.Info("transcription jobs enqueued for waiting recordings", "after", why, "jobs", n)
		}

		m.mu.Lock()
		if !m.passMore || m.closed {
			m.passing = false
			m.mu.Unlock()
			return
		}
		m.passMore = false
		m.mu.Unlock()
	}
}

// close refuses new work, cancels the running work and waits for it within the timeout. It is safe to repeat.
func (m *modelManager) close() error {
	m.mu.Lock()
	m.closed = true
	m.mu.Unlock()
	m.cancel()

	done := make(chan struct{})
	go func() {
		m.work.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-time.After(m.timeout):
		return errors.New("app: model work did not stop in time")
	}
}
