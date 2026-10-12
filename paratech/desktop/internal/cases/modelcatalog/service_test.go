package modelcatalog

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/services/modelselection"
	"paratech/desktop/internal/domain/transcription/sttmodel"
	"paratech/desktop/internal/domain/transcription/values"
	"paratech/desktop/testenv"
)

func TestNewRequiresEveryPort(t *testing.T) {
	w := newWorld(t, nil)
	full := Deps{
		Models: fakeModels{w}, Pins: fakePins{w}, Settings: fakeSettings{w}, Files: fakeFiles{w},
		Catalog: fakeCatalog{w}, Clock: testenv.NewFakeClock(t0), IDs: fakeIDs{w}, Tx: fakeTx{w},
	}
	if _, err := New(full); err != nil {
		t.Fatal(err)
	}
	for name, drop := range map[string]func(*Deps){
		"models": func(d *Deps) { d.Models = nil }, "pins": func(d *Deps) { d.Pins = nil },
		"settings": func(d *Deps) { d.Settings = nil }, "files": func(d *Deps) { d.Files = nil },
		"catalog": func(d *Deps) { d.Catalog = nil }, "clock": func(d *Deps) { d.Clock = nil },
		"ids": func(d *Deps) { d.IDs = nil }, "tx": func(d *Deps) { d.Tx = nil },
	} {
		d := full
		drop(&d)
		if _, err := New(d); !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("without %s: %v, want ErrInvalidConfig", name, err)
		}
	}
}

