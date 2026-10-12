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

// TestListAvailableIgnoresImportedModels: only a model installed from an entry marks it installed. An imported model
// with a catalog name neither passes for the entry nor blocks its real install.
func TestListAvailableIgnoresImportedModels(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	if _, err := e.svc.Register(ctx, Registration{Name: "base", ModelVersion: "2", File: e.putFile("own.bin", 5)}); err != nil {
		t.Fatalf("import with a catalog name and another version: %v", err)
	}
	e.seed("legacy", "tiny", modelRef(t, "legacy.bin")) // a row with the identity of tiny, not from the catalog

	installedOf := func() map[values.CatalogID]shared.SttModelID {
		t.Helper()
		got, err := e.svc.ListAvailable(ctx, values.Machine{})
		if err != nil {
			t.Fatal(err)
		}
		out := map[values.CatalogID]shared.SttModelID{}
		for _, a := range got {
			if a.InstalledID != "" {
				out[a.Entry.ID()] = a.InstalledID
			}
		}
		return out
	}
	if got := installedOf(); len(got) != 0 {
		t.Fatalf("imported models pass for catalog entries: %v", got)
	}

	base := e.install(catBase)
	if got := installedOf(); len(got) != 1 || got["base-q5_1"] != base.ID {
		t.Fatalf("installed entries = %v, want only base-q5_1 = %s", got, base.ID)
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
		{"corrupted file of the right size", func(e *env) Registration {
			corrupt := catBase.content()
			corrupt[len(corrupt)/2] ^= 0xff
			return Registration{CatalogID: "base-q5_1", File: e.putBytes("b.bin", corrupt)}
		}, values.ErrChecksumMismatch},
		{"another catalog model's file", func(e *env) Registration {
			return Registration{CatalogID: "base-q5_1", File: e.putBytes("b.bin", catTiny.content())}
		}, values.ErrSizeMismatch},
		{"wrong size", func(e *env) Registration {
			return Registration{CatalogID: "base-q5_1", File: e.putFile("b.bin", catBase.size+1)}
		}, values.ErrSizeMismatch},
		{"unknown catalog id", func(e *env) Registration {
			return Registration{CatalogID: "nope", File: e.putFile("b.bin", 1)}
		}, ErrNotInCatalog},
		{"catalog model with its own identity", func(e *env) Registration {
			return Registration{CatalogID: "base-q5_1", Name: "evil", File: e.putBytes("b.bin", catBase.content())}
		}, ErrInvalidRegistration},
		{"missing file", func(e *env) Registration {
			return Registration{CatalogID: "base-q5_1", File: modelRef(e.t, "missing.bin")}
		}, shared.ErrNotFound},
		{"imported model without a name", func(e *env) Registration {
			return Registration{ModelVersion: "1", File: e.putFile("own.bin", 5)}
		}, sttmodel.ErrInvalidModel},
		{"imported model with a catalog identity", func(e *env) Registration {
			return Registration{Name: "base", ModelVersion: "1", File: e.putBytes("own.bin", catBase.content())}
		}, values.ErrReservedIdentity},
		{"file outside models", func(e *env) Registration {
			ref, _ := shared.NewArtifactRef(shared.ArtifactExports, "own.bin")
			e.w.files[ref] = []byte{1}
			return Registration{Name: "own", ModelVersion: "1", File: ref}
		}, sttmodel.ErrInvalidModel},
		{"empty file", func(e *env) Registration {
			return Registration{Name: "own", ModelVersion: "1", File: e.putFile("own.bin", 0)}
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

// TestRegisterHonorsCancellation: the digest of a large file stops in the middle when the context ends.
func TestRegisterHonorsCancellation(t *testing.T) {
	e := newEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reads := 0
	e.w.onRead = func() {
		reads++
		cancel()
	}
	file := e.putFile("own.bin", 1<<20)

	_, err := e.svc.Register(ctx, Registration{Name: "own", ModelVersion: "1", File: file})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Register = %v, want context.Canceled", err)
	}
	if reads != 1 || len(e.w.models) != 0 {
		t.Fatalf("%d reads, %d models; want the digest to stop after the first read and nothing installed", reads, len(e.w.models))
	}
}

func TestRegisterImportedAndDuplicates(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	content := []byte("own model weights")
	own := Registration{
		Name: "own", ModelVersion: "2", Quantization: "f16", Capabilities: []values.Capability{"en"},
		File: e.putBytes("own.bin", content),
	}

	got, err := e.svc.Register(ctx, own)
	if err != nil || got.CatalogID != "" || got.SizeBytes != int64(len(content)) || got.Name != "own" ||
		got.Quantization != "f16" {
		t.Fatalf("Register imported = %+v, %v", got, err)
	}
	if got.SHA256 != digest(content) {
		t.Fatalf("stored digest %s, want the digest of the file %s", got.SHA256, digest(content))
	}

	own.File = e.putBytes("own-copy.bin", content)
	_, err = e.svc.Register(ctx, own)
	if !errors.Is(err, ErrAlreadyInstalled) || !errors.Is(err, shared.ErrDuplicate) {
		t.Fatalf("second own 2: %v, want ErrAlreadyInstalled", err)
	}

	e.install(catBase)
	_, err = e.svc.Register(ctx, Registration{CatalogID: "base-q5_1", File: e.putBytes("again.bin", catBase.content())})
	if !errors.Is(err, ErrAlreadyInstalled) {
		t.Fatalf("second base: %v, want ErrAlreadyInstalled", err)
	}
	if len(e.w.models) != 2 {
		t.Fatalf("installed %d models, want 2", len(e.w.models))
	}
}

// TestRegisterRefusesAFileInUse: one file per installed model, compared case-insensitively as on Windows.
func TestRegisterRefusesAFileInUse(t *testing.T) {
	cases := []struct {
		name string
		file string
	}{
		{"same name", "base-q5_1.bin"},
		{"name in other case", "Base-Q5_1.BIN"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			e.install(catBase)

			_, err := e.svc.Register(context.Background(), Registration{
				Name: "own", ModelVersion: "1", File: e.putBytes(tc.file, catBase.content()),
			})
			if !errors.Is(err, sttmodel.ErrFileInUse) || !errors.Is(err, shared.ErrDuplicate) {
				t.Fatalf("Register = %v, want sttmodel.ErrFileInUse", err)
			}
			if errors.Is(err, ErrAlreadyInstalled) {
				t.Fatalf("Register = %v: a file in use is not a second install of the same model", err)
			}
			if len(e.w.models) != 1 {
				t.Fatalf("installed %d models, want 1", len(e.w.models))
			}
		})
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
	tinyFile := modelRef(t, "tiny-q5_1.bin")

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
			if !e.w.hasModel(base.ID) || !e.w.hasFile(modelRef(t, "base-q5_1.bin")) || e.w.defaultModel() != base.ID {
				t.Fatal("a refused removal changed the row, the file or the default")
			}
		})
	}
}

