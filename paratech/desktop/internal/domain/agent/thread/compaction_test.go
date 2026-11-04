package thread

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"paratech/desktop/internal/domain/agent/values"
)

// withState returns a chat of n DONE requests with the stored state changed by edit.
func withState(t *testing.T, n int, edit func(*State)) *Thread {
	t.Helper()
	st := chatOf(t, n).State()
	st.Version = 1
	if edit != nil {
		edit(&st)
	}
	th, err := Restore(st)
	if err != nil {
		t.Fatal(err)
	}
	return th
}

// compactionOf runs CompactionRequest and checks that it changes nothing in the thread.
func compactionOf(t *testing.T, th *Thread, entries []values.HistoryEntry) (values.SummaryRequest, bool, error) {
	t.Helper()
	before := th.State()
	req, ok, err := th.CompactionRequest(entries)
	if !reflect.DeepEqual(th.State(), before) {
		t.Fatal("CompactionRequest changed the thread")
	}
	return req, ok, err
}

// TestCompactionRequestFollowsThePlan: the request is CompactionPlan of the next request, its pairs are those Window
// groups (dropped requests skipped, the reply built from the echo), and the previous summary goes with it.
func TestCompactionRequestFollowsThePlan(t *testing.T) {
	dropped := chatHistory(30, numbered)
	for i := range dropped {
		if dropped[i].Turn >= 3 && dropped[i].Turn <= 5 {
			dropped[i].DroppedBy = 31
		}
	}
	tests := []struct {
		name    string
		n       int
		entries []values.HistoryEntry
		summary int // through of the stored summary; 0: none
		nums    []int
	}{
		{"19 earlier", 19, chatHistory(19, numbered), 0, nil},
		{"20 earlier", 20, chatHistory(20, numbered), 0, []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}},
		{"dropped requests skipped", 30, dropped, 0, []int{1, 2, 6, 7, 8, 9, 10, 11, 12, 13}},
		{"after a summary", 30, chatHistory(30, numbered), 10, []int{11, 12, 13, 14, 15, 16, 17, 18, 19, 20}},
		{"summary, 19 after", 29, chatHistory(29, numbered), 10, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			th := withState(t, tt.n, func(st *State) {
				if tt.summary > 0 {
					st.Summary, st.SummaryAt = summaryOf(t, tt.summary), at(90)
				}
			})
			_, through, planned := CompactionPlan(tt.entries, tt.n+1, th.Summary())

			req, ok, err := compactionOf(t, th, tt.entries)
			if err != nil || ok != planned || ok != (tt.nums != nil) {
				t.Fatalf("CompactionRequest = %v, %v; plan %v", ok, err, planned)
			}
			if !ok {
				if !req.IsZero() {
					t.Fatalf("no compaction but a request %+v", req)
				}
				return
			}
			if req.Through() != through || req.Key() != fmt.Sprintf("cs:th1:%d", through) || req.Thread() != "th1" ||
				req.Previous() != th.Summary().Text() {
				t.Fatalf("request %s through %d, previous %q", req.Key(), req.Through(), req.Previous())
			}
			var want []values.SummaryPair
			for _, n := range tt.nums {
				want = append(want, values.SummaryPair{Student: numbered(n), Reply: fmt.Sprintf("reply: ответ %d", n)})
			}
			if got := req.Pairs(); !reflect.DeepEqual(got, want) {
				t.Fatalf("pairs %+v\nwant %+v", got, want)
			}
		})
	}
}

// The texts of one request are joined by a line break; the frozen reply line is the reply, as in Window.
func TestCompactionRequestPairSources(t *testing.T) {
	var entries []values.HistoryEntry
	for turn := 1; turn <= 20; turn++ {
		entries = append(entries, chatRequest(turn, numbered(turn))...)
	}
	entries = append(entries[:3:3], append([]values.HistoryEntry{entry(1, "и ещё", nil)}, entries[3:]...)...)
	for i := range entries {
		if entries[i].Turn == 2 {
			entries[i].ReplyLine = "reply: замороженная строка"
		}
	}
	req, ok, err := compactionOf(t, chatOf(t, 20), entries)
	if err != nil || !ok {
		t.Fatalf("CompactionRequest = %v, %v", ok, err)
	}
	p := req.Pairs()
	if p[0] != (values.SummaryPair{Student: "просьба 1\nи ещё", Reply: "reply: ответ 1"}) ||
		p[1] != (values.SummaryPair{Student: "просьба 2", Reply: "reply: замороженная строка"}) {
		t.Fatalf("pairs %+v", p[:2])
	}
}

