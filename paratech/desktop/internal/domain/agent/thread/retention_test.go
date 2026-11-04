package thread

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/agent/values"
)

func TestDetailsCutoff(t *testing.T) {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	if got, want := DetailsCutoff(now), now.AddDate(0, 0, -90); !got.Equal(want) {
		t.Fatalf("cutoff = %v, want %v", got, want)
	}
}

func TestFrozenReply(t *testing.T) {
	request := turnsOf([]values.HistoryEntry{
		entry(1, "вопрос", nil), echo(1, [2]string{"reply", `{"text":"ответ"}`}), results(1, values.ShownResult("c0")),
	})
	if got, err := FrozenReply(request); err != nil || got != Reply(request) || got != "reply: ответ" {
		t.Fatalf("FrozenReply = %q, %v", got, err)
	}
	if got, err := FrozenReply(turnsOf([]values.HistoryEntry{entry(1, "вопрос", nil)})); err != nil || got != NoReply {
		t.Fatalf("FrozenReply without cards = %q, %v", got, err)
	}
}

// purgeDetails is the purge as the store applies it to the entries of a chat: every request before current keeps its
// texts and gets its reply line frozen (if none yet); its calls and results go.
func purgeDetails(t *testing.T, entries []values.HistoryEntry, current int) []values.HistoryEntry {
	t.Helper()
	lines := map[int]string{}
	byTurn := map[int][]values.HistoryTurn{}
	for _, e := range entries {
		byTurn[e.Turn] = append(byTurn[e.Turn], e.Entry)
		if e.ReplyLine != "" {
			lines[e.Turn] = e.ReplyLine
		}
	}
	var out []values.HistoryEntry
	for _, e := range entries {
		if e.Turn >= current {
			out = append(out, e)
			continue
		}
		if _, ok := lines[e.Turn]; !ok {
			line, err := FrozenReply(byTurn[e.Turn])
			if err != nil {
				t.Fatal(err)
			}
			lines[e.Turn] = line
		}
		if e.Entry.StudentText == "" {
			continue
		}
		e.ReplyLine = lines[e.Turn]
		out = append(out, e)
	}
	return out
}

// After the purge of the earlier requests the window, the fill and the compaction plan are the same: they read only
// the texts and the reply lines of earlier requests.
func TestWindowFillAndCompactionUnchangedByThePurge(t *testing.T) {
	cannot := argsJSON(t, map[string]any{"your_part": []string{"отправить письмо"}, "our_part": []string{"черновик"}})
	request := func(turn int) []values.HistoryEntry {
		text := fmt.Sprintf("просьба %d %s", turn, strings.Repeat("т", 200*turn))
		switch turn % 3 {
		case 0:
			return []values.HistoryEntry{entry(turn, text, nil), echo(turn, [2]string{"reply", fmt.Sprintf(`{"text":"ответ %d"}`, turn)}),
				results(turn, values.ShownResult("c0"))}
		case 1:
			return []values.HistoryEntry{entry(turn, text, nil), echo(turn, [2]string{"list_subjects", `{}`}), results(turn, ok("c0")),
				echo(turn, [2]string{"cannot_do", cannot}), results(turn, values.ShownResult("c0")), entry(turn, "ещё текст", nil)}
		default:
			return []values.HistoryEntry{entry(turn, text, nil)}
		}
	}
	for _, n := range []int{3, 12, 19, 24, 30} {
		t.Run(fmt.Sprint(n, " requests"), func(t *testing.T) {
			var entries []values.HistoryEntry
			for turn := 1; turn <= n; turn++ {
				entries = append(entries, request(turn)...)
			}
			// Some requests froze their line when they ended; the older ones did not (before reply lines).
			for i := range entries {
				if entries[i].Turn > n/2 && entries[i].Turn < n {
					entries[i].ReplyLine = Reply(turnsOf(request(entries[i].Turn)))
				}
			}
			purged := purgeDetails(t, entries, n)
			if len(purged) >= len(entries) {
				t.Fatal("nothing purged")
			}
			for _, summary := range []values.HistorySummary{noSummary, mustSummary(t, 2)} {
				if got, want := Window(purged, n, summary), Window(entries, n, summary); !reflect.DeepEqual(got, want) {
					t.Fatalf("window after the purge = %+v\nbefore %+v", got, want)
				}
				next := append(purged[:len(purged):len(purged)], entry(n+1, "новая", nil))
				before := append(entries[:len(entries):len(entries)], entry(n+1, "новая", nil))
				if got, want := Fill(purgeDetails(t, next, n+1), n+1, summary), Fill(before, n+1, summary); got != want {
					t.Fatalf("fill after the purge = %+v, before %+v", got, want)
				}
				f1, t1, ok1 := CompactionPlan(purgeDetails(t, next, n+1), n+1, summary)
				f2, t2, ok2 := CompactionPlan(before, n+1, summary)
				if f1 != f2 || t1 != t2 || ok1 != ok2 {
					t.Fatalf("compaction plan after the purge = %d..%d %v, before %d..%d %v", f1, t1, ok1, f2, t2, ok2)
				}
			}
		})
	}
}
