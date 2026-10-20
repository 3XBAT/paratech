package sttspeed

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/settings/settings"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/services/speedestimate"
	"paratech/desktop/internal/domain/transcription/sttmodel"
	"paratech/desktop/internal/domain/transcription/values"
)

var t0 = time.Date(2026, 10, 19, 9, 0, 0, 0, time.UTC)

func entryJSON(id, family, quant string) string {
	return fmt.Sprintf(`{"id": %q, "name": %q, "version": "1", "size_bytes": 1000, "sha256": %q,
		"source_url": "https://example.com/%s.bin", "quantization": %q, "capabilities": ["multilingual"],
		"recommendation": {"min_cpu_cores": 2, "min_ram_mb": 1024, "preferred": false}, "family": %q}`,
		id, id, strings.Repeat(fmt.Sprintf("%x", len(id)%16), 64), id, quant, family)
}

func manifest() []byte {
	return []byte(`{"schema_version": 1, "models": [` + entryJSON("large-v3-f16", "large-v3", "f16") + `, ` +
		entryJSON("small-q5_1", "small", "q5_1") + `]}`)
}

type fakeModels struct {
	rows []*sttmodel.Model
	err  error
}

func (f *fakeModels) List(context.Context) ([]*sttmodel.Model, error) { return f.rows, f.err }

type fakeCatalog struct{ data []byte }

func (f fakeCatalog) Manifest(context.Context) ([]byte, error) { return f.data, nil }

type fakeSpeeds map[values.SpeedKey]values.MeasuredSpeed

func (f fakeSpeeds) Speeds(context.Context) (map[values.SpeedKey]values.MeasuredSpeed, error) {
	return f, nil
}

type fakeSettings struct {
	load shared.RecognitionLoad
	def  shared.SttModelID
}

func (f fakeSettings) Load(context.Context) (*settings.Settings, error) {
	return settings.Restore(settings.State{Version: 1, RecognitionLoad: f.load, DefaultSttModel: f.def})
}