func machine(t *testing.T, cores int, ramMB int64) values.Machine {
	t.Helper()
	m, err := values.NewMachine(cores, ramMB)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestListAvailable(t *testing.T) {
	e := newEnv(t)
	base := e.install(catBase)

	cases := []struct {
		name        string
		m           values.Machine
		recommended values.CatalogID
		slow        map[values.CatalogID]bool
	}{
		{"unknown machine", values.Machine{}, "tiny-q5_1", map[values.CatalogID]bool{}},
		{"strong machine", machine(t, 16, 32768), "base-q5_1", map[values.CatalogID]bool{}},
		{"weak machine", machine(t, 1, 768), "tiny-q5_1", map[values.CatalogID]bool{"base-q5_1": true, "large-v3": true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := e.svc.ListAvailable(context.Background(), tc.m)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 3 || got[0].Entry.ID() != "tiny-q5_1" || got[1].Entry.ID() != "base-q5_1" || got[2].Entry.ID() != "large-v3" {
				t.Fatalf("entries not in manifest order: %+v", got)
			}
			for _, a := range got {
				wantInstalled := shared.SttModelID("")
				if a.Entry.ID() == "base-q5_1" {
					wantInstalled = base.ID
				}
				if a.InstalledID != wantInstalled {
					t.Errorf("%s: InstalledID %q, want %q", a.Entry.ID(), a.InstalledID, wantInstalled)
				}
				if a.Recommended != (a.Entry.ID() == tc.recommended) {
					t.Errorf("%s: Recommended %v, want the recommendation %s", a.Entry.ID(), a.Recommended, tc.recommended)
				}
				if a.LikelySlow != tc.slow[a.Entry.ID()] {
					t.Errorf("%s: LikelySlow %v", a.Entry.ID(), a.LikelySlow)
				}
			}
		})
	}
}

func TestListAvailableRejectsBadManifest(t *testing.T) {
	e := newEnv(t)
	e.w.manifest = []byte(`{"schema_version": 1, "models": [{"id": "x"}]}`)
	if _, err := e.svc.ListAvailable(context.Background(), values.Machine{}); !errors.Is(err, values.ErrInvalidCatalog) {
		t.Fatalf("invalid manifest: %v, want values.ErrInvalidCatalog", err)
	}

	boom := errors.New("boom")
	e.w.manifestErr = boom
	if _, err := e.svc.ListAvailable(context.Background(), values.Machine{}); !errors.Is(err, boom) {
		t.Fatalf("source failure: %v", err)
	}
}

func TestRegisterCatalogModel(t *testing.T) {
	e := newEnv(t)
	got := e.install(catBase)

	want := InstalledModel{
		ID: "m1", Name: "base", ModelVersion: "1", CatalogID: "base-q5_1", SizeBytes: 20, SHA256: catBase.sum(),
		Quantization: "q5_1", InstalledAt: t0,
	}
	if got.ID != want.ID || got.Name != want.Name || got.ModelVersion != want.ModelVersion || got.CatalogID != want.CatalogID ||
		got.SizeBytes != want.SizeBytes || got.SHA256 != want.SHA256 || got.Quantization != want.Quantization ||
		!got.InstalledAt.Equal(want.InstalledAt) || len(got.Capabilities) != 1 || got.IsDefault {
		t.Fatalf("Register = %+v, want %+v", got, want)
	}

	list, err := e.svc.ListInstalled(context.Background())
	if err != nil || len(list) != 1 || list[0].ID != "m1" {
		t.Fatalf("ListInstalled = %+v, %v", list, err)
	}
}

func TestRegisterRejects(t *testing.T) {
	cases := []struct {
		name string
		reg  func(e *env) Registration
		want error
	}{
		{"wrong digest", func(e *env) Registration {
			return Registration{CatalogID: "base-q5_1", File: e.putFile("b.bin", catBase.size), SHA256: catTiny.sum()}
		}, values.ErrChecksumMismatch},
		{"wrong size", func(e *env) Registration {
			return Registration{CatalogID: "base-q5_1", File: e.putFile("b.bin", catBase.size+1), SHA256: catBase.sum()}
		}, values.ErrSizeMismatch},
		{"unknown catalog id", func(e *env) Registration {
			return Registration{CatalogID: "nope", File: e.putFile("b.bin", 1), SHA256: catBase.sum()}
		}, ErrNotInCatalog},
		{"catalog model with its own identity", func(e *env) Registration {
			return Registration{CatalogID: "base-q5_1", Name: "evil", File: e.putFile("b.bin", catBase.size), SHA256: catBase.sum()}
		}, ErrInvalidRegistration},
		{"missing file", func(e *env) Registration {
			ref, _ := shared.NewArtifactRef(shared.ArtifactModels, "missing.bin")
			return Registration{CatalogID: "base-q5_1", File: ref, SHA256: catBase.sum()}
		}, shared.ErrNotFound},
		{"imported model without a name", func(e *env) Registration {
			return Registration{ModelVersion: "1", File: e.putFile("own.bin", 5), SHA256: catBase.sum()}
		}, sttmodel.ErrInvalidModel},
		{"imported model with a bad digest", func(e *env) Registration {
			return Registration{Name: "own", ModelVersion: "1", File: e.putFile("own.bin", 5), SHA256: "abc"}
		}, sttmodel.ErrInvalidModel},
		{"file outside models", func(e *env) Registration {
			ref, _ := shared.NewArtifactRef(shared.ArtifactExports, "own.bin")
			e.w.files[ref] = []byte{1}
			return Registration{Name: "own", ModelVersion: "1", File: ref, SHA256: catBase.sum()}
		}, sttmodel.ErrInvalidModel},
		{"empty file", func(e *env) Registration {
			return Registration{Name: "own", ModelVersion: "1", File: e.putFile("own.bin", 0), SHA256: catBase.sum()}
		}, sttmodel.ErrInvalidModel},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			_, err := e.svc.Register(context.Background(), tc.reg(e))
			if !errors.Is(err, tc.want) {
				t.Fatalf("Register = %v, want %v", err, tc.want)
			}
			if len(e.w.models) != 0 {
				t.Fatalf("a rejected registration installed %v", e.w.models)
			}
		})
	}
}

