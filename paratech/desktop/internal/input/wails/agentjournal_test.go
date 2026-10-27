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
	"time"

	"paratech/desktop/internal/cases/agentjournal"
	"paratech/desktop/internal/cases/library"
	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
)

var journalNow = time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)

type fixedClock struct{}

func (fixedClock) Now() time.Time { return journalNow }

// fakeJournal returns what it is set to and records the calls.
type fakeJournal struct {
	lines   []agentjournal.Line
	listErr error

	entry   agentjournal.Entry
	entries []agentjournal.Entry
	undoErr error

	limit int
	ctx   context.Context
	calls []string
}

func (f *fakeJournal) List(_ context.Context, limit int) ([]agentjournal.Line, error) {
	f.limit = limit
	return f.lines, f.listErr
}

func (f *fakeJournal) Undo(ctx context.Context, id shared.ActionID) (agentjournal.Entry, error) {
	f.ctx = ctx
	f.calls = append(f.calls, "Undo "+id.String())
	return f.entry, f.undoErr
}

func (f *fakeJournal) UndoRequest(ctx context.Context, th shared.ThreadID, turn int) ([]agentjournal.Entry, error) {
	f.ctx = ctx
	f.calls = append(f.calls, fmt.Sprintf("UndoRequest %s %d", th, turn))
	return f.entries, f.undoErr
}

// newJournalAgent returns the binding on a fake journal, its app-lifetime ctx cancel and the log.
func newJournalAgent(t *testing.T, j *fakeJournal) (*AgentService, context.CancelFunc, *bytes.Buffer) {
	t.Helper()
	life, cancel := context.WithCancel(context.WithValue(context.Background(), appKey{}, "app"))
	t.Cleanup(cancel)
	var logs bytes.Buffer
	s := NewAgentService(AgentCore{
		Driver: &fakeAgentDriver{}, Link: readyLink(), Panel: NewAgentPanel(discard()), Life: life, Journal: j,
		Clock: fixedClock{}, Chats: &fakeChats{},
	}, slog.New(slog.NewTextHandler(&logs, nil)))
	return s, cancel, &logs
}

// journalLines are the lines of «Журнал» the tests read, newest first.
func journalLines() []agentjournal.Line {
	at := func(d time.Duration) time.Time { return journalNow.Add(-d) }
	return []agentjournal.Line{
		{
			Kind: agentjournal.LineAction, At: at(time.Hour), UpdatedAt: at(time.Minute), Subject: "sub-1", Action: "act-1",
			Thread: "th1", ChatTitle: "Матан и физика", Turn: 2, Tool: values.ToolCreateFolder, Status: action.StatusDone,
			CanUndo: true,
		},
		{
			Kind: agentjournal.LineProcessing, At: at(2 * time.Hour), UpdatedAt: at(time.Hour), Job: "job-1",
			JobType: "send_email", JobStatus: job.StatusCompleted,
		},
		{
			Kind: agentjournal.LineAction, At: at(3 * time.Hour), UpdatedAt: at(3 * time.Hour), Action: "act-2", Thread: "th2",
			Turn: 1, Tool: values.ToolProposeSend, Status: action.StatusProposed, NoUndo: action.NoUndoIrreversible,
		},
		{
			Kind: agentjournal.LineAction, At: at(48 * time.Hour), UpdatedAt: at(47 * time.Hour), Subject: "sub-2",
			Action: "act-3", Thread: "th3", Turn: 1, Tool: values.ToolEditDue, Status: action.StatusUndoRefused,
			NoUndo: action.NoUndoChanged,
		},
		{
			Kind: agentjournal.LineProcessing, At: at(10 * 24 * time.Hour), UpdatedAt: at(10 * 24 * time.Hour), Job: "job-2",
			JobType: "summarize", JobStatus: job.StatusFailed,
		},
		{ // older than the 90 days of the history
			Kind: agentjournal.LineAction, At: at(91 * 24 * time.Hour), Subject: "sub-1", Action: "act-old", Thread: "th1",
			Turn: 1, Tool: values.ToolCreateSubject, Status: action.StatusDone, CanUndo: true,
		},
	}
}

