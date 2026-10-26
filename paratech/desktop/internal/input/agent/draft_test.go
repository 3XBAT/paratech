package agent

import (
	"bytes"
	"context"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/cases/answering"
	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

// draftText is a draft of the reader's: it reaches the sink as a draft only, never the planner or the log.
const draftText = "Черновик: ряд сходится"

// streamAnswerer hands drafts to the sink as a streamed read would, then answers got or err. With wait set it waits
// for the ctx to end after the drafts (a read cut by «Стоп»), hands late, then returns the ctx error.
type streamAnswerer struct {
	drafts  [][]string
	late    []string
	wait    bool
	started chan struct{}
	got     answering.Asked
	err     error
}

func (a *streamAnswerer) Ask(ctx context.Context, _ string, _ shared.SubjectID, _ string, sink answering.DraftSink) (answering.Asked, error) {
	for _, d := range a.drafts {
		sink.Draft(d)
	}
	if a.wait {
		close(a.started)
		<-ctx.Done()
		sink.Draft(a.late)
		return answering.Asked{}, ctx.Err()
	}
	return a.got, a.err
}

// draftDriver runs one request whose step asks answer_question.
func draftDriver(t *testing.T, ans Answerer) (*Driver, *chanSink, *stopTurns, *bytes.Buffer) {
	t.Helper()
	r := newReads()
	turns := &stopTurns{first: step(thread.StatusExecuting, []agentturn.Call{answerCall(t, `{"question":"Что такое ряд?"}`)})}
	sink := &chanSink{}
	var buf bytes.Buffer
	deps := r.deps(turns, sink)
	deps.Answerer = ans
	deps.Log = slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	d, err := New(deps)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	return d, sink, turns, &buf
}

// draftsAndCard returns the drafts among the events, the index of the last one and of the first update that shows an
// answer card (-1 none).
func draftsAndCard(events []any) ([]Draft, int, int) {
	var drafts []Draft
	lastDraft, card := -1, -1
	for i, e := range events {
		switch e := e.(type) {
		case Draft:
			drafts = append(drafts, e)
			lastDraft = i
		case Update:
			for _, c := range e.Cards {
				if _, ok := c.(values.AnswerCard); ok && card < 0 {
					card = i
				}
			}
		}
	}
	return drafts, lastDraft, card
}

// The drafts of a read reach the sink bound to the request and the call, cleaned, without repeats, and all of them
// before the update with the answer card; the last says the answer was made. The planner and the log never see them.
func TestAnswerDraftsComeBeforeTheCard(t *testing.T) {
	ans := &streamAnswerer{
		drafts: [][]string{{"Ряд"}, {"Ряд"}, {"Ряд\u202e сходится"}, {draftText}},
		got:    answering.Asked{Answer: madeAnswer(t)},
	}
	d, sink, turns, logBuf := draftDriver(t, ans)
	if err := d.Ask(t.Context(), agentturn.AskInput{Text: "что такое ряд"}); err != nil {
		t.Fatal(err)
	}
	d.Wait()

	drafts, lastDraft, card := draftsAndCard(sink.events())
	want := [][]string{{"Ряд"}, {"Ряд сходится"}, {draftText}, nil}
	if len(drafts) != len(want) {
		t.Fatalf("drafts = %+v", drafts)
	}
	for i, d := range drafts {
		if d.Thread != th1 || d.Turn != 1 || d.CallID != "c1" || !slices.Equal(d.Texts, want[i]) {
			t.Fatalf("draft %d = %+v, want texts %q", i, d, want[i])
		}
		if wantEnd := (i == len(want)-1); (d.End != DraftReading) != wantEnd {
			t.Fatalf("draft %d end = %q", i, d.End)
		}
	}
	if drafts[len(drafts)-1].End != DraftAnswered {
		t.Fatalf("end = %q, want answered", drafts[len(drafts)-1].End)
	}
	if card < 0 || lastDraft > card {
		t.Fatalf("last draft at %d, answer card at %d: %+v", lastDraft, card, sink.events())
	}

	rep := turns.report()
	if len(rep.submitted) != 1 || len(rep.submitted[0].Outcomes) != 1 {
		t.Fatalf("report %+v", rep)
	}
	for _, s := range []string{"Черновик", "сходится"} {
		if strings.Contains(rep.submitted[0].Outcomes[0].Result.ResultJSON, s) || strings.Contains(logBuf.String(), s) {
			t.Fatalf("draft text %q left the panel: %s / %s", s, rep.submitted[0].Outcomes[0].Result.ResultJSON, logBuf.String())
		}
	}
}

// A read that ends without an answer drops its draft: the last draft says so, no text follows it.
func TestAnswerDraftsOfAFailedRead(t *testing.T) {
	tests := []struct {
		name string
		got  answering.Asked
		err  error
	}{
		{"no connection", answering.Asked{}, answering.ErrNoConnection},
		{"refused", answering.Asked{}, answering.ErrReaderRefused},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, sink, _, _ := draftDriver(t, &streamAnswerer{drafts: [][]string{{draftText}}, got: tt.got, err: tt.err})
			if err := d.Ask(t.Context(), agentturn.AskInput{Text: "что такое ряд"}); err != nil {
				t.Fatal(err)
			}
			d.Wait()
			drafts, _, card := draftsAndCard(sink.events())
			if len(drafts) != 2 || drafts[1].End != DraftDropped || drafts[1].Texts != nil || card >= 0 {
				t.Fatalf("drafts = %+v, card at %d", drafts, card)
			}
		})
	}
}

