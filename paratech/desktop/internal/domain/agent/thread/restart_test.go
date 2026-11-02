package thread

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"paratech/desktop/internal/domain/agent/values"
)

func TestRestartFromNeedsAFinalRequest(t *testing.T) {
	lesson := mention(t, values.KindLesson, "l7")
	again := Request{Mentions: []values.Mention{lesson}, Text: "заново"}
	tests := []struct {
		from    Status
		busy    bool // ErrTurnInProgress
		allowed bool
	}{
		{StatusPlanning, true, false},
		{StatusExecuting, true, false},
		{StatusAwaitingPlan, false, false},
		{StatusAwaitingAnswers, false, false},
		{StatusDone, false, true},
		{StatusStopped, false, true},
		{StatusFailed, false, true},
		{StatusCancelled, false, true},
	}
	for _, tt := range tests {
		t.Run(string(tt.from), func(t *testing.T) {
			th := reach(t, tt.from)
			before := th.State()
			err := th.RestartFrom(1, again, at(5))
			if !tt.allowed {
				if !errors.Is(err, ErrInvalidTransition) || errors.Is(err, ErrTurnInProgress) != tt.busy {
					t.Fatalf("RestartFrom = %v, want invalid transition (busy %v)", err, tt.busy)
				}
				if !reflect.DeepEqual(th.State(), before) {
					t.Fatalf("a refused restart changed the chat")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			turn, _ := th.Turn()
			if turn.Number() != 2 || turn.Status() != StatusPlanning || turn.DropsFrom() != 1 ||
				!reflect.DeepEqual(turn.Mentions(), again.Mentions) || th.HistoryFrom() != 0 {
				t.Fatalf("restarted = request %d %s drops from %d, mentions %v, history from %d",
					turn.Number(), turn.Status(), turn.DropsFrom(), turn.Mentions(), th.HistoryFrom())
			}
		})
	}
}

func TestRestartFromRefusals(t *testing.T) {
	th := chatOf(t, 3)
	before := th.State()
	for _, n := range []int{-1, 0, 4} {
		if err := th.RestartFrom(n, Request{}, at(50)); !errors.Is(err, ErrInvalidTransition) {
			t.Fatalf("from request %d of 3 = %v, want invalid transition", n, err)
		}
	}
	tooMany := make([]values.Mention, values.MaxMentions+1)
	for i := range tooMany {
		tooMany[i] = mention(t, values.KindMaterial, fmt.Sprintf("m%d", i))
	}
	if err := th.RestartFrom(2, Request{Mentions: tooMany}, at(50)); !errors.Is(err, ErrInvalidThread) {
		t.Fatalf("too many mentions = %v", err)
	}
	if !reflect.DeepEqual(th.State(), before) {
		t.Fatalf("a refused restart changed the chat")
	}
	if err := emptyChat(t).RestartFrom(1, Request{}, at(1)); !errors.Is(err, ErrNoTurn) {
		t.Fatalf("an empty chat = %v", err)
	}
}

// The summary goes when it covers a dropped request and stays when the dropped requests all follow it.
func TestRestartFromSummary(t *testing.T) {
	tests := []struct {
		from int
		kept bool
	}{
		{1, false}, {4, false}, {5, false}, {6, true}, {10, true},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.from), func(t *testing.T) {
			th := chatOf(t, 10)
			summary := summaryOf(t, 5)
			if err := th.Compact(summary, at(30)); err != nil {
				t.Fatal(err)
			}
			if err := th.RestartFrom(tt.from, Request{Text: "заново"}, at(40)); err != nil {
				t.Fatal(err)
			}
			if kept := th.Summary() == summary && th.SummaryAt().Equal(at(30)); kept != tt.kept {
				t.Fatalf("summary %+v at %v, want kept %v", th.Summary(), th.SummaryAt(), tt.kept)
			}
			if !tt.kept && (!th.Summary().IsZero() || !th.SummaryAt().IsZero()) {
				t.Fatalf("summary not cleared: %+v", th.Summary())
			}
			if back := roundTrip(t, th); back.Summary() != th.Summary() {
				t.Fatalf("restored summary %+v", back.Summary())
			}
		})
	}
}

