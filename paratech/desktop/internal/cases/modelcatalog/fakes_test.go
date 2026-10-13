package modelcatalog

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"maps"
	"sort"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/settings/settings"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/sttmodel"
	"paratech/desktop/internal/domain/transcription/values"
	"paratech/desktop/testenv"
)

// world is an in-memory store behind all fake ports. It follows the SQLite contract the use case relies on: version
// compare-and-swap, a unique (name, version), the RESTRICT of the settings default, serialized transactions with
// rollback, and writes only inside Tx.Do.
type world struct {
	t *testing.T

	mu       sync.Mutex
	models   map[shared.SttModelID]sttmodel.State
	settings settings.State // Version 0: no row yet
	pins     map[shared.JobID]pinRow
	files    map[shared.ArtifactRef][]byte
	manifest []byte
	nextID   int

	// Fault injection.
	manifestErr       error
	settingsConflicts int    // the next saves of the settings fail with a version conflict
	fileDeleteErr     error  // returned by the file delete
	beforeTx          func() // runs at the start of every outermost transaction, outside every lock
	onRead            func() // runs before every read of an opened file

	txMu sync.Mutex
}

type pinRow struct {
	model  shared.SttModelID
	status job.Status
}

type txKey struct{}

func inTx(ctx context.Context) bool { return ctx.Value(txKey{}) != nil }

func newWorld(t *testing.T, manifest []byte) *world {
	return &world{
		t: t, models: map[shared.SttModelID]sttmodel.State{}, pins: map[shared.JobID]pinRow{},
		files: map[shared.ArtifactRef][]byte{}, manifest: manifest,
	}
}

func (w *world) requireTx(ctx context.Context, op string) {
	if !inTx(ctx) {
		w.t.Errorf("%s outside Tx.Do", op)
	}
}

// requireNoTx guards the Tx contract: no file I/O while the write lock is held.
func (w *world) requireNoTx(ctx context.Context, op string) {
	if inTx(ctx) {
		w.t.Errorf("%s inside Tx.Do", op)
	}
}

func (w *world) pin(id shared.JobID, model shared.SttModelID, status job.Status) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pins[id] = pinRow{model: model, status: status}
}

func (w *world) hasFile(ref shared.ArtifactRef) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, ok := w.files[ref]
	return ok
}

func (w *world) defaultModel() shared.SttModelID {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.settings.DefaultSttModel
}

func (w *world) hasModel(id shared.SttModelID) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, ok := w.models[id]
	return ok
}

// fakeTx serializes transactions and restores the rows when fn fails, like a rolled back SQLite transaction.
type fakeTx struct{ w *world }

func (x fakeTx) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	if inTx(ctx) {
		return fn(ctx)
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	x.w.mu.Lock()
	hook := x.w.beforeTx
	x.w.mu.Unlock()
	if hook != nil {
		hook()
	}

	x.w.txMu.Lock()
	defer x.w.txMu.Unlock()

	x.w.mu.Lock()
	models, st, pins := maps.Clone(x.w.models), x.w.settings, maps.Clone(x.w.pins)
	x.w.mu.Unlock()

	err := fn(context.WithValue(ctx, txKey{}, true))
	if err != nil {
		x.w.mu.Lock()
		x.w.models, x.w.settings, x.w.pins = models, st, pins
		x.w.mu.Unlock()
	}
	return err
}

type fakeModels struct{ w *world }

func modelState(m *sttmodel.Model, version int64) sttmodel.State {
	return sttmodel.State{
		Spec: sttmodel.Spec{
			ID: m.ID(), Name: m.Name(), ModelVersion: m.ModelVersion(), CatalogID: m.CatalogID(), File: m.File(),
			SizeBytes: m.SizeBytes(), SHA256: m.SHA256(), Quantization: m.Quantization(), Capabilities: m.Capabilities(),
		},
		Version: version, InstalledAt: m.InstalledAt(),
	}
}

func (f fakeModels) Get(_ context.Context, id shared.SttModelID) (*sttmodel.Model, error) {
	f.w.mu.Lock()
	defer f.w.mu.Unlock()
	st, ok := f.w.models[id]
	if !ok {
		return nil, fmt.Errorf("model %s: %w", id, shared.ErrNotFound)
	}
	return sttmodel.Restore(st)
}

func (f fakeModels) List(context.Context) ([]*sttmodel.Model, error) {
	f.w.mu.Lock()
	defer f.w.mu.Unlock()
	out := make([]*sttmodel.Model, 0, len(f.w.models))
	for _, st := range f.w.models {
		m, err := sttmodel.Restore(st)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Name() != b.Name() {
			return a.Name() < b.Name()
		}
		if a.ModelVersion() != b.ModelVersion() {
			return a.ModelVersion() < b.ModelVersion()
		}
		return a.ID() < b.ID()
	})
	return out, nil
}

