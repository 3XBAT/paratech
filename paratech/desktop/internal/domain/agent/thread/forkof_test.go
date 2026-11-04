package thread

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"paratech/desktop/internal/domain/agent/values"
)

// pastOf is what the store reads of chatOf's requests nums: one text each, a frozen line, its own times.
func pastOf(nums ...int) []PastRequest {
	out := make([]PastRequest, 0, len(nums))
	for _, n := range nums {
		out = append(out, PastRequest{
			Number: n, Texts: []string{fmt.Sprintf("просьба %d", n)}, ReplyLine: fmt.Sprintf("reply: ответ %d", n),
			StartedAt: at(2 * n), EndedAt: at(2*n + 1),
		})
	}
	return out
}

func forkOf(t *testing.T, src *Thread, n int, past []PastRequest) Fork {
	t.Helper()
	f, err := ForkOf(src, n, past, "th-fork", at(500))
	if err != nil {
		t.Fatalf("ForkOf: %v", err)
	}
	return f
}

func numbersOf(rs []ForkedRequest) []int {
	out := []int{}
	for _, r := range rs {
		out = append(out, r.Number)
	}
	return out
}

// A fork copies the requests before n with their numbers, texts, mentions, scope, line and times; hidden ones (not in
// past) leave gaps. The new chat is a fresh one: version 0, no provenance, history from the start, the last copy
// current and DONE with nothing of a running request.
func TestForkOfCopiesEarlierRequests(t *testing.T) {
	src := chatOf(t, 6)
	if err := src.Observe(ref(t, values.KindLesson, "l1")); err != nil {
		t.Fatal(err)
	}
	scope, err := values.NewScope(ref(t, values.KindSubject, "s1"), values.ScreenToday, values.Ref{}, values.Ref{})
	if err != nil {
		t.Fatal(err)
	}
	past := pastOf(1, 2, 5, 6) // 3 and 4 are hidden
	past[1].Scope, past[1].Mentions = scope, []values.Mention{mention(t, values.KindLesson, "l7")}
	past[1].Texts = []string{"первый текст", "ответы на вопросы"}

	f := forkOf(t, src, 6, past)
	if got := numbersOf(f.Requests); !reflect.DeepEqual(got, []int{1, 2, 5}) {
		t.Fatalf("copies = %v", got)
	}
	c := f.Requests[1]
	if c.Scope != scope || !reflect.DeepEqual(c.Mentions, past[1].Mentions) || !reflect.DeepEqual(c.Texts, past[1].Texts) ||
		c.ReplyLine != "reply: ответ 2" || !c.StartedAt.Equal(at(4)) || !c.UpdatedAt.Equal(at(5)) {
		t.Fatalf("copy 2 = %+v", c)
	}

	s := f.Thread.State()
	if s.Version != 0 || s.ID != "th-fork" || s.ForkedFrom != "th1" || s.Turns != 5 || s.HistoryFrom != 0 ||
		len(s.Seen) != 0 || !s.ArchivedAt.IsZero() || !s.CreatedAt.Equal(at(500)) || !s.UpdatedAt.Equal(at(500)) ||
		!s.LastActive.Equal(at(500)) || f.Thread.ForkedFrom() != "th1" {
		t.Fatalf("fork = %+v", s)
	}
	want := TurnState{Number: 5, Status: StatusDone, ReplyLine: "reply: ответ 5", StartedAt: at(10), UpdatedAt: at(11)}
	if !reflect.DeepEqual(*s.Current, want) || !reflect.DeepEqual(*s.Current, f.Requests[2].State()) {
		t.Fatalf("current = %+v\nwant %+v", *s.Current, want)
	}
	for _, r := range f.Requests {
		st := r.State()
		if st.Budget != (Budget{}) || st.StepOpen || st.ProposedPlan != nil || st.ApprovedPlan != nil || st.Used != nil ||
			st.Open != nil || st.Created != nil || st.DroppedBy != 0 || st.DropsFrom != 0 || st.Status != StatusDone {
			t.Errorf("copy %d carries more than its texts: %+v", r.Number, st)
		}
	}

	// It is a valid stored chat once saved, and the source did not change.
	saved := f.Thread.State()
	saved.Version = 1
	back, err := Restore(saved)
	if err != nil || !reflect.DeepEqual(back.State(), saved) {
		t.Fatalf("Restore of the fork = %v", err)
	}
	if src.Version() != 0 || src.State().Turns != 6 || !src.Seen(ref(t, values.KindLesson, "l1")) || f.Thread.Seen(ref(t, values.KindLesson, "l1")) {
		t.Fatalf("source changed or provenance copied")
	}
}

// The next request of a fork is its own: the step key names the new chat and the number after the last copy.
func TestForkOfFirstRequestKey(t *testing.T) {
	f := forkOf(t, chatOf(t, 3), 3, pastOf(1, 2, 3))
	if err := f.Thread.Ask(Request{Text: "просьба 3 заново"}, at(501)); err != nil {
		t.Fatal(err)
	}
	key, err := f.Thread.BeginStep(at(502))
	if err != nil || key != "ag:th-fork:3:1" {
		t.Fatalf("BeginStep = %q, %v", key, err)
	}
}

