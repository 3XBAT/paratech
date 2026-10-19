package transcribe

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/domain/settings/settings"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/values"
)

type fakeSettings struct {
	load shared.RecognitionLoad
	err  error
}

func (f *fakeSettings) Load(context.Context) (*settings.Settings, error) {
	if f.err != nil {
		return nil, f.err
	}
	return settings.Restore(settings.State{Version: 1, RecognitionLoad: f.load})
}

type fakeSpeeds struct {
	mu      sync.Mutex
	rows    map[values.SpeedKey]values.MeasuredSpeed
	getErr  error
	saveErr error
	saves   int
}

func (f *fakeSpeeds) Speed(_ context.Context, k values.SpeedKey) (values.MeasuredSpeed, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rows[k], f.getErr
}

func (f *fakeSpeeds) SaveSpeed(_ context.Context, k values.SpeedKey, s values.MeasuredSpeed, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.saveErr != nil {
		return f.saveErr
	}
	f.saves++
	f.rows[k] = s
	return nil
}

// withLoad rebuilds the Service with the load setting and the speed store; the engine takes took of the fake clock.
func (e *env) withLoad(t *testing.T, st SettingsReader, sp SpeedStore, took time.Duration) {
	t.Helper()
	svc, err := New(Deps{
		Engine: engineFunc(func(ctx context.Context, audio, model, out shared.ArtifactRef, progress func(int)) error {
			e.clock.Advance(took)
			data, err := e.doc.Encode()
			if err != nil {
				return err
			}
			return e.artifacts.Write(ctx, out, bytes.NewReader(data))
		}),
		Transcripts: e.transcripts, Recordings: e.recordings, Models: e.models, Artifacts: e.artifacts,
		Events: e.events, Clock: e.clock, Live: e.live, Settings: st, Speeds: sp,
	})
	if err != nil {
		t.Fatal(err)
	}
	e.svc = svc
}

// TASK-084: the mode is read at the start of every run and handed to the engine; an unreadable setting runs gentle.
func TestHandlePassesTheLoadMode(t *testing.T) {
	tests := []struct {
		name     string
		settings SettingsReader
		want     shared.RecognitionLoad
	}{
		{"no setting port", nil, shared.LoadGentle},
		{"max", &fakeSettings{load: shared.LoadMax}, shared.LoadMax},
		{"normal", &fakeSettings{load: shared.LoadNormal}, shared.LoadNormal},
		{"unreadable", &fakeSettings{err: errors.New("db locked")}, shared.LoadGentle},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			svc, err := New(Deps{
				Engine: e.engine, Transcripts: e.transcripts, Recordings: e.recordings, Models: e.models,
				Artifacts: e.artifacts, Events: e.events, Clock: e.clock, Settings: tt.settings,
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := svc.Handle(context.Background(), e.defaultJob(t), (&reporter{}).report); err != nil {
				t.Fatalf("Handle: %v", err)
			}
			if calls := e.engine.Calls(); len(calls) != 1 || calls[0].Load != tt.want {
				t.Fatalf("engine calls = %+v, want one in %q", calls, tt.want)
			}
		})
	}
}

// TASK-084: a successful run of the 1 h recording adds its speed for the pinned model in the mode it ran in.
func TestHandleMeasuresTheSpeed(t *testing.T) {
	e := newEnv(t)
	speeds := &fakeSpeeds{rows: map[values.SpeedKey]values.MeasuredSpeed{}}
	e.withLoad(t, &fakeSettings{load: shared.LoadNormal}, speeds, 30*time.Minute)

	if err := e.svc.Handle(context.Background(), e.defaultJob(t), (&reporter{}).report); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	key := values.SpeedKey{Model: "m-1", Load: shared.LoadNormal}
	if got := speeds.rows[key]; got.Factor() != 0.5 || got.Samples() != 1 {
		t.Fatalf("measured %+v, want factor 0.5 over 1 run", got)
	}

	// A rerun after the document is stored does not run the engine and measures nothing.
	if err := e.svc.Handle(context.Background(), e.defaultJob(t), (&reporter{}).report); err != nil {
		t.Fatalf("Handle again: %v", err)
	}
	if speeds.saves != 1 {
		t.Errorf("saves = %d, want 1", speeds.saves)
	}
}

func TestHandleSpeedFailuresDoNotFailTheJob(t *testing.T) {
	tests := []struct {
		name   string
		speeds *fakeSpeeds
		took   time.Duration
		saves  int
	}{
		{"read fails", &fakeSpeeds{rows: map[values.SpeedKey]values.MeasuredSpeed{}, getErr: errors.New("busy")}, time.Minute, 0},
		{"save fails", &fakeSpeeds{rows: map[values.SpeedKey]values.MeasuredSpeed{}, saveErr: shared.ErrConflict}, time.Minute, 0},
		{"absurd run", &fakeSpeeds{rows: map[values.SpeedKey]values.MeasuredSpeed{}}, time.Millisecond, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			e.withLoad(t, nil, tt.speeds, tt.took)
			if err := e.svc.Handle(context.Background(), e.defaultJob(t), (&reporter{}).report); err != nil {
				t.Fatalf("Handle: %v", err)
			}
			if tt.speeds.saves != tt.saves || len(e.events.list()) != 1 {
				t.Errorf("saves %d, events %d; want %d saves and the transcript published", tt.speeds.saves,
					len(e.events.list()), tt.saves)
			}
		})
	}
}