func (f fakeModels) Save(ctx context.Context, m *sttmodel.Model) error {
	f.w.requireTx(ctx, "ModelRepository.Save")
	f.w.mu.Lock()
	defer f.w.mu.Unlock()

	cur, exists := f.w.models[m.ID()]
	if m.Version() == 0 {
		for _, other := range f.w.models {
			if other.Name == m.Name() && other.ModelVersion == m.ModelVersion() {
				return shared.ErrDuplicate
			}
		}
		if exists {
			return shared.ErrVersionConflict
		}
	} else if !exists || cur.Version != m.Version() {
		return shared.ErrVersionConflict
	}

	next := m.Version() + 1
	f.w.models[m.ID()] = modelState(m, next)
	m.Persisted(next)
	return nil
}

func (f fakeModels) Delete(ctx context.Context, id shared.SttModelID) error {
	f.w.requireTx(ctx, "ModelRepository.Delete")
	f.w.mu.Lock()
	defer f.w.mu.Unlock()

	if _, ok := f.w.models[id]; !ok {
		return fmt.Errorf("model %s: %w", id, shared.ErrNotFound)
	}
	if f.w.settings.DefaultSttModel == id {
		return fmt.Errorf("model %s is the default: %w", id, shared.ErrConflict) // ON DELETE RESTRICT
	}
	delete(f.w.models, id)
	return nil
}

type fakeSettings struct{ w *world }

func (f fakeSettings) Load(context.Context) (*settings.Settings, error) {
	f.w.mu.Lock()
	defer f.w.mu.Unlock()
	if f.w.settings.Version == 0 {
		return settings.New(), nil
	}
	return settings.Restore(f.w.settings)
}

func (f fakeSettings) Save(ctx context.Context, s *settings.Settings) error {
	f.w.requireTx(ctx, "SettingsRepository.Save")
	f.w.mu.Lock()
	defer f.w.mu.Unlock()

	if f.w.settingsConflicts > 0 {
		f.w.settingsConflicts--
		return shared.ErrVersionConflict
	}
	if s.Version() != f.w.settings.Version {
		return shared.ErrVersionConflict
	}
	if id := s.DefaultSttModel(); id != "" {
		if _, ok := f.w.models[id]; !ok {
			return fmt.Errorf("default %s is not installed: %w", id, shared.ErrConflict) // foreign key
		}
	}

	next := s.Version() + 1
	f.w.settings = settings.State{
		Version: next, RecordingPromptsEnabled: s.RecordingPromptsEnabled(), ProcessOnlyOnACPower: s.ProcessOnlyOnACPower(),
		NotifyOnProcessingComplete: s.NotifyOnProcessingComplete(), DefaultSttModel: s.DefaultSttModel(),
		UpdatedAt: s.UpdatedAt(),
	}
	s.Persisted(next)
	return nil
}

type fakePins struct{ w *world }

func (f fakePins) IsPinnedByJobInStatuses(_ context.Context, id shared.SttModelID, statuses []job.Status) (bool, error) {
	f.w.mu.Lock()
	defer f.w.mu.Unlock()
	for _, p := range f.w.pins {
		if p.model != id {
			continue
		}
		for _, s := range statuses {
			if p.status == s {
				return true, nil
			}
		}
	}
	return false, nil
}

type fakeFiles struct{ w *world }

func (f fakeFiles) Open(ctx context.Context, ref shared.ArtifactRef) (io.ReadCloser, error) {
	f.w.requireNoTx(ctx, "ArtifactStore.Open")
	f.w.mu.Lock()
	defer f.w.mu.Unlock()
	b, ok := f.w.files[ref]
	if !ok {
		return nil, shared.ErrNotFound
	}
	return io.NopCloser(hookedReader{r: bytes.NewReader(b), hook: f.w.onRead}), nil
}

// hookedReader runs hook before every read, to act in the middle of a long read.
type hookedReader struct {
	r    io.Reader
	hook func()
}

func (h hookedReader) Read(p []byte) (int, error) {
	if h.hook != nil {
		h.hook()
	}
	return h.r.Read(p)
}

func (f fakeFiles) Write(_ context.Context, ref shared.ArtifactRef, content io.Reader) error {
	b, err := io.ReadAll(content)
	if err != nil {
		return err
	}
	f.w.mu.Lock()
	defer f.w.mu.Unlock()
	f.w.files[ref] = b
	return nil
}

func (f fakeFiles) Size(ctx context.Context, ref shared.ArtifactRef) (int64, error) {
	f.w.requireNoTx(ctx, "ArtifactStore.Size")
	f.w.mu.Lock()
	defer f.w.mu.Unlock()
	b, ok := f.w.files[ref]
	if !ok {
		return 0, fmt.Errorf("file %s: %w", ref, shared.ErrNotFound)
	}
	return int64(len(b)), nil
}

func (f fakeFiles) Delete(ctx context.Context, ref shared.ArtifactRef) error {
	f.w.requireNoTx(ctx, "ArtifactStore.Delete")
	f.w.mu.Lock()
	defer f.w.mu.Unlock()
	if f.w.fileDeleteErr != nil {
		return f.w.fileDeleteErr
	}
	delete(f.w.files, ref)
	return nil
}

type fakeCatalog struct{ w *world }

func (f fakeCatalog) Manifest(context.Context) ([]byte, error) {
	f.w.mu.Lock()
	defer f.w.mu.Unlock()
	return f.w.manifest, f.w.manifestErr
}