// Forking from the first request gives an empty chat with the title of a fork.
func TestForkOfFirstRequest(t *testing.T) {
	src := chatOf(t, 2)
	if err := src.Compact(summaryOf(t, 1), at(20)); err != nil {
		t.Fatal(err)
	}
	f := forkOf(t, src, 1, pastOf(1))
	s := f.Thread.State()
	if len(f.Requests) != 0 || s.Turns != 0 || s.Current != nil || !s.Summary.IsZero() || !s.SummaryAt.IsZero() ||
		!f.Thread.IsEmpty() || s.Title != DefaultTitle+" (fork)" {
		t.Fatalf("fork from 1 = %+v, %d copies", s, len(f.Requests))
	}
}

// A reply line is the frozen one, else Reply of the entries, as Window sends it for the source.
func TestForkOfReplyLine(t *testing.T) {
	past := pastOf(1, 2, 3)
	past[1].ReplyLine = ""
	past[1].Entries = turnsOf([]values.HistoryEntry{
		entry(2, "просьба 2", nil), echo(2, [2]string{"reply", `{"text":"из шагов"}`}), results(2, ok("c0")),
	})
	f := forkOf(t, chatOf(t, 3), 3, past)
	if got := f.Requests[1].ReplyLine; got != "reply: из шагов" || got != Reply(past[1].Entries) {
		t.Fatalf("line from entries = %q", got)
	}
	past[1].Entries = nil
	f = forkOf(t, chatOf(t, 3), 3, past)
	if got := f.Requests[1].ReplyLine; got != NoReply {
		t.Fatalf("line without entries = %q", got)
	}
	if got := f.Requests[0].ReplyLine; got != "reply: ответ 1" {
		t.Fatalf("frozen line = %q", got)
	}
}

// The summary goes with the fork only when it covers requests before n that are among the copies; then it is dated
// at the fork.
func TestForkOfSummary(t *testing.T) {
	tests := []struct {
		name    string
		through int
		n       int
		past    []int
		copied  bool
	}{
		{"before n", 2, 4, []int{1, 2, 3, 4}, true},
		{"just before n", 3, 4, []int{1, 2, 3, 4}, true},
		{"covers n", 4, 4, []int{1, 2, 3, 4}, false},
		{"covers after n", 5, 4, []int{1, 2, 3, 4}, false},
		{"past the last copy", 3, 4, []int{1, 2, 4}, false},
		{"up to the last copy", 2, 4, []int{1, 2, 4}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := chatOf(t, 6)
			if err := src.Compact(summaryOf(t, tt.through), at(30)); err != nil {
				t.Fatal(err)
			}
			f := forkOf(t, src, tt.n, pastOf(tt.past...))
			got := f.Thread.Summary()
			if tt.copied {
				if got != src.Summary() || !f.Thread.SummaryAt().Equal(at(500)) {
					t.Fatalf("summary = %+v at %v, want %+v at the fork", got, f.Thread.SummaryAt(), src.Summary())
				}
				saved := f.Thread.State()
				saved.Version = 1
				if _, err := Restore(saved); err != nil {
					t.Fatal(err)
				}
				return
			}
			if !got.IsZero() || !f.Thread.SummaryAt().IsZero() {
				t.Fatalf("summary copied: %+v", got)
			}
		})
	}
}

// The title is the source's with « (fork)», cut on a rune boundary and trimmed so the whole is at most 80 runes.
func TestForkOfTitle(t *testing.T) {
	keep := MaxTitleRunes - utf8.RuneCountInString(" (fork)")
	tests := []struct {
		name  string
		title string
		want  string
	}{
		{"short", "Физика", "Физика (fork)"},
		{"exactly fits", strings.Repeat("a", keep), strings.Repeat("a", keep) + " (fork)"},
		{"one over", strings.Repeat("a", keep+1), strings.Repeat("a", keep) + " (fork)"},
		{"very long", strings.Repeat("я", MaxTitleRunes), strings.Repeat("я", keep) + " (fork)"},
		{"space at the cut", strings.Repeat("ж", keep-2) + "  хвост", strings.Repeat("ж", keep-2) + " (fork)"},
		{"default", DefaultTitle, DefaultTitle + " (fork)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := chatOf(t, 2)
			if err := src.Rename(tt.title, at(10)); err != nil {
				t.Fatal(err)
			}
			got := forkOf(t, src, 2, pastOf(1, 2)).Thread.Title()
			if got != tt.want || utf8.RuneCountInString(got) > MaxTitleRunes || checkTitle(got) != nil {
				t.Fatalf("title = %q (%d runes), want %q", got, utf8.RuneCountInString(got), tt.want)
			}
		})
	}
}

