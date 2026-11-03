package agent

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"paratech/desktop/internal/cases/recordingprompt"
	"paratech/desktop/internal/cases/recordingsession"
	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/agent/services/actionpolicy"
	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/recording/prompt"
	recvalues "paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/shared"
)

// memRecorder is «Начать запись» and the running recording: the picker's preselection, the starts with their source
// and the stops. No device is touched.
type memRecorder struct {
	active     *recordingsession.Info
	sources    recordingprompt.Sources
	sourcesErr error
	problem    prompt.Problem // set: StartManual refuses with it
	started    []recvalues.Source
	stops      []shared.RecordingID
	stopErr    error
	next       int
}

func newMemRecorder() *memRecorder {
	return &memRecorder{sources: recordingprompt.Sources{Selected: recvalues.WholeComputer()}}
}

func (m *memRecorder) Sources(context.Context) (recordingprompt.Sources, error) {
	return m.sources, m.sourcesErr
}

func (m *memRecorder) StartManual(_ context.Context, source recvalues.Source) (recordingprompt.Manual, error) {
	m.started = append(m.started, source)
	if m.problem != "" {
		return recordingprompt.Manual{Problem: m.problem}, fmt.Errorf("start: %w", recordingprompt.ErrStartFailed)
	}
	m.next++
	id := shared.RecordingID(fmt.Sprintf("rec-%d", m.next))
	m.active = &recordingsession.Info{ID: id}
	return recordingprompt.Manual{Recording: id}, nil
}

func (m *memRecorder) Active() (recordingsession.Info, bool) {
	if m.active == nil {
		return recordingsession.Info{}, false
	}
	return *m.active, true
}

func (m *memRecorder) Stop(_ context.Context, id shared.RecordingID) (recordingsession.Info, error) {
	m.stops = append(m.stops, id)
	if m.stopErr != nil {
		return recordingsession.Info{}, m.stopErr
	}
	if m.active != nil && m.active.ID == id {
		m.active = nil
	}
	return recordingsession.Info{ID: id}, nil
}

func appSource(t *testing.T, app string) recvalues.Source {
	t.Helper()
	name, err := shared.ParseAppName(app)
	if err != nil {
		t.Fatal(err)
	}
	s, err := recvalues.SourceOf(name)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

const (
	startArgs = `{"action":"start"}`
	stopArgs  = `{"action":"stop"}`
)

// start records the source the page's picker preselects, at once; its Undo is StopRecording of exactly that
// recording, which stops it and keeps the audio.
func TestRecordingStartAndUndo(t *testing.T) {
	w, f := newControlsWorld(t)
	zoom := appSource(t, "zoom.exe")
	f.rec.sources = recordingprompt.Sources{Selected: zoom, Remembered: zoom.App()}

	r, e := run10(t, w, req, call10(t, "c1", values.ToolRecording, startArgs, actionpolicy.ClassU))
	if r.out.Result.ResultJSON != `{"changed":true,"done":true,"recording":"rec-1","state":"started"}` {
		t.Fatalf("result = %s", r.out.Result.ResultJSON)
	}
	if len(f.rec.started) != 1 || f.rec.started[0] != zoom {
		t.Fatalf("started = %v, want the preselected app", f.rec.started)
	}
	rec := mustRef(t, values.KindRecording, "rec-1")
	if e.Status != action.StatusDone || !e.CanUndo || e.Compensation != (action.StopRecording{Recording: rec}) {
		t.Fatalf("entry = %+v, want DONE with StopRecording rec-1", e)
	}
	if len(r.cards) != 1 || r.cards[0] != (values.ActionLine{Action: e.ID}) || len(r.out.Seen) != 1 || r.out.Seen[0] != rec {
		t.Fatalf("cards %+v, seen %+v", r.cards, r.out.Seen)
	}

	// The same call again answers from its entry: nothing starts twice.
	again, _ := run10(t, w, req, call10(t, "c1", values.ToolRecording, startArgs, actionpolicy.ClassU))
	if again.out.Result.ResultJSON != r.out.Result.ResultJSON || len(f.rec.started) != 1 {
		t.Fatalf("replay = %s, starts %d", again.out.Result.ResultJSON, len(f.rec.started))
	}

	if u := undo(t, w, e); u.Status != action.StatusUndone || f.rec.active != nil || len(f.rec.stops) != 1 || f.rec.stops[0] != "rec-1" {
		t.Fatalf("undo = %+v, active %+v, stops %v", u, f.rec.active, f.rec.stops)
	}
}

// The source mirrors the page: whatever the core preselects, and the whole computer when the apps cannot be listed
// (the page then offers only it). The model gives no source.
func TestRecordingStartSource(t *testing.T) {
	tests := []struct {
		name    string
		sources recordingprompt.Sources
		err     error
		want    recvalues.Source
	}{
		{"remembered app open", recordingprompt.Sources{Selected: appSource(t, "zoom.exe")}, nil, appSource(t, "zoom.exe")},
		{"another app sounds", recordingprompt.Sources{Selected: appSource(t, "chrome.exe"), AutoSelected: true}, nil, appSource(t, "chrome.exe")},
		{"remembered app closed", recordingprompt.Sources{Selected: recvalues.WholeComputer(), Remembered: appSource(t, "zoom.exe").App()}, nil, recvalues.WholeComputer()},
		{"apps cannot be listed", recordingprompt.Sources{Selected: appSource(t, "zoom.exe")}, errors.New("wasapi"), recvalues.WholeComputer()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, f := newControlsWorld(t)
			f.rec.sources, f.rec.sourcesErr = tt.sources, tt.err
			run10(t, w, req, call10(t, "c1", values.ToolRecording, startArgs, actionpolicy.ClassU))
			if len(f.rec.started) != 1 || f.rec.started[0] != tt.want {
				t.Fatalf("started = %v, want %v", f.rec.started, tt.want)
			}
		})
	}
}