// The JSON contract of «Журнал» the page reads.
func TestAgentHistoryContract(t *testing.T) {
	j := &fakeJournal{lines: journalLines()}
	s, _, _ := newJournalAgent(t, j)
	h := s.History(context.Background(), AgentHistoryFilter{})
	if h.Problem != "" || len(h.Lines) != 5 || j.limit != agentjournal.MaxList {
		t.Fatalf("History = %+v, limit %d", h, j.limit)
	}
	got, err := json.Marshal(h.Lines[:3])
	if err != nil {
		t.Fatal(err)
	}
	want := `[` +
		`{"kind":"action","at":"2026-10-22T11:00:00Z","updatedAt":"2026-10-22T11:59:00Z","subjectId":"sub-1",` +
		`"actionId":"act-1","threadId":"th1","chatTitle":"Матан и физика","turn":2,"tool":"create_folder","jobId":"",` +
		`"jobType":"","status":"DONE","undoable":true,"cannotUndo":false,"noUndo":""},` +
		`{"kind":"processing","at":"2026-10-22T10:00:00Z","updatedAt":"2026-10-22T11:00:00Z","subjectId":"",` +
		`"actionId":"","threadId":"","chatTitle":"","turn":0,"tool":"","jobId":"job-1","jobType":"send_email",` +
		`"status":"COMPLETED","undoable":false,"cannotUndo":true,"noUndo":"sent"},` +
		`{"kind":"action","at":"2026-10-22T09:00:00Z","updatedAt":"2026-10-22T09:00:00Z","subjectId":"",` +
		`"actionId":"act-2","threadId":"th2","chatTitle":"","turn":1,"tool":"propose_send","jobId":"","jobType":"","status":"PROPOSED",` +
		`"undoable":false,"cannotUndo":true,"noUndo":"irreversible"}]`
	if string(got) != want {
		t.Errorf("lines JSON =\n%s\nwant\n%s", got, want)
	}
	if l := h.Lines[3]; l.NoUndo != "changed" || l.CannotUndo || l.Undoable {
		t.Errorf("refused line = %+v", l)
	}
	if l := h.Lines[4]; l.NoUndo != "" || l.CannotUndo || l.Status != "FAILED" {
		t.Errorf("processing line = %+v", l)
	}
	b, _ := json.Marshal(AgentHistoryView{})
	if string(b) != `{"problem":"","lines":null}` {
		t.Errorf("view JSON = %s", b)
	}
}

// Only a completed send_email line is «Отменить нельзя» (sent); every other status says nothing about undo.
func TestAgentHistorySendEmailNoUndo(t *testing.T) {
	for _, st := range job.Statuses() {
		t.Run(string(st), func(t *testing.T) {
			v := journalLineView(agentjournal.Line{
				Kind: agentjournal.LineProcessing, At: journalNow, Job: "job-1", JobType: "send_email", JobStatus: st,
			})
			sent := st == job.StatusCompleted
			if v.Undoable || v.CannotUndo != sent || (v.NoUndo == AgentNoUndoSent) != sent || (!sent && v.NoUndo != "") {
				t.Errorf("line = %+v", v)
			}
		})
	}
}

