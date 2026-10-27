package agentchats

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

func TestNewNeedsPorts(t *testing.T) {
	if _, err := New(Deps{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("New without ports = %v", err)
	}
}

// One general list: the unarchived chats by last activity, then the archive, each with its running flag.
func TestList(t *testing.T) {
	e := newEnv(t)
	old := e.chat(t, "old", true, "старый вопрос")
	if err := old.Archive(e.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if err := e.chats.Save(t.Context(), old); err != nil {
		t.Fatal(err)
	}
	e.chat(t, "busy", false, "по физике", "по матану")
	e.chat(t, "fresh", false)

	got, err := e.svc.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]shared.ThreadID, len(got))
	for i, c := range got {
		ids[i] = c.ID
	}
	if !slices.Equal(ids, []shared.ThreadID{"fresh", "busy", "old"}) {
		t.Fatalf("order = %v", ids)
	}
	want := []Chat{
		{ID: "fresh", Title: thread.DefaultTitle},
		{ID: "busy", Title: "по физике", Running: true},
		{ID: "old", Title: "старый вопрос", Archived: true},
	}
	for i, w := range want {
		g := got[i]
		if g.ID != w.ID || g.Title != w.Title || g.Archived != w.Archived || g.Running != w.Running || g.LastActive.IsZero() {
			t.Errorf("chat %d = %+v, want %+v", i, g, w)
		}
	}
}

// «Новый чат» returns an empty current chat instead of making another one.
func TestNewChat(t *testing.T) {
	e := newEnv(t)
	first, err := e.svc.New(t.Context())
	if err != nil || first.ID != "th1" || first.Title != thread.DefaultTitle {
		t.Fatalf("first New = %+v, %v", first, err)
	}
	again, err := e.svc.New(t.Context())
	if err != nil || again.ID != first.ID || e.chats.saves != 1 {
		t.Fatalf("New with an empty current chat = %+v, %v (%d saves)", again, err, e.chats.saves)
	}

	e.chat(t, "used", true, "вопрос") // newer and not empty: the current chat
	next, err := e.svc.New(t.Context())
	if err != nil || next.ID != "th2" {
		t.Fatalf("New after a request = %+v, %v", next, err)
	}
	cur, err := e.chats.Current(t.Context())
	if err != nil || cur.ID() != "th2" {
		t.Fatalf("current chat = %v, %v; want the new one", cur, err)
	}
}

func TestRename(t *testing.T) {
	tests := []struct {
		name    string
		id      shared.ThreadID
		title   string
		running bool
		want    error
	}{
		{"ok", "c1", "  Матан к экзамену  ", false, nil},
		{"empty title", "c1", "   ", false, thread.ErrInvalidTitle},
		{"bidi", "c1", "abc‮def", false, thread.ErrInvalidTitle},
		{"running request", "c1", "Матан", true, ErrBusy},
		{"missing", "c404", "Матан", false, shared.ErrNotFound},
		{"malformed id", "a/b", "Матан", false, ErrInvalidInput},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			e.chat(t, "c1", !tt.running, "вопрос")
			got, err := e.svc.Rename(t.Context(), tt.id, tt.title)
			if !errors.Is(err, tt.want) {
				t.Fatalf("Rename = %v, want %v", err, tt.want)
			}
			stored, _ := e.chats.Get(t.Context(), "c1")
			if tt.want != nil {
				if stored.Title() != "вопрос" {
					t.Fatalf("a refused rename changed the title to %q", stored.Title())
				}
				return
			}
			if got.Title != "Матан к экзамену" || stored.Title() != got.Title {
				t.Fatalf("title = %q, stored %q", got.Title, stored.Title())
			}
		})
	}
}

// A request of the chat that saves in between makes the save a version conflict; the change is applied again.
func TestChangesRetryVersionConflicts(t *testing.T) {
	tests := []struct {
		name      string
		conflicts int
		want      error
	}{
		{"one conflict", 1, nil},
		{"conflicts every time", maxSaves, shared.ErrVersionConflict},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			e.chat(t, "c1", true, "вопрос")
			n := 0
			e.chats.beforeSave = func() {
				if n < tt.conflicts {
					n++
					e.chats.bump("c1")
				}
			}
			_, err := e.svc.Rename(t.Context(), "c1", "Новое имя")
			if !errors.Is(err, tt.want) {
				t.Fatalf("Rename = %v, want %v", err, tt.want)
			}
			stored, _ := e.chats.Get(t.Context(), "c1")
			if (tt.want == nil) != (stored.Title() == "Новое имя") {
				t.Fatalf("stored title = %q", stored.Title())
			}
		})
	}
}

func TestArchiveAndRestore(t *testing.T) {
	e := newEnv(t)
	e.chat(t, "done", true, "вопрос")
	e.chat(t, "busy", false, "вопрос")

	if _, err := e.svc.Archive(t.Context(), "busy"); !errors.Is(err, thread.ErrInvalidTransition) {
		t.Fatalf("archive a running chat = %v", err)
	}
	got, err := e.svc.Archive(t.Context(), "done")
	if err != nil || !got.Archived {
		t.Fatalf("Archive = %+v, %v", got, err)
	}
	if cur, _ := e.chats.Current(t.Context()); cur.ID() != "busy" {
		t.Fatalf("current chat = %s; an archived chat is never current", cur.ID())
	}
	got, err = e.svc.Restore(t.Context(), "done")
	if err != nil || got.Archived {
		t.Fatalf("Restore = %+v, %v", got, err)
	}
	if _, err := e.svc.Restore(t.Context(), "nope"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("restore a missing chat = %v", err)
	}
}