// A second restart over requests the first one already dropped, and a restart from the current request itself.
func TestRestartFromAgain(t *testing.T) {
	th := chatOf(t, 5)
	if err := th.RestartFrom(3, Request{Text: "заново"}, at(20)); err != nil {
		t.Fatal(err)
	}
	if err := th.Complete(at(21)); err != nil {
		t.Fatal(err)
	}
	if err := th.RestartFrom(2, Request{Text: "ещё раз"}, at(22)); err != nil {
		t.Fatal(err)
	}
	turn, _ := th.Turn()
	if turn.Number() != 7 || turn.DropsFrom() != 2 {
		t.Fatalf("second restart = request %d drops from %d", turn.Number(), turn.DropsFrom())
	}
	if err := th.Fail(at(23)); err != nil {
		t.Fatal(err)
	}
	if err := th.RestartFrom(7, Request{Text: "последнее"}, at(24)); err != nil {
		t.Fatal(err)
	}
	if turn, _ := th.Turn(); turn.Number() != 8 || turn.DropsFrom() != 7 {
		t.Fatalf("restart from the current request = request %d drops from %d", turn.Number(), turn.DropsFrom())
	}
	if back, _ := roundTrip(t, th).Turn(); back.DropsFrom() != 7 {
		t.Fatalf("restored drops from %d", back.DropsFrom())
	}

	// A new request after it drops nothing.
	if err := th.Complete(at(25)); err != nil {
		t.Fatal(err)
	}
	if err := th.Ask(Request{Text: "дальше"}, at(26)); err != nil {
		t.Fatal(err)
	}
	if turn, _ := th.Turn(); turn.DropsFrom() != 0 {
		t.Fatalf("an ordinary request drops from %d", turn.DropsFrom())
	}
}

func TestRestoreChecksDropsFrom(t *testing.T) {
	th := chatOf(t, 3)
	if err := th.RestartFrom(2, Request{}, at(20)); err != nil {
		t.Fatal(err)
	}
	for _, from := range []int{-1, 4, 5} {
		st := th.State()
		st.Version = 1
		st.Current.DropsFrom = from
		if _, err := Restore(st); !errors.Is(err, ErrInvalidThread) {
			t.Fatalf("drops from %d of request 4 = %v", from, err)
		}
	}
}

// restarted is the stored history after request 22 dropped requests from…21 of a 21-request chat, its own text and
// reply included.
func restarted(from int) []values.HistoryEntry {
	entries := chatHistory(21, numbered)
	for i := range entries {
		if entries[i].Turn >= from {
			entries[i].DroppedBy = 22
		}
	}
	return append(entries, chatRequest(22, numbered(22))...)
}

// The ring drops after a restart: dropped requests are neither sent nor an overflow, and the window and Fill agree.
func TestRingDropsAfterRestart(t *testing.T) {
	full := chatHistory(21, numbered)
	if f := Fill(full, 22, noSummary); !f.Overflow || f.Percent() != 100 {
		t.Fatalf("before the restart %+v", f)
	}

	entries := restarted(5)
	f := Fill(entries, 23, noSummary)
	if f.Overflow || f.Pairs != 5 || f.Percent() != 25 || f.Level() != FillNormal {
		t.Fatalf("after the restart %+v (%d %%)", f, f.Percent())
	}
	next := append(entries, entry(23, numbered(23), nil))
	if _, nums := earlierOf(t, Window(next, 23, noSummary)); fmt.Sprint(nums) != "[1 2 3 4 22]" {
		t.Fatalf("window carries %v, want [1 2 3 4 22]", nums)
	}

	// The chat's NextFill reads the same with the thread's summary and history start.
	th := chatOf(t, 21)
	if err := th.RestartFrom(5, Request{Text: numbered(22)}, at(60)); err != nil {
		t.Fatal(err)
	}
	if err := th.Complete(at(61)); err != nil {
		t.Fatal(err)
	}
	if got := th.NextFill(entries); got != f {
		t.Fatalf("NextFill = %+v, want %+v", got, f)
	}
}

