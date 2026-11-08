package wails

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"

	"paratech/desktop/internal/cases/jobrunner"
	"paratech/desktop/internal/cases/transcribe"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/services/modelselection"
)

// TASK-392: «Отменить расшифровку» answers by the state it finds, so a repeated call is safe; only an invalid id is an
// error.
func TestActivityServiceCancelTranscription(t *testing.T) {
	tests := []struct {
		name      string
		id        string
		active    shared.JobID
		activeErr error
		cancelErr error
		want      string
		cancelled shared.JobID // the job handed to the runner; "" = none
		logged    bool
	}{
		{name: "running or waiting", id: "r1", active: "j1", want: "cancelled", cancelled: "j1"},
		{name: "nothing to cancel", id: "r1", want: "not_running"},
		{name: "the attempt is ending", id: "r1", active: "j1", want: "ending", cancelled: "j1",
			cancelErr: fmt.Errorf("%w: j1 is RUNNING", jobrunner.ErrNotCancellable)},
		{name: "gone meanwhile", id: "r1", active: "j1", want: "not_running", cancelled: "j1", cancelErr: shared.ErrNotFound},
		{name: "the runner fails", id: "r1", active: "j1", want: "failed", cancelled: "j1", logged: true,
			cancelErr: errors.New("database is locked")},
		{name: "the lookup fails", id: "r1", want: "failed", activeErr: errors.New("database is locked"), logged: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			forcer := &fakeForcer{err: tt.cancelErr}
			r := &fakeRetrier{active: tt.active, activeErr: tt.activeErr}
			s := NewActivityService(&fakeActivity{}, &fakeStopper{}, forcer, r, slog.New(slog.NewTextHandler(&logs, nil)))
			got, err := s.CancelTranscription(context.Background(), tt.id)
			if err != nil || got.Result != tt.want {
				t.Fatalf("CancelTranscription = %+v, %v; want %q", got, err, tt.want)
			}
			if forcer.got != tt.cancelled {
				t.Fatalf("cancelled %q, want %q", forcer.got, tt.cancelled)
			}
			if logged := logs.Len() > 0; logged != tt.logged {
				t.Fatalf("logged %v, want %v:\n%s", logged, tt.logged, logs.String())
			}
		})
	}
}

// TASK-392: «Расшифровать снова» maps the use case's refusals to codes; only an invalid id is an error.
func TestActivityServiceRetranscribe(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"started", nil, "started"},
		{"not allowed", fmt.Errorf("transcribe: start again r1: %w: x", transcribe.ErrNotAllowed), "not_allowed"},
		{"no model", fmt.Errorf("x: %w: %w", transcribe.ErrNoModel, modelselection.ErrModelNotInstalled), "no_model"},
		{"storage", errors.New(`open C:\Users\student\db: locked`), "failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &fakeRetrier{err: tt.err}
			s := NewActivityService(&fakeActivity{}, &fakeStopper{}, &fakeForcer{}, r, discard())
			got, err := s.Retranscribe(context.Background(), "r1")
			if err != nil || got.Result != tt.want {
				t.Fatalf("Retranscribe = %+v, %v; want %q", got, err, tt.want)
			}
			if strings.Join(r.calls, ";") != "retranscribe r1" {
				t.Fatalf("calls = %v", r.calls)
			}
		})
	}
}

func TestActivityServiceTranscriptionInvalidID(t *testing.T) {
	for _, id := range []string{"", strings.Repeat("r", 129), "r\n1"} {
		r, forcer := &fakeRetrier{active: "j1"}, &fakeForcer{}
		s := NewActivityService(&fakeActivity{}, &fakeStopper{}, forcer, r, discard())
		if _, err := s.CancelTranscription(context.Background(), id); !errors.Is(err, ErrInvalidID) {
			t.Fatalf("CancelTranscription(%q) = %v, want ErrInvalidID", id, err)
		}
		if _, err := s.Retranscribe(context.Background(), id); !errors.Is(err, ErrInvalidID) {
			t.Fatalf("Retranscribe(%q) = %v, want ErrInvalidID", id, err)
		}
		if len(r.calls) != 0 || forcer.got != "" {
			t.Fatalf("an invalid id reached the core: %v %q", r.calls, forcer.got)
		}
	}
}

// The page reads the answer by these names and codes (frontend/src/bindings/activity.ts).
func TestTranscriptionAnswerJSON(t *testing.T) {
	b, err := json.Marshal(TranscriptionAnswer{Result: transcriptionCancelled})
	if err != nil || string(b) != `{"result":"cancelled"}` {
		t.Fatalf("JSON = %s, %v", b, err)
	}
	src, err := os.ReadFile("../../../frontend/src/bindings/activity.ts")
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{
		transcriptionCancelled, transcriptionNotRunning, transcriptionEnding, transcriptionStarted, transcriptionNotAllowed,
		transcriptionNoModel, transcriptionFailed, `canCancel`, `transcriptionCancelled`, `retranscribe`,
	} {
		if !strings.Contains(string(src), `"`+code+`"`) {
			t.Errorf("frontend/src/bindings/activity.ts does not list %q", code)
		}
	}
}
