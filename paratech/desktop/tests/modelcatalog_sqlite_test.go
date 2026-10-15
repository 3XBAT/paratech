package tests

// Scenario tests of the model manager (TASK-026) on the real SQLite adapter and the real file store; only the clock,
// the id generator and the catalog source are fakes (ARCH-61).

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/filestore"
	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/modelcatalog"
	"paratech/desktop/internal/cases/transcribe"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/services/modelselection"
	"paratech/desktop/internal/domain/transcription/sttmodel"
	"paratech/desktop/internal/domain/transcription/values"
	"paratech/desktop/testenv"
)

var (
	_ modelcatalog.ModelRepository = (*sqlite.ModelRepository)(nil)
	_ modelcatalog.ModelPins       = (*sqlite.ModelPins)(nil)
	_ modelcatalog.Tx              = (*sqlite.DB)(nil)
	_ transcribe.ModelReader       = (*sqlite.ModelRepository)(nil)
)

var catalogT0 = time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)

// modelFile is the content of one fake model file and its catalog entry.
type modelFile struct {
	id, name string
	content  []byte
}

func (f modelFile) sum() values.SHA256 {
	s := sha256.Sum256(f.content)
	return values.SHA256(hex.EncodeToString(s[:]))
}

var (
	fileTiny = modelFile{id: "tiny-q5_1", name: "tiny", content: bytes.Repeat([]byte("t"), 1000)}
	fileBase = modelFile{id: "base-q5_1", name: "base", content: bytes.Repeat([]byte("b"), 2000)}
	fileOwn  = modelFile{id: "own", name: "own", content: bytes.Repeat([]byte("o"), 500)}
)

func catalogManifest(files ...modelFile) []byte {
	var b bytes.Buffer
	b.WriteString(`{"schema_version": 1, "models": [`)
	for i, f := range files {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, `{"id": %q, "name": %q, "version": "1", "size_bytes": %d, "sha256": %q,
			"source_url": "https://example.com/%s.bin", "quantization": "q5_1",
			"recommendation": {"min_cpu_cores": 2, "min_ram_mb": 1024, "preferred": true}}`,
			f.id, f.name, len(f.content), f.sum(), f.id)
	}
	b.WriteString(`]}`)
	return b.Bytes()
}

type staticCatalog []byte

func (c staticCatalog) Manifest(context.Context) ([]byte, error) { return c, nil }

type seqIDs struct{ n atomic.Int64 }

func (s *seqIDs) NewSttModelID() shared.SttModelID {
	return shared.SttModelID(fmt.Sprintf("model-%d", s.n.Add(1)))
}

// catalogEnv is one run of the app: database, file store and the model manager on top.
type catalogEnv struct {
	t     *testing.T
	db    *sqlite.DB
	files *filestore.Store
	jobs  *sqlite.JobRepository
	svc   *modelcatalog.Service
	clock *testenv.FakeClock
}

func openCatalogEnv(t *testing.T, dir string, clock *testenv.FakeClock, ids *seqIDs) *catalogEnv {
	t.Helper()
	db, err := sqlite.Open(context.Background(), sqlite.Options{Path: filepath.Join(dir, "app.db"), Migrate: true})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	files, err := filestore.New(filepath.Join(dir, "data"))
	if err != nil {
		_ = db.Close()
		t.Fatalf("open file store: %v", err)
	}
	svc, err := modelcatalog.New(modelcatalog.Deps{
		Models: sqlite.NewModelRepository(db), Pins: sqlite.NewModelPins(db), Settings: sqlite.NewSettingsRepository(db),
		Files: files, Catalog: staticCatalog(catalogManifest(fileTiny, fileBase)), Clock: clock, IDs: ids, Tx: db,
	})
	if err != nil {
		t.Fatal(err)
	}
	return &catalogEnv{t: t, db: db, files: files, jobs: sqlite.NewJobRepository(db), svc: svc, clock: clock}
}

