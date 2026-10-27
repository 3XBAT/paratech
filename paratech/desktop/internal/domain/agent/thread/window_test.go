package thread

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"paratech/desktop/internal/domain/agent/values"
)

// chatRequest is a stored earlier request: its text, a reply echo and its result.
func chatRequest(turn int, text string) []values.HistoryEntry {
	return []values.HistoryEntry{
		entry(turn, text, nil), echo(turn, [2]string{"reply", `{"text":"ответ ` + fmt.Sprint(turn) + `"}`}),
		results(turn, values.ShownResult("c0")),
	}
}

func chatHistory(n int, text func(int) string) []values.HistoryEntry {
	var out []values.HistoryEntry
	for turn := 1; turn <= n; turn++ {
		out = append(out, chatRequest(turn, text(turn))...)
	}
	return out
}

func numbered(turn int) string { return fmt.Sprintf("просьба %d", turn) }

// earlierOf returns the history part of a window (everything before the current text) and the numbers of the
// requests it carries.
func earlierOf(t *testing.T, w []values.HistoryTurn) ([]values.HistoryTurn, []int) {
	t.Helper()
	h := w[:len(w)-1]
	var nums []int
	for i := 0; i < len(h); i += 2 {
		var n int
		if _, err := fmt.Sscanf(h[i].StudentText, "просьба %d", &n); err != nil || h[i+1].AgentReply == "" {
			t.Fatalf("turn %d = %+v, %+v: not a pair", i, h[i], h[i+1])
		}
		nums = append(nums, n)
	}
	return h, nums
}

// TestWindowShiftsBySteps sends 30 requests in a row: up to 20 earlier answers go whole; then the start moves 5
// requests at a time, so 16–20 earlier answers go and the history prefix stays byte-identical for up to 5 requests.
func TestWindowShiftsBySteps(t *testing.T) {
	var prev []values.HistoryTurn
	prevFirst, run := 0, 0
	for n := 1; n <= 30; n++ {
		entries := append(chatHistory(n-1, numbered), entry(n, numbered(n), nil))
		got := Window(entries, n)
		if last := got[len(got)-1]; last.StudentText != numbered(n) {
			t.Fatalf("request %d: last turn %+v", n, last)
		}
		history, nums := earlierOf(t, got)

		want := n - 1
		if n > MaxHistoryTurns+1 {
			want = 16 + (n-22)%WindowStep
		}
		if len(nums) != want {
			t.Fatalf("request %d sends %d earlier answers, want %d", n, len(nums), want)
		}
		if len(nums) > 0 && ((nums[0]-1)%WindowStep != 0 || nums[len(nums)-1] != n-1) {
			t.Fatalf("request %d sends requests %v", n, nums)
		}

		first := 0
		if len(nums) > 0 {
			first = nums[0]
		}
		if first == prevFirst {
			run++
			// The earlier history of the previous request is a prefix of this one, entry by entry.
			if len(prev) > 0 && !reflect.DeepEqual(history[:len(prev)], prev) {
				t.Fatalf("request %d: history prefix changed", n)
			}
		} else {
			if prevFirst != 0 && first != prevFirst+WindowStep {
				t.Fatalf("request %d: start moved from %d to %d", n, prevFirst, first)
			}
			run = 1
		}
		if n > MaxHistoryTurns+1 && run > WindowStep {
			t.Fatalf("request %d: the same start for %d requests", n, run)
		}
		prev, prevFirst = history, first
	}
}

func TestWindowSkipsDroppedRequests(t *testing.T) {
	entries := chatHistory(6, numbered)
	for i := range entries {
		if entries[i].Turn >= 3 && entries[i].Turn <= 5 {
			entries[i].DroppedBy = 7
		}
	}
	entries = append(entries, entry(7, numbered(7), nil))
	_, nums := earlierOf(t, Window(entries, 7))
	if fmt.Sprint(nums) != "[1 2 6]" {
		t.Fatalf("window carries %v, want [1 2 6]", nums)
	}
	if f := Fill(entries[:len(entries)-1], 7); f.Pairs != 3 || f.Overflow {
		t.Fatalf("Fill = %+v", f)
	}
}