func TestRegisterImportedAndDuplicates(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	own := Registration{
		Name: "own", ModelVersion: "2", Quantization: "f16", Capabilities: []values.Capability{"en"},
		File: e.putFile("own.bin", 7), SHA256: catLarge.sum(),
	}
	got, err := e.svc.Register(ctx, own)
	if err != nil || got.CatalogID != "" || got.SizeBytes != 7 || got.Name != "own" || got.Quantization != "f16" {
		t.Fatalf("Register imported = %+v, %v", got, err)
	}

	own.File = e.putFile("own-copy.bin", 7)
	_, err = e.svc.Register(ctx, own)
	if !errors.Is(err, ErrAlreadyInstalled) || !errors.Is(err, shared.ErrDuplicate) {
		t.Fatalf("second own 2: %v, want ErrAlreadyInstalled", err)
	}

	e.install(catBase)
	_, err = e.svc.Register(ctx, Registration{CatalogID: "base-q5_1", File: e.putFile("again.bin", catBase.size), SHA256: catBase.sum()})
	if !errors.Is(err, ErrAlreadyInstalled) {
		t.Fatalf("second base: %v, want ErrAlreadyInstalled", err)
	}
	if len(e.w.models) != 2 {
		t.Fatalf("installed %d models, want 2", len(e.w.models))
	}
}

// TestSetDefaultSwitches is test step 2: install two models, change the default, check (AC-07).
func TestSetDefaultSwitches(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	tiny, base := e.install(catTiny), e.install(catBase)

	for _, id := range []shared.SttModelID{tiny.ID, base.ID, base.ID} {
		e.clock.Advance(time.Minute)
		if err := e.svc.SetDefault(ctx, id); err != nil {
			t.Fatalf("SetDefault(%s): %v", id, err)
		}
		list, err := e.svc.ListInstalled(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range list {
			if m.IsDefault != (m.ID == id) {
				t.Fatalf("after SetDefault(%s): %s IsDefault=%v", id, m.ID, m.IsDefault)
			}
		}
		pinned, err := e.svc.ResolveForJob(ctx, "")
		if err != nil || pinned.ID != id {
			t.Fatalf("ResolveForJob after SetDefault(%s) = %+v, %v", id, pinned, err)
		}
	}
}

func TestSetDefaultRejects(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if err := e.svc.SetDefault(ctx, "m404"); !errors.Is(err, modelselection.ErrModelNotInstalled) {
		t.Fatalf("not installed: %v", err)
	}
	if err := e.svc.SetDefault(ctx, "a/b"); !errors.Is(err, shared.ErrInvalidID) {
		t.Fatalf("bad id: %v", err)
	}
	if e.w.settings.Version != 0 {
		t.Fatal("a rejected selection stored the settings")
	}
}

func TestSetDefaultRetriesVersionConflicts(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	base := e.install(catBase)

	e.w.settingsConflicts = conflictAttempts - 1
	if err := e.svc.SetDefault(ctx, base.ID); err != nil {
		t.Fatalf("SetDefault after %d conflicts: %v", conflictAttempts-1, err)
	}
	if e.w.defaultModel() != base.ID {
		t.Fatal("default not stored")
	}

	tiny := e.install(catTiny)
	e.w.settingsConflicts = conflictAttempts
	if err := e.svc.SetDefault(ctx, tiny.ID); !errors.Is(err, shared.ErrVersionConflict) {
		t.Fatalf("SetDefault with endless conflicts: %v, want shared.ErrVersionConflict", err)
	}
	if e.w.defaultModel() != base.ID {
		t.Fatal("a failed selection changed the default")
	}
}

func TestDelete(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	tiny, base := e.install(catTiny), e.install(catBase)
	tinyFile, _ := shared.NewArtifactRef(shared.ArtifactModels, "tiny-q5_1.bin")

	if err := e.svc.Delete(ctx, tiny.ID); err != nil {
		t.Fatal(err)
	}
	if e.w.hasModel(tiny.ID) || e.w.hasFile(tinyFile) {
		t.Fatal("the model row or its file survived")
	}
	if !e.w.hasModel(base.ID) {
		t.Fatal("another model was removed")
	}
	if err := e.svc.Delete(ctx, tiny.ID); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("second Delete: %v, want shared.ErrNotFound", err)
	}
}