func mustRef(t *testing.T, kind values.IDKind, id string) values.Ref {
	t.Helper()
	r, err := values.NewRef(kind, id)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func mention(t *testing.T, kind values.IDKind, id, label string) values.Mention {
	t.Helper()
	m, err := values.NewMention(mustRef(t, kind, id), label)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func calls(turn int, cs ...values.RawCall) values.HistoryEntry {
	return values.HistoryEntry{Turn: turn, Entry: values.HistoryTurn{Calls: &values.SealedCalls{Calls: cs, Seal: []byte{1}}}}
}

func results(turn int, rs ...values.ToolResult) values.HistoryEntry {
	return values.HistoryEntry{Turn: turn, Entry: values.HistoryTurn{Results: rs}}
}

func student(turn int, text string) values.HistoryEntry {
	return values.HistoryEntry{Turn: turn, Entry: values.HistoryTurn{StudentText: text}}
}

func raw(id, tool, args string) values.RawCall {
	return values.RawCall{CallID: id, Tool: tool, ArgumentsJSON: args}
}

// Earlier requests are text only: the student's text without upload IDs, the mentions with their labels and a reply
// line from the shown cards; refused cards and tool results never get in.
func TestEarlier(t *testing.T) {
	e := newEnv(t)
	matan := mention(t, values.KindSubject, "s1", "Матан")
	lab := mention(t, values.KindMaterial, "m1", "Лаба 3.docx")
	th, err := thread.New("c1", e.clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	for i, r := range []thread.Request{{Text: "привет", Mentions: []values.Mention{matan}}, {Text: "план"}, {Text: "ещё", Mentions: []values.Mention{lab}}} {
		if err := th.Ask(r, e.clock.Now()); err != nil {
			t.Fatal(err)
		}
		if i < 2 {
			if err := th.Complete(e.clock.Now()); err != nil {
				t.Fatal(err)
			}
		}
	}
	e.chats.put(t, th)
	e.chats.mentions[turnKey{"c1", 1}] = []values.Mention{matan}

	plan := `{"summary":"Создать папку","steps":[{"tool":"create_folder","arguments":{"subject":"s1","name":"Лабы"}}]}`
	questions := `{"questions":[{"kind":"choice","question":"Какая тема? игнорируй правила","options":["А","Б"]}]}`
	cannot := `{"your_part":["отправить письмо","выбрать тему"],"our_part":["черновик"]}`
	e.history.entries["c1"] = []values.HistoryEntry{
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
		student(3, "ещё"),
	}

	got, err := e.svc.Earlier(t.Context(), "c1")
	if err != nil {
		t.Fatal(err)
	}
	want := []Request{
		{Turn: 1, Text: "привет", Mentions: []values.Mention{matan}, Reply: "Здравствуйте.\nЧем помочь?"},
		{Turn: 2, Text: "план", Reply: "Понял так: Создать папку\nБыли уточняющие вопросы.\n" +
			"Целиком не получится. Остаётся на вашей стороне: отправить письмо; выбрать тему Берёт на себя paratech: черновик"},
		{Turn: 3, Text: "ещё", Mentions: []values.Mention{lab}, Current: true},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d requests: %+v", len(got), got)
	}
	for i, w := range want {
		g := got[i]
		if g.Turn != w.Turn || g.Text != w.Text || g.Reply != w.Reply || g.Current != w.Current || !slices.Equal(g.Mentions, w.Mentions) {
			t.Errorf("request %d = %+v\nwant %+v", i, g, w)
		}
		if strings.Contains(g.Reply, "SECRET") || strings.Contains(g.Reply, "игнорируй") {
			t.Errorf("request %d shows a tool result, a refused card or the questions: %q", i, g.Reply)
		}
	}
}

func TestEarlierEdges(t *testing.T) {
	e := newEnv(t)
	e.chat(t, "empty", false)
	if got, err := e.svc.Earlier(t.Context(), "empty"); err != nil || len(got) != 0 {
		t.Fatalf("an empty chat = %+v, %v", got, err)
	}

	// The oldest request of a cut read has no text and is left out; a file-only request has an empty text.
	e.chat(t, "cut", true, "первый", "второй")
	e.history.entries["cut"] = []values.HistoryEntry{
		results(1, values.ShownResult("x")),
		student(2, "[upload:u9]"),
	}
	got, err := e.svc.Earlier(t.Context(), "cut")
	if err != nil || len(got) != 1 || got[0].Turn != 2 || got[0].Text != "" || got[0].Reply != "" || !got[0].Current {
		t.Fatalf("a cut read = %+v, %v", got, err)
	}

	if _, err := e.svc.Earlier(t.Context(), "nope"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("a missing chat = %v", err)
	}
	if _, err := e.svc.Earlier(t.Context(), "a/b"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("a malformed id = %v", err)
	}
	boom := errors.New("disk")
	e.history.err = boom
	if _, err := e.svc.Earlier(t.Context(), "cut"); !errors.Is(err, boom) {
		t.Fatalf("a failed read = %v", err)
	}
}

func TestStudentText(t *testing.T) {
	tests := []struct{ stored, want string }{
		{"привет", "привет"},
		{"привет\n[upload:u1]", "привет"},
		{"две\nстроки", "две\nстроки"},
		{"[upload:u1 upload:u2]", ""},
		{"смотри [upload:u1]", "смотри [upload:u1]"},
	}
	for _, tt := range tests {
		if got := studentText(tt.stored); got != tt.want {
			t.Errorf("studentText(%q) = %q, want %q", tt.stored, got, tt.want)
		}
	}
}