func (e *catalogEnv) close() {
	if err := e.files.Close(); err != nil {
		e.t.Errorf("close file store: %v", err)
	}
	if err := e.db.Close(); err != nil {
		e.t.Errorf("close database: %v", err)
	}
}

// store writes the model file the way a download or import would, and returns its reference.
func (e *catalogEnv) store(f modelFile) shared.ArtifactRef {
	e.t.Helper()
	ref, err := shared.NewArtifactRef(shared.ArtifactModels, f.id+".bin")
	if err != nil {
		e.t.Fatal(err)
	}
	if err := e.files.Write(context.Background(), ref, bytes.NewReader(f.content)); err != nil {
		e.t.Fatal(err)
	}
	return ref
}

func (e *catalogEnv) installCatalog(f modelFile) modelcatalog.InstalledModel {
	e.t.Helper()
	m, err := e.svc.Register(context.Background(), modelcatalog.Registration{
		CatalogID: values.CatalogID(f.id), File: e.store(f),
	})
	if err != nil {
		e.t.Fatalf("install %s: %v", f.id, err)
	}
	return m
}

func (e *catalogEnv) defaultID() shared.SttModelID {
	e.t.Helper()
	list, err := e.svc.ListInstalled(context.Background())
	if err != nil {
		e.t.Fatal(err)
	}
	for _, m := range list {
		if m.IsDefault {
			return m.ID
		}
	}
	return ""
}

// enqueueSTT stores a QUEUED stt job pinned to the model the use case resolves (AC-08), in one transaction.
func (e *catalogEnv) enqueueSTT(ctx context.Context, id string, requested shared.SttModelID) (*job.Job, error) {
	var stored *job.Job
	err := e.db.Do(ctx, func(ctx context.Context) error {
		pinned, err := e.svc.ResolveForJob(ctx, requested)
		if err != nil {
			return err
		}
		j, err := job.New(job.Spec{
			ID: shared.JobID(id), Type: "stt", Key: "stt:" + id, PinnedModelID: pinned.ID, MaxAttempts: 3,
		}, e.clock.Now())
		if err != nil {
			return err
		}
		if _, _, err := e.jobs.Enqueue(ctx, j); err != nil {
			return err
		}
		stored = j
		return nil
	})
	return stored, err
}