func TestDeleteClearsTheDefaultFirst(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	base := e.install(catBase)
	if err := e.svc.SetDefault(ctx, base.ID); err != nil {
		t.Fatal(err)
	}

	if err := e.svc.Delete(ctx, base.ID); err != nil {
		t.Fatal(err)
	}
	if e.w.defaultModel() != "" || e.w.hasModel(base.ID) {
		t.Fatalf("default %q, model present %v; want both gone", e.w.defaultModel(), e.w.hasModel(base.ID))
	}
}

// TestDeletePinned is test step 3: removing a model that an unfinished job uses fails and changes nothing.
func TestDeletePinned(t *testing.T) {
	for _, status := range job.Statuses() {
		t.Run(string(status), func(t *testing.T) {
			e := newEnv(t)
			ctx := context.Background()
			base := e.install(catBase)
			if err := e.svc.SetDefault(ctx, base.ID); err != nil {
				t.Fatal(err)
			}
			e.w.pin("j1", base.ID, status)
			file, _ := shared.NewArtifactRef(shared.ArtifactModels, "base-q5_1.bin")

			err := e.svc.Delete(ctx, base.ID)
			if status.IsTerminal() {
				if err != nil {
					t.Fatalf("a %s job blocks the removal: %v", status, err)
				}
				return
			}
			if !errors.Is(err, sttmodel.ErrPinned) {
				t.Fatalf("Delete with a %s job: %v, want sttmodel.ErrPinned", status, err)
			}
			if !e.w.hasModel(base.ID) || !e.w.hasFile(file) || e.w.defaultModel() != base.ID {
				t.Fatal("a refused removal changed the row, the file or the default")
			}
		})
	}
}

func TestDeleteKeepsTheRowWhenTheFileStays(t *testing.T) {
	e := newEnv(t)
	base := e.install(catBase)
	e.w.fileDeleteErr = shared.ErrStorageBusy

	if err := e.svc.Delete(context.Background(), base.ID); !errors.Is(err, shared.ErrStorageBusy) {
		t.Fatalf("Delete = %v, want the storage error", err)
	}
	if !e.w.hasModel(base.ID) {
		t.Fatal("the row was removed although the file is still there")
	}

	e.w.fileDeleteErr = nil
	if err := e.svc.Delete(context.Background(), base.ID); err != nil {
		t.Fatalf("repeated Delete: %v", err)
	}
}

// TestDeleteWhileSelectedAgain covers ADR-0004(g): the user selects the model as default between the clearing and the
// row delete; the database refuses and the removal starts over.
func TestDeleteWhileSelectedAgain(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	base := e.install(catBase)

	reselections := 0
	e.w.beforeFileDelete = func() {
		if reselections < 1 {
			reselections++
			if err := e.svc.SetDefault(ctx, base.ID); err != nil {
				t.Errorf("SetDefault during the removal: %v", err)
			}
		}
	}
	if err := e.svc.Delete(ctx, base.ID); err != nil {
		t.Fatalf("Delete after one reselection: %v", err)
	}
	if e.w.hasModel(base.ID) || e.w.defaultModel() != "" {
		t.Fatal("model or default survived")
	}

	stubborn := e.install(catTiny)
	e.w.beforeFileDelete = func() {
		if err := e.svc.SetDefault(ctx, stubborn.ID); err != nil {
			t.Errorf("SetDefault during the removal: %v", err)
		}
	}
	err := e.svc.Delete(ctx, stubborn.ID)
	if !errors.Is(err, ErrDefaultReselected) || !errors.Is(err, shared.ErrConflict) {
		t.Fatalf("Delete against a default selected every time: %v, want ErrDefaultReselected", err)
	}
	if !e.w.hasModel(stubborn.ID) || e.w.defaultModel() != stubborn.ID {
		t.Fatal("the database contract was broken: the default model was removed")
	}
}

