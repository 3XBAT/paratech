package settings

import (
	"errors"
	"testing"
	"time"

	"paratech/desktop/internal/domain/shared"
)

var (
	t0 = time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)
	t1 = t0.Add(time.Minute)
)

// snapshot is every observable value, so tests compare the whole aggregate at once.
type snapshot struct {
	version                 int64
	prompts, acOnly, notify bool
	model                   shared.SttModelID
	updatedAt               time.Time
}

func snap(s *Settings) snapshot {
	return snapshot{
		version: s.Version(), prompts: s.RecordingPromptsEnabled(), acOnly: s.ProcessOnlyOnACPower(),
		notify: s.NotifyOnProcessingComplete(), model: s.DefaultSttModel(), updatedAt: s.UpdatedAt(),
	}
}

func TestNewHasDefaults(t *testing.T) {
	want := snapshot{prompts: true, acOnly: true, notify: true}
	if got := snap(New()); got != want {
		t.Fatalf("New() = %+v, want %+v", got, want)
	}
}

func TestRestore(t *testing.T) {
	valid := State{
		Version: 3, RecordingPromptsEnabled: false, ProcessOnlyOnACPower: false, NotifyOnProcessingComplete: true,
		DefaultSttModel: "model-1", UpdatedAt: t0,
	}

	tests := []struct {
		name    string
		mutate  func(*State)
		want    snapshot
		wantErr error
	}{
		{name: "valid", want: snapshot{version: 3, notify: true, model: "model-1", updatedAt: t0}},
		{
			name: "stored defaults never changed",
			mutate: func(s *State) {
				*s = State{Version: 1, RecordingPromptsEnabled: true, ProcessOnlyOnACPower: true, NotifyOnProcessingComplete: true}
			},
			want: snapshot{version: 1, prompts: true, acOnly: true, notify: true},
		},
		{name: "no default model", mutate: func(s *State) { s.DefaultSttModel = "" }, want: snapshot{version: 3, notify: true, updatedAt: t0}},
		{name: "version 0", mutate: func(s *State) { s.Version = 0 }, wantErr: ErrInvalidSettings},
		{name: "negative version", mutate: func(s *State) { s.Version = -1 }, wantErr: ErrInvalidSettings},
		{name: "malformed model id", mutate: func(s *State) { s.DefaultSttModel = "a/b" }, wantErr: ErrInvalidSettings},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := valid
			if tc.mutate != nil {
				tc.mutate(&st)
			}

			got, err := Restore(st)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) || got != nil {
					t.Fatalf("Restore = %v, %v; want nil, %v", got, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Restore: %v", err)
			}
			if snap(got) != tc.want {
				t.Fatalf("Restore = %+v, want %+v", snap(got), tc.want)
			}
		})
	}
}

