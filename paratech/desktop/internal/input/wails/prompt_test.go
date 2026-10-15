package wails

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
	_ "time/tzdata" // the zone test must not depend on the zone files of the machine

	"paratech/desktop/internal/cases/recordingprompt"
	"paratech/desktop/internal/domain/recording/prompt"
)

// fakePrompt is the recording prompt use case behind the binding.
type fakePrompt struct {
	view   recordingprompt.View
	has    bool
	err    error
	calls  []string
	gotIDs []prompt.ID
}

func (f *fakePrompt) Current() (recordingprompt.View, bool) { return f.view, f.has }

func (f *fakePrompt) OfferAdHoc(context.Context) (recordingprompt.View, error) {
	f.calls = append(f.calls, "offer")
	return f.view, f.err
}

func (f *fakePrompt) Start(_ context.Context, id prompt.ID) (recordingprompt.View, error) {
	return f.record("start", id)
}

func (f *fakePrompt) Snooze(_ context.Context, id prompt.ID) (recordingprompt.View, error) {
	return f.record("snooze", id)
}

func (f *fakePrompt) Skip(_ context.Context, id prompt.ID) (recordingprompt.View, error) {
	return f.record("skip", id)
}

func (f *fakePrompt) record(call string, id prompt.ID) (recordingprompt.View, error) {
	f.calls = append(f.calls, call)
	f.gotIDs = append(f.gotIDs, id)
	return f.view, f.err
}

var lessonStart = time.Date(2026, 10, 15, 6, 0, 0, 0, time.UTC) // 09:00 in Moscow

func lessonView(zone string) recordingprompt.View {
	return recordingprompt.View{
		ID: "prompt-1", Kind: prompt.KindLesson, State: prompt.StateOffered, CanSnooze: true,
		Lesson: prompt.Lesson{
			ID: "lesson-1", Title: "<b>History</b>", Start: lessonStart, End: lessonStart.Add(90 * time.Minute), TimeZone: zone,
		},
	}
}