func TestAgentHistoryFilters(t *testing.T) {
	tests := []struct {
		name   string
		filter AgentHistoryFilter
		want   []string // actionId or jobId of the lines
	}{
		{"all of 90 days", AgentHistoryFilter{}, []string{"act-1", "job-1", "act-2", "act-3", "job-2"}},
		{"90 days", AgentHistoryFilter{Days: 90}, []string{"act-1", "job-1", "act-2", "act-3", "job-2"}},
		{"last day", AgentHistoryFilter{Days: 1}, []string{"act-1", "job-1", "act-2"}},
		{"actions", AgentHistoryFilter{Kind: "action"}, []string{"act-1", "act-2", "act-3"}},
		{"processing", AgentHistoryFilter{Kind: "processing"}, []string{"job-1", "job-2"}},
		{"subject", AgentHistoryFilter{SubjectID: "sub-1"}, []string{"act-1"}},
		{"status", AgentHistoryFilter{Status: "FAILED"}, []string{"job-2"}},
		{"undoable", AgentHistoryFilter{Undoable: true}, []string{"act-1"}},
		{"combined", AgentHistoryFilter{Kind: "action", Days: 7, Status: "UNDO_REFUSED"}, []string{"act-3"}},
		{"nothing", AgentHistoryFilter{SubjectID: "sub-9"}, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _, _ := newJournalAgent(t, &fakeJournal{lines: journalLines()})
			h := s.History(context.Background(), tt.filter)
			if h.Problem != "" || h.Lines == nil {
				t.Fatalf("History = %+v", h)
			}
			got := []string{}
			for _, l := range h.Lines {
				got = append(got, l.ActionID+l.JobID)
			}
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("lines = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAgentHistoryRefusesMalformedFilters(t *testing.T) {
	tests := []struct {
		name   string
		filter AgentHistoryFilter
	}{
		{"negative days", AgentHistoryFilter{Days: -1}},
		{"over 90 days", AgentHistoryFilter{Days: 91}},
		{"kind", AgentHistoryFilter{Kind: "email"}},
		{"status", AgentHistoryFilter{Status: "done"}},
		{"subject id", AgentHistoryFilter{SubjectID: `..\x`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			j := &fakeJournal{lines: journalLines()}
			s, _, _ := newJournalAgent(t, j)
			h := s.History(context.Background(), tt.filter)
			if h.Problem != AgentProblemInvalid || h.Lines == nil || len(h.Lines) != 0 || j.limit != 0 {
				t.Errorf("History = %+v (journal read: %v)", h, j.limit != 0)
			}
		})
	}
}

// A failed read is an enum: its text never reaches the page.
func TestAgentHistoryFailed(t *testing.T) {
	s, _, logs := newJournalAgent(t, &fakeJournal{listErr: errors.New(`sqlite: C:\Users\student\secret`)})
	h := s.History(context.Background(), AgentHistoryFilter{})
	b, _ := json.Marshal(h)
	if string(b) != `{"problem":"failed","lines":[]}` {
		t.Errorf("History = %s", b)
	}
	if !strings.Contains(logs.String(), opAgentHistory) {
		t.Errorf("the cause must be logged, got %q", logs.String())
	}
}

func TestAgentUndo(t *testing.T) {
	tests := []struct {
		name  string
		entry agentjournal.Entry
		err   error
		want  AgentUndoView
		logs  bool
	}{
		{"undone", agentjournal.Entry{ID: "act-1", Status: action.StatusUndone, NoUndo: action.NoUndoUndone},
			nil, AgentUndoView{ActionID: "act-1", Status: "UNDONE"}, false},
		{"changed since", agentjournal.Entry{ID: "act-1", Status: action.StatusUndoRefused, NoUndo: action.NoUndoChanged},
			nil, AgentUndoView{ActionID: "act-1", Status: "UNDO_REFUSED", Refusal: AgentUndoChanged}, false},
		{"result ready", agentjournal.Entry{ID: "act-1", Status: action.StatusUndoRefused, NoUndo: action.NoUndoResultReady},
			nil, AgentUndoView{ActionID: "act-1", Status: "UNDO_REFUSED", Refusal: AgentUndoResultReady}, false},
		{"cannot stop", agentjournal.Entry{ID: "act-1", Status: action.StatusUndoRefused, NoUndo: action.NoUndoCannotStop},
			nil, AgentUndoView{ActionID: "act-1", Status: "UNDO_REFUSED", Refusal: AgentUndoCannotStop}, false},
		{"not found", agentjournal.Entry{}, fmt.Errorf("agentjournal: begin undo: %w", shared.ErrNotFound),
			AgentUndoView{ActionID: "act-1", Refusal: AgentUndoNotFound}, false},
		{"not undoable", agentjournal.Entry{}, fmt.Errorf("agentjournal: begin undo: %w", action.ErrInvalidTransition),
			AgentUndoView{ActionID: "act-1", Refusal: AgentUndoNotUndoable}, false},
		{"name taken", agentjournal.Entry{}, fmt.Errorf("agentjournal: finish undo: %w", library.ErrNameTaken),
			AgentUndoView{ActionID: "act-1", Refusal: AgentUndoNameTaken}, false},
		{"folder taken", agentjournal.Entry{}, fmt.Errorf("agentjournal: finish undo: %w", library.ErrFolderTaken),
			AgentUndoView{ActionID: "act-1", Refusal: AgentUndoNameTaken}, false},
		{"refusal as an error", agentjournal.Entry{}, fmt.Errorf("x: %w", action.ErrCannotStop),
			AgentUndoView{ActionID: "act-1", Refusal: AgentUndoCannotStop}, false},
		{"result ready before undo", agentjournal.Entry{}, fmt.Errorf("agentjournal: begin undo: %w", action.ErrResultReady),
			AgentUndoView{ActionID: "act-1", Refusal: AgentUndoResultReady}, false},
		{"no compensator", agentjournal.Entry{}, fmt.Errorf("x: %w", agentjournal.ErrUndoUnavailable),
			AgentUndoView{ActionID: "act-1", Problem: AgentProblemUnavailable}, false},
		{"concurrent change", agentjournal.Entry{}, fmt.Errorf("agentjournal: finish undo: %w", shared.ErrVersionConflict),
			AgentUndoView{ActionID: "act-1", Refusal: AgentUndoBusy}, false},
		{"failed", agentjournal.Entry{}, errors.New(`sqlite: C:\Users\student\secret`),
			AgentUndoView{ActionID: "act-1", Problem: AgentProblemFailed}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			j := &fakeJournal{entry: tt.entry, undoErr: tt.err}
			s, _, logs := newJournalAgent(t, j)
			got := s.Undo(endedCall(), "act-1")
			if got != tt.want {
				t.Errorf("Undo = %+v, want %+v", got, tt.want)
			}
			if len(j.calls) != 1 || j.calls[0] != "Undo act-1" {
				t.Fatalf("calls = %q", j.calls)
			}
			// The call's ctx has ended: the undo runs on the app's.
			if j.ctx.Value(appKey{}) != "app" || j.ctx.Err() != nil {
				t.Errorf("the journal got the call's ctx")
			}
			if b, _ := json.Marshal(got); strings.Contains(string(b), "secret") {
				t.Errorf("the view leaks the error: %s", b)
			}
			if tt.logs != strings.Contains(logs.String(), opAgentUndo) {
				t.Errorf("log = %q", logs.String())
			}
		})
	}
}

func TestAgentUndoRefusesBeforeTheJournal(t *testing.T) {
	j := &fakeJournal{}
	s, cancel, _ := newJournalAgent(t, j)
	for _, id := range []string{"", "a/b", `..\x`, "CON"} {
		if got := s.Undo(context.Background(), id); got.Problem != AgentProblemInvalid {
			t.Errorf("Undo(%q) = %+v", id, got)
		}
	}
	for _, ref := range []AgentTurnRef{{ThreadID: "th1"}, {Turn: 1}, {ThreadID: "a b", Turn: 1}} {
		if got := s.UndoRequest(context.Background(), ref); got.Problem != AgentProblemInvalid || got.Actions == nil {
			t.Errorf("UndoRequest(%+v) = %+v", ref, got)
		}
	}
	cancel() // the app is closing
	if got := s.Undo(context.Background(), "act-1"); got.Problem != AgentProblemClosed {
		t.Errorf("Undo while closing = %+v", got)
	}
	if got := s.UndoRequest(context.Background(), AgentTurnRef{ThreadID: "th1", Turn: 1}); got.Problem != AgentProblemClosed {
		t.Errorf("UndoRequest while closing = %+v", got)
	}
	if len(j.calls) != 0 {
		t.Errorf("the journal was called: %q", j.calls)
	}
}

func TestAgentUndoRequest(t *testing.T) {
	entries := []agentjournal.Entry{
		{ID: "act-2", Status: action.StatusUndone},
		{ID: "act-1", Status: action.StatusUndoRefused, NoUndo: action.NoUndoChanged},
	}
	tests := []struct {
		name    string
		entries []agentjournal.Entry
		err     error
		want    string
	}{
		{"every action", entries, nil,
			`{"problem":"","actions":[{"problem":"","actionId":"act-2","status":"UNDONE","refusal":""},` +
				`{"problem":"","actionId":"act-1","status":"UNDO_REFUSED","refusal":"changed"}]}`},
		{"nothing to undo", nil, nil, `{"problem":"","actions":[]}`},
		{"some failed", entries[:1], errors.New(`action act-1: C:\Users\student\secret`),
			`{"problem":"failed","actions":[{"problem":"","actionId":"act-2","status":"UNDONE","refusal":""}]}`},
		{"no compensator", nil, fmt.Errorf("x: %w", agentjournal.ErrUndoUnavailable), `{"problem":"unavailable","actions":[]}`},
		{"concurrent change", entries[:1], fmt.Errorf("action act-1: %w", shared.ErrVersionConflict),
			`{"problem":"busy","actions":[{"problem":"","actionId":"act-2","status":"UNDONE","refusal":""}]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			j := &fakeJournal{entries: tt.entries, undoErr: tt.err}
			s, _, _ := newJournalAgent(t, j)
			got, err := json.Marshal(s.UndoRequest(endedCall(), AgentTurnRef{ThreadID: "th1", Turn: 3}))
			if err != nil || string(got) != tt.want {
				t.Errorf("UndoRequest = %s, %v\nwant %s", got, err, tt.want)
			}
			if len(j.calls) != 1 || j.calls[0] != "UndoRequest th1 3" || j.ctx.Value(appKey{}) != "app" {
				t.Errorf("calls = %q", j.calls)
			}
		})
	}
}
