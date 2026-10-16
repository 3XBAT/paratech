package app

// TASK-030: the models page binding on the wired core (real SQLite, filestore, localfile, model catalog, transcription
// queue, job runner and whispercli with the fake binary); only the open dialog is fake.

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/transcribe"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/input/wails"
	"paratech/desktop/testenv"
)

// modelPicker "chooses" the file at path in the open dialog.
type modelPicker struct {
	mu   sync.Mutex
	path string
}

func (p *modelPicker) PickModelFile(context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.path, nil
}

func (p *modelPicker) set(path string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.path = path
}

// modelsPage is the binding of the models page on the opened core.
func modelsPage(core wails.Core, picker wails.ModelFilePicker) *wails.ModelsService {
	return wails.NewModelsService(wails.ModelsDeps{
		Catalog: core.Models, Downloads: core.Downloads, Imports: core.Imports, Links: core.Links, Picker: picker,
		SpeechReady: core.SpeechReady,
	}, slog.New(slog.DiscardHandler))
}

// importThroughPage picks a fresh file with content and confirms it with the digest the page was shown.
func importThroughPage(t *testing.T, page *wails.ModelsService, picker *modelPicker, file, content string) wails.InstalledModelItem {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), file)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	picker.set(path)
	sel, err := page.ChooseImportFile(ctx)
	if err != nil {
		t.Fatalf("ChooseImportFile: %v", err)
	}
	m, err := page.ConfirmImport(ctx, wails.ModelImportConfirmation{
		SelectionID: sel.SelectionID, SHA256: sel.SHA256, Name: sel.SuggestedName, Version: sel.SuggestedVersion,
		Quantization: sel.SuggestedQuantization,
	})
	if err != nil {
		t.Fatalf("ConfirmImport: %v", err)
	}
	return m
}