// The page reads these names (frontend/src/bindings/prompt.ts); times are shown in the lesson's zone.
func TestPromptServiceCurrent(t *testing.T) {
	tests := []struct {
		name string
		uc   *fakePrompt
		want string
	}{
		{name: "no prompt", uc: &fakePrompt{}, want: `null`},
		{
			name: "lesson in its zone", uc: &fakePrompt{has: true, view: lessonView("Europe/Moscow")},
			want: `{"id":"prompt-1","kind":"lesson","title":"\u003cb\u003eHistory\u003c/b\u003e","start":"09:00","end":"10:30",` +
				`"state":"OFFERED","problem":"","canSnooze":true}`,
		},
		{
			name: "unknown zone falls back to UTC", uc: &fakePrompt{has: true, view: lessonView("Mars/Olympus")},
			want: `{"id":"prompt-1","kind":"lesson","title":"\u003cb\u003eHistory\u003c/b\u003e","start":"06:00","end":"07:30",` +
				`"state":"OFFERED","problem":"","canSnooze":true}`,
		},
		{
			name: "machine zone is never used", uc: &fakePrompt{has: true, view: lessonView("Local")},
			want: `{"id":"prompt-1","kind":"lesson","title":"\u003cb\u003eHistory\u003c/b\u003e","start":"06:00","end":"07:30",` +
				`"state":"OFFERED","problem":"","canSnooze":true}`,
		},
		{
			name: "ad hoc with a problem", uc: &fakePrompt{has: true, view: recordingprompt.View{
				ID: "prompt-2", Kind: prompt.KindAdHoc, State: prompt.StateOffered, Problem: prompt.ProblemUnavailable,
			}},
			want: `{"id":"prompt-2","kind":"adhoc","title":"","start":"","end":"","state":"OFFERED","problem":"unavailable","canSnooze":false}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := json.Marshal(NewPromptService(tt.uc, discard()).Current())
			if err != nil {
				t.Fatal(err)
			}
			if string(raw) != tt.want {
				t.Fatalf("JSON =\n%s\nwant\n%s", raw, tt.want)
			}
		})
	}
}

// Every problem kind has its own code; the page has a fixed text for each.
func TestPromptProblemCodes(t *testing.T) {
	want := map[prompt.Problem]string{
		prompt.ProblemUnavailable: "unavailable", prompt.ProblemInProgress: "in_progress",
		prompt.ProblemRecovering: "recovering", prompt.ProblemFailed: "failed",
	}
	for _, p := range prompt.Problems() {
		if got := problemCode(p); got != want[p] || got == "" {
			t.Errorf("problemCode(%s) = %q, want %q", p, got, want[p])
		}
	}
	if problemCode(prompt.ProblemNone) != "" {
		t.Error("no problem must have an empty code")
	}
}

// Actions reach the use case with the page's ID; refusals become fixed texts and a failed start is a view, not an
// error, so the page shows its problem.
func TestPromptServiceActions(t *testing.T) {
	ctx := context.Background()
	failed := lessonView("Europe/Moscow")
	failed.Problem = prompt.ProblemInProgress
	tests := []struct {
		name    string
		err     error
		wantErr error
		problem string
	}{
		{name: "ok"},
		{name: "start failed", err: fmt.Errorf("start: %w: busy", recordingprompt.ErrStartFailed), problem: "in_progress"},
		{name: "stale prompt", err: fmt.Errorf("start: %w", recordingprompt.ErrNoPrompt), wantErr: ErrPromptGone},
		{name: "start running", err: fmt.Errorf("start: %w", prompt.ErrInvalidTransition), wantErr: ErrPromptRefused},
		{name: "snooze not allowed", err: fmt.Errorf("snooze: %w", prompt.ErrSnoozeNotAllowed), wantErr: ErrPromptRefused},
		{name: "unexpected", err: errors.New(`open C:\Users\student\db: locked`), wantErr: ErrActionFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			view := lessonView("Europe/Moscow")
			if tt.problem != "" {
				view = failed
			}
			var logs bytes.Buffer
			uc := &fakePrompt{view: view, err: tt.err}
			s := NewPromptService(uc, slog.New(slog.NewTextHandler(&logs, nil)))
			calls := map[string]func() (PromptView, error){
				"start":  func() (PromptView, error) { return s.Start(ctx, "prompt-1") },
				"snooze": func() (PromptView, error) { return s.Snooze(ctx, "prompt-1") },
				"skip":   func() (PromptView, error) { return s.Skip(ctx, "prompt-1") },
			}
			for name, call := range calls {
				got, err := call()
				if tt.wantErr != nil {
					if !errors.Is(err, tt.wantErr) || strings.Contains(err.Error(), "student") || got != (PromptView{}) {
						t.Errorf("%s = %+v, %v; want only %v", name, got, err, tt.wantErr)
					}
					continue
				}
				if err != nil || got.ID != "prompt-1" || got.Problem != tt.problem {
					t.Errorf("%s = %+v, %v", name, got, err)
				}
			}
			if !slices.Equal(uc.gotIDs, []prompt.ID{"prompt-1", "prompt-1", "prompt-1"}) {
				t.Errorf("ids %v", uc.gotIDs)
			}
			if tt.wantErr == ErrActionFailed && !strings.Contains(logs.String(), "locked") {
				t.Errorf("the cause must be logged, got %q", logs.String())
			}
		})
	}
}

func TestPromptServiceRefusesInvalidIDs(t *testing.T) {
	uc := &fakePrompt{}
	s := NewPromptService(uc, discard())
	for _, id := range []string{"", "../x", "a b", strings.Repeat("p", 65)} {
		if _, err := s.Start(context.Background(), id); !errors.Is(err, ErrInvalidID) {
			t.Errorf("Start(%q) = %v, want ErrInvalidID", id, err)
		}
	}
	if len(uc.calls) != 0 {
		t.Fatalf("invalid IDs reached the use case: %v", uc.calls)
	}
}

// The tray's offer is not callable from the page, and its error is only logged.
func TestPromptServiceOfferAdHoc(t *testing.T) {
	var logs bytes.Buffer
	uc := &fakePrompt{err: errors.New("popup not running")}
	NewPromptService(uc, slog.New(slog.NewTextHandler(&logs, nil))).offerAdHoc(context.Background())
	if !slices.Equal(uc.calls, []string{"offer"}) || !strings.Contains(logs.String(), "popup not running") {
		t.Fatalf("calls %v, log %q", uc.calls, logs.String())
	}
}

// The popup may match the error messages, so the frontend wrapper lists each one PromptService returns.
func TestFrontendKnowsPromptErrors(t *testing.T) {
	src, err := os.ReadFile("../../../frontend/src/bindings/prompt.ts")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range []error{ErrInvalidID, ErrPromptGone, ErrPromptRefused, ErrActionFailed} {
		if !strings.Contains(string(src), `"`+e.Error()+`"`) {
			t.Errorf("frontend/src/bindings/prompt.ts does not list the error %q", e)
		}
	}
}

// Every exported method is callable from the page: keep the set to the four the popup uses.
func TestPromptServiceMethodSet(t *testing.T) {
	typ := reflect.TypeFor[*PromptService]()
	var got []string
	for i := range typ.NumMethod() {
		got = append(got, typ.Method(i).Name)
	}
	if want := []string{"Current", "Skip", "Snooze", "Start"}; !slices.Equal(got, want) {
		t.Fatalf("exported methods %v, want %v", got, want)
	}
}