// A restart from n brings back everything before n whatever history start an expired-request Restart set (TASK-249):
// the start goes to 0, is stored, and NextFill counts the earlier requests instead of an overflow.
func TestRestartFromRestoresHistoryStart(t *testing.T) {
	tests := []struct {
		name     string
		start, n int
		pairs    int // earlier pairs NextFill counts after the restarted request completes
	}{
		{"all stays all", 0, 3, 2},
		{"restart before the start", 5, 3, 2},
		{"restart at the start", 5, 5, 4},
		{"restart after the start", 5, 8, 7},
		{"restart from the first request", 12, 1, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := chatOf(t, 12).State()
			st.Version, st.HistoryFrom = 1, tt.start
			th, err := Restore(st)
			if err != nil {
				t.Fatal(err)
			}
			if err := th.RestartFrom(tt.n, Request{Text: numbered(13)}, at(60)); err != nil {
				t.Fatal(err)
			}
			if th.HistoryFrom() != 0 {
				t.Fatalf("history from %d, want 0", th.HistoryFrom())
			}
			if back := roundTrip(t, th); back.HistoryFrom() != 0 {
				t.Fatalf("restored history from %d", back.HistoryFrom())
			}
			if err := th.Complete(at(61)); err != nil {
				t.Fatal(err)
			}

			// Requests n…12 are dropped by 13; the restarted request itself is the last pair.
			entries := chatHistory(12, numbered)
			for i := range entries {
				if entries[i].Turn >= tt.n {
					entries[i].DroppedBy = 13
				}
			}
			entries = append(entries, chatRequest(13, numbered(13))...)
			f := th.NextFill(entries)
			if f.Pairs != tt.pairs+1 || f.Overflow {
				t.Fatalf("%+v, want %d pairs, no overflow", f, tt.pairs+1)
			}
			if f != Fill(entries, 14, noSummary) {
				t.Fatalf("%+v differs from Fill", f)
			}
		})
	}
}

// The same through the real paths: an expired request restarts (history start 2), then the student restarts from it.
func TestRestartFromAfterExpiredRestart(t *testing.T) {
	th := chatOf(t, 1)
	if err := th.Ask(Request{Text: "второй"}, at(50)); err != nil {
		t.Fatal(err)
	}
	if _, err := th.BeginStep(at(51)); err != nil {
		t.Fatal(err)
	}
	if err := th.Stop(values.StopExpired, at(52)); err != nil {
		t.Fatal(err)
	}
	if err := th.Restart(at(53)); err != nil {
		t.Fatal(err)
	}
	if th.HistoryFrom() != 3 {
		t.Fatalf("after the expired restart history from %d", th.HistoryFrom())
	}
	if err := th.Complete(at(54)); err != nil {
		t.Fatal(err)
	}
	if err := th.RestartFrom(2, Request{Text: "заново"}, at(55)); err != nil {
		t.Fatal(err)
	}
	if turn, _ := th.Turn(); turn.Number() != 4 || turn.DropsFrom() != 2 || th.HistoryFrom() != 0 {
		t.Fatalf("restarted = request %d drops from %d, history from %d", turn.Number(), turn.DropsFrom(), th.HistoryFrom())
	}
}

// After an expired request restarted (HistoryFrom) the earlier requests are no longer sent: NextFill reports them as
// an overflow instead of counting them (TASK-203 follow-up), while the pairs from HistoryFrom on are what a step sends.
func TestNextFillHonoursHistoryStart(t *testing.T) {
	entries := chatHistory(12, numbered)
	st := chatOf(t, 12).State()
	st.Version = 1
	tests := []struct {
		from     int
		pairs    int
		overflow bool
	}{
		{0, 12, false},
		{1, 12, false},
		{5, 8, true},
		{12, 1, true},
	}
	for _, tt := range tests {
		st.HistoryFrom = tt.from
		th, err := Restore(st)
		if err != nil {
			t.Fatal(err)
		}
		f := th.NextFill(entries)
		if f.Pairs != tt.pairs || f.Overflow != tt.overflow {
			t.Fatalf("from %d: %+v, want %d pairs, overflow %v", tt.from, f, tt.pairs, tt.overflow)
		}
		if !tt.overflow && f != Fill(entries, 13, noSummary) {
			t.Fatalf("from %d: %+v differs from Fill", tt.from, f)
		}
	}

	// Requests the summary covers are no overflow even before the history start.
	st.HistoryFrom, st.Summary, st.SummaryAt = 5, summaryOf(t, 4), at(90)
	th, err := Restore(st)
	if err != nil {
		t.Fatal(err)
	}
	if f := th.NextFill(entries); f.Overflow || f.Pairs != 8 {
		t.Fatalf("with a summary of 1–4: %+v", f)
	}
}