// An earlier request without a text is not copied (numbers may have gaps); the last copy that remains is current, and
// none gives an empty chat. n itself must have a text.
func TestForkOfSkipsRequestWithoutText(t *testing.T) {
	tests := []struct {
		name      string
		noText    []int
		wantCopy  []int
		wantTurns int
	}{
		{"middle", []int{2}, []int{1, 3}, 3},
		{"would-be last", []int{3}, []int{1, 2}, 2},
		{"all earlier", []int{1, 2, 3}, []int{}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			past := pastOf(1, 2, 3, 4)
			for _, n := range tt.noText {
				past[n-1].Texts = nil
			}
			f := forkOf(t, chatOf(t, 4), 4, past)
			if got := numbersOf(f.Requests); !reflect.DeepEqual(got, tt.wantCopy) {
				t.Fatalf("copies = %v, want %v", got, tt.wantCopy)
			}
			s := f.Thread.State()
			if s.Turns != tt.wantTurns || (tt.wantTurns == 0) != (s.Current == nil) ||
				(s.Current != nil && s.Current.Number != tt.wantTurns) {
				t.Fatalf("turns = %d, current = %+v", s.Turns, s.Current)
			}
		})
	}
}

// The summary rule uses the last copy that remains: a summary over a skipped would-be-last request is dropped.
func TestForkOfSummaryWithSkippedLast(t *testing.T) {
	src := chatOf(t, 4)
	if err := src.Compact(summaryOf(t, 3), at(20)); err != nil {
		t.Fatal(err)
	}
	past := pastOf(1, 2, 3, 4)
	past[2].Texts = nil
	f := forkOf(t, src, 4, past)
	if s := f.Thread.State(); s.Turns != 2 || !s.Summary.IsZero() {
		t.Fatalf("turns = %d, summary through %d", s.Turns, s.Summary.Through())
	}
}

// A request that is not one to fork from, or inputs that do not fit, are ErrInvalidFork and give nothing.
func TestForkOfInvalid(t *testing.T) {
	src := chatOf(t, 3)
	edit := func(change func([]PastRequest) []PastRequest) []PastRequest { return change(pastOf(1, 2, 3)) }
	tests := []struct {
		name string
		src  *Thread
		n    int
		past []PastRequest
	}{
		{"no source", nil, 1, pastOf(1)},
		{"n is 0", src, 0, pastOf(1)},
		{"n after the last", src, 4, pastOf(1, 2, 3, 4)},
		{"empty past", src, 3, nil},
		{"n is hidden", src, 3, pastOf(1, 2)},
		{"past after n", src, 2, pastOf(1, 2, 3)},
		{"unordered", src, 3, pastOf(2, 1, 3)},
		{"repeated", src, 3, pastOf(1, 1, 3)},
		{"number 0", src, 3, pastOf(0, 1, 3)},
		{"only empty texts of n", src, 3, edit(func(p []PastRequest) []PastRequest { p[2].Texts = []string{""}; return p })},
		{"empty text", src, 3, edit(func(p []PastRequest) []PastRequest { p[0].Texts = []string{"a", ""}; return p })},
		{"no text of n", src, 3, edit(func(p []PastRequest) []PastRequest { p[2].Texts = nil; return p })},
		{"too long text", src, 3, edit(func(p []PastRequest) []PastRequest {
			p[0].Texts = []string{strings.Repeat("a", values.MaxStudentTextBytes+1)}
			return p
		})},
		{"no start time", src, 3, edit(func(p []PastRequest) []PastRequest { p[0].StartedAt = time.Time{}; return p })},
		{"ended before started", src, 3, edit(func(p []PastRequest) []PastRequest { p[1].EndedAt = at(0); return p })},
		{"bad frozen line", src, 3, edit(func(p []PastRequest) []PastRequest { p[0].ReplyLine = " "; return p })},
		{"repeated mentions", src, 3, edit(func(p []PastRequest) []PastRequest {
			m := mention(t, values.KindLesson, "l1")
			p[0].Mentions = []values.Mention{m, m}
			return p
		})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := ForkOf(tt.src, tt.n, tt.past, "th-fork", at(500))
			if !errors.Is(err, ErrInvalidFork) || f.Thread != nil || f.Requests != nil {
				t.Fatalf("ForkOf = %+v, %v; want ErrInvalidFork", f, err)
			}
		})
	}
}

// A bad new id or no time is the thread's own error; a fork onto its source id is refused.
func TestForkOfNewChat(t *testing.T) {
	src := chatOf(t, 2)
	if _, err := ForkOf(src, 2, pastOf(1, 2), "a/b", at(500)); !errors.Is(err, ErrInvalidThread) {
		t.Errorf("bad id: %v", err)
	}
	if _, err := ForkOf(src, 2, pastOf(1, 2), "th-fork", time.Time{}); !errors.Is(err, ErrInvalidThread) {
		t.Errorf("no time: %v", err)
	}
	if _, err := ForkOf(src, 2, pastOf(1, 2), "th1", at(500)); !errors.Is(err, ErrInvalidFork) {
		t.Errorf("onto itself: %v", err)
	}
}
