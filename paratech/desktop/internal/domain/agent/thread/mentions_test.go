package thread

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"paratech/desktop/internal/domain/agent/values"
)

// mentioned sets the mention IDs of request turn on its entries, as the store does.
func mentioned(entries []values.HistoryEntry, turn int, refs ...values.Ref) []values.HistoryEntry {
	for i := range entries {
		if entries[i].Turn == turn {
			entries[i].Mentions = refs
		}
	}
	return entries
}

// mentionsText renders step mentions as "kind:id" ("*" marks a current one).
func mentionsText(ms []values.StepMention) string {
	parts := make([]string, 0, len(ms))
	for _, m := range ms {
		s := string(m.Ref.Kind()) + ":" + m.Ref.ID()
		if m.Current {
			s = "*" + s
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, " ")
}

func TestStepMentions(t *testing.T) {
	l := func(id string) values.Ref { return ref(t, values.KindLesson, id) }
	s := func(id string) values.Ref { return ref(t, values.KindSubject, id) }

	tests := []struct {
		name    string
		entries func() []values.HistoryEntry
		current int
		summary values.HistorySummary
		now     []values.Ref
		want    string
	}{
		{
			name:    "current only",
			entries: func() []values.HistoryEntry { return []values.HistoryEntry{entry(1, "что было?", nil)} },
			current: 1, now: []values.Ref{l("l1"), s("s1")},
			want: "*lesson:l1 *subject:s1",
		},
		{
			name: "earlier newest first, the current not repeated",
			entries: func() []values.HistoryEntry {
				e := chatHistory(3, numbered)
				e = mentioned(e, 1, l("l1"), s("s1"))
				e = mentioned(e, 3, l("l3"), l("l1"))
				return append(e, entry(4, numbered(4), nil))
			},
			current: 4, now: []values.Ref{s("s1")},
			want: "*subject:s1 lesson:l3 lesson:l1",
		},
		{
			name: "dropped request gives none",
			entries: func() []values.HistoryEntry {
				e := mentioned(chatHistory(2, numbered), 2, l("l2"))
				e = mentioned(e, 1, l("l1"))
				for i := range e {
					if e[i].Turn == 2 {
						e[i].DroppedBy = 3
					}
				}
				return append(e, entry(3, numbered(3), nil))
			},
			current: 3,
			want:    "lesson:l1",
		},
		{
			name: "covered by the summary gives none",
			entries: func() []values.HistoryEntry {
				e := mentioned(chatHistory(3, numbered), 1, l("l1"))
				e = mentioned(e, 2, l("l2"))
				e = mentioned(e, 3, l("l3"))
				return append(e, entry(4, numbered(4), nil))
			},
			current: 4, summary: summaryOf(t, 2),
			want: "lesson:l3",
		},
		{
			name: "cut off by the window start gives none",
			entries: func() []values.HistoryEntry {
				e := chatHistory(25, numbered)
				for n := 1; n <= 25; n++ {
					e = mentioned(e, n, l(fmt.Sprintf("l%d", n)))
				}
				return append(e, entry(26, numbered(26), nil))
			},
			current: 26,
			// Window sends requests 6..25 (start on a block of 5); the 16 newest of them fit.
			want: "lesson:l25 lesson:l24 lesson:l23 lesson:l22 lesson:l21 lesson:l20 lesson:l19 lesson:l18 " +
				"lesson:l17 lesson:l16 lesson:l15 lesson:l14 lesson:l13 lesson:l12 lesson:l11 lesson:l10",
		},
		{
			name: "at most 16, the current first",
			entries: func() []values.HistoryEntry {
				e := mentioned(chatHistory(2, numbered), 1, l("a1"), l("a2"), l("a3"), l("a4"), l("a5"))
				e = mentioned(e, 2, l("b1"), l("b2"), l("b3"), l("b4"), l("b5"))
				return append(e, entry(3, numbered(3), nil))
			},
			current: 3,
			now:     []values.Ref{s("c1"), s("c2"), s("c3"), s("c4"), s("c5"), s("c6"), s("c7"), s("c8"), s("c9")},
			want: "*subject:c1 *subject:c2 *subject:c3 *subject:c4 *subject:c5 *subject:c6 *subject:c7 *subject:c8 " +
				"lesson:b1 lesson:b2 lesson:b3 lesson:b4 lesson:b5 lesson:a1 lesson:a2 lesson:a3",
		},
		{
			name: "a request without its text first sends no pairs",
			entries: func() []values.HistoryEntry {
				e := mentioned(chatHistory(1, numbered), 1, l("l1"))
				return append(e, echo(2, [2]string{"list_subjects", `{}`}))
			},
			current: 2, now: []values.Ref{s("s1")},
			want: "*subject:s1",
		},
		{
			name: "mentions of the current entries are not earlier ones",
			entries: func() []values.HistoryEntry {
				return mentioned([]values.HistoryEntry{entry(1, "что было?", nil)}, 1, l("stale"))
			},
			current: 1,
			want:    "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entries := tt.entries()
			got := StepMentions(entries, tt.current, tt.summary, tt.now)
			if mentionsText(got) != tt.want {
				t.Fatalf("StepMentions = %q\nwant           %q", mentionsText(got), tt.want)
			}
			if again := StepMentions(entries, tt.current, tt.summary, tt.now); mentionsText(again) != tt.want {
				t.Fatal("not deterministic")
			}
		})
	}
}