// Texts over the Hub limits are cut on a rune boundary, keeping the beginning; a long frozen reply line too.
func TestCompactionRequestCutsToLimits(t *testing.T) {
	long := strings.Repeat("я", values.MaxStudentTextBytes/2) // a whole student text of 4 KiB
	var entries []values.HistoryEntry
	for turn := 1; turn <= 20; turn++ {
		entries = append(entries, entry(turn, "a"+long[:len(long)-2], nil), entry(turn, long, nil))
		entries = append(entries, chatRequest(turn, "x")[1:]...)
	}
	for i := range entries {
		entries[i].ReplyLine = "reply: " + strings.Repeat("ж", values.MaxSummaryRunes)
	}
	req, ok, err := compactionOf(t, chatOf(t, 20), entries)
	if err != nil || !ok {
		t.Fatalf("CompactionRequest = %v, %v", ok, err)
	}
	for _, p := range req.Pairs() {
		if len(p.Student) > values.MaxSummaryStudentBytes || len(p.Reply) > values.MaxSummaryReplyBytes ||
			!utf8.ValidString(p.Student) || !utf8.ValidString(p.Reply) {
			t.Fatalf("a part over its limit: %d, %d bytes", len(p.Student), len(p.Reply))
		}
		if !strings.HasPrefix("a"+long[:len(long)-2]+"\n"+long, p.Student) || len(p.Student) < values.MaxSummaryStudentBytes-1 ||
			!strings.HasPrefix(entries[0].ReplyLine, p.Reply) || len(p.Reply) < values.MaxSummaryReplyBytes-1 {
			t.Fatalf("a cut lost the beginning or too much: %d, %d bytes", len(p.Student), len(p.Reply))
		}
	}
	if req.EncodedSize() > values.MaxSummaryDataBytes {
		t.Fatalf("encoded %d bytes", req.EncodedSize())
	}
}

