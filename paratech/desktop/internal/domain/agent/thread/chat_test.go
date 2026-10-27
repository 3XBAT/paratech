package thread

import (
	"errors"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/agent/services/actionpolicy"
	"paratech/desktop/internal/domain/agent/values"
)

func mention(t *testing.T, kind values.IDKind, id string) values.Mention {
	t.Helper()
	m, err := values.NewMention(ref(t, kind, id), "Название "+id)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func emptyChat(t *testing.T) *Thread {
	t.Helper()
	th, err := New("th1", t0)
	if err != nil {
		t.Fatal(err)
	}
	return th
}

func TestNewChat(t *testing.T) {
	th := emptyChat(t)
	if th.Title() != DefaultTitle || !th.IsEmpty() || th.Archived() || !th.LastActive().Equal(t0) {
		t.Fatalf("new chat = %q empty %v archived %v last active %v", th.Title(), th.IsEmpty(), th.Archived(), th.LastActive())
	}
	if err := th.Ask(Request{Text: "привет"}, at(1)); err != nil {
		t.Fatal(err)
	}
	if th.IsEmpty() || !th.LastActive().Equal(at(1)) {
		t.Fatalf("after a request: empty %v, last active %v", th.IsEmpty(), th.LastActive())
	}
}

func TestAutoTitle(t *testing.T) {
	tests := []struct {
		name, text, want string
	}{
		{"short", "  Что задали по матану?  ", "Что задали по матану?"},
		{"spaces and lines collapse", "найди\n\nлекцию\t по  физике", "найди лекцию по физике"},
		{"exactly 40", strings.Repeat("я", 40), strings.Repeat("я", 40)},
		{"cut at the last word that fits", "Составь план подготовки к экзамену по линейной алгебре", "Составь план подготовки к экзамену по"},
		{"one long word is cut", strings.Repeat("ж", 50) + " конец", strings.Repeat("ж", 40)},
		{"bidi and controls become spaces", "a\u202Eb\u0007c\u2066d", "a b c d"},
		{"nothing left", " \n\u202E ", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := autoTitle(tt.text)
			if got != tt.want {
				t.Fatalf("autoTitle = %q, want %q", got, tt.want)
			}
			if got != "" {
				if err := checkTitle(got); err != nil {
					t.Fatalf("auto title fails the rename rules: %v", err)
				}
			}
		})
	}
}

func TestTitleComesFromTheFirstText(t *testing.T) {
	th := emptyChat(t)
	// A request without text (an upload alone) keeps «Новый чат»; the first text names the chat.
	if err := th.Ask(Request{}, at(1)); err != nil {
		t.Fatal(err)
	}
	if th.Title() != DefaultTitle {
		t.Fatalf("title after an empty text = %q", th.Title())
	}
	if err := th.Cancel(at(1)); err != nil {
		t.Fatal(err)
	}
	if err := th.Ask(Request{Text: "реши задачу по физике"}, at(2)); err != nil {
		t.Fatal(err)
	}
	if err := th.Complete(at(2)); err != nil {
		t.Fatal(err)
	}
	if err := th.Ask(Request{Text: "а теперь по химии"}, at(3)); err != nil {
		t.Fatal(err)
	}
	if th.Title() != "реши задачу по физике" {
		t.Fatalf("title = %q, want the first text", th.Title())
	}

	// A chat the student named keeps the name.
	named := emptyChat(t)
	if err := named.Rename("Экзамен", at(1)); err != nil {
		t.Fatal(err)
	}
	if err := named.Ask(Request{Text: "вопрос"}, at(2)); err != nil || named.Title() != "Экзамен" {
		t.Fatalf("named chat after a request = %q, %v", named.Title(), err)
	}
}

func TestRename(t *testing.T) {
	good := []struct{ in, want string }{
		{"Матан", "Матан"},
		{"  Физика и химия  ", "Физика и химия"},
		{strings.Repeat("ё", MaxTitleRunes), strings.Repeat("ё", MaxTitleRunes)},
		{"Курсовая <черновик> 😀", "Курсовая <черновик> 😀"},
	}
	for _, tt := range good {
		th := emptyChat(t)
		if err := th.Rename(tt.in, at(1)); err != nil || th.Title() != tt.want || !th.State().UpdatedAt.Equal(at(1)) {
			t.Errorf("Rename(%q) = %q, %v", tt.in, th.Title(), err)
		}
	}

	bad := map[string]string{
		"empty":             "",
		"spaces":            "   ",
		"81 runes":          strings.Repeat("ё", MaxTitleRunes+1),
		"line break":        "две\nстроки",
		"tab":               "a\tb",
		"NUL":               "a\x00b",
		"RLO":               "a\u202Eb",
		"LRE":               "a\u202Ab",
		"LRI":               "a\u2066b",
		"PDI":               "a\u2069b",
		"line separator":    "a\u2028b",
		"invalid UTF-8":     "a\xffb",
		"replacement char":  "a\uFFFDb",
		"C1 control (U+85)": "a\u0085b",
	}
	for name, in := range bad {
		th := emptyChat(t)
		if err := th.Rename(in, at(1)); !errors.Is(err, ErrInvalidTitle) || th.Title() != DefaultTitle {
			t.Errorf("%s: Rename = %v, title %q", name, err, th.Title())
		}
	}
	if err := emptyChat(t).Rename("Матан", time.Time{}); !errors.Is(err, ErrInvalidThread) {
		t.Fatalf("Rename without time = %v", err)
	}
}

