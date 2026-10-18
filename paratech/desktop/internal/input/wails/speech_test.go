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
	"sync"
	"testing"

	"paratech/desktop/internal/cases/speechcomponent"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/values"
)

// fakeSpeech is the speech component use case with a settable status.
type fakeSpeech struct {
	mu        sync.Mutex
	status    speechcomponent.Status
	startErr  error
	starts    int
	cancelled int
}

func (f *fakeSpeech) Status() speechcomponent.Status {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.status.State == "" {
		return speechcomponent.Status{State: speechcomponent.StateMissing}
	}
	return f.status
}

func (f *fakeSpeech) set(st speechcomponent.Status) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status = st
}

func (f *fakeSpeech) StartInstall() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.starts++
	return f.startErr
}

func (f *fakeSpeech) CancelInstall() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cancelled++
}

func TestSpeechStatusView(t *testing.T) {
	tests := []struct {
		name   string
		status speechcomponent.Status
		want   SpeechStatusView
	}{
		{"ready", speechcomponent.Status{State: speechcomponent.StateReady}, SpeechStatusView{State: "ready"}},
		{"missing", speechcomponent.Status{State: speechcomponent.StateMissing},
			SpeechStatusView{State: "missing", Message: string(SpeechMissing), CanInstall: true}},
		{"damaged", speechcomponent.Status{State: speechcomponent.StateDamaged},
			SpeechStatusView{State: "damaged", Message: string(SpeechDamaged), CanInstall: true}},
		{"installing", speechcomponent.Status{State: speechcomponent.StateInstalling, Stage: speechcomponent.StageDownloading,
			DoneBytes: 5, TotalBytes: 9},
			SpeechStatusView{State: "installing", Stage: "downloading", DoneBytes: 5, TotalBytes: 9, CanCancel: true}},
		{"failed", speechcomponent.Status{State: speechcomponent.StateFailed, Err: speechcomponent.ErrDownloadFailed},
			SpeechStatusView{State: "failed", Message: string(ErrSpeechOffline), CanInstall: true}},
		// A reinstall does not fix verified files that did not start, at start or right after an install.
		{"not started", speechcomponent.Status{State: speechcomponent.StateFailed, Err: speechcomponent.ErrActivationFailed},
			SpeechStatusView{State: "failed", Message: string(ErrSpeechNotStarted)}},
		{"not started after an install", speechcomponent.Status{State: speechcomponent.StateFailed,
			Err: fmt.Errorf("speechcomponent: install: %w: %w", speechcomponent.ErrActivationFailed, errors.New("engine"))},
			SpeechStatusView{State: "failed", Message: string(ErrSpeechNotStarted)}},
		{"not verified after an install", speechcomponent.Status{State: speechcomponent.StateFailed,
			Err: speechcomponent.ErrNotVerified},
			SpeechStatusView{State: "failed", Message: string(ErrSpeechCorrupted), CanInstall: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewSpeechService(&fakeSpeech{status: tt.status}, discard())
			if got := s.Status(context.Background()); got != tt.want {
				t.Fatalf("Status = %+v, want %+v", got, tt.want)
			}
		})
	}

	b, err := json.Marshal(SpeechStatusView{})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(b, &fields); err != nil {
		t.Fatal(err)
	}
	var keys []string
	for k := range fields {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	if got := strings.Join(keys, ","); got != "canCancel,canInstall,doneBytes,message,stage,state,totalBytes" {
		t.Fatalf("JSON fields %s", got)
	}
}

func TestSpeechErrorTexts(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{fmt.Errorf("x: %w", speechcomponent.ErrDownloadFailed), string(ErrSpeechOffline)},
		{values.ErrHostNotAllowed, string(ErrSpeechRefused)},
		{values.ErrUnexpectedRange, string(ErrSpeechRefused)},
		{values.ErrChecksumMismatch, string(ErrSpeechCorrupted)},
		{values.ErrSizeMismatch, string(ErrSpeechCorrupted)},
		{fmt.Errorf("%w: %w", values.ErrInvalidComponentArchive, values.ErrSizeMismatch), string(ErrSpeechCorrupted)},
		{speechcomponent.ErrNotVerified, string(ErrSpeechCorrupted)},
		{fmt.Errorf("%w: %w", speechcomponent.ErrActivationFailed, errors.New("runner stopped")), string(ErrSpeechNotStarted)},
		{&values.InsufficientSpaceError{Required: 80 << 20, Available: 1 << 20}, "Недостаточно места на диске: нужно 80 МБ, свободно 1 МБ"},
		{shared.ErrStorageFull, string(ErrNotEnoughSpace)},
		{shared.ErrStoragePermission, string(ErrSpeechNoAccess)},
		{context.Canceled, string(ErrSpeechFailed)},
		{errors.New("something else"), string(ErrSpeechFailed)},
	}
	for _, tt := range tests {
		if got := speechErrorText(tt.err); got != tt.want {
			t.Errorf("speechErrorText(%v) = %q, want %q", tt.err, got, tt.want)
		}
	}
}