// start while a recording runs starts nothing and journals nothing; it names the running one.
func TestRecordingStartWhileRecording(t *testing.T) {
	w, f := newControlsWorld(t)
	f.rec.active = &recordingsession.Info{ID: "rec-7"}
	r, e := run10(t, w, req, call10(t, "c1", values.ToolRecording, startArgs, actionpolicy.ClassU))
	if r.out.Result.ResultJSON != `{"changed":false,"done":true,"recording":"rec-7","state":"already_recording"}` ||
		e.ID != "" || len(f.rec.started) != 0 || len(r.cards) != 0 {
		t.Fatalf("result = %s, entry %+v, starts %d", r.out.Result.ResultJSON, e, len(f.rec.started))
	}
}

// A refused start is an honest error of its problem; the entry is FAILED.
func TestRecordingStartRefused(t *testing.T) {
	tests := []struct {
		problem prompt.Problem
		want    string
	}{
		{prompt.ProblemSourceGone, `{"error":"not_found"}`},
		{prompt.ProblemInProgress, `{"error":"not_now"}`},
		{prompt.ProblemRecovering, `{"error":"not_now"}`},
		{prompt.ProblemUnavailable, `{"error":"unavailable"}`},
		{prompt.ProblemFailed, `{"error":"failed"}`},
	}
	for _, tt := range tests {
		t.Run(string(tt.problem), func(t *testing.T) {
			w, f := newControlsWorld(t)
			f.rec.problem = tt.problem
			r, e := run10(t, w, req, call10(t, "c1", values.ToolRecording, startArgs, actionpolicy.ClassU))
			if r.out.Result.ResultJSON != tt.want || e.Status != action.StatusFailed || e.CanUndo {
				t.Fatalf("result = %s, entry %+v", r.out.Result.ResultJSON, e)
			}
		})
	}
}

// Undo of a start after the student stopped it (or while another recording runs) stops nothing: the effect of the
// Undo is already there.
func TestRecordingUndoAfterTheStudentStopped(t *testing.T) {
	for name, change := range map[string]func(f *controlFakes){
		"stopped":           func(f *controlFakes) { f.rec.active = nil },
		"another one runs":  func(f *controlFakes) { f.rec.active = &recordingsession.Info{ID: "rec-9"} },
		"the session ended": func(f *controlFakes) { f.rec.active, f.rec.stopErr = nil, recordingsession.ErrNotRunning },
	} {
		t.Run(name, func(t *testing.T) {
			w, f := newControlsWorld(t)
			_, e := run10(t, w, req, call10(t, "c1", values.ToolRecording, startArgs, actionpolicy.ClassU))
			change(f)
			if u := undo(t, w, e); u.Status != action.StatusUndone || len(f.rec.stops) != 0 {
				t.Fatalf("undo = %+v, stops %v", u, f.rec.stops)
			}
			if name == "another one runs" && f.rec.active == nil {
				t.Fatal("the other recording was stopped")
			}
		})
	}
}