func TestChanges(t *testing.T) {
	defaults := snapshot{prompts: true, acOnly: true, notify: true}

	tests := []struct {
		name    string
		apply   func(*Settings) error
		want    snapshot
		wantErr error
	}{
		{
			name:  "AC-02: disable recording prompts",
			apply: func(s *Settings) error { return s.SetRecordingPromptsEnabled(false, t1) },
			want:  snapshot{acOnly: true, notify: true, updatedAt: t1},
		},
		{
			name: "AC-02: re-enable recording prompts",
			apply: func(s *Settings) error {
				if err := s.SetRecordingPromptsEnabled(false, t0); err != nil {
					return err
				}
				return s.SetRecordingPromptsEnabled(true, t1)
			},
			want: snapshot{prompts: true, acOnly: true, notify: true, updatedAt: t1},
		},
		{
			name:  "AC-22: disable the AC-power restriction",
			apply: func(s *Settings) error { return s.SetProcessOnlyOnACPower(false, t1) },
			want:  snapshot{prompts: true, notify: true, updatedAt: t1},
		},
		{
			name:  "disable notifications",
			apply: func(s *Settings) error { return s.SetNotifyOnProcessingComplete(false, t1) },
			want:  snapshot{prompts: true, acOnly: true, updatedAt: t1},
		},
		{
			name:  "select default model",
			apply: func(s *Settings) error { return s.SelectDefaultSttModel("model-1", t1) },
			want:  snapshot{prompts: true, acOnly: true, notify: true, model: "model-1", updatedAt: t1},
		},
		{
			name: "clear default model",
			apply: func(s *Settings) error {
				if err := s.SelectDefaultSttModel("model-1", t0); err != nil {
					return err
				}
				return s.ClearDefaultSttModel(t1)
			},
			want: snapshot{prompts: true, acOnly: true, notify: true, updatedAt: t1},
		},
		{
			name:  "setting the current value is a no-op",
			apply: func(s *Settings) error { return s.SetProcessOnlyOnACPower(true, t1) },
			want:  defaults,
		},
		{
			name:  "clearing without a default is a no-op",
			apply: func(s *Settings) error { return s.ClearDefaultSttModel(t1) },
			want:  defaults,
		},
		{
			name:    "missing change time",
			apply:   func(s *Settings) error { return s.SetRecordingPromptsEnabled(false, time.Time{}) },
			want:    defaults,
			wantErr: ErrInvalidSettings,
		},
		{
			name:    "missing change time with the current value",
			apply:   func(s *Settings) error { return s.SetNotifyOnProcessingComplete(true, time.Time{}) },
			want:    defaults,
			wantErr: ErrInvalidSettings,
		},
		{
			name:    "empty model id",
			apply:   func(s *Settings) error { return s.SelectDefaultSttModel("", t1) },
			want:    defaults,
			wantErr: ErrInvalidSettings,
		},
		{
			name:    "malformed model id",
			apply:   func(s *Settings) error { return s.SelectDefaultSttModel("../model", t1) },
			want:    defaults,
			wantErr: ErrInvalidSettings,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := New()

			err := tc.apply(s)
			if tc.wantErr == nil && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if snap(s) != tc.want {
				t.Fatalf("settings = %+v, want %+v", snap(s), tc.want)
			}
		})
	}
}

func TestChangeKeepsVersion(t *testing.T) {
	s, err := Restore(State{Version: 4, RecordingPromptsEnabled: true})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.SetRecordingPromptsEnabled(false, t1); err != nil {
		t.Fatal(err)
	}
	if s.Version() != 4 {
		t.Fatalf("Version() = %d after a change, want 4: only the repository moves it", s.Version())
	}

	s.Persisted(5)
	if s.Version() != 5 {
		t.Fatalf("Version() = %d after Persisted(5)", s.Version())
	}
}

// TASK-059: the picker remembers an app by its executable name; empty is the whole computer.
func TestRecordingSource(t *testing.T) {
	s := New()
	if s.RecordingSource() != "" {
		t.Fatalf("default source %q, want the whole computer", s.RecordingSource())
	}
	if err := s.SetRecordingSource("zoom.exe", t1); err != nil || s.RecordingSource() != "zoom.exe" || !s.UpdatedAt().Equal(t1) {
		t.Fatalf("SetRecordingSource = %v, source %q, updated %v", err, s.RecordingSource(), s.UpdatedAt())
	}
	for _, bad := range []shared.AppName{"Zoom.exe", `c:\zoom.exe`, "zoom", "zo\x00om.exe"} {
		if err := s.SetRecordingSource(bad, t1); !errors.Is(err, ErrInvalidSettings) || s.RecordingSource() != "zoom.exe" {
			t.Errorf("SetRecordingSource(%q) = %v, source %q", bad, err, s.RecordingSource())
		}
	}
	if err := s.SetRecordingSource("zoom.exe", time.Time{}); !errors.Is(err, ErrInvalidSettings) {
		t.Errorf("no change time: %v", err)
	}
	if err := s.SetRecordingSource("", t1.Add(time.Minute)); err != nil || s.RecordingSource() != "" {
		t.Fatalf("back to the whole computer: %v, %q", err, s.RecordingSource())
	}

	got, err := Restore(State{Version: 2, RecordingSource: "chrome.exe"})
	if err != nil || got.RecordingSource() != "chrome.exe" {
		t.Fatalf("Restore = %v, %v", got, err)
	}
	if _, err := Restore(State{Version: 2, RecordingSource: "../chrome.exe"}); !errors.Is(err, ErrInvalidSettings) {
		t.Fatalf("Restore of a tampered source = %v", err)
	}
}