// «Стоп» during a streamed read: the draft the reader hands after the cut is not passed, the call's drafts end as
// dropped and the read gets no outcome.
func TestStopCutsDrafts(t *testing.T) {
	ans := &streamAnswerer{drafts: [][]string{{"Ряд"}}, late: []string{draftText}, wait: true, started: make(chan struct{})}
	d, sink, turns, _ := draftDriver(t, ans)
	if err := d.Ask(t.Context(), agentturn.AskInput{Text: "что такое ряд"}); err != nil {
		t.Fatal(err)
	}
	<-ans.started
	if err := d.Stop(t.Context(), ref1); err != nil {
		t.Fatal(err)
	}
	d.Wait()

	drafts, _, _ := draftsAndCard(sink.events())
	if len(drafts) != 2 || !slices.Equal(drafts[0].Texts, []string{"Ряд"}) || drafts[1].End != DraftDropped || drafts[1].Texts != nil {
		t.Fatalf("drafts = %+v", drafts)
	}
	if rep := turns.report(); len(rep.interrupted) != 1 || len(rep.interrupted[0].Outcomes) != 0 {
		t.Fatalf("report %+v", rep)
	}
}

// A call bound to its request passes only changed, non-empty drafts while it runs, and its end only after a draft.
func TestCallDraft(t *testing.T) {
	base := Draft{Thread: th1, Turn: 2, CallID: "c7"}

	sink := &chanSink{}
	c := &callDraft{ctx: t.Context(), sink: sink, base: base}
	c.Draft([]string{"\u200b"}) // empty once cleaned
	c.end(true)
	c.Draft([]string{"после конца"})
	if len(sink.drafts) != 0 {
		t.Fatalf("drafts without a text = %+v", sink.drafts)
	}

	sink = &chanSink{}
	ctx, cancel := context.WithCancel(t.Context())
	c = &callDraft{ctx: ctx, sink: sink, base: base}
	c.Draft([]string{"а"})
	cancel()
	c.Draft([]string{"а", "б"})
	c.end(true) // an answer that came after the cut is still dropped
	if len(sink.drafts) != 2 || sink.drafts[0].CallID != "c7" || sink.drafts[0].Turn != 2 || sink.drafts[1].End != DraftDropped {
		t.Fatalf("drafts = %+v", sink.drafts)
	}
}

func TestCleanTexts(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"plain", []string{"Ряд сходится.", "Второе\nутверждение"}, []string{"Ряд сходится.", "Второе\nутверждение"}},
		{"bidi override", []string{"abc\u202edcba\u202c"}, []string{"abcdcba"}},
		{"bidi isolates and marks", []string{"\u2066x\u2069\u200e\u200f\u061c"}, []string{"x"}},
		{"zero width and bom", []string{"a\u200bb\u200dc\ufeff"}, []string{"abc"}},
		{"soft hyphen", []string{"сход\u00adится"}, []string{"сходится"}},
		{"empty dropped", []string{"\u202e", "", "текст"}, []string{"текст"}},
		{"none", nil, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cleanTexts(tt.in); !slices.Equal(got, tt.want) {
				t.Fatalf("cleanTexts(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