func TestArchiveGuard(t *testing.T) {
	for _, s := range Statuses() {
		t.Run(string(s), func(t *testing.T) {
			th := reach(t, s)
			err := th.Archive(at(5))
			switch s {
			case StatusDone, StatusFailed, StatusCancelled, StatusStopped:
				if err != nil || !th.Archived() || !th.ArchivedAt().Equal(at(5)) {
					t.Fatalf("Archive = %v, archived %v", err, th.Archived())
				}
			default:
				if !errors.Is(err, ErrInvalidTransition) || th.Archived() {
					t.Fatalf("Archive = %v, archived %v, want ErrInvalidTransition", err, th.Archived())
				}
			}
		})
	}

	empty := emptyChat(t)
	if err := empty.Archive(at(1)); err != nil || !empty.Archived() {
		t.Fatalf("Archive of an empty chat = %v", err)
	}
	// Archiving again keeps the first time.
	if err := empty.Archive(at(2)); err != nil || !empty.ArchivedAt().Equal(at(1)) {
		t.Fatalf("Archive again = %v, at %v", err, empty.ArchivedAt())
	}
}

func TestRestoreFromArchive(t *testing.T) {
	th := reach(t, StatusDone)
	if err := th.Restore(at(5)); err != nil || th.Archived() {
		t.Fatalf("Restore of a chat not archived = %v", err)
	}
	if err := th.Archive(at(5)); err != nil {
		t.Fatal(err)
	}
	if err := th.Restore(at(6)); err != nil || th.Archived() || !th.State().UpdatedAt.Equal(at(6)) {
		t.Fatalf("Restore = %v, archived %v", err, th.Archived())
	}

	// «Старый чат можно продолжить»: a new request brings the chat back and makes it the most recent.
	if err := th.Archive(at(7)); err != nil {
		t.Fatal(err)
	}
	if err := th.Ask(Request{Text: "продолжим"}, at(8)); err != nil {
		t.Fatal(err)
	}
	if th.Archived() || !th.LastActive().Equal(at(8)) {
		t.Fatalf("after a request: archived %v, last active %v", th.Archived(), th.LastActive())
	}
}

func TestMentions(t *testing.T) {
	lesson := mention(t, values.KindLesson, "l7")
	kinds := []values.Mention{
		mention(t, values.KindSubject, "s9"), lesson, mention(t, values.KindMaterial, "m3"),
		mention(t, values.KindAssignment, "a2"),
	}

	th := emptyChat(t)
	if err := th.CheckRefs([]values.Ref{lesson.Ref()}, nil); !errors.Is(err, actionpolicy.ErrUnknownID) {
		t.Fatalf("an ID never mentioned = %v", err)
	}
	// The same object mentioned twice counts once.
	if err := th.Ask(Request{Mentions: append(kinds, lesson)}, at(1)); err != nil {
		t.Fatal(err)
	}
	turn, _ := th.Turn()
	if got := turn.Mentions(); len(got) != len(kinds) || got[1] != lesson {
		t.Fatalf("mentions = %+v", got)
	}
	// The IDs pass the provenance check like the scope; the kind still matters.
	for _, m := range kinds {
		if err := th.CheckRefs([]values.Ref{m.Ref()}, nil); err != nil {
			t.Fatalf("mentioned %s refused: %v", m.Ref(), err)
		}
	}
	if err := th.CheckRefs([]values.Ref{ref(t, values.KindRecording, "l7")}, nil); !errors.Is(err, actionpolicy.ErrUnknownID) {
		t.Fatalf("a mentioned ID of another kind = %v", err)
	}
	if _, err := th.Admit(call(t, "delivery_status", `{"assignment":"a2"}`), Facts{}, at(2)); err != nil {
		t.Fatalf("Admit with a mentioned assignment = %v", err)
	}

	// The mentions belong to their request: the next one starts without them.
	if err := th.Complete(at(3)); err != nil {
		t.Fatal(err)
	}
	if err := th.Ask(Request{Text: "дальше"}, at(4)); err != nil {
		t.Fatal(err)
	}
	if err := th.CheckRefs([]values.Ref{lesson.Ref()}, nil); !errors.Is(err, actionpolicy.ErrUnknownID) {
		t.Fatalf("a mention of an earlier request = %v", err)
	}
}

