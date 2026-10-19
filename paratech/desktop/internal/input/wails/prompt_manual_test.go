package wails

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"paratech/desktop/internal/cases/recordingprompt"
	"paratech/desktop/internal/domain/recording/prompt"
	"paratech/desktop/internal/domain/recording/values"
)

// TASK-081: the picker of the diagnostics page remembers the source through the use case; a bad name never reaches
// it and a failed save is a fixed error with the cause only in the log.
func TestPromptServiceSelectSource(t *testing.T) {
	ctx := context.Background()
	uc := &fakePrompt{}
	s := NewPromptService(uc, discard())
	for _, app := range []string{"", "zoom.exe", " Chrome.EXE "} {
		if err := s.SelectSource(ctx, app); err != nil {
			t.Fatalf("SelectSource(%q) = %v", app, err)
		}
	}
	want := []values.Source{values.WholeComputer(), mustSource(t, "zoom.exe"), mustSource(t, "chrome.exe")}
	if !slices.Equal(uc.gotSources, want) {
		t.Fatalf("sources %v, want %v", uc.gotSources, want)
	}

	uc.calls = nil
	for _, app := range []string{" ", "zoom", `c:\apps\zoom.exe`, "../zoom.exe"} {
		if err := s.SelectSource(ctx, app); !errors.Is(err, ErrInvalidSource) {
			t.Errorf("SelectSource(%q) = %v, want ErrInvalidSource", app, err)
		}
	}
	if len(uc.calls) != 0 {
		t.Fatalf("invalid sources reached the use case: %v", uc.calls)
	}

	var logs bytes.Buffer
	uc.selectErr = errors.New(`remember the source: open C:\Users\student\db: locked`)
	s = NewPromptService(uc, slog.New(slog.NewTextHandler(&logs, nil)))
	err := s.SelectSource(ctx, "zoom.exe")
	if !errors.Is(err, ErrActionFailed) || strings.Contains(err.Error(), "student") || !strings.Contains(logs.String(), "locked") {
		t.Fatalf("SelectSource = %v, log %q; want ErrActionFailed and the cause logged", err, logs.String())
	}
}

// TASK-081: a start from the main window answers started or the popup's problem code; only an unexpected error is an
// error.
func TestPromptServiceStartRecording(t *testing.T) {
	tests := []struct {
		name    string
		manual  recordingprompt.Manual
		err     error
		want    string
		wantErr error
	}{
		{
			name: "linked to the lesson in progress", manual: recordingprompt.Manual{Recording: "rec-1", Lesson: "lesson-1"},
			want: `{"started":true,"problem":"","recordingId":"rec-1","linked":true}`,
		},
		{
			name: "without a lesson", manual: recordingprompt.Manual{Recording: "rec-2"},
			want: `{"started":true,"problem":"","recordingId":"rec-2","linked":false}`,
		},
		{
			name:   "a recording runs",
			manual: recordingprompt.Manual{Lesson: "lesson-1", Problem: prompt.ProblemInProgress},
			err:    fmt.Errorf("start: %w: busy", recordingprompt.ErrStartFailed),
			want:   `{"started":false,"problem":"in_progress","recordingId":"","linked":false}`,
		},
		{
			name: "recovering", manual: recordingprompt.Manual{Problem: prompt.ProblemRecovering},
			err:  fmt.Errorf("start: %w: %w", recordingprompt.ErrStartFailed, recordingprompt.ErrNotReady),
			want: `{"started":false,"problem":"recovering","recordingId":"","linked":false}`,
		},
		{
			name: "app not running", manual: recordingprompt.Manual{Problem: prompt.ProblemSourceGone},
			err:  fmt.Errorf("start: %w", recordingprompt.ErrStartFailed),
			want: `{"started":false,"problem":"source_gone","recordingId":"","linked":false}`,
		},
		{
			name: "failed without a problem kind", err: fmt.Errorf("start: %w", recordingprompt.ErrStartFailed),
			want: `{"started":false,"problem":"failed","recordingId":"","linked":false}`,
		},
		{name: "unexpected", err: errors.New(`open C:\Users\student\db: locked`), wantErr: ErrActionFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			uc := &fakePrompt{manual: tt.manual, manualErr: tt.err}
			got, err := NewPromptService(uc, slog.New(slog.NewTextHandler(&logs, nil))).StartRecording(context.Background(), "zoom.exe")
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) || strings.Contains(err.Error(), "student") || got != (RecordingStart{}) {
					t.Fatalf("StartRecording = %+v, %v; want only %v", got, err, tt.wantErr)
				}
				if !strings.Contains(logs.String(), "locked") {
					t.Fatalf("the cause must be logged, got %q", logs.String())
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			if string(raw) != tt.want {
				t.Fatalf("JSON =\n%s\nwant\n%s", raw, tt.want)
			}
			if !slices.Equal(uc.gotSources, []values.Source{mustSource(t, "zoom.exe")}) {
				t.Fatalf("sources %v", uc.gotSources)
			}
		})
	}
}

func TestPromptServiceStartRecordingRefusesInvalidSources(t *testing.T) {
	uc := &fakePrompt{}
	s := NewPromptService(uc, discard())
	for _, app := range []string{" ", "zoom", "con.exe", "a\x00.exe"} {
		if _, err := s.StartRecording(context.Background(), app); !errors.Is(err, ErrInvalidSource) {
			t.Errorf("StartRecording(%q) = %v, want ErrInvalidSource", app, err)
		}
	}
	if len(uc.calls) != 0 {
		t.Fatalf("invalid sources reached the use case: %v", uc.calls)
	}
}