// TestDeleteWhilePinnedAfterTheFile covers a job that pins the model after the pre-check: the row delete re-checks.
func TestDeleteWhilePinnedAfterTheFile(t *testing.T) {
	e := newEnv(t)
	base := e.install(catBase)
	e.w.beforeFileDelete = func() { e.w.pin("late", base.ID, job.StatusQueued) }

	if err := e.svc.Delete(context.Background(), base.ID); !errors.Is(err, sttmodel.ErrPinned) {
		t.Fatalf("Delete = %v, want sttmodel.ErrPinned from the final check", err)
	}
	if !e.w.hasModel(base.ID) {
		t.Fatal("a pinned model row was removed")
	}
}

func TestResolveForJob(t *testing.T) {
	ctx := context.Background()

	e := newEnv(t)
	if _, err := e.svc.ResolveForJob(ctx, ""); !errors.Is(err, modelselection.ErrModelNotInstalled) {
		t.Fatalf("nothing installed: %v", err)
	}

	base := e.install(catBase)
	pinned, err := e.svc.ResolveForJob(ctx, "")
	if err != nil || pinned != (PinnedModel{ID: base.ID, Name: "base", ModelVersion: "1"}) {
		t.Fatalf("only model = %+v, %v", pinned, err)
	}

	tiny := e.install(catTiny)
	if _, err := e.svc.ResolveForJob(ctx, ""); !errors.Is(err, modelselection.ErrNoDefaultModel) {
		t.Fatalf("two models, no default: %v", err)
	}
	if pinned, err = e.svc.ResolveForJob(ctx, tiny.ID); err != nil || pinned.ID != tiny.ID || pinned.Name != "tiny" {
		t.Fatalf("requested = %+v, %v", pinned, err)
	}
	if _, err := e.svc.ResolveForJob(ctx, "m404"); !errors.Is(err, modelselection.ErrModelNotInstalled) {
		t.Fatalf("requested missing: %v", err)
	}
}

// TestConcurrentDeleteSelectAndPin races removals, default selections and job pins on one model. Whatever the
// interleaving: a removed model is neither the default nor pinned by an unfinished job, a pinned model is never
// removed, and every call returns one of the documented outcomes.
func TestConcurrentDeleteSelectAndPin(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	for round := range 30 {
		e := newEnv(t)
		ctx := context.Background()
		base, tiny := e.install(catBase), e.install(catTiny)

		var wg sync.WaitGroup
		errs := make(chan error, 64)
		for i := range 3 {
			wg.Add(3)
			go func() {
				defer wg.Done()
				if err := e.svc.Delete(ctx, base.ID); err != nil && !errors.Is(err, sttmodel.ErrPinned) &&
					!errors.Is(err, shared.ErrNotFound) && !errors.Is(err, ErrDefaultReselected) {
					errs <- fmt.Errorf("Delete: %w", err)
				}
			}()
			go func() {
				defer wg.Done()
				target := base.ID
				if i%2 == 1 {
					target = tiny.ID
				}
				if err := e.svc.SetDefault(ctx, target); err != nil && !errors.Is(err, modelselection.ErrModelNotInstalled) {
					errs <- fmt.Errorf("SetDefault: %w", err)
				}
			}()
			go func() {
				defer wg.Done()
				// Resolve and pin in one transaction, as the enqueue of an STT job does.
				err := fakeTx{e.w}.Do(ctx, func(ctx context.Context) error {
					p, err := e.svc.ResolveForJob(ctx, base.ID)
					if err != nil {
						return err
					}
					e.w.pin(shared.JobID(fmt.Sprintf("j%d", i)), p.ID, job.StatusRunning)
					return nil
				})
				if err != nil && !errors.Is(err, modelselection.ErrModelNotInstalled) {
					errs <- fmt.Errorf("pin: %w", err)
				}
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Errorf("round %d: %v", round, err)
		}

		pinned, _ := fakePins{e.w}.IsPinnedByJobInStatuses(ctx, base.ID, job.ActiveStatuses())
		switch {
		case e.w.hasModel(base.ID):
		case pinned:
			t.Fatalf("round %d: a removed model is pinned by a running job", round)
		case e.w.defaultModel() == base.ID:
			t.Fatalf("round %d: a removed model is the default", round)
		}
	}
}
