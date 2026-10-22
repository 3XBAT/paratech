package app

import (
	"context"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	agentloop "paratech/desktop/internal/input/agent"
	"paratech/desktop/internal/input/wails"
)

// The agent core is wired and its startup recovery runs: a request left running stops as «нет связи», a journal
// entry left PENDING becomes UNKNOWN.
func TestAgentRecoveryAtStart(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	// The core runs on the real clock; the seeded rows are older than the start.
	at := time.Now().Add(-time.Hour).UTC()

	db, err := sqlite.Open(ctx, sqlite.Options{Path: filepath.Join(dir, dbFile), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	th, err := thread.New("th1", thread.NoSubject, at)
	if err != nil {
		t.Fatal(err)
	}
	if err := th.StartRequest(values.Scope{}, at); err != nil {
		t.Fatal(err)
	}
	if _, err := th.BeginStep(at); err != nil {
		t.Fatal(err)
	}
	if err := sqlite.NewAgentThreadRepository(db).Save(ctx, th); err != nil {
		t.Fatal(err)
	}
	pending, err := action.New(action.Spec{ID: "a1", Thread: "th1", Turn: 1, CallID: "c1", Tool: values.ToolCreateSubject}, at)
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlite.NewAgentActionRepository(db).Save(ctx, pending); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	a, _ := newTestAppWith(t, dir)
	startCore(t, a)
	if a.core.agent == nil || a.core.journal == nil || a.core.driver == nil {
		t.Fatal("agent core not wired")
	}
	got, err := sqlite.NewAgentThreadRepository(a.core.db).Get(ctx, "th1")
	if err != nil {
		t.Fatal(err)
	}
	if turn, _ := got.Turn(); turn.Status() != thread.StatusStopped || turn.StopReason() != values.StopOffline || !turn.StepOpen() {
		t.Fatalf("request after start = %s %s open %v", turn.Status(), turn.StopReason(), turn.StepOpen())
	}
	entry, err := sqlite.NewAgentActionRepository(a.core.db).Get(ctx, "a1")
	if err != nil || entry.Status() != action.StatusUnknown {
		t.Fatalf("journal entry after start = %v, %v", entry, err)
	}
}

type panelEvents struct {
	mu   sync.Mutex
	data []any
}

func (p *panelEvents) Emit(name string, data ...any) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if name == wails.AgentUpdateEvent && len(data) == 1 {
		p.data = append(p.data, data[0])
	}
	return false
}

// The agent panel is wired: its calls reach the Driver on the app's ctx, the Driver's updates come back as events,
// and the panel is read-only while the Hub is not set up.
func TestAgentPanelWiring(t *testing.T) {
	ctx := context.Background()
	a, _ := newTestAppWith(t, t.TempDir())
	ui := startCore(t, a)
	if ui.Agent.Driver == nil || ui.Agent.Link == nil || ui.Agent.Panel == nil || ui.Agent.Life != a.startCtx {
		t.Fatalf("agent core = %+v", ui.Agent)
	}
	events := &panelEvents{}
	ui.Agent.Panel.Attach(events)
	panel := wails.NewAgentService(ui.Agent, slog.New(slog.DiscardHandler))

	if got := panel.State(ctx); got != (wails.AgentState{Link: wails.AgentLinkNoHub, ReadOnly: true}) {
		t.Errorf("State without a Hub = %+v", got)
	}
	if got := panel.Ask(ctx, wails.AgentAskInput{Text: "Что задали?"}); got.Problem != wails.AgentProblemOffline {
		t.Errorf("Ask without a Hub = %+v", got)
	}

	// A button of a request that does not exist: the Driver runs it and reports the problem as an event.
	if got := panel.Continue(ctx, wails.AgentTurnRef{ThreadID: "th-none", Turn: 1}); got.Problem != "" {
		t.Fatalf("Continue = %+v", got)
	}
	a.core.driver.Wait()
	events.mu.Lock()
	defer events.mu.Unlock()
	if len(events.data) != 1 {
		t.Fatalf("events = %+v", events.data)
	}
	v, ok := events.data[0].(wails.AgentUpdateView)
	if !ok || v.ThreadID != "th-none" || v.Turn != 1 || v.Problem == "" || len(v.Cards) != 0 {
		t.Errorf("event = %+v", events.data[0])
	}

	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if ui.Agent.Life.Err() == nil {
		t.Error("the agent's ctx must end with the app")
	}
}

// panelSink maps every field of the Driver's update.
func TestPanelSinkMapsUpdates(t *testing.T) {
	events := &panelEvents{}
	p := wails.NewAgentPanel(slog.New(slog.DiscardHandler))
	p.Attach(events)
	subject, err := values.NewRef(values.KindSubject, "sub-1")
	if err != nil {
		t.Fatal(err)
	}
	scope, err := values.NewScope(subject, values.ScreenToday, values.Ref{}, values.Ref{})
	if err != nil {
		t.Fatal(err)
	}
	panelSink{p}.Publish(agentloop.Update{
		Thread: "th1", Turn: 2, Scope: scope, Status: thread.StatusStopped, StopReason: values.StopOffline,
		Cards: []values.Card{values.StoppedCard{Reason: values.StopOffline}}, Problem: agentloop.ProblemBusy,
	})
	v := events.data[0].(wails.AgentUpdateView)
	if v.ThreadID != "th1" || v.Turn != 2 || v.SubjectID != "sub-1" || v.Screen != "today" || v.Status != "STOPPED" ||
		v.StopReason != "offline" || v.Problem != "busy" || len(v.Cards) != 1 || v.Cards[0].StopReason != "offline" {
		t.Errorf("view = %+v", v)
	}
}