// TestDeleteLateRefusalKeepsTheFile: a pin or a default selection that appears between the pre-check and the row
// delete refuses the removal, and neither the row nor the file is touched.
func TestDeleteLateRefusalKeepsTheFile(t *testing.T) {
	const removalTx = 2 // the clearing of the default is the first transaction of Delete

	cases := []struct {
		name  string
		setup func(e *env, id shared.SttModelID)
		want  error
	}{
		{"pinned by a job", func(e *env, id shared.SttModelID) {
			e.atTx(removalTx, func() { e.w.pin("late", id, job.StatusQueued) })
		}, sttmodel.ErrPinned},
		{"selected as default every time", func(e *env, id shared.SttModelID) {
			e.beforeEachTx(func() {
				if e.w.defaultModel() == "" {
					if err := e.svc.SetDefault(context.Background(), id); err != nil {
						e.t.Errorf("SetDefault during the removal: %v", err)
					}
				}
			})
		}, ErrDefaultReselected},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			base := e.install(catBase)
			tc.setup(e, base.ID)

			if err := e.svc.Delete(context.Background(), base.ID); !errors.Is(err, tc.want) {
				t.Fatalf("Delete = %v, want %v", err, tc.want)
			}
			if !e.w.hasModel(base.ID) || !e.w.hasFile(modelRef(t, "base-q5_1.bin")) {
				t.Fatal("a refused removal destroyed the row or the file")
			}
		})
	}
}

