package wails

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/cases/modelcatalog"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/services/modelselection"
	"paratech/desktop/internal/domain/transcription/sttmodel"
	"paratech/desktop/internal/domain/transcription/values"
)

// fakeCatalog is the model catalog behind the binding; safe for concurrent use.
type fakeCatalog struct {
	mu        sync.Mutex
	installed []modelcatalog.InstalledModel
	available []modelcatalog.AvailableModel
	listErr   error
	page      values.SourceURL
	pageErr   error
	actionErr error
	machines  []values.Machine
	pages     []values.CatalogID
	// What CurrentModel returns.
	resolved   modelcatalog.PinnedModel
	resolveErr error
	defaults   []shared.SttModelID
	deleted    []shared.SttModelID
}

func (f *fakeCatalog) ListAvailable(_ context.Context, m values.Machine) ([]modelcatalog.AvailableModel, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.machines = append(f.machines, m)
	return f.available, f.listErr
}

func (f *fakeCatalog) ListInstalled(context.Context) ([]modelcatalog.InstalledModel, error) {
	return f.installed, f.listErr
}

func (f *fakeCatalog) CurrentModel(context.Context) (modelcatalog.PinnedModel, error) {
	return f.resolved, f.resolveErr
}

func (f *fakeCatalog) SourcePage(_ context.Context, id values.CatalogID) (values.SourceURL, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pages = append(f.pages, id)
	return f.page, f.pageErr
}

func (f *fakeCatalog) SetDefault(_ context.Context, id shared.SttModelID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.defaults = append(f.defaults, id)
	return f.actionErr
}

func (f *fakeCatalog) Delete(_ context.Context, id shared.SttModelID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, id)
	return f.actionErr
}

// fakeDownloads records the download commands.
type fakeDownloads struct {
	mu         sync.Mutex
	plan       modelcatalog.DownloadPlan
	planErr    error
	startErr   error
	discardErr error
	state      DownloadState
	started    []values.CatalogID
	discarded  []values.CatalogID
	cancels    int
}

func (f *fakeDownloads) Preflight(_ context.Context, id values.CatalogID) (modelcatalog.DownloadPlan, error) {
	p := f.plan
	p.CatalogID = id
	return p, f.planErr
}

func (f *fakeDownloads) StartDownload(id values.CatalogID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.started = append(f.started, id)
	return f.startErr
}

func (f *fakeDownloads) CancelDownload() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cancels++
}

func (f *fakeDownloads) DownloadState() DownloadState { return f.state }

func (f *fakeDownloads) DiscardPartial(_ context.Context, id values.CatalogID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.discarded = append(f.discarded, id)
	return f.discardErr
}

type catalogImport struct {
	id   values.CatalogID
	path string
}

// fakeImports records the imports; safe for concurrent use.
type fakeImports struct {
	mu         sync.Mutex
	preview    modelcatalog.ImportPreview
	inspectErr error
	model      modelcatalog.InstalledModel
	importErr  error
	inspected  []string
	customs    []modelcatalog.CustomImport
	catalogs   []catalogImport
}

func (f *fakeImports) Inspect(_ context.Context, path string) (modelcatalog.ImportPreview, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inspected = append(f.inspected, path)
	return f.preview, f.inspectErr
}

func (f *fakeImports) ImportCatalog(_ context.Context, id values.CatalogID, path string) (modelcatalog.InstalledModel, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.catalogs = append(f.catalogs, catalogImport{id: id, path: path})
	return f.model, f.importErr
}

func (f *fakeImports) ImportCustom(_ context.Context, req modelcatalog.CustomImport) (modelcatalog.InstalledModel, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.customs = append(f.customs, req)
	return f.model, f.importErr
}

func (f *fakeImports) customCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.customs)
}

// fakeLinks records the opened pages.
type fakeLinks struct {
	mu     sync.Mutex
	err    error
	opened []string
}

func (f *fakeLinks) OpenLink(_ context.Context, u values.SourceURL) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.opened = append(f.opened, u.String())
	return f.err
}

// modelFixture is a binding on fakes; the picker "chooses" path.
type modelFixture struct {
	catalog   *fakeCatalog
	downloads *fakeDownloads
	imports   *fakeImports
	links     *fakeLinks
	path      string
	pickErr   error
	logs      *bytes.Buffer
	s         *ModelsService
}