// stop is done at once without a compensation (О-7 а): DONE without «Отменить», the line offers «Начать запись».
func TestRecordingStop(t *testing.T) {
	w, f := newControlsWorld(t)
	f.rec.active = &recordingsession.Info{ID: "rec-9"}
	r, e := run10(t, w, req, call10(t, "c1", values.ToolRecording, stopArgs, actionpolicy.ClassU))
	if r.out.Result.ResultJSON != `{"changed":true,"done":true,"recording":"rec-9","state":"stopped"}` {
		t.Fatalf("result = %s", r.out.Result.ResultJSON)
	}
	if f.rec.active != nil || len(f.rec.stops) != 1 || f.rec.stops[0] != "rec-9" {
		t.Fatalf("active %+v, stops %v", f.rec.active, f.rec.stops)
	}
	if e.Status != action.StatusDone || e.Compensation != nil || e.CanUndo || e.NoUndo != action.NoUndoIrreversible {
		t.Fatalf("entry = %+v, want DONE without an Undo", e)
	}
	line := values.ActionLine{Action: e.ID, Offer: values.OfferStartRecording}
	if len(r.cards) != 1 || r.cards[0] != line || line.Validate() != nil {
		t.Fatalf("cards = %+v, want the line offering «Начать запись»", r.cards)
	}
	if _, err := w.journal.Undo(t.Context(), e.ID); err == nil {
		t.Fatal("a stop was undone")
	}
	if len(f.rec.started) != 0 {
		t.Fatal("an Undo of a stop started a recording")
	}

	again, _ := run10(t, w, req, call10(t, "c1", values.ToolRecording, stopArgs, actionpolicy.ClassU))
	if again.out.Result.ResultJSON != `{"changed":true,"done":true,"state":"stopped"}` || len(f.rec.stops) != 1 {
		t.Fatalf("replay = %s, stops %v", again.out.Result.ResultJSON, f.rec.stops)
	}
}

// stop without a running recording, or one that ended meanwhile, stops nothing: not_recording.
func TestRecordingStopWithoutARecording(t *testing.T) {
	w, f := newControlsWorld(t)
	r, e := run10(t, w, req, call10(t, "c1", values.ToolRecording, stopArgs, actionpolicy.ClassU))
	if r.out.Result.ResultJSON != `{"changed":false,"done":true,"state":"not_recording"}` || e.ID != "" || len(f.rec.stops) != 0 {
		t.Fatalf("result = %s, entry %+v", r.out.Result.ResultJSON, e)
	}

	f.rec.active, f.rec.stopErr = &recordingsession.Info{ID: "rec-9"}, recordingsession.ErrNotRunning
	r, e = run10(t, w, req, call10(t, "c2", values.ToolRecording, stopArgs, actionpolicy.ClassU))
	if r.out.Result.ResultJSON != `{"changed":false,"done":true,"state":"not_recording"}` || e.Status != action.StatusFailed || len(r.cards) != 0 {
		t.Fatalf("ended meanwhile = %s, entry %+v", r.out.Result.ResultJSON, e)
	}
}