// TestDeleteWhileSelectedAgain covers ADR-0004(g): the user selects the model as default between the clearing and the
// row delete once; the database refuses and the removal starts over and succeeds.
func TestDeleteWhileSelectedAgain(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	base := e.install(catBase)
	if err := e.svc.SetDefault(ctx, base.ID); err != nil {
		t.Fatal(err)
	}

	e.atTx(2, func() {
		if err := e.svc.SetDefault(ctx, base.ID); err != nil {
			t.Errorf("SetDefault during the removal: %v", err)
		}
	})
	if err := e.svc.Delete(ctx, base.ID); err != nil {
		t.Fatalf("Delete after one reselection: %v", err)
	}
	if e.w.hasModel(base.ID) || e.w.defaultModel() != "" || e.w.hasFile(modelRef(t, "base-q5_1.bin")) {
		t.Fatal("model, default or file survived")
	}
}

// TestDeleteFileFailureLeavesAnOrphan: the row is gone before the file; a failed file delete is reported, and the file
// stays as unreferenced garbage that a repeated Delete no longer knows.
func TestDeleteFileFailureLeavesAnOrphan(t *testing.T) {
	e := newEnv(t)
	base := e.install(catBase)
	e.w.fileDeleteErr = shared.ErrStorageBusy

	if err := e.svc.Delete(context.Background(), base.ID); !errors.Is(err, shared.ErrStorageBusy) {
		t.Fatalf("Delete = %v, want the storage error", err)
	}
	if e.w.hasModel(base.ID) || !e.w.hasFile(modelRef(t, "base-q5_1.bin")) {
		t.Fatal("want the row removed and the file left")
	}

	e.w.fileDeleteErr = nil
	if err := e.svc.Delete(context.Background(), base.ID); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("repeated Delete: %v, want shared.ErrNotFound", err)
	}
	if _, err := e.svc.Register(context.Background(), Registration{
		CatalogID: "base-q5_1", File: e.putBytes("base-q5_1.bin", catBase.content()),
	}); err != nil {
		t.Fatalf("reinstall over the orphan: %v", err)
	}
}

// TestDeleteAfterConcurrentRemoval: another removal deleted the row after the pre-check (and failed on the file);
// this one still deletes the file it knows.
func TestDeleteAfterConcurrentRemoval(t *testing.T) {
	e := newEnv(t)
	base := e.install(catBase)
	e.atTx(2, func() {
		e.w.mu.Lock()
		defer e.w.mu.Unlock()
		delete(e.w.models, base.ID)
	})

	if err := e.svc.Delete(context.Background(), base.ID); err != nil {
		t.Fatalf("Delete = %v, want success", err)
	}
	if e.w.hasFile(modelRef(t, "base-q5_1.bin")) {
		t.Fatal("the file survived")
	}
}

// TestDeleteKeepsASharedFile: rows that share a file (stored before one file per model was enforced) keep it until the
// last of them is removed.
func TestDeleteKeepsASharedFile(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	file := e.putFile("shared.bin", 1)
	e.seed("a", "own-a", file)
	e.seed("b", "own-b", modelRef(t, "SHARED.bin"))

	if err := e.svc.Delete(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if e.w.hasModel("a") || !e.w.hasFile(file) {
		t.Fatal("want row a removed and the shared file kept")
	}

	if err := e.svc.Delete(ctx, "b"); err != nil {
		t.Fatal(err)
	}
	if e.w.hasModel("b") {
		t.Fatal("row b survived")
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
// removed, an installed model keeps its file, and every call returns one of the documented outcomes.
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
		installed := e.w.hasModel(base.ID)
		switch {
		case installed && !e.w.hasFile(modelRef(t, "base-q5_1.bin")):
			t.Fatalf("round %d: an installed model lost its file", round)
		case installed:
		case pinned:
			t.Fatalf("round %d: a removed model is pinned by a running job", round)
		case e.w.defaultModel() == base.ID:
			t.Fatalf("round %d: a removed model is the default", round)
		}
	}
}