// TestModelManagerScenario: several models installed, the default survives a restart, a QUEUED and then RUNNING job
// blocks the removal of its model, which succeeds after the job completes (AC-07, AC-08, PRD section 14 case 11).
func TestModelManagerScenario(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	clock, ids := testenv.NewFakeClock(catalogT0), &seqIDs{}

	e := openCatalogEnv(t, dir, clock, ids)
	tiny, base := e.installCatalog(fileTiny), e.installCatalog(fileBase)
	ownConfirmed, err := values.ConfirmImport(fileOwn.sum())
	if err != nil {
		t.Fatal(err)
	}
	own, err := e.svc.Register(ctx, modelcatalog.Registration{
		Name: "own", ModelVersion: "1", File: e.store(fileOwn), Confirmation: ownConfirmed,
	})
	if err != nil {
		t.Fatal(err)
	}
	if own.SHA256 != fileOwn.sum() {
		t.Fatalf("imported model digest %s, want the digest of its file %s", own.SHA256, fileOwn.sum())
	}
	if _, err := e.svc.ResolveForJob(ctx, ""); !errors.Is(err, modelselection.ErrNoDefaultModel) {
		t.Fatalf("three models without a default: %v", err)
	}

	clock.Advance(time.Minute)
	if err := e.svc.SetDefault(ctx, tiny.ID); err != nil {
		t.Fatal(err)
	}
	clock.Advance(time.Minute)
	if err := e.svc.SetDefault(ctx, base.ID); err != nil {
		t.Fatal(err)
	}
	e.close()

	e = openCatalogEnv(t, dir, clock, ids)
	defer e.close()
	installed, err := e.svc.ListInstalled(ctx)
	if err != nil || len(installed) != 3 {
		t.Fatalf("after restart: %+v, %v", installed, err)
	}
	if got := e.defaultID(); got != base.ID {
		t.Fatalf("default after restart = %q, want %q", got, base.ID)
	}
	available, err := e.svc.ListAvailable(ctx, values.Machine{})
	if err != nil || len(available) != 2 || available[0].InstalledID != tiny.ID || available[1].InstalledID != base.ID {
		t.Fatalf("ListAvailable = %+v, %v", available, err)
	}

	j, err := e.enqueueSTT(ctx, "j1", "")
	if err != nil || j.PinnedModelID() != base.ID {
		t.Fatalf("STT job pins %q, %v; want the default %q (AC-08)", j.PinnedModelID(), err, base.ID)
	}
	if err := e.svc.Delete(ctx, base.ID); !errors.Is(err, sttmodel.ErrPinned) {
		t.Fatalf("Delete with a QUEUED job: %v, want sttmodel.ErrPinned", err)
	}

	filter, err := job.NewClaimFilter([]job.Type{"stt"}, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	running, ok, err := e.jobs.Claim(ctx, clock.Now(), filter)
	if err != nil || !ok || running.Status() != job.StatusRunning {
		t.Fatalf("Claim = %v, %v, %v", running, ok, err)
	}
	if err := e.svc.Delete(ctx, base.ID); !errors.Is(err, sttmodel.ErrPinned) {
		t.Fatalf("Delete with a RUNNING job: %v, want sttmodel.ErrPinned", err)
	}
	if size, err := e.files.Size(ctx, mustModelRef(t, fileBase)); err != nil || size != int64(len(fileBase.content)) {
		t.Fatalf("the file of a pinned model changed: %d, %v", size, err)
	}
	if e.defaultID() != base.ID {
		t.Fatal("a refused removal cleared the default")
	}

	clock.Advance(time.Minute)
	if err := running.Complete(clock.Now()); err != nil {
		t.Fatal(err)
	}
	if err := e.jobs.Save(ctx, running); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Delete(ctx, base.ID); err != nil {
		t.Fatalf("Delete after the job completed: %v", err)
	}

	if _, err := e.files.Size(ctx, mustModelRef(t, fileBase)); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("model file after removal: %v, want shared.ErrNotFound", err)
	}
	if got := e.defaultID(); got != "" {
		t.Fatalf("default after removing it = %q, want none", got)
	}
	installed, err = e.svc.ListInstalled(ctx)
	if err != nil || len(installed) != 2 || installed[0].ID != own.ID || installed[1].ID != tiny.ID {
		t.Fatalf("installed after removal = %+v, %v", installed, err)
	}
	if err := e.svc.SetDefault(ctx, base.ID); !errors.Is(err, modelselection.ErrModelNotInstalled) {
		t.Fatalf("selecting the removed model: %v", err)
	}
}