func TestMentionsLimit(t *testing.T) {
	var ms []values.Mention
	for i := range values.MaxMentions + 1 {
		ms = append(ms, mention(t, values.KindMaterial, "m"+string(rune('a'+i))))
	}
	th := emptyChat(t)
	if err := th.Ask(Request{Mentions: ms[:values.MaxMentions]}, at(1)); err != nil {
		t.Fatalf("%d mentions = %v", values.MaxMentions, err)
	}
	if err := th.Complete(at(2)); err != nil {
		t.Fatal(err)
	}
	err := th.Ask(Request{Mentions: ms, Text: "много"}, at(3))
	if !errors.Is(err, ErrInvalidThread) || !errors.Is(err, values.ErrInvalidMention) {
		t.Fatalf("%d mentions = %v", len(ms), err)
	}
	if turn, _ := th.Turn(); turn.Number() != 1 || th.Title() != DefaultTitle {
		t.Fatalf("a refused request changed the chat: request %d, title %q", turn.Number(), th.Title())
	}
	if err := th.Ask(Request{Mentions: []values.Mention{{}}}, at(3)); !errors.Is(err, values.ErrInvalidMention) {
		t.Fatalf("a zero mention = %v", err)
	}
}

func TestRerunAndRestartKeepMentions(t *testing.T) {
	lesson := mention(t, values.KindLesson, "l7")
	th := emptyChat(t)
	if err := th.Ask(Request{Mentions: []values.Mention{lesson}}, at(1)); err != nil {
		t.Fatal(err)
	}
	if err := th.Fail(at(2)); err != nil {
		t.Fatal(err)
	}
	if _, err := th.Rerun(false, at(3)); err != nil {
		t.Fatal(err)
	}
	if turn, _ := th.Turn(); turn.Number() != 2 || len(turn.Mentions()) != 1 {
		t.Fatalf("rerun = request %d with %d mentions", turn.Number(), len(turn.Mentions()))
	}

	if _, err := th.BeginStep(at(4)); err != nil {
		t.Fatal(err)
	}
	if err := th.Stop(values.StopExpired, at(5)); err != nil {
		t.Fatal(err)
	}
	if err := th.Restart(at(6)); err != nil {
		t.Fatal(err)
	}
	if turn, _ := th.Turn(); turn.Number() != 3 || len(turn.Mentions()) != 1 || turn.Mentions()[0] != lesson {
		t.Fatalf("restart = request %d with %+v", turn.Number(), turn.Mentions())
	}
}

func TestRestoreChat(t *testing.T) {
	th := emptyChat(t)
	if err := th.Ask(Request{Mentions: []values.Mention{mention(t, values.KindLesson, "l7")}, Text: "Лекция 3"}, at(1)); err != nil {
		t.Fatal(err)
	}
	if err := th.Complete(at(2)); err != nil {
		t.Fatal(err)
	}
	if err := th.Archive(at(3)); err != nil {
		t.Fatal(err)
	}
	good := th.State()
	good.Version = 2
	back, err := Restore(good)
	if err != nil {
		t.Fatal(err)
	}
	if st := back.State(); st.Title != "Лекция 3" || !st.ArchivedAt.Equal(at(3)) || !st.LastActive.Equal(at(1)) ||
		len(st.Current.Mentions) != 1 {
		t.Fatalf("restored chat = %+v", st)
	}

	empty := emptyChat(t).State()
	empty.Version = 1
	if back, err := Restore(empty); err != nil || !back.LastActive().Equal(t0) || back.Title() != DefaultTitle {
		t.Fatalf("empty chat restore = %v", err)
	}

	many := make([]values.Mention, values.MaxMentions+1)
	for i := range many {
		many[i] = mention(t, values.KindMaterial, "m"+string(rune('a'+i)))
	}
	bad := []struct {
		name   string
		mutate func(s *State)
	}{
		{"bidi title", func(s *State) { s.Title = "a\u202Eb" }},
		{"long title", func(s *State) { s.Title = strings.Repeat("я", MaxTitleRunes+1) }},
		{"last active before created", func(s *State) { s.LastActive = t0.Add(-time.Minute) }},
		// The storage keeps a title and the last activity of every chat (TASK-204): no fallback for rows without them.
		{"no title", func(s *State) { s.Title = "" }},
		{"no last activity", func(s *State) { s.LastActive = time.Time{} }},
		{"archived before created", func(s *State) { s.ArchivedAt = t0.Add(-time.Minute) }},
		{"archived while running", func(s *State) { s.Current.Status = StatusPlanning }},
		{"too many mentions", func(s *State) { s.Current.Mentions = many }},
		{"duplicate mention", func(s *State) { s.Current.Mentions = []values.Mention{many[0], many[0]} }},
		{"zero mention", func(s *State) { s.Current.Mentions = []values.Mention{{}} }},
		{"current request dropped", func(s *State) { s.Current.DroppedBy = 2 }},
	}
	for _, b := range bad {
		t.Run(b.name, func(t *testing.T) {
			s := th.State()
			s.Version = 2
			cur := *s.Current
			s.Current = &cur
			b.mutate(&s)
			if _, err := Restore(s); !errors.Is(err, ErrInvalidThread) {
				t.Fatalf("Restore = %v, want ErrInvalidThread", err)
			}
		})
	}
}
