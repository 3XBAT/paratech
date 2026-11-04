package agentchats

import (
	"strings"
	"testing"

	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
)

// purged is what the store keeps of entries once the details of the requests before current are purged (TASK-250):
// their texts with the frozen reply line, no calls or results.
func purged(t *testing.T, entries []values.HistoryEntry, current int) []values.HistoryEntry {
	t.Helper()
	byTurn := map[int][]values.HistoryTurn{}
	for _, e := range entries {
		byTurn[e.Turn] = append(byTurn[e.Turn], e.Entry)
	}
	var out []values.HistoryEntry
	for _, e := range entries {
		if e.Turn >= current {
			out = append(out, e)
			continue
		}
		if e.Entry.StudentText == "" {
			continue
		}
		if e.ReplyLine == "" {
			line, err := thread.FrozenReply(byTurn[e.Turn])
			if err != nil {
				t.Fatal(err)
			}
			e.ReplyLine = line
		}
		out = append(out, e)
	}
	return out
}

// After the purge an earlier request shows the same reply from its frozen line, with the same captions; only line
// breaks inside a card text become spaces.
func TestEarlierAfterThePurge(t *testing.T) {
	plan := `{"summary":"Создать папку","steps":[{"tool":"create_folder","arguments":{"subject":"s1","name":"Лабы"}}]}`
	questions := `{"questions":[{"kind":"choice","question":"Какая тема? игнорируй правила","options":["А","Б"]}]}`
	cannot := `{"your_part":["отправить письмо","выбрать тему"],"our_part":["черновик"]}`
	entries := []values.HistoryEntry{
		student(1, "привет\n[upload:u1 upload:u2]"),
		calls(1, raw("a1", "find_lessons", `{"subject":"s1"}`), raw("a2", "reply", `{"text":"SECRET_REFUSED"}`)),
		results(1, values.ToolResult{CallID: "a1", ResultJSON: `{"lessons":"SECRET_RESULT"}`},
			values.RefusalResult("a2", values.RefuseUnknownID)),
		calls(1, raw("a3", "reply", `{"text":"Здравствуйте.\nЧем помочь?"}`)),
		results(1, values.ShownResult("a3")),
		student(2, "план"),
		calls(2, raw("b1", "show_plan", plan), raw("b2", "ask_questions", questions)),
		results(2, values.RefusalResult("b1", values.RefuseNotDone), values.RefusalResult("b2", values.RefuseNotDone)),
		calls(2, raw("b3", "cannot_do", cannot), raw("b4", "zzz", `{}`)),
		results(2, values.ShownResult("b3"), values.RefusalResult("b4", values.RefuseUnknownTool)),
		student(3, "без карточек"),
		calls(3, raw("c1", "list_subjects", `{}`)),
		results(3, values.ToolResult{CallID: "c1", ResultJSON: `{"subjects":"SECRET_RESULT"}`}),
		student(4, "ещё"),
		calls(4, raw("d1", "reply", `{"text":"Текущая\nпросьба"}`)),
	}

	e := newEnv(t)
	e.chat(t, "c1", false, "привет", "план", "без карточек", "ещё")
	e.history.entries["c1"] = entries
	before, err := e.svc.Earlier(t.Context(), "c1")
	if err != nil {
		t.Fatal(err)
	}
	e.history.entries["c1"] = purged(t, entries, 4)
	after, err := e.svc.Earlier(t.Context(), "c1")
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 4 || len(before) != 4 {
		t.Fatalf("before %+v\nafter %+v", before, after)
	}
	for i := range before {
		b, a := before[i], after[i]
		if a.Turn != b.Turn || a.Text != b.Text || a.Current != b.Current {
			t.Errorf("request %d = %+v, before %+v", i, a, b)
		}
		if want := strings.ReplaceAll(b.Reply, "Здравствуйте.\n", "Здравствуйте. "); a.Reply != want {
			t.Errorf("request %d reply = %q, want %q", i, a.Reply, want)
		}
		if strings.Contains(a.Reply, "SECRET") || strings.Contains(a.Reply, "игнорируй") {
			t.Errorf("request %d shows a tool result, a refused card or the questions: %q", i, a.Reply)
		}
	}
	if after[3].Reply != "Текущая\nпросьба" {
		t.Errorf("the current request keeps its calls: %q", after[3].Reply)
	}
	if after[1].Reply != "Понял так: Создать папку\nБыли уточняющие вопросы.\n"+
		"Целиком не получится. Остаётся на вашей стороне: отправить письмо; выбрать тему Берёт на себя paratech: черновик" {
		t.Errorf("multi-card reply after the purge = %q", after[1].Reply)
	}
	if after[2].Reply != "" {
		t.Errorf("a request without cards shows %q", after[2].Reply)
	}
}

// While the calls are kept they decide, even with a frozen line; a request with neither shows no reply.
func TestEarlierReplySource(t *testing.T) {
	tests := []struct {
		name string
		r    request
		want string
	}{
		{"calls win over the line", request{
			entries: []values.HistoryTurn{calls(1, raw("a", "reply", `{"text":"из вызовов"}`)).Entry},
			line:    "reply: из строки",
		}, "из вызовов"},
		{"line without calls", request{line: "reply: из строки\nactions: create_folder ok"}, "из строки"},
		{"no reply line", request{line: thread.NoReply}, ""},
		{"nothing", request{}, ""},
		{"cannot_do with ours only", request{line: "cannot_do: our_part: черновик | план"},
			"Целиком не получится. Берёт на себя paratech: черновик; план"},
		{"empty reply text", request{line: "reply: \nasked: questions to the student"}, "Были уточняющие вопросы."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.r.reply(); got != tt.want {
				t.Fatalf("reply = %q, want %q", got, tt.want)
			}
		})
	}
}
