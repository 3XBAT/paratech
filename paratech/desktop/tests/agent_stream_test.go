package tests

// Scenario tests of the streamed reply (TASK-340, ADR-0026 addendum) on the real SQLite, the real Hub client, the real
// step use case and the Driver with its reply drafts; the Hub is the fake that speaks the Connect contract.

import (
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"

	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/activity"
	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/cases/schedulesync"
	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	agentloop "paratech/desktop/internal/input/agent"
	"paratech/desktop/testenv"
)

// streamSink keeps the updates and drafts in the order the panel would get them.
type streamSink struct {
	mu     sync.Mutex
	events []any
}

func (s *streamSink) Publish(u agentloop.Update) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, u)
}

func (s *streamSink) Draft(d agentloop.Draft) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, d)
}

// shown is the panel's story: "draft:<text>", "end:<end>", "update:<status>" (progress updates left out).
func (s *streamSink) shown() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, ev := range s.events {
		switch ev := ev.(type) {
		case agentloop.Draft:
			if !ev.Reply || ev.CallID != agentloop.ReplyDraftCall {
				out = append(out, "foreign draft")
				continue
			}
			if ev.End != agentloop.DraftReading {
				out = append(out, "end:"+string(ev.End))
				continue
			}
			out = append(out, "draft:"+strings.Join(ev.Texts, "|"))
		case agentloop.Update:
			if !ev.Progress {
				out = append(out, "update:"+ev.Status.String())
			}
		}
	}
	return out
}

func (s *streamSink) last(t *testing.T) agentloop.Update {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.events) - 1; i >= 0; i-- {
		if u, ok := s.events[i].(agentloop.Update); ok {
			return u
		}
	}
	t.Fatal("no update")
	return agentloop.Update{}
}

type streamEnv struct {
	*driverEnv
	sink   *streamSink
	stream *agentloop.Driver
}