type fakeIDs struct{ w *world }

func (f fakeIDs) NewSttModelID() shared.SttModelID {
	f.w.mu.Lock()
	defer f.w.mu.Unlock()
	f.w.nextID++
	return shared.SttModelID(fmt.Sprintf("m%d", f.w.nextID))
}

// Catalog of the tests: two preferred small models and one opt-in large model.
var (
	t0 = time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)

	catTiny  = catalogModel{id: "tiny-q5_1", name: "tiny", size: 10, cores: 1, ramMB: 512, preferred: true}
	catBase  = catalogModel{id: "base-q5_1", name: "base", size: 20, cores: 2, ramMB: 1024, preferred: true}
	catLarge = catalogModel{id: "large-v3", name: "large", size: 40, cores: 8, ramMB: 8192}
)

type catalogModel struct {
	id, name  string
	size      int64
	cores     int
	ramMB     int64
	preferred bool
}

// content is the file of the model: size zero bytes; the sizes differ, so the digests do.
func (c catalogModel) content() []byte { return make([]byte, c.size) }

func (c catalogModel) sum() values.SHA256 { return digest(c.content()) }

func digest(b []byte) values.SHA256 {
	s := sha256.Sum256(b)
	return values.SHA256(hex.EncodeToString(s[:]))
}

func (c catalogModel) json() string {
	return fmt.Sprintf(`{"id": %q, "name": %q, "version": "1", "size_bytes": %d, "sha256": %q,
		"source_url": "https://example.com/%s.bin", "quantization": "q5_1", "capabilities": ["multilingual"],
		"recommendation": {"min_cpu_cores": %d, "min_ram_mb": %d, "preferred": %v}}`,
		c.id, c.name, c.size, c.sum(), c.id, c.cores, c.ramMB, c.preferred)
}

func testManifest() []byte {
	return []byte(`{"schema_version": 1, "models": [` + catTiny.json() + `, ` + catBase.json() + `, ` + catLarge.json() + `]}`)
}

// env is a Service on the fakes.
type env struct {
	t     *testing.T
	w     *world
	clock *testenv.FakeClock
	svc   *Service
}

func newEnv(t *testing.T) *env {
	t.Helper()
	w := newWorld(t, testManifest())
	clock := testenv.NewFakeClock(t0)
	svc, err := New(Deps{
		Models: fakeModels{w}, Pins: fakePins{w}, Settings: fakeSettings{w}, Files: fakeFiles{w},
		Catalog: fakeCatalog{w}, Clock: clock, IDs: fakeIDs{w}, Tx: fakeTx{w},
	})
	if err != nil {
		t.Fatal(err)
	}
	return &env{t: t, w: w, clock: clock, svc: svc}
}

// putFile stores a file of size zero bytes at models/<name>.
func (e *env) putFile(name string, size int64) shared.ArtifactRef {
	e.t.Helper()
	return e.putBytes(name, make([]byte, size))
}

// putBytes stores content at models/<name>.
func (e *env) putBytes(name string, content []byte) shared.ArtifactRef {
	e.t.Helper()
	ref := modelRef(e.t, name)
	if err := (fakeFiles{e.w}).Write(context.Background(), ref, bytes.NewReader(content)); err != nil {
		e.t.Fatal(err)
	}
	return ref
}

func modelRef(t *testing.T, name string) shared.ArtifactRef {
	t.Helper()
	ref, err := shared.NewArtifactRef(shared.ArtifactModels, name)
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

// install stores the file of a catalog model and registers it.
func (e *env) install(c catalogModel) InstalledModel {
	e.t.Helper()
	m, err := e.svc.Register(context.Background(), Registration{
		CatalogID: values.CatalogID(c.id), File: e.putBytes(c.id+".bin", c.content()),
	})
	if err != nil {
		e.t.Fatalf("install %s: %v", c.id, err)
	}
	return m
}

// seed stores a model row directly, bypassing Register, like data written before a rule existed.
func (e *env) seed(id shared.SttModelID, name values.ModelName, file shared.ArtifactRef) {
	e.t.Helper()
	e.w.mu.Lock()
	defer e.w.mu.Unlock()
	e.w.models[id] = sttmodel.State{
		Spec: sttmodel.Spec{
			ID: id, Name: name, ModelVersion: "1", File: file, SizeBytes: 1, SHA256: catTiny.sum(),
		},
		Version: 1, InstalledAt: t0,
	}
}

// beforeEachTx runs fn at the start of every outermost transaction; transactions that fn opens itself skip it.
// Single-goroutine tests only.
func (e *env) beforeEachTx(fn func()) {
	busy := false
	e.w.mu.Lock()
	defer e.w.mu.Unlock()
	e.w.beforeTx = func() {
		if busy {
			return
		}
		busy = true
		defer func() { busy = false }()
		fn()
	}
}

// atTx runs fn once, at the start of the n-th outermost transaction from now (1-based).
func (e *env) atTx(n int, fn func()) {
	count := 0
	e.beforeEachTx(func() {
		count++
		if count == n {
			fn()
		}
	})
}