// No text the pages show names the technical parts (TASK-072).
func TestSpeechTextsHaveNoTechnicalNames(t *testing.T) {
	texts := []speechText{SpeechMissing, SpeechDamaged, ErrSpeechOffline, ErrSpeechCorrupted, ErrSpeechRefused,
		ErrSpeechNoAccess, ErrSpeechNotStarted, ErrSpeechFailed, ErrSpeechInstalling, ErrSpeechAlreadyWork}
	model := []modelError{ErrModelFileTooLarge, ErrModelFileEmpty, ErrModelFileType}
	all := make([]string, 0, len(texts)+len(model))
	for _, s := range texts {
		all = append(all, string(s))
	}
	for _, s := range model {
		all = append(all, string(s))
	}
	for _, s := range all {
		low := strings.ToLower(s)
		for _, word := range []string{"whisper", "exe", "dll", "learning-assistant"} {
			if strings.Contains(low, word) {
				t.Errorf("%q names %q", s, word)
			}
		}
	}
}

func TestSpeechInstallActions(t *testing.T) {
	var logs bytes.Buffer
	uc := &fakeSpeech{}
	s := NewSpeechService(uc, slog.New(slog.NewTextHandler(&logs, nil)))
	ctx := context.Background()

	if err := s.StartInstall(ctx); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		err  error
		want error
	}{
		{speechcomponent.ErrInstallInProgress, ErrSpeechInstalling},
		{speechcomponent.ErrAlreadyActive, ErrSpeechAlreadyWork},
		{speechcomponent.ErrClosed, ErrSpeechFailed},
	} {
		uc.startErr = tt.err
		if err := s.StartInstall(ctx); !errors.Is(err, tt.want) {
			t.Errorf("StartInstall with %v = %v, want %v", tt.err, err, tt.want)
		}
	}
	s.CancelInstall()
	if uc.starts != 4 || uc.cancelled != 1 {
		t.Fatalf("starts %d, cancels %d", uc.starts, uc.cancelled)
	}
	if !strings.Contains(logs.String(), "speech component") {
		t.Fatalf("the unexpected cause must be logged: %q", logs.String())
	}
}

// The models page reads the status on every List: an install while the app runs shows at once.
func TestModelsListShowsTheSpeechStatusNow(t *testing.T) {
	f := newModelFixture(t)
	speech := &fakeSpeech{status: speechcomponent.Status{State: speechcomponent.StateMissing}}
	f.s.deps.Speech = speech
	ctx := context.Background()
	if v, err := f.s.List(ctx); err != nil || v.SpeechReady {
		t.Fatalf("List = %+v, %v; want not ready", v.SpeechReady, err)
	}
	speech.set(speechcomponent.Status{State: speechcomponent.StateReady})
	if v, err := f.s.List(ctx); err != nil || !v.SpeechReady {
		t.Fatalf("List = %+v, %v; want ready", v.SpeechReady, err)
	}
}
