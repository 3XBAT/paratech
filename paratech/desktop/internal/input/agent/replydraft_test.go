package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

func replyStep() agentturn.Step {
	return agentturn.Step{Thread: "th1", Turn: 2, Status: thread.StatusPlanning}
}

func replyDrafts(t *testing.T) (*Replies, *chanSink) {
	t.Helper()
	sink := &chanSink{}
	r, err := NewReplies(sink)
	if err != nil {
		t.Fatal(err)
	}
	return r, sink
}

func TestNewRepliesNeedsASink(t *testing.T) {
	if _, err := NewReplies(nil); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("NewReplies(nil) = %v", err)
	}
}

// The reply draft passes the whole text so far as one cleaned entry, marked Reply under the fixed call id; unchanged
// and empty texts pass nothing, and nothing passes after the end.
func TestReplyDraftPassesTheTextSoFar(t *testing.T) {
	tests := []struct {
		name     string
		answered bool
		end      DraftEnd
	}{
		{"answered", true, DraftAnswered},
		{"failed", false, DraftFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, sink := replyDrafts(t)
			ctx, d := r.Reply(t.Context(), replyStep())
			d.Text("Ряд")
			d.Text("Ряд")                // unchanged
			d.Text("\u200b\u202e")       // nothing left once cleaned
			d.Text("Ряд\u202e сходится") // a bidi control goes
			d.End(tt.answered)
			d.Text("после конца")
			d.End(true)

			base := Draft{Thread: "th1", Turn: 2, CallID: ReplyDraftCall, Reply: true}
			want := []Draft{withTexts(base, "Ряд"), withTexts(base, "Ряд сходится"), withEnd(base, tt.end)}
			if got := sink.drafts; !sameDrafts(got, want) {
				t.Fatalf("drafts = %+v\nwant %+v", got, want)
			}
			if ctx.Err() == nil {
				t.Fatal("the ctx of the Hub call outlives the step")
			}
		})
	}
}

func withTexts(d Draft, s string) Draft { d.Texts = []string{s}; return d }

func withEnd(d Draft, e DraftEnd) Draft { d.End = e; return d }

func sameDrafts(a, b []Draft) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		x, y := a[i], b[i]
		if x.Thread != y.Thread || x.Turn != y.Turn || x.CallID != y.CallID || x.End != y.End || x.Reply != y.Reply ||
			len(x.Texts) != len(y.Texts) || (len(x.Texts) > 0 && x.Texts[0] != y.Texts[0]) {
			return false
		}
	}
	return true
}

// A step that wrote nothing tells the sink nothing.
func TestReplyDraftWithoutTextIsSilent(t *testing.T) {
	r, sink := replyDrafts(t)
	_, d := r.Reply(t.Context(), replyStep())
	d.End(true)
	if len(sink.drafts) != 0 {
		t.Fatalf("drafts = %+v", sink.drafts)
	}
}

// Cut (a stored «Стоп») ends the Hub call at once and drops the draft; later texts and an answered end pass nothing.
func TestReplyDraftCut(t *testing.T) {
	r, sink := replyDrafts(t)
	ctx, d := r.Reply(t.Context(), replyStep())
	d.Text("начало")
	r.Cut(agentturn.TurnRef{Thread: "th1", Turn: 1}) // another request: nothing happens
	if ctx.Err() != nil {
		t.Fatal("a cut of another request ended the call")
	}
	r.Cut(agentturn.TurnRef{Thread: "th1", Turn: 2})
	if ctx.Err() == nil {
		t.Fatal("the Hub call goes on after the cut")
	}
	d.Text("начало ответа")
	d.End(true)
	base := Draft{Thread: "th1", Turn: 2, CallID: ReplyDraftCall, Reply: true}
	if want := []Draft{withTexts(base, "начало"), withEnd(base, DraftDropped)}; !sameDrafts(sink.drafts, want) {
		t.Fatalf("drafts = %+v", sink.drafts)
	}
	r.Cut(agentturn.TurnRef{Thread: "th1", Turn: 2}) // ended: forgotten
	if len(r.open) != 0 {
		t.Fatalf("open drafts = %d", len(r.open))
	}
}

// The ctx of the operation ending (the app closes) stops the draft too; the end is then a drop.
func TestReplyDraftStopsWithItsContext(t *testing.T) {
	r, sink := replyDrafts(t)
	ctx, cancel := context.WithCancel(t.Context())
	_, d := r.Reply(ctx, replyStep())
	d.Text("начало")
	cancel()
	d.Text("начало ответа")
	d.End(true)
	if len(sink.drafts) != 2 || sink.drafts[1].End != DraftDropped {
		t.Fatalf("drafts = %+v", sink.drafts)
	}
}

// A stored «Стоп» through the Driver cuts the reply draft of the request; a refused one does not.
func TestDriverStopCutsTheReplyDraft(t *testing.T) {
	tests := []struct {
		name string
		err  error
		cut  bool
	}{
		{"stored", nil, true},
		{"refused", thread.ErrInvalidTransition, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, _ := replyDrafts(t)
			ctx, _ := r.Reply(t.Context(), replyStep())
			stopped := agentturn.Step{Thread: "th1", Turn: 2, Status: thread.StatusStopped, StopReason: values.StopUser}
			turns := &scriptedTurns{steps: []agentturn.Step{stopped}, errs: []error{tt.err}}
			d := newReads().deps(turns, &chanSink{})
			d.Replies = r
			drv, err := New(d)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(drv.Close)
			if err := drv.Stop(t.Context(), agentturn.TurnRef{Thread: "th1", Turn: 2}); err != nil {
				t.Fatal(err)
			}
			drv.Wait()
			if cut := ctx.Err() != nil; cut != tt.cut {
				t.Fatalf("cut = %v, want %v", cut, tt.cut)
			}
		})
	}
}

