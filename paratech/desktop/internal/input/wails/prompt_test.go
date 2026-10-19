package wails

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/cases/recordingprompt"
	"paratech/desktop/internal/domain/recording/prompt"
	"paratech/desktop/internal/domain/recording/values"
)

// fakePrompt is the recording prompt use case behind the binding.
type fakePrompt struct {
	view       recordingprompt.View
	has        bool
	err        error
	calls      []string
	gotIDs     []prompt.ID
	gotSources []values.Source

	sources    recordingprompt.Sources
	sourcesErr error

	manual    recordingprompt.Manual
	manualErr error
	selectErr error

	advice recordingprompt.Advice
}

func (f *fakePrompt) Current() (recordingprompt.View, bool) { return f.view, f.has }

func (f *fakePrompt) Advice() (recordingprompt.Advice, bool) {
	return f.advice, !f.advice.Advice.IsZero()
}

func (f *fakePrompt) Sources(context.Context) (recordingprompt.Sources, error) {
	f.calls = append(f.calls, "sources")
	return f.sources, f.sourcesErr
}

func (f *fakePrompt) OfferAdHoc(context.Context) (recordingprompt.View, error) {
	f.calls = append(f.calls, "offer")
	return f.view, f.err
}

func (f *fakePrompt) Start(_ context.Context, id prompt.ID, source values.Source) (recordingprompt.View, error) {
	f.gotSources = append(f.gotSources, source)
	return f.record("start", id)
}

func (f *fakePrompt) Skip(_ context.Context, id prompt.ID) (recordingprompt.View, error) {
	return f.record("skip", id)
}

func (f *fakePrompt) SelectSource(_ context.Context, source values.Source) error {
	f.calls = append(f.calls, "select")
	f.gotSources = append(f.gotSources, source)
	return f.selectErr
}

func (f *fakePrompt) StartManual(_ context.Context, source values.Source) (recordingprompt.Manual, error) {
	f.calls = append(f.calls, "manual")
	f.gotSources = append(f.gotSources, source)
	return f.manual, f.manualErr
}

func (f *fakePrompt) record(call string, id prompt.ID) (recordingprompt.View, error) {
	f.calls = append(f.calls, call)
	f.gotIDs = append(f.gotIDs, id)
	return f.view, f.err
}

var lessonStart = time.Date(2026, 10, 15, 6, 0, 0, 0, time.UTC) // 09:00 in Moscow

func lessonView(zone string) recordingprompt.View {
	return recordingprompt.View{
		ID: "prompt-1", Kind: prompt.KindLesson, State: prompt.StateOffered,
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
				`"state":"OFFERED","problem":""}`,
		},
		{
			name: "unknown zone falls back to UTC", uc: &fakePrompt{has: true, view: lessonView("Mars/Olympus")},
			want: `{"id":"prompt-1","kind":"lesson","title":"\u003cb\u003eHistory\u003c/b\u003e","start":"06:00","end":"07:30",` +
				`"state":"OFFERED","problem":""}`,
		},
		{
			name: "machine zone is never used", uc: &fakePrompt{has: true, view: lessonView("Local")},
			want: `{"id":"prompt-1","kind":"lesson","title":"\u003cb\u003eHistory\u003c/b\u003e","start":"06:00","end":"07:30",` +
				`"state":"OFFERED","problem":""}`,
		},
		{
			name: "ad hoc with a problem", uc: &fakePrompt{has: true, view: recordingprompt.View{
				ID: "prompt-2", Kind: prompt.KindAdHoc, State: prompt.StateOffered, Problem: prompt.ProblemUnavailable,
			}},
			want: `{"id":"prompt-2","kind":"adhoc","title":"","start":"","end":"","state":"OFFERED","problem":"unavailable"}`,
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

// zoneChildEnv marks the child process of TestPromptZoneWithoutGoRoot.
const zoneChildEnv = "WAILS_TEST_ZONE_CHILD"

// The installed app has no Go tree: on Windows (no system zone database) only the zone data embedded by prompt.go can
// load the lesson's zone. The child runs this test with an empty GOROOT and no ZONEINFO.
func TestPromptZoneWithoutGoRoot(t *testing.T) {
	if os.Getenv(zoneChildEnv) == "1" {
		if got := zoneOf("Europe/Moscow"); got.String() != "Europe/Moscow" {
			t.Fatalf("zoneOf(Europe/Moscow) = %s without GOROOT's zone files", got)
		}
		return
	}

	env := []string{zoneChildEnv + "=1", "GOROOT=" + t.TempDir()}
	for _, kv := range os.Environ() {
		if k, _, _ := strings.Cut(kv, "="); !strings.EqualFold(k, "GOROOT") && !strings.EqualFold(k, "ZONEINFO") {
			env = append(env, kv)
		}
	}
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestPromptZoneWithoutGoRoot$", "-test.count=1")
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child: %v\n%s", err, out)
	}
}

// Every problem kind has its own code; the page has a fixed text for each.
func TestPromptProblemCodes(t *testing.T) {
	want := map[prompt.Problem]string{
		prompt.ProblemUnavailable: "unavailable", prompt.ProblemInProgress: "in_progress",
		prompt.ProblemRecovering: "recovering", prompt.ProblemFailed: "failed", prompt.ProblemSourceGone: "source_gone",
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
				"start": func() (PromptView, error) { return s.Start(ctx, "prompt-1", "") },
				"skip":  func() (PromptView, error) { return s.Skip(ctx, "prompt-1") },
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
			if !slices.Equal(uc.gotIDs, []prompt.ID{"prompt-1", "prompt-1"}) {
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
		if _, err := s.Start(context.Background(), id, ""); !errors.Is(err, ErrInvalidID) {
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
	for _, e := range []error{ErrInvalidID, ErrPromptGone, ErrPromptRefused, ErrActionFailed, ErrInvalidSource} {
		if !strings.Contains(string(src), `"`+e.Error()+`"`) {
			t.Errorf("frontend/src/bindings/prompt.ts does not list the error %q", e)
		}
	}
}

// Every exported method is callable from the pages: the four the popup uses (no Snooze since TASK-076), the source
// picker and start of the main window (TASK-081) and the silent-source advice of the popup (TASK-082).
func TestPromptServiceMethodSet(t *testing.T) {
	typ := reflect.TypeFor[*PromptService]()
	var got []string
	for i := range typ.NumMethod() {
		got = append(got, typ.Method(i).Name)
	}
	if want := []string{"Advice", "Current", "SelectSource", "Skip", "Sources", "Start", "StartRecording"}; !slices.Equal(got, want) {
		t.Fatalf("exported methods %v, want %v", got, want)
	}
}