// After a restart: an Undo of a start left UNDOING finishes (the recording no longer runs); a start or a stop left
// PENDING is UNKNOWN (no Undo); a DONE start keeps StopRecording and a DONE stop keeps none.
func TestRecordingRecover(t *testing.T) {
	w, f := newControlsWorld(t)
	_, start := run10(t, w, req, call10(t, "c1", values.ToolRecording, startArgs, actionpolicy.ClassU))
	if _, err := w.journal.BeginUndo(t.Context(), start.ID); err != nil {
		t.Fatal(err)
	}
	f.rec.active = nil // the app stopped; the session recovery finalized the recording

	crash(t, w, call10(t, "c2", values.ToolRecording, startArgs, actionpolicy.ClassU))
	f.rec.active = nil
	_, doneStart := run10(t, w, req, call10(t, "c3", values.ToolRecording, startArgs, actionpolicy.ClassU))
	_, doneStop := run10(t, w, req, call10(t, "c4", values.ToolRecording, stopArgs, actionpolicy.ClassU))

	if e := recovered(t, w, "c1"); e.Status != action.StatusUndone || len(f.rec.stops) != 1 || f.rec.stops[0] != "rec-3" {
		t.Fatalf("UNDOING after restart = %+v, stops %v (only c4's)", e, f.rec.stops)
	}
	if e := recovered(t, w, "c2"); e.Status != action.StatusUnknown || e.Compensation != nil {
		t.Fatalf("PENDING start = %+v", e)
	}
	rec := mustRef(t, values.KindRecording, "rec-3")
	if e := recovered(t, w, "c3"); e.ID != doneStart.ID || e.Status != action.StatusDone || e.Compensation != (action.StopRecording{Recording: rec}) {
		t.Fatalf("DONE start = %+v", e)
	}
	if e := recovered(t, w, "c4"); e.ID != doneStop.ID || e.Status != action.StatusDone || e.Compensation != nil || e.CanUndo {
		t.Fatalf("DONE stop = %+v", e)
	}
}

// Without the recording use cases the tool answers "unavailable".
func TestRecordingUnwired(t *testing.T) {
	w := newWorld(t)
	r, _ := run10(t, w, req, call10(t, "c1", values.ToolRecording, startArgs, actionpolicy.ClassU))
	if r.out.Result.ResultJSON != `{"error":"unavailable"}` {
		t.Fatalf("recording = %s", r.out.Result.ResultJSON)
	}
}

// --- catalog and admission of the four control tools -------------------------------------------------------

// Catalog 10 is not served yet (TASK-326): at catalog 9 the four tools do not exist.
func TestControlToolsInvisibleAtCatalog9(t *testing.T) {
	if values.CatalogVersion != 9 {
		t.Skipf("catalog %d is served: this pin is for catalog 9", values.CatalogVersion)
	}
	for _, tool := range []values.Tool{values.ToolGetSettings, values.ToolSetSetting, values.ToolQuotaStatus, values.ToolRecording} {
		if _, err := values.ParseTool(string(tool)); err == nil {
			t.Errorf("%s is a tool of catalog 9", tool)
		}
		if _, err := values.SpecOf(tool); err == nil {
			t.Errorf("%s has a spec at catalog 9", tool)
		}
		if _, err := values.ParseCall("c1", string(tool), `{}`); err == nil {
			t.Errorf("%s parses at catalog 9", tool)
		}
	}
}

// The thread admits get_settings and quota_status as reads, set_setting and recording as actions without «Да», and
// only recording{stop} without a compensation.
func TestControlToolsAdmission(t *testing.T) {
	tests := []struct {
		tool, args string
		want       thread.Admission
	}{
		{"get_settings", `{}`, thread.Admission{Class: actionpolicy.ClassR, PlanStep: -1}},
		{"quota_status", `{}`, thread.Admission{Class: actionpolicy.ClassR, PlanStep: -1}},
		{"set_setting", setArgs("ac_power_only", "on"), thread.Admission{Class: actionpolicy.ClassU, PlanStep: -1}},
		{"recording", startArgs, thread.Admission{Class: actionpolicy.ClassU, PlanStep: -1}},
		{"recording", stopArgs, thread.Admission{Class: actionpolicy.ClassU, PlanStep: -1, NoUndo: true}},
	}
	for _, tt := range tests {
		t.Run(tt.tool+tt.args, func(t *testing.T) {
			th, err := thread.New("th1", now)
			if err != nil {
				t.Fatal(err)
			}
			if err := th.StartTurn(now); err != nil {
				t.Fatal(err)
			}
			c, err := values.ParseCallAt("c1", tt.tool, tt.args, values.CatalogWave10)
			if err != nil {
				t.Fatal(err)
			}
			got, err := th.Admit(c, thread.Facts{Context: values.RefSet{}}, now)
			if err != nil || got != tt.want {
				t.Fatalf("Admit = %+v, %v; want %+v", got, err, tt.want)
			}
		})
	}
}
