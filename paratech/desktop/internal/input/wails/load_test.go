package wails

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	"paratech/desktop/internal/cases/modelcatalog"
	"paratech/desktop/internal/cases/preferences"
	"paratech/desktop/internal/cases/sttspeed"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/services/speedestimate"
	"paratech/desktop/internal/domain/transcription/values"
)

type fakeSpeed struct {
	o   sttspeed.Overview
	err error
}

func (f *fakeSpeed) Overview(context.Context) (sttspeed.Overview, error) { return f.o, f.err }

// estimatesOf are the table estimates of a large model on 8 CPUs in the mode load, measured in normal as 0.5.
func estimatesOf(t *testing.T, load shared.RecognitionLoad) sttspeed.ModelEstimates {
	t.Helper()
	half, err := values.RestoreMeasuredSpeed(0.5, 3)
	if err != nil {
		t.Fatal(err)
	}
	est := speedestimate.New()
	out := sttspeed.ModelEstimates{}
	for _, l := range shared.RecognitionLoads() {
		var m values.MeasuredSpeed
		if l == shared.LoadNormal {
			m = half
		}
		e := est.Estimate(values.FamilyLargeV3, "f16", l, 8, m)
		out.Modes = append(out.Modes, e)
		if l == load {
			out.Current = e
		}
	}
	return out
}

func TestModelLoadEstimatesJSON(t *testing.T) {
	got := modelLoadEstimates(estimatesOf(t, shared.LoadGentle))
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	// 1.6 * (4 / 3.2)^0.7 h for gentle on 8 CPUs; normal measured at 30 min; max on 8 threads.
	want := `{"current":{"mode":"gentle","secondsPerHour":6734,"source":"table","heavy":true},"modes":[` +
		`{"mode":"gentle","secondsPerHour":6734,"source":"table","heavy":true},` +
		`{"mode":"normal","secondsPerHour":1800,"source":"measured","heavy":false},` +
		`{"mode":"max","secondsPerHour":3546,"source":"table","heavy":false}]}`
	if string(raw) != want {
		t.Fatalf("JSON =\n%s\nwant\n%s", raw, want)
	}
}

// TASK-084: the settings page sets the mode and shows the estimates of the model in use next to it.
func TestSettingsServiceRecognitionLoad(t *testing.T) {
	ctx := context.Background()
	uc := &fakeSettings{t: preferences.Toggles{RecognitionLoad: shared.LoadGentle}}
	speed := &fakeSpeed{o: sttspeed.Overview{
		Load: shared.LoadMax, InUse: "m-1", InUseName: "large-v3",
		Installed: map[shared.SttModelID]sttspeed.ModelEstimates{"m-1": estimatesOf(t, shared.LoadMax)},
	}}
	s := NewSettingsService(uc, discard()).withSpeed(speed)

	got, err := s.SetRecognitionLoad(ctx, "max")
	if err != nil {
		t.Fatal(err)
	}
	if got.RecognitionLoad != "max" || uc.t.RecognitionLoad != shared.LoadMax {
		t.Fatalf("mode %q, stored %q", got.RecognitionLoad, uc.t.RecognitionLoad)
	}
	if got.LoadEstimatesModel != "large-v3" || got.LoadEstimates == nil ||
		!reflect.DeepEqual(*got.LoadEstimates, *modelLoadEstimates(estimatesOf(t, shared.LoadMax))) {
		t.Fatalf("estimates %+v of %q", got.LoadEstimates, got.LoadEstimatesModel)
	}

	if _, err := s.SetRecognitionLoad(ctx, "turbo"); !errors.Is(err, ErrUnknownRecognitionLoad) {
		t.Errorf("unknown mode: %v", err)
	}
	if uc.t.RecognitionLoad != shared.LoadMax {
		t.Errorf("a refused mode reached the use case: %q", uc.t.RecognitionLoad)
	}

	// No model in use, or no estimate: the toggles are shown without one.
	speed.o.InUse = ""
	if got, err := s.GetSettings(ctx); err != nil || got.LoadEstimates != nil || got.LoadEstimatesModel != "" {
		t.Errorf("no model in use: %+v, %v", got, err)
	}
	var logs bytes.Buffer
	broken := NewSettingsService(uc, slog.New(slog.NewTextHandler(&logs, nil))).withSpeed(&fakeSpeed{err: errors.New("db")})
	if got, err := broken.GetSettings(ctx); err != nil || got.LoadEstimates != nil || got.RecognitionLoad != "max" {
		t.Errorf("failing estimates: %+v, %v", got, err)
	}
	if !strings.Contains(logs.String(), "db") {
		t.Errorf("the estimate failure is not logged: %q", logs.String())
	}

	failing := NewSettingsService(&fakeSettings{err: errors.New("locked")}, discard())
	if _, err := failing.SetRecognitionLoad(ctx, "normal"); !errors.Is(err, ErrSettingsUnavailable) {
		t.Errorf("failing use case: %v", err)
	}
}

// TASK-084: the models page shows the estimates of every installed and catalog model and the current mode.
func TestModelsListEstimates(t *testing.T) {
	f := newModelFixture(t)
	f.catalog.installed = []modelcatalog.InstalledModel{{ID: "m-1", Name: "base", ModelVersion: "1", CatalogID: "base-q5_1"}}
	f.catalog.available = []modelcatalog.AvailableModel{{Entry: catalogEntry(t, ""), InstalledID: "m-1"}}
	f.catalog.resolved = modelcatalog.PinnedModel{ID: "m-1", Name: "base", ModelVersion: "1"}
	est := estimatesOf(t, shared.LoadNormal)
	f.s.deps.Speed = &fakeSpeed{o: sttspeed.Overview{
		Load:      shared.LoadNormal,
		Installed: map[shared.SttModelID]sttspeed.ModelEstimates{"m-1": est},
		Catalog:   map[values.CatalogID]sttspeed.ModelEstimates{"base-q5_1": est},
	}}

	got, err := f.s.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := modelLoadEstimates(est)
	if got.RecognitionLoad != "normal" || len(got.Installed) != 1 || len(got.Available) != 1 ||
		!reflect.DeepEqual(got.Installed[0].Estimates, want) || !reflect.DeepEqual(got.Available[0].Estimates, want) {
		t.Fatalf("List = %+v", got)
	}
	if got.Installed[0].Estimates.Current.Source != "measured" || got.Installed[0].Estimates.Current.SecondsPerHour != 1800 {
		t.Errorf("current estimate %+v, want measured 1800 s", got.Installed[0].Estimates.Current)
	}

	// The estimates are extra: a failure leaves them out, the list is still shown.
	f.s.deps.Speed = &fakeSpeed{err: errors.New("db")}
	got, err = f.s.List(context.Background())
	if err != nil || got.RecognitionLoad != "" || got.Installed[0].Estimates != nil || got.Available[0].Estimates != nil {
		t.Fatalf("failing estimates: %+v, %v", got, err)
	}
}

func TestActivityRemainingJSON(t *testing.T) {
	raw, err := json.Marshal(RecordingItem{RemainingSeconds: 1800, HasRemaining: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"remainingSeconds":1800,"hasRemaining":true`) {
		t.Fatalf("JSON %s lacks the time left", raw)
	}
}