// TestStepMentionsFollowTheWindow: an earlier mention goes exactly when Window sends the pair of its request.
func TestStepMentionsFollowTheWindow(t *testing.T) {
	for n := 2; n <= 40; n++ {
		e := chatHistory(n-1, numbered)
		for turn := 1; turn < n; turn++ {
			e = mentioned(e, turn, ref(t, values.KindLesson, fmt.Sprintf("l%d", turn)))
		}
		e = append(e, entry(n, numbered(n), nil))
		_, nums := earlierOf(t, Window(e, n, noSummary))
		got := StepMentions(e, n, noSummary, nil)

		want := len(nums)
		if want > values.MaxStepMentions {
			want = values.MaxStepMentions
		}
		if len(got) != want {
			t.Fatalf("request %d: %d mentions, window sends %d pairs", n, len(got), len(nums))
		}
		for _, m := range got {
			var turn int
			if _, err := fmt.Sscanf(m.Ref.ID(), "l%d", &turn); err != nil || !slices.Contains(nums, turn) || m.Current {
				t.Fatalf("request %d sends %v, its request is not in the window %v", n, m, nums)
			}
		}
	}
}

// TestSummaryIsNoSourceOfMentions: an ID written in the summary text is neither sent nor accepted.
func TestSummaryIsNoSourceOfMentions(t *testing.T) {
	sum, err := values.ParseHistorySummary(2, "сводка: урок lesson:l-secret и предмет s-secret")
	if err != nil {
		t.Fatal(err)
	}
	e := append(chatHistory(2, numbered), entry(3, numbered(3), nil))
	got := StepMentions(e, 3, sum, nil)
	if len(got) != 0 {
		t.Fatalf("the summary gave mentions %v", got)
	}

	th := askedThread(t)
	context := values.NewRefSet(values.StepMentionRefs(got)...)
	if err := th.CheckRefs([]values.Ref{ref(t, values.KindLesson, "l-secret")}, context); err == nil {
		t.Fatal("an ID of the summary passed the provenance check")
	}
}

// TestEarlierMentionsPassTheProvenanceCheck: the IDs StepMentions returns are known as the step context.
func TestEarlierMentionsPassTheProvenanceCheck(t *testing.T) {
	l1 := ref(t, values.KindLesson, "l1")
	e := mentioned(chatHistory(1, numbered), 1, l1)
	e = append(e, entry(2, numbered(2), nil))
	th := askedThread(t)
	if err := th.CheckRefs([]values.Ref{l1}, values.RefSet{}); err == nil {
		t.Fatal("l1 known without the step mentions")
	}
	context := values.NewRefSet(values.StepMentionRefs(StepMentions(e, 2, noSummary, nil))...)
	if err := th.CheckRefs([]values.Ref{l1}, context); err != nil {
		t.Fatalf("CheckRefs = %v", err)
	}
}

func askedThread(t *testing.T) *Thread {
	t.Helper()
	th, err := New("th1", t0)
	if err != nil {
		t.Fatal(err)
	}
	if err := th.Ask(Request{Text: "что было"}, at(1)); err != nil {
		t.Fatal(err)
	}
	return th
}