// sttJobsOf returns the STT jobs of the recording.
func sttJobsOf(t *testing.T, jobs *sqlite.JobRepository, rec string) []*job.Job {
	t.Helper()
	all, _, err := jobs.ListActivity(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	return slices.DeleteFunc(all, func(j *job.Job) bool {
		return j.Type() != transcribe.JobType || !strings.HasPrefix(j.Key(), "stt:"+rec+":")
	})
}

// A recording that waited for a model gets its STT job as soon as the page installs one, without a restart; with a
// second model and no default it waits again until the page selects the default (AC-07, AC-08).
func TestModelsPageInstallsWithoutRestart(t *testing.T) {
	ctx := context.Background()
	a, _ := newTestAppWith(t, t.TempDir())
	core := startCore(t, a)
	picker := &modelPicker{}
	page := modelsPage(core, picker)
	jobs := sqlite.NewJobRepository(a.core.db)

	view, err := page.List(ctx)
	if err != nil || len(view.Installed) != 0 || len(view.Available) == 0 || !view.SpeechReady || view.MachineKnown ||
		view.NeedsDefault {
		t.Fatalf("List = %+v, %v; want nothing installed, the embedded catalog, whisper-cli found", view, err)
	}
	if a := view.Available[0]; a.CatalogID != "small-q5_1" || !a.Recommended || a.Tier != "balanced" || a.EnglishOnly ||
		a.Installed || !a.HasSourcePage {
		t.Fatalf("first catalog model %+v, want small-q5_1 recommended, balanced, multilingual, not installed", a)
	}

	// Completed while no model was installed: the startup sweep already ran and found no model.
	completedRecording(t, a, "rec-waiting")
	first := importThroughPage(t, page, picker, "ggml-base-q5_1.bin", testenv.WhisperOK)
	if first.Name != "base" || first.Quantization != "q5_1" || first.FromCatalog || first.IsDefault {
		t.Fatalf("imported %+v, want base q5_1 outside the catalog, not the default", first)
	}
	testenv.Eventually(t, "the waiting recording gets its STT job", func() bool {
		return len(sttJobsOf(t, jobs, "rec-waiting")) == 1
	}, nil)
	if j := sttJobsOf(t, jobs, "rec-waiting")[0]; j.Key() != "stt:rec-waiting:"+first.ID || string(j.PinnedModelID()) != first.ID {
		t.Fatalf("STT job key %q pin %q, want the only installed model %s", j.Key(), j.PinnedModelID(), first.ID)
	}
	view, err = page.List(ctx)
	if err != nil || len(view.Installed) != 1 || !view.Installed[0].InUse || view.NeedsDefault {
		t.Fatalf("List = %+v, %v; want the only model in use without a default", view, err)
	}

	// Two models and no default: nothing can be pinned until the page selects one.
	completedRecording(t, a, "rec-later")
	second := importThroughPage(t, page, picker, "ggml-small.bin", "other weights")
	if got := sttJobsOf(t, jobs, "rec-later"); len(got) != 0 {
		t.Fatalf("jobs %v without a default model", got)
	}
	view, err = page.List(ctx)
	if err != nil || len(view.Installed) != 2 || view.Installed[0].InUse || view.Installed[1].InUse || !view.NeedsDefault {
		t.Fatalf("List = %+v, %v; want no model in use and a default needed", view, err)
	}
	if err := page.SetDefault(ctx, second.ID); err != nil {
		t.Fatal(err)
	}
	testenv.Eventually(t, "the recording gets its STT job after the default is set", func() bool {
		return len(sttJobsOf(t, jobs, "rec-later")) == 1
	}, nil)
	if j := sttJobsOf(t, jobs, "rec-later")[0]; string(j.PinnedModelID()) != second.ID {
		t.Fatalf("STT job pinned to %q, want the default %s", j.PinnedModelID(), second.ID)
	}

	view, err = page.List(ctx)
	if err != nil || len(view.Installed) != 2 || view.NeedsDefault {
		t.Fatalf("List = %+v, %v; want both models and the default set", view, err)
	}
	for _, m := range view.Installed {
		if m.IsDefault != (m.ID == second.ID) || m.InUse != m.IsDefault || m.SHA256 == "" || m.SizeBytes == 0 {
			t.Fatalf("installed %+v: only %s is the default and in use", m, second.ID)
		}
	}

	// The same file again: the page says so, nothing is installed twice.
	path := filepath.Join(t.TempDir(), "ggml-base-q5_1.bin")
	if err := os.WriteFile(path, []byte(testenv.WhisperOK), 0o600); err != nil {
		t.Fatal(err)
	}
	picker.set(path)
	sel, err := page.ChooseImportFile(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = page.ConfirmImport(ctx, wails.ModelImportConfirmation{SelectionID: sel.SelectionID, SHA256: sel.SHA256, Name: "base", Version: "1"})
	if !errors.Is(err, wails.ErrModelAlreadyInstalled) {
		t.Fatalf("second import of base 1: %v, want ErrModelAlreadyInstalled", err)
	}
}

// A file swapped after the page showed its digest is refused and not installed (AC-23).
func TestModelsPageRefusesASwappedFile(t *testing.T) {
	ctx := context.Background()
	a, _ := newTestAppWith(t, t.TempDir())
	core := startCore(t, a)
	picker := &modelPicker{}
	page := modelsPage(core, picker)

	path := filepath.Join(t.TempDir(), "ggml-base.bin")
	if err := os.WriteFile(path, []byte("weights v1"), 0o600); err != nil {
		t.Fatal(err)
	}
	picker.set(path)
	sel, err := page.ChooseImportFile(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("weights v2"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = page.ConfirmImport(ctx, wails.ModelImportConfirmation{SelectionID: sel.SelectionID, SHA256: sel.SHA256, Name: "base", Version: "1"})
	if !errors.Is(err, wails.ErrModelFileChanged) {
		t.Fatalf("ConfirmImport of a swapped file: %v, want ErrModelFileChanged", err)
	}
	if view, err := page.List(ctx); err != nil || len(view.Installed) != 0 {
		t.Fatalf("List = %+v, %v; want nothing installed", view, err)
	}
}

// The download runs on the manager's goroutine and the page polls its state; a model the embedded catalog does not
// list is not downloaded, which the page shows as a text.
func TestModelsPageDownloadOfAnUnknownModel(t *testing.T) {
	ctx := context.Background()
	a, _ := newTestAppWith(t, t.TempDir())
	page := modelsPage(startCore(t, a), &modelPicker{})

	if _, err := page.PreflightDownload(ctx, "tiny-q5_1"); !errors.Is(err, wails.ErrModelNotInCatalog) {
		t.Fatalf("PreflightDownload = %v, want ErrModelNotInCatalog", err)
	}
	if err := page.StartDownload(ctx, "tiny-q5_1"); err != nil {
		t.Fatal(err)
	}
	var view wails.ModelsView
	testenv.Eventually(t, "the download ends", func() bool {
		var err error
		view, err = page.List(ctx)
		return err == nil && !view.Download.Running
	}, nil)
	if d := view.Download; d.CatalogID != "tiny-q5_1" || d.Phase != "failed" || d.Error != string(wails.ErrModelNotInCatalog) {
		t.Fatalf("download %+v, want failed: not in the catalog", d)
	}
	if err := page.OpenSourcePage(ctx, "tiny-q5_1"); !errors.Is(err, wails.ErrModelNotInCatalog) {
		t.Fatalf("OpenSourcePage = %v, want ErrModelNotInCatalog", err)
	}
}

// Before the startup cleanups ran, an import waits instead of writing an artifact (ADR-0009); Close ends the wait.
func TestModelsPageWritesWaitForTheStartup(t *testing.T) {
	a, _ := newTestAppWith(t, t.TempDir())
	core, err := a.openCore(context.Background()) // not started
	if err != nil {
		t.Fatal(err)
	}
	picker := &modelPicker{}
	page := modelsPage(core, picker)
	path := filepath.Join(t.TempDir(), "ggml-base.bin")
	if err := os.WriteFile(path, []byte(testenv.WhisperOK), 0o600); err != nil {
		t.Fatal(err)
	}
	picker.set(path)
	sel, err := page.ChooseImportFile(context.Background()) // reading the picked file writes nothing
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := page.ConfirmImport(context.Background(), wails.ModelImportConfirmation{
			SelectionID: sel.SelectionID, SHA256: sel.SHA256, Name: "base", Version: "1",
		})
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("the import did not wait for the startup: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err == nil {
		t.Fatal("an import cut short by the shutdown must fail")
	}
}

// fakePending counts the transcription passes and fails the test when two overlap.
type fakePending struct {
	running atomic.Int32
	calls   atomic.Int32
	overlap atomic.Bool
}

func (f *fakePending) EnqueuePending(context.Context) (int, error) {
	if f.running.Add(1) > 1 {
		f.overlap.Store(true)
	}
	f.calls.Add(1)
	time.Sleep(time.Millisecond)
	f.running.Add(-1)
	return 0, nil
}

// Installs racing each other never run two passes at once, and the last one is always followed by a pass.
func TestModelPassesDoNotOverlap(t *testing.T) {
	pending := &fakePending{}
	m := newModelManager(&core{}, pending, slog.New(slog.DiscardHandler), 5*time.Second)
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() { m.passAfter("test") })
	}
	wg.Wait()
	testenv.Eventually(t, "the passes end", func() bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		return !m.passing
	}, nil)
	if pending.overlap.Load() || pending.calls.Load() == 0 {
		t.Fatalf("overlap %v, %d passes", pending.overlap.Load(), pending.calls.Load())
	}
	if err := m.close(); err != nil {
		t.Fatal(err)
	}
	m.passAfter("after close")
	if err := m.close(); err != nil {
		t.Fatal(err)
	}
}