// newStreamEnv is the driver env with the reply drafts wired as app wires them: the step use case shows them, the
// Driver cuts them on «Стоп».
func newStreamEnv(t *testing.T) *streamEnv {
	t.Helper()
	e := &streamEnv{driverEnv: newDriverEnv(t), sink: &streamSink{}}
	replies, err := agentloop.NewReplies(e.sink)
	if err != nil {
		t.Fatal(err)
	}
	facts, err := agentloop.NewFacts(e.knowledge)
	if err != nil {
		t.Fatal(err)
	}
	subjects, err := agentloop.NewBaseSubjects(driverLibrary{})
	if err != nil {
		t.Fatal(err)
	}
	turns, err := agentturn.New(agentturn.Deps{
		Model: e.model, Threads: sqlite.NewAgentThreadRepository(e.db), History: sqlite.NewAgentHistory(e.db, e.clock),
		Subjects: subjects, Facts: facts, Tx: e.db, Clock: e.clock, IDs: e.ids, Drafts: replies,
		Log: slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatal(err)
	}
	jobs, err := activity.New(sqlite.NewRecordingRepository(e.db), sqlite.NewJobRepository(e.db),
		sqlite.NewLessonRepository(e.db), nil, schedulesync.CalendarSyncJobType)
	if err != nil {
		t.Fatal(err)
	}
	if e.stream, err = agentloop.New(agentloop.Deps{
		Turns: turns, Sink: e.sink, Library: driverLibrary{}, Knowledge: e.knowledge, Activity: driverActivity{jobs},
		Assignments: driverAssignments{}, Deliveries: driverDeliveries{}, Replies: replies, Clock: e.clock,
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.stream.Close)
	return e
}

func todayScope(t *testing.T) values.Scope {
	t.Helper()
	sc, err := values.NewScope(values.Ref{}, values.ScreenToday, values.Ref{}, values.Ref{})
	if err != nil {
		t.Fatal(err)
	}
	return sc
}

func (e *streamEnv) ask(t *testing.T, text string) agentloop.Update {
	t.Helper()
	if err := e.stream.Ask(t.Context(), agentturn.AskInput{Scope: todayScope(t), Text: text}); err != nil {
		t.Fatal(err)
	}
	e.stream.Wait()
	return e.sink.last(t)
}

// storedText is everything the thread's history keeps, for checks that a draft never reached it.
func (e *streamEnv) storedText(t *testing.T) string {
	t.Helper()
	entries, err := sqlite.NewAgentHistory(e.db, e.clock).Since(t.Context(), "th1", 0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%+v", entries)
}

func replyText(t *testing.T, u agentloop.Update) string {
	t.Helper()
	for _, c := range u.Cards {
		if r, ok := c.(values.ReplyCard); ok {
			return string(r.Text)
		}
	}
	t.Fatalf("no reply card in %+v", u)
	return ""
}

// A streamed reply shows as drafts (the whole text so far each time), its last draft ends answered, then the update
// with the validated reply card; the stream was used, not the unary call.
func TestStreamedReplyShowsDraftsThenTheCard(t *testing.T) {
	e := newStreamEnv(t)
	e.hub.ThenStream([]string{"Ряд ", "сходится,\n", "если q < 1"},
		testenv.AgentCall{ID: "r1", Tool: "reply", Args: `{"text":"Ряд сходится,\nесли q < 1"}`})

	up := e.ask(t, "когда сходится ряд")
	if up.Status != thread.StatusDone || replyText(t, up) != "Ряд сходится,\nесли q < 1" {
		t.Fatalf("final update = %+v", up)
	}
	want := []string{"draft:Ряд ", "draft:Ряд сходится,\n", "draft:Ряд сходится,\nесли q < 1", "end:answered", "update:DONE"}
	if got := e.sink.shown(); !slices.Equal(got, want) {
		t.Fatalf("panel got %q\nwant %q", got, want)
	}
	if streams, unary := e.hub.Calls(); streams != 1 || unary != 0 {
		t.Fatalf("hub calls: %d streams, %d unary", streams, unary)
	}
}

// The final reply is the checked result, not the streamed text: the card shows it, and the streamed text is stored
// nowhere and never goes back to the planner (hard rule 3).
func TestStreamedReplyFinalWins(t *testing.T) {
	e := newStreamEnv(t)
	e.hub.ThenStream([]string{"Черновой текст, который модель потом переписала"},
		testenv.AgentCall{ID: "r1", Tool: "reply", Args: `{"text":"Итоговый ответ"}`}).
		Then(testenv.AgentCall{ID: "r2", Tool: "reply", Args: `{"text":"Второй ответ"}`})

	up := e.ask(t, "вопрос")
	if replyText(t, up) != "Итоговый ответ" {
		t.Fatalf("final update = %+v", up)
	}
	if strings.Contains(e.storedText(t), "Черновой") {
		t.Fatal("the streamed text reached the history")
	}
	e.ask(t, "ещё вопрос")
	reqs := e.hub.Requests()
	if raw := fmt.Sprint(reqs[len(reqs)-1]); strings.Contains(raw, "Черновой") || !strings.Contains(raw, "Итоговый ответ") {
		t.Fatalf("the next step's history: %s", raw)
	}
}

// The Hub step fails after deltas: the draft ends as failed (the panel keeps the shown text, BUG-20261104-agent-stream)
// before the update of the stopped request, which says why honestly and offers «Повторить»; the draft never reaches the
// history; Retry runs the step under a new key and the reply comes.
func TestStreamedReplyFailureKeepsTheDraft(t *testing.T) {
	e := newStreamEnv(t)
	e.hub.ThenStreamFails("Половина ответа")

	up := e.ask(t, "вопрос")
	if up.Status != thread.StatusStopped || up.StopReason != values.StopFailed ||
		!slices.Contains(up.Cards, values.Card(values.StoppedCard{Reason: values.StopFailed})) {
		t.Fatalf("failed update = %+v", up)
	}
	want := []string{"draft:Половина ответа", "end:failed", "update:STOPPED"}
	if got := e.sink.shown(); !slices.Equal(got, want) {
		t.Fatalf("panel got %q\nwant %q", got, want)
	}
	if strings.Contains(e.storedText(t), "Половина") {
		t.Fatal("the draft reached the history")
	}

	e.hub.Then(testenv.AgentCall{ID: "r1", Tool: "reply", Args: `{"text":"Готово"}`})
	if err := e.stream.Retry(t.Context(), agentturn.TurnRef{Thread: up.Thread, Turn: up.Turn}); err != nil {
		t.Fatal(err)
	}
	e.stream.Wait()
	if up := e.sink.last(t); up.Status != thread.StatusDone || replyText(t, up) != "Готово" {
		t.Fatalf("after Retry = %+v", up)
	}
	reqs := e.hub.Requests()
	if reqs[0].GetIdempotencyKey() == reqs[len(reqs)-1].GetIdempotencyKey() {
		t.Fatal("Retry reused the failed key")
	}
}

// «Стоп» mid-stream: the stream is cut at once, the draft dropped, the request stopped by the student with nothing of
// the step stored; the Hub finished the step, so «Продолжить» replays it for free and the card comes.
func TestStopCutsTheStreamedReply(t *testing.T) {
	e := newStreamEnv(t)
	e.hub.ThenStreamHeld([]string{"Начало ответа"},
		testenv.AgentCall{ID: "r1", Tool: "reply", Args: `{"text":"Полный ответ"}`})

	if err := e.stream.Ask(t.Context(), agentturn.AskInput{Scope: todayScope(t), Text: "вопрос"}); err != nil {
		t.Fatal(err)
	}
	<-e.hub.Sent()
	testenv.Eventually(t, "the draft", func() bool { return slices.Contains(e.sink.shown(), "draft:Начало ответа") }, nil)
	ref := agentturn.TurnRef{Thread: "th1", Turn: 1}
	if err := e.stream.Stop(t.Context(), ref); err != nil {
		t.Fatal(err)
	}
	e.stream.Wait()

	shown := e.sink.shown()
	if !slices.Contains(shown, "end:dropped") || slices.Contains(shown, "end:answered") {
		t.Fatalf("panel got %q", shown)
	}
	if up := e.sink.last(t); up.Status != thread.StatusStopped || up.StopReason != values.StopUser {
		t.Fatalf("last update = %+v", up)
	}
	th, err := sqlite.NewAgentThreadRepository(e.db).Get(t.Context(), "th1")
	if err != nil {
		t.Fatal(err)
	}
	if turn, _ := th.Turn(); turn.Status() != thread.StatusStopped || turn.StopReason() != values.StopUser || !turn.StepOpen() {
		t.Fatalf("stored request = %s %s, open step %v", turn.Status(), turn.StopReason(), turn.StepOpen())
	}
	if stored := e.storedText(t); strings.Contains(stored, "Начало") || strings.Contains(stored, "Полный") {
		t.Fatalf("history after the cut step: %s", stored)
	}

	if err := e.stream.Continue(t.Context(), ref); err != nil {
		t.Fatal(err)
	}
	e.stream.Wait()
	if up := e.sink.last(t); up.Status != thread.StatusDone || replyText(t, up) != "Полный ответ" {
		t.Fatalf("after Continue = %+v", up)
	}
	if n := e.hub.Executed(); n != 1 {
		t.Fatalf("the Hub ran %d steps, want the one paid step replayed", n)
	}
}

// An older Hub without AgentStepStream: the step falls back to AgentStep with the same key, no draft, the card comes.
func TestStreamedReplyFallsBackOnAnOlderHub(t *testing.T) {
	e := newStreamEnv(t)
	e.hub.WithoutStream().Then(testenv.AgentCall{ID: "r1", Tool: "reply", Args: `{"text":"Ответ"}`})

	up := e.ask(t, "вопрос")
	if up.Status != thread.StatusDone || replyText(t, up) != "Ответ" {
		t.Fatalf("final update = %+v", up)
	}
	if got := e.sink.shown(); !slices.Equal(got, []string{"update:DONE"}) {
		t.Fatalf("panel got %q", got)
	}
	if streams, unary := e.hub.Calls(); streams != 0 || unary != 1 {
		t.Fatalf("hub calls: %d streams, %d unary", streams, unary)
	}
}