func mustModelRef(t *testing.T, f modelFile) shared.ArtifactRef {
	t.Helper()
	ref, err := shared.NewArtifactRef(shared.ArtifactModels, f.id+".bin")
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

// TestModelManagerRegisterVerifiesTheFile: on the real file store a corrupted catalog file is refused by its own
// digest, and a file of an installed model cannot be registered again under another name or letter case.
func TestModelManagerRegisterVerifiesTheFile(t *testing.T) {
	ctx := context.Background()
	e := openCatalogEnv(t, t.TempDir(), testenv.NewFakeClock(catalogT0), &seqIDs{})
	defer e.close()

	corrupt := fileBase
	corrupt.content = bytes.Clone(fileBase.content)
	corrupt.content[0] ^= 0xff
	_, err := e.svc.Register(ctx, modelcatalog.Registration{CatalogID: values.CatalogID(fileBase.id), File: e.store(corrupt)})
	if !errors.Is(err, values.ErrChecksumMismatch) {
		t.Fatalf("corrupted file: %v, want values.ErrChecksumMismatch", err)
	}

	base := e.installCatalog(fileBase)
	upper, err := shared.NewArtifactRef(shared.ArtifactModels, "BASE-Q5_1.bin")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		ref          shared.ArtifactRef
		mayBeMissing bool // only on a case-sensitive file system (not Windows)
	}{
		{mustModelRef(t, fileBase), false},
		{upper, true},
	}
	baseConfirmed, err := values.ConfirmImport(fileBase.sum())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		_, err := e.svc.Register(ctx, modelcatalog.Registration{
			Name: "own", ModelVersion: "1", File: tc.ref, Confirmation: baseConfirmed,
		})
		missing := tc.mayBeMissing && errors.Is(err, shared.ErrNotFound)
		if !errors.Is(err, sttmodel.ErrFileInUse) && !missing {
			t.Fatalf("register %s again: %v, want sttmodel.ErrFileInUse", tc.ref, err)
		}
	}

	installed, err := e.svc.ListInstalled(ctx)
	if err != nil || len(installed) != 1 || installed[0].ID != base.ID {
		t.Fatalf("installed = %+v, %v; want only %s", installed, err, base.ID)
	}
}

// TestModelManagerConcurrentDeleteSelectAndPin races removal, default selection and job pinning of one model on the
// real database. Whatever the order: a removed model is neither the default (the foreign key) nor pinned by an
// unfinished job, an installed model keeps its file, and every call ends with one of the documented outcomes.
func TestModelManagerConcurrentDeleteSelectAndPin(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	ctx := context.Background()

	for round := range 5 {
		e := openCatalogEnv(t, t.TempDir(), testenv.NewFakeClock(catalogT0), &seqIDs{})
		tiny, base := e.installCatalog(fileTiny), e.installCatalog(fileBase)

		var wg sync.WaitGroup
		errs := make(chan error, 64)
		for i := range 4 {
			wg.Add(3)
			go func() {
				defer wg.Done()
				err := e.svc.Delete(ctx, base.ID)
				if err != nil && !errors.Is(err, sttmodel.ErrPinned) && !errors.Is(err, shared.ErrNotFound) &&
					!errors.Is(err, modelcatalog.ErrDefaultReselected) {
					errs <- fmt.Errorf("Delete: %w", err)
				}
			}()
			go func() {
				defer wg.Done()
				target := base.ID
				if i%2 == 1 {
					target = tiny.ID
				}
				err := e.svc.SetDefault(ctx, target)
				if err != nil && !errors.Is(err, modelselection.ErrModelNotInstalled) {
					errs <- fmt.Errorf("SetDefault: %w", err)
				}
			}()
			go func() {
				defer wg.Done()
				_, err := e.enqueueSTT(ctx, fmt.Sprintf("j%d", i), base.ID)
				if err != nil && !errors.Is(err, modelselection.ErrModelNotInstalled) {
					errs <- fmt.Errorf("enqueue: %w", err)
				}
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Errorf("round %d: %v", round, err)
		}

		_, getErr := sqlite.NewModelRepository(e.db).Get(ctx, base.ID)
		pinned, err := sqlite.NewModelPins(e.db).IsPinnedByJobInStatuses(ctx, base.ID, job.ActiveStatuses())
		if err != nil {
			t.Fatal(err)
		}
		removed := errors.Is(getErr, shared.ErrNotFound)
		if !removed {
			if _, err := e.files.Size(ctx, mustModelRef(t, fileBase)); err != nil {
				t.Errorf("round %d: an installed model lost its file: %v", round, err)
			}
		}
		if removed && pinned {
			t.Errorf("round %d: a removed model is pinned by an unfinished job", round)
		}
		if removed && e.defaultID() == base.ID {
			t.Errorf("round %d: a removed model is the default", round)
		}
		e.close()
	}
}