// TestFillMatchesWindow: on the same entries Fill counts exactly what Window sends before the next text.
func TestFillMatchesWindow(t *testing.T) {
	full := func(turn int) string {
		return numbered(turn) + " " + strings.Repeat("я", (values.MaxStudentTextBytes-20)/2)
	}
	dropped := chatHistory(30, numbered)
	for i := range dropped {
		if dropped[i].Turn >= 10 && dropped[i].Turn <= 24 {
			dropped[i].DroppedBy = 25
		}
	}
	cutOff := chatHistory(8, numbered)[1:] // request 1 lost its text to the read limit

	tests := []struct {
		name     string
		entries  []values.HistoryEntry
		overflow bool
	}{
		{"empty chat", nil, false},
		{"one earlier", chatHistory(1, numbered), false},
		{"20 earlier", chatHistory(20, numbered), false},
		{"21 earlier", chatHistory(21, numbered), true},
		{"29 earlier", chatHistory(29, numbered), true},
		{"bytes", chatHistory(14, full), true},
		{"dropped", dropped, false},
		{"text cut off", cutOff, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			next := 1
			if len(tt.entries) > 0 {
				next = tt.entries[len(tt.entries)-1].Turn + 1
			}
			f := Fill(tt.entries, next)
			w := Window(append(tt.entries, entry(next, numbered(next), nil)), next)
			history, _ := earlierOf(t, w)
			bytes := 0
			for _, h := range history {
				bytes += h.Size()
			}
			if f.Pairs != len(history)/2 || f.Bytes != bytes || f.Turns != len(w) {
				t.Fatalf("Fill = %+v, Window sends %d pairs, %d bytes, %d turns", f, len(history)/2, bytes, len(w))
			}
			if f.Overflow != tt.overflow {
				t.Fatalf("Overflow = %v, want %v", f.Overflow, tt.overflow)
			}
			want := max(float64(f.Pairs)/MaxHistoryTurns, float64(f.Bytes)/MaxHistoryBytes, float64(f.Turns)/MaxRequestTurns)
			if f.Share != want || f.Share < 0 || f.Share > 1 {
				t.Fatalf("Share = %v, want %v", f.Share, want)
			}
		})
	}

	if f := Fill(chatHistory(20, numbered), 21); f.Share != 1 {
		t.Fatalf("20 pairs fill %v, want the whole window", f.Share)
	}
}

// TestLabelNeverReachesTheHistory: a mention's label is display text; the window carries the student's text and the
// replies only, and no type sent to the Hub can hold a mention.
func TestLabelNeverReachesTheHistory(t *testing.T) {
	const label = "Лекция-метка-только-для-показа"
	m, err := values.NewMention(ref(t, values.KindLesson, "l9"), label)
	if err != nil {
		t.Fatal(err)
	}
	th, err := New("th1", t0)
	if err != nil {
		t.Fatal(err)
	}
	if err := th.Ask(Request{Mentions: []values.Mention{m}, Text: "что было на лекции"}, at(1)); err != nil {
		t.Fatal(err)
	}
	entries := append(chatRequest(1, "что было на лекции"), entry(2, "а ещё", nil))
	got := Window(entries, 2)
	if s := fmt.Sprintf("%#v", got); strings.Contains(s, label) {
		t.Fatalf("the window holds the label: %s", s)
	}

	mention := reflect.TypeFor[values.Mention]()
	for _, root := range []reflect.Type{reflect.TypeFor[values.StepRequest](), reflect.TypeFor[values.HistoryTurn]()} {
		if path, ok := reaches(root, mention, map[reflect.Type]bool{}); ok {
			t.Fatalf("%s reaches a mention through %s", root, path)
		}
	}
}

// reaches reports whether a value of type from can hold a value of type to, and through which field.
func reaches(from, to reflect.Type, seen map[reflect.Type]bool) (string, bool) {
	if from == to {
		return from.String(), true
	}
	if seen[from] {
		return "", false
	}
	seen[from] = true
	switch from.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array:
		return reaches(from.Elem(), to, seen)
	case reflect.Map:
		if p, ok := reaches(from.Key(), to, seen); ok {
			return p, true
		}
		return reaches(from.Elem(), to, seen)
	case reflect.Struct:
		for i := range from.NumField() {
			f := from.Field(i)
			if p, ok := reaches(f.Type, to, seen); ok {
				return from.String() + "." + f.Name + " > " + p, true
			}
		}
	}
	return "", false
}