const (
	shownSum = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	otherSum = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func newModelFixture(t *testing.T) *modelFixture {
	t.Helper()
	f := &modelFixture{
		catalog: &fakeCatalog{}, downloads: &fakeDownloads{}, links: &fakeLinks{}, logs: &bytes.Buffer{},
		imports: &fakeImports{
			preview: modelcatalog.ImportPreview{SizeBytes: 57 << 20, SHA256: shownSum},
			model:   modelcatalog.InstalledModel{ID: "m-1", Name: "base.en", ModelVersion: "1", SHA256: shownSum},
		},
		path: filepath.Join(t.TempDir(), "ggml-base.en-q5_1.bin"),
	}
	picker := modelPickerFunc(func(context.Context) (string, error) { return f.path, f.pickErr })
	f.s = NewModelsService(ModelsDeps{
		Catalog: f.catalog, Downloads: f.downloads, Imports: f.imports, Links: f.links, Picker: picker, SpeechReady: true,
	}, slog.New(slog.NewTextHandler(f.logs, nil)))
	return f
}

func catalogEntry(t *testing.T, page string) values.CatalogEntry {
	t.Helper()
	src, err := values.ParseSourceURL("https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-base-q5_1.bin")
	if err != nil {
		t.Fatal(err)
	}
	var pageURL values.SourceURL
	if page != "" {
		if pageURL, err = values.ParseSourceURL(page); err != nil {
			t.Fatal(err)
		}
	}
	e, err := values.NewCatalogEntry(values.CatalogEntrySpec{
		ID: "base-q5_1", Name: "base", Version: "1", SizeBytes: 59707625, SHA256: shownSum, SourceURL: src,
		SourcePage: pageURL, Quantization: "q5_1", Capabilities: []values.Capability{"multilingual"},
		Recommendation: values.Recommendation{MinCPUCores: 2, MinRAMMB: 1024, Preferred: true}, Tier: values.TierFast,
	})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestModelsListShowsInstalledAndAvailable(t *testing.T) {
	f := newModelFixture(t)
	at := time.Date(2026, 10, 15, 9, 30, 0, 0, time.FixedZone("MSK", 3*3600))
	f.catalog.installed = []modelcatalog.InstalledModel{
		{ID: "m-1", Name: "base", ModelVersion: "1", CatalogID: "base-q5_1", SizeBytes: 59707625, SHA256: shownSum,
			Quantization: "q5_1", Capabilities: []values.Capability{"multilingual"}, InstalledAt: at, IsDefault: true},
		{ID: "m-2", Name: "custom", ModelVersion: "2", SizeBytes: 10, SHA256: otherSum, InstalledAt: at},
	}
	f.catalog.available = []modelcatalog.AvailableModel{
		{Entry: catalogEntry(t, ""), InstalledID: "m-1", Recommended: true, LikelySlow: true},
	}
	f.catalog.resolved = modelcatalog.PinnedModel{ID: "m-1", Name: "base", ModelVersion: "1"}
	f.downloads.state = DownloadState{CatalogID: "base-q5_1", Phase: modelcatalog.PhaseDownloading, DoneBytes: 5, TotalBytes: 9, Running: true}

	got, err := f.s.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := ModelsView{
		Installed: []InstalledModelItem{
			{ID: "m-1", Name: "base", Version: "1", SizeBytes: 59707625, SHA256: shownSum, Quantization: "q5_1",
				Capabilities: []string{"multilingual"}, IsDefault: true, FromCatalog: true, CatalogID: "base-q5_1",
				InstalledAt: "2026-10-15T06:30:00Z", InUse: true},
			{ID: "m-2", Name: "custom", Version: "2", SizeBytes: 10, SHA256: otherSum, Capabilities: []string{},
				InstalledAt: "2026-10-15T06:30:00Z"},
		},
		Available: []AvailableModelItem{{
			CatalogID: "base-q5_1", Name: "base", Version: "1", SizeBytes: 59707625, Quantization: "q5_1",
			Capabilities: []string{"multilingual"}, MinCPUCores: 2, MinRAMMB: 1024, Recommended: true, LikelySlow: true,
			Installed: true, InstalledID: "m-1", HasSourcePage: true, Tier: "fast",
		}},
		SpeechReady:    true,
		Download:       ModelDownload{CatalogID: "base-q5_1", Phase: "downloading", DoneBytes: 5, TotalBytes: 9, Running: true},
		ModelsHomePage: ModelsHomePage,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("List =\n%+v\nwant\n%+v", got, want)
	}
	if len(f.catalog.machines) != 1 || f.catalog.machines[0].Known() {
		t.Fatalf("machines %+v, want one unknown machine (no hardware probe yet)", f.catalog.machines)
	}

	// The empty catalog of this build: empty lists, not null.
	f.catalog.installed, f.catalog.available = nil, nil
	f.catalog.resolved, f.catalog.resolveErr = modelcatalog.PinnedModel{}, modelselection.ErrModelNotInstalled
	raw, err := json.Marshal(must(f.s.List(context.Background())))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"installed":[]`) || !strings.Contains(string(raw), `"available":[]`) {
		t.Fatalf("JSON %s, want empty arrays", raw)
	}

	f.catalog.listErr = errors.New(`open C:\Users\student\db: locked`)
	if _, err := f.s.List(context.Background()); !errors.Is(err, ErrModelsUnavailable) {
		t.Fatalf("List with a failing catalog: %v", err)
	}
}

// The page shows the model new STT jobs use and a missing default as the model selection says; it does not repeat the
// rule (ARCH-44, AC-07, AC-08).
func TestModelsListModelInUse(t *testing.T) {
	one := []modelcatalog.InstalledModel{{ID: "m-1", Name: "base", ModelVersion: "1"}}
	two := append(slices.Clone(one), modelcatalog.InstalledModel{ID: "m-2", Name: "small", ModelVersion: "1"})
	twoWithDefault := slices.Clone(two)
	twoWithDefault[1].IsDefault = true
	wrapped := func(err error) error { return fmt.Errorf("modelcatalog: current model: %w", err) }

	tests := []struct {
		name         string
		installed    []modelcatalog.InstalledModel
		resolved     shared.SttModelID
		err          error
		inUse        []bool
		needsDefault bool
	}{
		{"nothing installed", nil, "", wrapped(modelselection.ErrModelNotInstalled), []bool{}, false},
		{"the only model without a default", one, "m-1", nil, []bool{true}, false},
		{"two models without a default", two, "", wrapped(modelselection.ErrNoDefaultModel), []bool{false, false}, true},
		{"two models with a default", twoWithDefault, "m-2", nil, []bool{false, true}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newModelFixture(t)
			f.catalog.installed = tt.installed
			f.catalog.resolved, f.catalog.resolveErr = modelcatalog.PinnedModel{ID: tt.resolved}, tt.err

			view, err := f.s.List(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			inUse := make([]bool, 0, len(view.Installed))
			for _, m := range view.Installed {
				inUse = append(inUse, m.InUse)
			}
			if !slices.Equal(inUse, tt.inUse) || view.NeedsDefault != tt.needsDefault {
				t.Fatalf("inUse %v needsDefault %v, want %v %v", inUse, view.NeedsDefault, tt.inUse, tt.needsDefault)
			}
		})
	}

	// Any other failure of the selection fails the list like the other reads; the cause goes only to the log.
	f := newModelFixture(t)
	f.catalog.installed = one
	f.catalog.resolveErr = errors.New("database is locked")
	_, err := f.s.List(context.Background())
	if !errors.Is(err, ErrModelsUnavailable) || !strings.Contains(f.logs.String(), "database is locked") {
		t.Fatalf("List with a failing selection: %v, log %s; want ErrModelsUnavailable and the cause logged", err, f.logs)
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// The page reads these names (frontend/src/bindings/models.ts).
func TestModelsJSONNames(t *testing.T) {
	tests := []struct {
		v    any
		keys string
	}{
		{ModelsView{}, "available,download,installed,machineKnown,modelsHomePage,needsDefault,speechReady"},
		{InstalledModelItem{}, "capabilities,catalogId,fromCatalog,id,inUse,installedAt,isDefault,name,quantization,sha256,sizeBytes,version"},
		{AvailableModelItem{}, "capabilities,catalogId,englishOnly,family,hasSourcePage,installed,installedId,likelySlow,load,minCpuCores,minRamMb,name,quantization,recommended,sizeBytes,tier,version"},
		{ModelDownload{}, "catalogId,doneBytes,error,phase,running,totalBytes"},
		{DownloadPlanView{}, "availableBytes,catalogId,enough,haveBytes,requiredBytes,totalBytes,warning"},
		{ModelFileSelection{}, "cancelled,fileName,selectionId,sha256,sizeBytes,suggestedName,suggestedQuantization,suggestedVersion"},
		{ModelImportConfirmation{}, "name,quantization,selectionId,sha256,version"},
		{CatalogImportResult{}, "cancelled,model"},
	}
	for _, tt := range tests {
		raw, err := json.Marshal(tt.v)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		if got := strings.Join(keys, ","); got != tt.keys {
			t.Errorf("%T keys %s, want %s", tt.v, got, tt.keys)
		}
	}
}

func TestModelsCustomImport(t *testing.T) {
	ctx := context.Background()
	f := newModelFixture(t)

	sel, err := f.s.ChooseImportFile(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := ModelFileSelection{
		SelectionID: sel.SelectionID, FileName: "ggml-base.en-q5_1.bin", SizeBytes: 57 << 20, SHA256: shownSum,
		SuggestedName: "base.en", SuggestedVersion: "1", SuggestedQuantization: "q5_1",
	}
	if sel.SelectionID == "" || sel != want {
		t.Fatalf("ChooseImportFile = %+v, want %+v", sel, want)
	}
	if len(f.imports.inspected) != 1 || f.imports.inspected[0] != f.path {
		t.Fatalf("Inspect got %v, want the picked path", f.imports.inspected)
	}
	if f.imports.customCount() != 0 {
		t.Fatal("choosing a file must install nothing")
	}

	m, err := f.s.ConfirmImport(ctx, ModelImportConfirmation{
		SelectionID: sel.SelectionID, SHA256: strings.ToUpper(shownSum), Name: " base.en ", Version: "1", Quantization: "Q5_1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != "m-1" || m.SHA256 != shownSum {
		t.Fatalf("ConfirmImport = %+v", m)
	}
	req := f.imports.customs[0]
	if req.Path != f.path || req.Name != "base.en" || req.ModelVersion != "1" || req.Quantization != "q5_1" ||
		req.Confirmation.SHA256() != shownSum {
		t.Fatalf("ImportCustom got %+v, want the picked path and the shown digest as the confirmation", req)
	}

	// The selection is used up.
	if _, err := f.s.ConfirmImport(ctx, ModelImportConfirmation{SelectionID: sel.SelectionID, SHA256: shownSum, Name: "x", Version: "1"}); !errors.Is(err, ErrModelNoSelection) {
		t.Fatalf("second ConfirmImport: %v", err)
	}
	if f.imports.customCount() != 1 {
		t.Fatalf("ImportCustom called %d times, want once", f.imports.customCount())
	}
}

// Confirm without inspect, with another id, another or malformed digest, or twice is refused without an import.
func TestModelsConfirmImportRefuses(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name      string
		choose    bool
		edit      func(c *ModelImportConfirmation)
		want      error
		keepsFile bool // the selection is still pending afterwards
	}{
		{name: "without inspect", edit: func(*ModelImportConfirmation) {}, want: ErrModelNoSelection},
		{name: "wrong id", choose: true, edit: func(c *ModelImportConfirmation) { c.SelectionID = "999" }, want: ErrModelNoSelection, keepsFile: true},
		{name: "empty id", choose: true, edit: func(c *ModelImportConfirmation) { c.SelectionID = "" }, want: ErrModelNoSelection, keepsFile: true},
		{name: "another digest", choose: true, edit: func(c *ModelImportConfirmation) { c.SHA256 = otherSum }, want: ErrModelConfirmMismatch},
		{name: "no digest", choose: true, edit: func(c *ModelImportConfirmation) { c.SHA256 = "" }, want: ErrModelConfirmMismatch},
		{name: "malformed digest", choose: true, edit: func(c *ModelImportConfirmation) { c.SHA256 = shownSum[:63] + "z" }, want: ErrModelConfirmMismatch},
		{name: "bad name", choose: true, edit: func(c *ModelImportConfirmation) { c.Name = "-base" }, want: ErrModelBadName, keepsFile: true},
		{name: "empty name", choose: true, edit: func(c *ModelImportConfirmation) { c.Name = "" }, want: ErrModelBadName, keepsFile: true},
		{name: "bad version", choose: true, edit: func(c *ModelImportConfirmation) { c.Version = "1/2" }, want: ErrModelBadVersion, keepsFile: true},
		{name: "bad quantization", choose: true, edit: func(c *ModelImportConfirmation) { c.Quantization = "q5-1" }, want: ErrModelBadQuantization, keepsFile: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newModelFixture(t)
			c := ModelImportConfirmation{SelectionID: "1", SHA256: shownSum, Name: "base", Version: "1"}
			if tt.choose {
				sel, err := f.s.ChooseImportFile(ctx)
				if err != nil {
					t.Fatal(err)
				}
				c.SelectionID = sel.SelectionID
			}
			valid := c
			tt.edit(&c)
			if _, err := f.s.ConfirmImport(ctx, c); !errors.Is(err, tt.want) {
				t.Fatalf("ConfirmImport = %v, want %v", err, tt.want)
			}
			if f.imports.customCount() != 0 {
				t.Fatal("ImportCustom was called")
			}

			_, err := f.s.ConfirmImport(ctx, valid)
			if tt.keepsFile && err != nil {
				t.Fatalf("the selection must stay after the refusal: %v", err)
			}
			if !tt.keepsFile && !errors.Is(err, ErrModelNoSelection) {
				t.Fatalf("the selection must be gone after the refusal: %v", err)
			}
		})
	}
}

func TestModelsChooseImportFile(t *testing.T) {
	ctx := context.Background()

	t.Run("cancel keeps the selection", func(t *testing.T) {
		f := newModelFixture(t)
		sel, err := f.s.ChooseImportFile(ctx)
		if err != nil {
			t.Fatal(err)
		}
		f.path = ""
		got, err := f.s.ChooseImportFile(ctx)
		if err != nil || got != (ModelFileSelection{Cancelled: true}) {
			t.Fatalf("cancelled dialog = %+v, %v", got, err)
		}
		if _, err := f.s.ConfirmImport(ctx, ModelImportConfirmation{SelectionID: sel.SelectionID, SHA256: shownSum, Name: "b", Version: "1"}); err != nil {
			t.Fatalf("the selection was dropped by a cancelled dialog: %v", err)
		}
	})

	t.Run("wrong extension", func(t *testing.T) {
		f := newModelFixture(t)
		first, err := f.s.ChooseImportFile(ctx)
		if err != nil {
			t.Fatal(err)
		}
		f.path = filepath.Join(t.TempDir(), "lecture.mp4")
		if _, err := f.s.ChooseImportFile(ctx); !errors.Is(err, ErrModelFileType) {
			t.Fatalf("ChooseImportFile = %v, want ErrModelFileType", err)
		}
		if len(f.imports.inspected) != 1 {
			t.Fatal("a file of another type must not be hashed")
		}
		if _, err := f.s.ConfirmImport(ctx, ModelImportConfirmation{SelectionID: first.SelectionID, SHA256: shownSum, Name: "b", Version: "1"}); !errors.Is(err, ErrModelNoSelection) {
			t.Fatalf("a new pick must drop the old selection: %v", err)
		}
	})

	t.Run("dialog fails", func(t *testing.T) {
		f := newModelFixture(t)
		f.pickErr = errors.New("dialog broke")
		if _, err := f.s.ChooseImportFile(ctx); !errors.Is(err, ErrModelActionFailed) {
			t.Fatalf("ChooseImportFile = %v", err)
		}
		if !strings.Contains(f.logs.String(), "dialog broke") {
			t.Fatalf("the cause must be logged: %s", f.logs.String())
		}
	})

	t.Run("inspect fails", func(t *testing.T) {
		f := newModelFixture(t)
		f.imports.inspectErr = fmt.Errorf("inspect: %w", values.ErrImportTooLarge)
		if _, err := f.s.ChooseImportFile(ctx); !errors.Is(err, ErrModelFileTooLarge) {
			t.Fatalf("ChooseImportFile = %v, want ErrModelFileTooLarge", err)
		}
		if _, err := f.s.ConfirmImport(ctx, ModelImportConfirmation{SelectionID: "1", SHA256: shownSum, Name: "b", Version: "1"}); !errors.Is(err, ErrModelNoSelection) {
			t.Fatalf("a failed inspect must hold nothing: %v", err)
		}
	})

	t.Run("busy", func(t *testing.T) {
		f := newModelFixture(t)
		release := make(chan struct{})
		entered := make(chan struct{})
		f.s.deps.Picker = modelPickerFunc(func(context.Context) (string, error) {
			close(entered)
			<-release
			return "", nil
		})
		done := make(chan error)
		go func() { _, err := f.s.ChooseImportFile(ctx); done <- err }()
		<-entered
		if _, err := f.s.ChooseImportFile(ctx); !errors.Is(err, ErrModelChoosing) {
			t.Errorf("second dialog: %v, want ErrModelChoosing", err)
		}
		if _, err := f.s.ImportCatalogFile(ctx, "base-q5_1"); !errors.Is(err, ErrModelChoosing) {
			t.Errorf("catalog dialog: %v, want ErrModelChoosing", err)
		}
		close(release)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	})
}

// Racing confirmations of one selection install it once.
func TestModelsConfirmImportOnce(t *testing.T) {
	ctx := context.Background()
	f := newModelFixture(t)
	sel, err := f.s.ChooseImportFile(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			_, _ = f.s.ConfirmImport(ctx, ModelImportConfirmation{SelectionID: sel.SelectionID, SHA256: shownSum, Name: "b", Version: "1"})
			f.s.DiscardImport(sel.SelectionID)
		})
	}
	wg.Wait()
	if n := f.imports.customCount(); n != 1 {
		t.Fatalf("ImportCustom called %d times, want once", n)
	}
}

func TestModelsDiscardImport(t *testing.T) {
	ctx := context.Background()
	f := newModelFixture(t)
	sel, err := f.s.ChooseImportFile(ctx)
	if err != nil {
		t.Fatal(err)
	}
	f.s.DiscardImport("other")
	f.s.DiscardImport(sel.SelectionID)
	if _, err := f.s.ConfirmImport(ctx, ModelImportConfirmation{SelectionID: sel.SelectionID, SHA256: shownSum, Name: "b", Version: "1"}); !errors.Is(err, ErrModelNoSelection) {
		t.Fatalf("ConfirmImport after discard: %v", err)
	}
}

func TestModelsImportCatalogFile(t *testing.T) {
	ctx := context.Background()
	f := newModelFixture(t)
	f.imports.model = modelcatalog.InstalledModel{ID: "m-3", Name: "base", ModelVersion: "1", CatalogID: "base-q5_1"}

	got, err := f.s.ImportCatalogFile(ctx, "base-q5_1")
	if err != nil || got.Cancelled || got.Model == nil || got.Model.ID != "m-3" || !got.Model.FromCatalog {
		t.Fatalf("ImportCatalogFile = %+v, %v", got, err)
	}
	if len(f.imports.catalogs) != 1 || f.imports.catalogs[0] != (catalogImport{id: "base-q5_1", path: f.path}) {
		t.Fatalf("ImportCatalog got %+v", f.imports.catalogs)
	}

	f.imports.importErr = fmt.Errorf("import: %w", values.ErrChecksumMismatch)
	if _, err := f.s.ImportCatalogFile(ctx, "base-q5_1"); !errors.Is(err, ErrModelChecksum) {
		t.Fatalf("checksum mismatch: %v", err)
	}

	f.path = ""
	if got, err := f.s.ImportCatalogFile(ctx, "base-q5_1"); err != nil || !got.Cancelled || got.Model != nil {
		t.Fatalf("cancelled = %+v, %v", got, err)
	}
	if _, err := f.s.ImportCatalogFile(ctx, "../x"); !errors.Is(err, ErrModelInvalidID) {
		t.Fatalf("invalid id: %v", err)
	}
	if len(f.imports.catalogs) != 2 {
		t.Fatalf("ImportCatalog called %d times, want 2", len(f.imports.catalogs))
	}
}

// Only a catalog id comes from the page: the opened URL is the catalog's or the fixed home page, never page text.
func TestModelsOpenPages(t *testing.T) {
	ctx := context.Background()
	f := newModelFixture(t)
	page := "https://huggingface.co/ggerganov/whisper.cpp/blob/main/ggml-base-q5_1.bin"
	f.catalog.page = catalogEntry(t, page).SourcePage()

	if err := f.s.OpenSourcePage(ctx, "base-q5_1"); err != nil {
		t.Fatal(err)
	}
	if err := f.s.OpenModelsHomePage(ctx); err != nil {
		t.Fatal(err)
	}
	if want := []string{page, ModelsHomePage}; !slices.Equal(f.links.opened, want) {
		t.Fatalf("opened %v, want %v", f.links.opened, want)
	}

	for _, id := range []string{"https://evil.example/x", "base q5", "", "a/b", `"x"`} {
		if err := f.s.OpenSourcePage(ctx, id); !errors.Is(err, ErrModelInvalidID) {
			t.Errorf("OpenSourcePage(%q) = %v, want ErrModelInvalidID", id, err)
		}
	}
	if len(f.catalog.pages) != 1 || len(f.links.opened) != 2 {
		t.Fatalf("a page-provided string reached the catalog %v or the opener %v", f.catalog.pages, f.links.opened)
	}

	f.catalog.pageErr = fmt.Errorf("source page: %w", modelcatalog.ErrNotInCatalog)
	if err := f.s.OpenSourcePage(ctx, "gone"); !errors.Is(err, ErrModelNotInCatalog) {
		t.Fatalf("unknown entry: %v", err)
	}
	if len(f.links.opened) != 2 {
		t.Fatal("nothing must be opened for an unknown entry")
	}

	f.catalog.pageErr = nil
	f.links.err = errors.New("explorer failed")
	if err := f.s.OpenSourcePage(ctx, "base-q5_1"); !errors.Is(err, ErrModelLinkFailed) {
		t.Fatalf("opener failure: %v", err)
	}
	if err := f.s.OpenModelsHomePage(ctx); !errors.Is(err, ErrModelLinkFailed) {
		t.Fatalf("opener failure: %v", err)
	}
}

func TestModelsDownloadCommands(t *testing.T) {
	ctx := context.Background()
	f := newModelFixture(t)
	f.downloads.plan = modelcatalog.DownloadPlan{TotalBytes: 3 << 30, HaveBytes: 1 << 30, RequiredBytes: 2<<30 + 512<<20, AvailableBytes: 300 << 20}

	plan, err := f.s.PreflightDownload(ctx, "large-v3")
	if err != nil {
		t.Fatal(err)
	}
	want := DownloadPlanView{
		CatalogID: "large-v3", TotalBytes: 3 << 30, HaveBytes: 1 << 30, RequiredBytes: 2<<30 + 512<<20,
		AvailableBytes: 300 << 20, Warning: "Недостаточно места на диске: нужно 2,5 ГБ, свободно 300 МБ",
	}
	if plan != want {
		t.Fatalf("PreflightDownload = %+v, want %+v", plan, want)
	}
	f.downloads.plan.Enough = true
	if plan, err := f.s.PreflightDownload(ctx, "large-v3"); err != nil || !plan.Enough || plan.Warning != "" {
		t.Fatalf("enough space = %+v, %v", plan, err)
	}

	if err := f.s.StartDownload(ctx, "large-v3"); err != nil {
		t.Fatal(err)
	}
	f.downloads.startErr = fmt.Errorf("start: %w", modelcatalog.ErrDownloadInProgress)
	if err := f.s.StartDownload(ctx, "large-v3"); !errors.Is(err, ErrModelDownloading) {
		t.Fatalf("second StartDownload: %v", err)
	}
	f.s.CancelDownload()
	if err := f.s.DiscardPartial(ctx, "large-v3"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"", "../x", "https://evil.example/x"} {
		if err := f.s.StartDownload(ctx, id); !errors.Is(err, ErrModelInvalidID) {
			t.Errorf("StartDownload(%q) = %v", id, err)
		}
		if err := f.s.DiscardPartial(ctx, id); !errors.Is(err, ErrModelInvalidID) {
			t.Errorf("DiscardPartial(%q) = %v", id, err)
		}
		if _, err := f.s.PreflightDownload(ctx, id); !errors.Is(err, ErrModelInvalidID) {
			t.Errorf("PreflightDownload(%q) = %v", id, err)
		}
	}
	if len(f.downloads.started) != 2 || f.downloads.cancels != 1 || len(f.downloads.discarded) != 1 {
		t.Fatalf("started %v, cancels %d, discarded %v", f.downloads.started, f.downloads.cancels, f.downloads.discarded)
	}
}

func TestModelsDownloadView(t *testing.T) {
	space := &values.InsufficientSpaceError{Required: 2 << 30, Available: 1 << 30}
	tests := []struct {
		name string
		st   DownloadState
		want ModelDownload
	}{
		{name: "none", want: ModelDownload{}},
		{name: "running", st: DownloadState{CatalogID: "b", Phase: modelcatalog.PhaseVerifying, DoneBytes: 9, TotalBytes: 9, Running: true},
			want: ModelDownload{CatalogID: "b", Phase: "verifying", DoneBytes: 9, TotalBytes: 9, Running: true}},
		{name: "installed", st: DownloadState{CatalogID: "b", Phase: modelcatalog.PhaseInstalled, DoneBytes: 9, TotalBytes: 9},
			want: ModelDownload{CatalogID: "b", Phase: "installed", DoneBytes: 9, TotalBytes: 9}},
		{name: "cancelled", st: DownloadState{CatalogID: "b", Phase: modelcatalog.PhaseFailed, DoneBytes: 4, TotalBytes: 9, Cancelled: true, Err: modelcatalog.ErrDownloadInterrupted},
			want: ModelDownload{CatalogID: "b", Phase: "cancelled", DoneBytes: 4, TotalBytes: 9, Error: string(ErrModelDownloadStopped)}},
		{name: "checksum", st: DownloadState{CatalogID: "b", Phase: modelcatalog.PhaseFailed, Err: fmt.Errorf("x: %w", values.ErrChecksumMismatch)},
			want: ModelDownload{CatalogID: "b", Phase: "failed", Error: string(ErrModelChecksum)}},
		{name: "space", st: DownloadState{CatalogID: "b", Err: fmt.Errorf("x: %w", space)},
			want: ModelDownload{CatalogID: "b", Phase: "failed", Error: "Недостаточно места на диске: нужно 2,0 ГБ, свободно 1,0 ГБ"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := downloadView(tt.st); got != tt.want {
				t.Fatalf("downloadView = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestModelsSetDefaultAndDelete(t *testing.T) {
	ctx := context.Background()
	f := newModelFixture(t)
	if err := f.s.SetDefault(ctx, "m-1"); err != nil {
		t.Fatal(err)
	}
	if err := f.s.Delete(ctx, "m-2"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.catalog.defaults, []shared.SttModelID{"m-1"}) || !slices.Equal(f.catalog.deleted, []shared.SttModelID{"m-2"}) {
		t.Fatalf("defaults %v, deleted %v", f.catalog.defaults, f.catalog.deleted)
	}
	for _, id := range []string{"", "a/b", "..", strings.Repeat("x", 200)} {
		if err := f.s.SetDefault(ctx, id); !errors.Is(err, ErrModelInvalidID) {
			t.Errorf("SetDefault(%q) = %v", id, err)
		}
		if err := f.s.Delete(ctx, id); !errors.Is(err, ErrModelInvalidID) {
			t.Errorf("Delete(%q) = %v", id, err)
		}
	}

	f.catalog.actionErr = fmt.Errorf("delete: %w", sttmodel.ErrPinned)
	if err := f.s.Delete(ctx, "m-2"); !errors.Is(err, ErrModelPinned) {
		t.Fatalf("pinned: %v", err)
	}
	f.catalog.actionErr = fmt.Errorf("set default: %w", modelselection.ErrModelNotInstalled)
	if err := f.s.SetDefault(ctx, "m-9"); !errors.Is(err, ErrModelNotFound) {
		t.Fatalf("not installed: %v", err)
	}
}

// AC 3 of TASK-030: every expected failure has an understandable text; the sizes of a disk space error are in it.
func TestModelsErrorTexts(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		notFound modelError
		want     string
	}{
		{name: "checksum", err: fmt.Errorf("register: %w", values.ErrChecksumMismatch), want: string(ErrModelChecksum)},
		{name: "confirmed file changed", err: fmt.Errorf("register: %w", values.ErrConfirmationMismatch), want: string(ErrModelFileChanged)},
		{name: "size", err: errors.Join(values.ErrSizeMismatch, errors.New("delete failed")), want: string(ErrModelSizeMismatch)},
		{name: "space", err: fmt.Errorf("import: %w", &values.InsufficientSpaceError{Required: 1536 << 20, Available: 300 << 20}),
			want: "Недостаточно места на диске: нужно 1,5 ГБ, свободно 300 МБ"},
		{name: "disk full while writing", err: fmt.Errorf("write: %w", shared.ErrStorageFull), want: string(ErrNotEnoughSpace)},
		{name: "already installed", err: fmt.Errorf("import: %w", modelcatalog.ErrAlreadyInstalled), want: string(ErrModelAlreadyInstalled)},
		{name: "interrupted", err: fmt.Errorf("download: %w: eof", modelcatalog.ErrDownloadInterrupted), want: string(ErrModelDownloadInterrupted)},
		{name: "too large", err: values.ErrImportTooLarge, want: string(ErrModelFileTooLarge)},
		{name: "empty", err: fmt.Errorf("%w: 0 bytes", values.ErrInvalidSize), want: string(ErrModelFileEmpty)},
		{name: "not regular", err: fmt.Errorf("%w: a directory", values.ErrInvalidImportFile), want: string(ErrModelFileNotRegular)},
		{name: "reserved identity", err: values.ErrReservedIdentity, want: string(ErrModelReservedName)},
		{name: "file in use", err: sttmodel.ErrFileInUse, want: string(ErrModelFileInUse)},
		{name: "pinned", err: sttmodel.ErrPinned, want: string(ErrModelPinned)},
		{name: "default reselected", err: modelcatalog.ErrDefaultReselected, want: string(ErrModelDefaultRace)},
		{name: "host not allowed", err: values.ErrHostNotAllowed, want: string(ErrModelSourceRefused)},
		{name: "not in catalog", err: modelcatalog.ErrNotInCatalog, notFound: ErrModelFileMissing, want: string(ErrModelNotInCatalog)},
		{name: "file missing", err: fmt.Errorf("open: %w", shared.ErrNotFound), notFound: ErrModelFileMissing, want: string(ErrModelFileMissing)},
		{name: "model missing", err: fmt.Errorf("get: %w", shared.ErrNotFound), notFound: ErrModelNotFound, want: string(ErrModelNotFound)},
		{name: "no access", err: shared.ErrStoragePermission, want: string(ErrModelNoAccess)},
		{name: "unknown", err: errors.New("disk I/O error"), want: string(ErrModelActionFailed)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pageError(tt.err, tt.notFound)
			if got.Error() != tt.want {
				t.Fatalf("pageError(%v) = %q, want %q", tt.err, got, tt.want)
			}
		})
	}
	if space := pageError(&values.InsufficientSpaceError{}, ""); !errors.Is(space, ErrNotEnoughSpace) {
		t.Fatal("a disk space error must match ErrNotEnoughSpace")
	}
}

// The page sees only the fixed text; the cause (here with a local path) goes to the log.
func TestModelsHideErrorDetails(t *testing.T) {
	ctx := context.Background()
	f := newModelFixture(t)
	f.imports.importErr = errors.New(`copy C:\Users\student\ggml.bin: disk I/O error`)
	sel, err := f.s.ChooseImportFile(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.s.ConfirmImport(ctx, ModelImportConfirmation{SelectionID: sel.SelectionID, SHA256: shownSum, Name: "b", Version: "1"})
	if !errors.Is(err, ErrModelActionFailed) || strings.Contains(err.Error(), "student") {
		t.Fatalf("ConfirmImport = %v, want only ErrModelActionFailed", err)
	}
	if !strings.Contains(f.logs.String(), "disk I/O error") {
		t.Fatalf("the cause must be logged: %s", f.logs.String())
	}
}

func TestSuggestIdentity(t *testing.T) {
	tests := []struct{ file, name, quant string }{
		{"ggml-base.en-q5_1.bin", "base.en", "q5_1"},
		{"ggml-large-v3-turbo.bin", "large-v3-turbo", ""},
		{"ggml-large-v3-turbo-q8_0.bin", "large-v3-turbo", "q8_0"},
		{"ggml-small-F16.bin", "small", "f16"},
		{"my model.bin", "", ""},
		{"-x.bin", "", ""},
		{".bin", "", ""},
		{"ggml-" + strings.Repeat("a", 70) + ".bin", "", ""},
	}
	for _, tt := range tests {
		if name, quant := suggestIdentity(tt.file); name != tt.name || quant != tt.quant {
			t.Errorf("suggestIdentity(%q) = %q, %q; want %q, %q", tt.file, name, quant, tt.name, tt.quant)
		}
	}
}

func TestSizeText(t *testing.T) {
	tests := []struct {
		n    int64
		want string
	}{
		{0, "0 Б"}, {1023, "1023 Б"}, {12 << 10, "12 КБ"}, {300 << 20, "300 МБ"}, {1536 << 20, "1,5 ГБ"}, {8 << 30, "8,0 ГБ"},
	}
	for _, tt := range tests {
		if got := sizeText(tt.n); got != tt.want {
			t.Errorf("sizeText(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

// The page gets the tier and whether a model knows English only, under fixed JSON names (TASK-067).
func TestModelsListTierAndLanguage(t *testing.T) {
	src, err := values.ParseSourceURL("https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-base.en-q5_1.bin")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name        string
		tier        values.ModelTier
		caps        []values.Capability
		wantTier    string
		wantEnglish bool
	}{
		{"english only", values.TierFast, []values.Capability{values.CapabilityEnglish}, "fast", true},
		{"multilingual", values.TierMax, []values.Capability{values.CapabilityMultilingual}, "max", false},
		{"no tier, no capabilities", values.TierNone, nil, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, err := values.NewCatalogEntry(values.CatalogEntrySpec{
				ID: "base.en-q5_1", Name: "base.en-q5_1", Version: "1", SizeBytes: 59721011, SHA256: shownSum,
				SourceURL: src, Capabilities: tc.caps, Tier: tc.tier,
			})
			if err != nil {
				t.Fatal(err)
			}
			f := newModelFixture(t)
			f.catalog.available = []modelcatalog.AvailableModel{{Entry: e}}
			f.catalog.resolveErr = modelselection.ErrModelNotInstalled

			view, err := f.s.List(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if a := view.Available[0]; a.Tier != tc.wantTier || a.EnglishOnly != tc.wantEnglish {
				t.Fatalf("item %+v, want tier %q englishOnly %v", a, tc.wantTier, tc.wantEnglish)
			}
			raw, err := json.Marshal(view.Available[0])
			if err != nil {
				t.Fatal(err)
			}
			want := fmt.Sprintf(`"tier":%q,"englishOnly":%v`, tc.wantTier, tc.wantEnglish)
			if !strings.Contains(string(raw), want) {
				t.Fatalf("JSON %s, want %s", raw, want)
			}
		})
	}
}

// The page gets the family and the load (1..5, 0 unknown) computed by the domain rule, under fixed JSON names
// (TASK-070).
func TestModelsListFamilyAndLoad(t *testing.T) {
	src, err := values.ParseSourceURL("https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-medium.bin")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name       string
		family     values.ModelFamily
		quant      values.Quantization
		wantFamily string
		wantLoad   int
	}{
		{"full medium", values.FamilyMedium, "f16", "medium", 4},
		{"quantized medium", values.FamilyMedium, "q5_0", "medium", 3},
		{"quantized tiny", values.FamilyTiny, "q5_1", "tiny", 1},
		{"full large-v3", values.FamilyLargeV3, "f16", "large-v3", 5},
		{"no family", values.FamilyNone, "f16", "", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, err := values.NewCatalogEntry(values.CatalogEntrySpec{
				ID: "medium-f16", Name: "medium-f16", Version: "1", SizeBytes: 1533763059, SHA256: shownSum,
				SourceURL: src, Quantization: tc.quant, Family: tc.family,
			})
			if err != nil {
				t.Fatal(err)
			}
			f := newModelFixture(t)
			f.catalog.available = []modelcatalog.AvailableModel{{Entry: e}}
			f.catalog.resolveErr = modelselection.ErrModelNotInstalled

			view, err := f.s.List(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if a := view.Available[0]; a.Family != tc.wantFamily || a.Load != tc.wantLoad {
				t.Fatalf("item %+v, want family %q load %d", a, tc.wantFamily, tc.wantLoad)
			}
			raw, err := json.Marshal(view.Available[0])
			if err != nil {
				t.Fatal(err)
			}
			want := fmt.Sprintf(`"family":%q,"load":%d`, tc.wantFamily, tc.wantLoad)
			if !strings.Contains(string(raw), want) {
				t.Fatalf("JSON %s, want %s", raw, want)
			}
		})
	}
}