// Through the real step use case and the Driver: the drafts of a streamed reply come before the update with the reply
// card, the last one ends answered; the card is the validated reply, not the draft.
func TestStreamedReplyThroughTheDriver(t *testing.T) {
	r, sink := replyDrafts(t)
	model := &scriptModel{
		steps: [][]values.RawCall{{{CallID: "r1", Tool: "reply", ArgumentsJSON: `{"text":"Итог"}`}}},
		stream: func(_ context.Context, draft func(string)) error {
			draft("Черно")
			draft("Черновик")
			return nil
		},
	}
	drv := streamDriver(t, model, r, sink)
	if err := drv.Ask(t.Context(), agentturn.AskInput{Text: "что такое ряд"}); err != nil {
		t.Fatal(err)
	}
	drv.Wait()

	var kinds []string
	for _, ev := range sink.events() {
		switch ev := ev.(type) {
		case Draft:
			kinds = append(kinds, "draft:"+string(ev.End))
		case Update:
			if !ev.Progress {
				kinds = append(kinds, "update:"+ev.Status.String())
			}
		}
	}
	if want := []string{"draft:", "draft:", "draft:answered", "update:DONE"}; !equalStrings(kinds, want) {
		t.Fatalf("events = %v, want %v", kinds, want)
	}
	ups := sink.all()
	last := ups[len(ups)-1]
	if len(last.Cards) != 1 || last.Cards[0].(values.ReplyCard).Text != "Итог" {
		t.Fatalf("cards = %+v", last.Cards)
	}
}

// A step that fails after its drafts (a Hub timeout, a broken stream) ends them failed, not dropped, before the update
// of the stopped request: the text the Hub delivered stays shown.
func TestFailedStreamKeepsTheReplyDraft(t *testing.T) {
	r, sink := replyDrafts(t)
	model := &scriptModel{stream: func(_ context.Context, draft func(string)) error {
		draft("половина")
		return errors.New("stream broke")
	}}
	drv := streamDriver(t, model, r, sink)
	if err := drv.Ask(t.Context(), agentturn.AskInput{Text: "что такое ряд"}); err != nil {
		t.Fatal(err)
	}
	drv.Wait()
	ds := sink.drafts
	if len(ds) != 2 || ds[0].Texts[0] != "половина" || ds[1].End != DraftFailed {
		t.Fatalf("drafts = %+v", ds)
	}
	var kinds []string
	for _, ev := range sink.events() {
		switch ev := ev.(type) {
		case Draft:
			kinds = append(kinds, "draft:"+string(ev.End))
		case Update:
			if !ev.Progress {
				kinds = append(kinds, "update:"+ev.Status.String())
			}
		}
	}
	if want := []string{"draft:", "draft:failed", "update:STOPPED"}; !equalStrings(kinds, want) {
		t.Fatalf("events = %v, want %v", kinds, want)
	}
	ups := sink.all()
	if last := ups[len(ups)-1]; last.Status != thread.StatusStopped {
		t.Fatalf("last update = %+v", last)
	}
}

// «Стоп» while the reply streams: the stream's ctx ends, the draft is dropped, the request is stopped by the student.
func TestStopDuringAStreamedReply(t *testing.T) {
	r, sink := replyDrafts(t)
	streaming := make(chan struct{})
	model := &scriptModel{stream: func(ctx context.Context, draft func(string)) error {
		draft("начало")
		close(streaming)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Second):
			return errors.New("the stream was never cut")
		}
	}}
	drv := streamDriver(t, model, r, sink)
	if err := drv.Ask(t.Context(), agentturn.AskInput{Text: "что такое ряд"}); err != nil {
		t.Fatal(err)
	}
	<-streaming
	if err := drv.Stop(t.Context(), agentturn.TurnRef{Thread: "th1", Turn: 1}); err != nil {
		t.Fatal(err)
	}
	drv.Wait()
	ds := sink.drafts
	if len(ds) != 2 || ds[1].End != DraftDropped {
		t.Fatalf("drafts = %+v", ds)
	}
	ups := sink.all()
	if last := ups[len(ups)-1]; last.Status != thread.StatusStopped || last.StopReason != values.StopUser {
		t.Fatalf("last update = %+v", last)
	}
}

func streamDriver(t *testing.T, model *scriptModel, r *Replies, sink *chanSink) *Driver {
	t.Helper()
	facts, err := NewFacts(&memLessons{})
	if err != nil {
		t.Fatal(err)
	}
	store := &turnStore{threads: map[shared.ThreadID]*thread.Thread{}, history: map[shared.ThreadID][]values.HistoryEntry{}}
	turns, err := agentturn.New(agentturn.Deps{
		Model: model, Threads: store, History: store, Subjects: noSubjects{}, Facts: facts, Tx: &directTx{},
		Clock: fixedClock{}, IDs: store, Drafts: r,
	})
	if err != nil {
		t.Fatal(err)
	}
	d := newReads().deps(turns, sink)
	d.Replies = r
	drv, err := New(d)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(drv.Close)
	return drv
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
