package wails

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"paratech/desktop/internal/cases/activity"
	"paratech/desktop/internal/cases/recordingprompt"
	"paratech/desktop/internal/cases/recordingsession"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/shared"
)

func ownerAdvice(t *testing.T) values.SourceAdvice {
	t.Helper()
	return values.SourceAdvice{
		Silent: "browser.exe", SilentName: "Яндекс Браузер", Suggest: mustSource(t, "comet.exe"), SuggestName: "<b>Comet</b>",
	}
}

const ownerAdviceJSON = `{"recordingId":"r1","silentApp":"browser.exe","silentName":"Яндекс Браузер",` +
	`"suggestApp":"comet.exe","suggestName":"\u003cb\u003eComet\u003c/b\u003e"}`

// TASK-082: the row of the running recording carries its advice; the other rows have none.
func TestActivityServiceListsTheAdvice(t *testing.T) {
	uc := &fakeActivity{recs: []activity.Recording{
		{ID: "r1", Status: recording.StatusRecording, Stoppable: true, Chain: activity.Chain{Stage: activity.StageRecording}},
		{ID: "r0", Status: recording.StatusCompleted, Chain: activity.Chain{
			Stage: activity.StageFailed, ErrorCode: activity.ErrorSilentRecording, Action: activity.ActionCheckSound,
		}},
	}}
	stopper := &fakeStopper{running: true, active: recordingsession.Info{ID: "r1", Advice: ownerAdvice(t)}}
	got, err := NewActivityService(uc, stopper, &fakeForcer{}, discard()).ListRecordings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(got[0].Advice)
	if err != nil || string(b) != ownerAdviceJSON {
		t.Fatalf("advice JSON = %s, %v; want %s", b, err, ownerAdviceJSON)
	}
	if got[1].Advice != nil || got[1].ErrorCode != "silentRecording" || got[1].Action != "checkSound" {
		t.Fatalf("silent row %+v", got[1])
	}

	// The whole computer is suggested with empty app and name.
	stopper.active.Advice.Suggest, stopper.active.Advice.SuggestName = values.WholeComputer(), ""
	got, _ = NewActivityService(uc, stopper, &fakeForcer{}, discard()).ListRecordings(context.Background())
	if a := got[0].Advice; a == nil || a.SuggestApp != "" || a.SuggestName != "" {
		t.Fatalf("advice %+v", a)
	}
}

func TestActivityServiceSwitchSource(t *testing.T) {
	secret := errors.New(`C:\Users\student\secret.db is locked`)
	tests := []struct {
		name    string
		id, app string
		err     error
		want    SourceSwitch
		wantErr error
	}{
		{name: "switched", id: "r1", app: "comet.exe", want: SourceSwitch{Switched: true}},
		{name: "to the whole computer", id: "r1", app: "", want: SourceSwitch{Switched: true}},
		{name: "the app is gone", id: "r1", app: "comet.exe",
			err: fmt.Errorf("switch: %w", recording.ErrSourceNotRunning), want: SourceSwitch{Problem: "source_gone"}},
		{name: "refused", id: "r1", app: "comet.exe",
			err: fmt.Errorf("switch: %w", recordingsession.ErrSwitchRefused), want: SourceSwitch{Problem: "refused"}},
		{name: "not running", id: "r1", app: "comet.exe",
			err: fmt.Errorf("switch: %w", recordingsession.ErrNotRunning), wantErr: ErrRecordingNotRunning},
		{name: "unknown", id: "r1", app: "comet.exe", err: shared.ErrNotFound, wantErr: ErrItemNotFound},
		{name: "other failure", id: "r1", app: "comet.exe", err: secret, wantErr: ErrActionFailed},
		{name: "bad id", id: `..\db`, app: "comet.exe", wantErr: ErrInvalidID},
		{name: "bad app", id: "r1", app: `C:\x\comet.exe`, wantErr: ErrInvalidSource},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			stopper := &fakeStopper{err: tt.err}
			s := NewActivityService(&fakeActivity{}, stopper, &fakeForcer{}, slog.New(slog.NewTextHandler(&logs, nil)))
			got, err := s.SwitchSource(context.Background(), tt.id, tt.app)
			if !errors.Is(err, tt.wantErr) || got != tt.want {
				t.Fatalf("SwitchSource = %+v, %v; want %+v, %v", got, err, tt.want, tt.wantErr)
			}
			if tt.wantErr == ErrInvalidID || tt.wantErr == ErrInvalidSource {
				if len(stopper.calls) != 0 {
					t.Fatal("a malformed request reached the use case")
				}
				return
			}
			if stopper.gotSource.App().String() != tt.app {
				t.Fatalf("source %v, want %q", stopper.gotSource, tt.app)
			}
			if err != nil && strings.Contains(err.Error(), "secret") {
				t.Fatal("the cause reached the page")
			}
		})
	}
}

func TestActivityServiceDismissAdvice(t *testing.T) {
	tests := []struct {
		name string
		id   string
		err  error
		want error
	}{
		{name: "dismissed", id: "r1"},
		{name: "not running", id: "r1", err: recordingsession.ErrNotRunning, want: ErrRecordingNotRunning},
		{name: "unknown", id: "r1", err: shared.ErrNotFound, want: ErrItemNotFound},
		{name: "bad id", id: "", want: ErrInvalidID},
	}
	for _, tt := range tests {
		stopper := &fakeStopper{err: tt.err}
		err := NewActivityService(&fakeActivity{}, stopper, &fakeForcer{}, discard()).DismissAdvice(context.Background(), tt.id)
		if !errors.Is(err, tt.want) {
			t.Errorf("%s: DismissAdvice = %v, want %v", tt.name, err, tt.want)
		}
	}
}

// The popup reads the advice it shows.
func TestPromptServiceAdvice(t *testing.T) {
	if got := NewPromptService(&fakePrompt{}, discard()).Advice(); got != nil {
		t.Fatalf("Advice without one = %+v", got)
	}
	uc := &fakePrompt{advice: recordingprompt.Advice{Recording: "r1", Advice: ownerAdvice(t)}}
	b, err := json.Marshal(NewPromptService(uc, discard()).Advice())
	if err != nil || string(b) != ownerAdviceJSON {
		t.Fatalf("Advice JSON = %s, %v", b, err)
	}
}