func catalogModel(t *testing.T, id shared.SttModelID, entry values.CatalogID) *sttmodel.Model {
	t.Helper()
	cat, err := values.ParseCatalog(manifest())
	if err != nil {
		t.Fatal(err)
	}
	e, ok := cat.Entry(entry)
	if !ok {
		t.Fatalf("no entry %s", entry)
	}
	file, err := shared.NewArtifactRef(shared.ArtifactModels, string(id)+".bin")
	if err != nil {
		t.Fatal(err)
	}
	m, err := sttmodel.New(sttmodel.CatalogSpec(id, e, file), t0)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func importedModel(t *testing.T, id shared.SttModelID) *sttmodel.Model {
	t.Helper()
	file, err := shared.NewArtifactRef(shared.ArtifactModels, string(id)+".bin")
	if err != nil {
		t.Fatal(err)
	}
	m, err := sttmodel.New(sttmodel.Spec{
		ID: id, Name: "custom", ModelVersion: "1", File: file, SizeBytes: 1000,
		SHA256: values.SHA256(strings.Repeat("a", 64)), Quantization: "q5_0",
		Capabilities: []values.Capability{"multilingual"},
	}, t0)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func measured(t *testing.T, factor float64) values.MeasuredSpeed {
	t.Helper()
	s, err := values.RestoreMeasuredSpeed(factor, 2)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func newService(t *testing.T, models []*sttmodel.Model, speeds fakeSpeeds, st fakeSettings) *Service {
	t.Helper()
	s, err := New(Deps{Models: &fakeModels{rows: models}, Catalog: fakeCatalog{manifest()}, Speeds: speeds, Settings: st}, 8)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestNewRequiresPorts(t *testing.T) {
	ok := Deps{Models: &fakeModels{}, Catalog: fakeCatalog{}, Speeds: fakeSpeeds{}, Settings: fakeSettings{}}
	if _, err := New(ok, 0); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("no CPUs: %v", err)
	}
	missing := ok
	missing.Speeds = nil
	if _, err := New(missing, 4); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("no speeds: %v", err)
	}
}

// TASK-084: every model has an estimate per mode; the current mode is marked; a measured speed of an installed model
// replaces the table for that mode only, also on its catalog entry.
func TestOverview(t *testing.T) {
	large := catalogModel(t, "m-large", "large-v3-f16")
	custom := importedModel(t, "m-custom")
	speeds := fakeSpeeds{
		{Model: "m-large", Load: shared.LoadGentle}:  measured(t, 2.5),
		{Model: "m-custom", Load: shared.LoadNormal}: measured(t, 0.4),
	}
	s := newService(t, []*sttmodel.Model{large, custom}, speeds, fakeSettings{load: shared.LoadGentle, def: "m-large"})

	got, err := s.Overview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Load != shared.LoadGentle || got.InUse != "m-large" || got.InUseName != "large-v3-f16" {
		t.Errorf("load %q, in use %q %q", got.Load, got.InUse, got.InUseName)
	}
	if len(got.Catalog) != 2 || len(got.Installed) != 2 {
		t.Fatalf("catalog %d, installed %d estimates", len(got.Catalog), len(got.Installed))
	}

	l := got.Installed["m-large"]
	if l.Current.Source() != speedestimate.SourceMeasured || l.Current.PerHour() != 150*time.Minute || !l.Current.Heavy() {
		t.Errorf("large in gentle: %q %v heavy %v; want measured 2h30m, heavy", l.Current.Source(), l.Current.PerHour(),
			l.Current.Heavy())
	}
	if len(l.Modes) != 3 || l.Modes[1].Load() != shared.LoadNormal || l.Modes[1].Source() != speedestimate.SourceTable ||
		l.Modes[1].PerHour() != 96*time.Minute {
		t.Errorf("large modes: %+v; want normal from the table, 1h36m", l.Modes)
	}
	if got.Catalog["large-v3-f16"].Current != l.Current {
		t.Errorf("the catalog entry does not show the measured speed of its installed model")
	}

	small := got.Catalog["small-q5_1"]
	if small.Current.Source() != speedestimate.SourceTable || small.Current.Heavy() {
		t.Errorf("small not installed: %+v", small.Current)
	}

	c := got.Installed["m-custom"]
	if c.Current.Known() || c.Modes[1].Source() != speedestimate.SourceMeasured || c.Modes[1].PerHour() != 24*time.Minute {
		t.Errorf("imported model: current %+v, normal %+v; want unknown, then measured 24m", c.Current, c.Modes[1])
	}
}

func TestOverviewWithoutAModelInUse(t *testing.T) {
	s := newService(t, []*sttmodel.Model{catalogModel(t, "a", "large-v3-f16"), catalogModel(t, "b", "small-q5_1")},
		fakeSpeeds{}, fakeSettings{load: shared.LoadMax})
	got, err := s.Overview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.InUse != "" || got.Load != shared.LoadMax || got.Installed["a"].Current.Load() != shared.LoadMax {
		t.Errorf("two models, no default: in use %q, load %q", got.InUse, got.Load)
	}
}

// TASK-111: a time only from a speed measured on this computer, never from the rough table.
func TestTranscriptionTime(t *testing.T) {
	speeds := fakeSpeeds{
		{Model: "m-large", Load: shared.LoadNormal}: measured(t, 0.5),
		{Model: "m-small", Load: shared.LoadGentle}: measured(t, 0.2),
	}
	models := []*sttmodel.Model{catalogModel(t, "m-large", "large-v3-f16"), catalogModel(t, "m-small", "small-q5_1"),
		importedModel(t, "m-custom")}
	s := newService(t, models, speeds, fakeSettings{load: shared.LoadNormal})
	ctx := context.Background()

	if d, ok, err := s.TranscriptionTime(ctx, "m-large", 90*time.Minute); err != nil || !ok || d != 45*time.Minute {
		t.Errorf("measured: %v %v %v; want 45m", d, ok, err)
	}
	// Measured only in another mode: the table would give a time for normal, but it is not shown.
	if d, ok, err := s.TranscriptionTime(ctx, "m-small", time.Hour); err != nil || ok || d != 0 {
		t.Errorf("table estimate: %v %v %v; want no time", d, ok, err)
	}
	gentle := newService(t, models, speeds, fakeSettings{load: shared.LoadGentle})
	if d, ok, err := gentle.TranscriptionTime(ctx, "m-small", time.Hour); err != nil || !ok || d != 12*time.Minute {
		t.Errorf("measured in gentle: %v %v %v; want 12m", d, ok, err)
	}
	if d, ok, err := gentle.TranscriptionTime(ctx, "m-large", time.Hour); err != nil || ok || d != 0 {
		t.Errorf("large in gentle from the table: %v %v %v; want no time", d, ok, err)
	}
	if d, ok, err := s.TranscriptionTime(ctx, "m-custom", time.Hour); err != nil || ok || d != 0 {
		t.Errorf("imported, never measured: %v %v %v; want no estimate", d, ok, err)
	}
	if _, ok, err := s.TranscriptionTime(ctx, "gone", time.Hour); err != nil || ok {
		t.Errorf("not installed: %v %v", ok, err)
	}

	broken, err := New(Deps{
		Models: &fakeModels{err: errors.New("db")}, Catalog: fakeCatalog{manifest()}, Speeds: speeds,
		Settings: fakeSettings{},
	}, 8)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := broken.TranscriptionTime(ctx, "m-large", time.Hour); err == nil {
		t.Error("a failed read is not an error")
	}
}