// Texts whose JSON escapes double their size are shrunk, longest first, until the data fits; every pair stays.
func TestCompactionRequestFitsTheEncodedData(t *testing.T) {
	quotes := strings.Repeat(`"`, values.MaxSummaryStudentBytes)
	var entries []values.HistoryEntry
	for turn := 1; turn <= 20; turn++ {
		entries = append(entries, chatRequest(turn, quotes)...)
	}
	for i := range entries {
		entries[i].ReplyLine = strings.Repeat(`\`, values.MaxSummaryReplyBytes)
	}
	big, err := values.ParseHistorySummary(5, strings.Repeat(`"`, values.MaxSummaryRunes))
	if err != nil {
		t.Fatal(err)
	}
	th := withState(t, 20, func(st *State) { st.Summary, st.SummaryAt = big, at(90) })
	entries = entries[3*5:] // the summary covers 1–5

	req, ok, err := compactionOf(t, th, entries)
	if err != nil || !ok {
		t.Fatalf("CompactionRequest = %v, %v", ok, err)
	}
	// The last cut may take a little more than needed (an escape is two bytes), never a whole long part.
	if n := req.EncodedSize(); n > values.MaxSummaryDataBytes || n < values.MaxSummaryDataBytes-values.MaxSummaryStudentBytes {
		t.Fatalf("encoded %d bytes: over the limit or shrunk too much", n)
	}
	if len(req.Pairs()) != values.MaxSummaryPairs || req.Through() != 15 || req.Previous() != big.Text() {
		t.Fatalf("%d pairs through %d", len(req.Pairs()), req.Through())
	}
	for _, p := range req.Pairs() {
		if p.Student == "" || strings.Trim(p.Student, `"`) != "" || strings.Trim(p.Reply, `\`) != "" {
			t.Fatalf("pair %d/%d bytes is not a prefix", len(p.Student), len(p.Reply))
		}
	}
}

// A pair whose student text is blank once cleaned is left out; none left is nothing to do.
func TestCompactionRequestSkipsBlankTexts(t *testing.T) {
	blank := func(turn int) string {
		if turn%2 == 0 {
			return " \x00 "
		}
		return numbered(turn)
	}
	req, ok, err := compactionOf(t, chatOf(t, 20), chatHistory(20, blank))
	if err != nil || !ok || len(req.Pairs()) != 5 || req.Through() != 10 {
		t.Fatalf("CompactionRequest = %+v, %v, %v", req.Pairs(), ok, err)
	}
	for _, p := range req.Pairs() {
		if strings.TrimSpace(p.Student) == "" {
			t.Fatalf("a blank pair was sent: %+v", req.Pairs())
		}
	}

	req, ok, err = compactionOf(t, chatOf(t, 20), chatHistory(20, func(int) string { return "\x00" }))
	if err != nil || ok || !req.IsZero() {
		t.Fatalf("all blank: %+v, %v, %v", req, ok, err)
	}
}

// Only the requests from HistoryFrom on are planned and summarized, as only they are sent (NextFill).
func TestCompactionRequestFromHistoryStart(t *testing.T) {
	entries := chatHistory(25, numbered)
	tests := []struct {
		from  int
		first int // 0: no compaction
	}{
		{0, 1},
		{3, 3},
		{12, 0}, // 14 requests from 12 on: room left, the older ones are not the agent's to summarize
	}
	for _, tt := range tests {
		th := withState(t, 25, func(st *State) { st.HistoryFrom = tt.from })
		req, ok, err := compactionOf(t, th, entries)
		if err != nil || ok != (tt.first > 0) {
			t.Fatalf("from %d: %v, %v", tt.from, ok, err)
		}
		if !ok {
			continue
		}
		if p := req.Pairs(); p[0].Student != numbered(tt.first) || req.Through() != tt.first+CompactPairs-1 {
			t.Fatalf("from %d: first %q, through %d", tt.from, p[0].Student, req.Through())
		}
	}
}

func TestCompactionRequestState(t *testing.T) {
	long := func(turn int) string {
		return numbered(turn) + " " + strings.Repeat("я", (values.MaxStudentTextBytes-20)/2)
	}
	// Three requests of five long texts each fill the window by bytes.
	var three []values.HistoryEntry
	for turn := 1; turn <= 3; turn++ {
		for range 5 {
			three = append(three, entry(turn, long(turn), nil))
		}
		three = append(three, echo(turn, [2]string{"reply", `{"text":"ответ"}`}), results(turn, values.ShownResult("c0")))
	}

	empty, err := New("th1", t0)
	if err != nil {
		t.Fatal(err)
	}
	if req, ok, err := compactionOf(t, empty, nil); ok || err != nil || !req.IsZero() {
		t.Fatalf("an empty chat: %v, %v", ok, err)
	}

	planning := running(t, 2)
	if _, ok, err := compactionOf(t, planning, three); ok || !errors.Is(err, ErrTurnInProgress) || !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("a running request: %v, %v", ok, err)
	}

	// A request waiting for the student is not over: the summary stops before it.
	waiting := running(t, 2)
	if _, err := waiting.AskQuestions(at(101)); err != nil {
		t.Fatal(err)
	}
	req, ok, err := compactionOf(t, waiting, three)
	if err != nil || !ok || req.Through() != 2 || len(req.Pairs()) != 2 {
		t.Fatalf("a waiting request: %+v, %v, %v", req, ok, err)
	}
	if err := waiting.Compact(mustSummary(t, req.Through()), at(102)); err != nil {
		t.Fatalf("the planned summary is refused: %v", err)
	}

	done := chatOf(t, 3)
	req, ok, err = compactionOf(t, done, three)
	if err != nil || !ok || req.Through() != 3 {
		t.Fatalf("a final request: %+v, %v, %v", req, ok, err)
	}
}

func mustSummary(t *testing.T, through int) values.HistorySummary {
	t.Helper()
	s, err := values.ParseHistorySummary(through, "сводка")
	if err != nil {
		t.Fatal(err)
	}
	return s
}
