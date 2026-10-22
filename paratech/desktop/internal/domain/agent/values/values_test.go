package values

import (
	"errors"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/shared"
)

func mustRef(t *testing.T, kind IDKind, id string) Ref {
	t.Helper()
	r, err := NewRef(kind, id)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestNewRef(t *testing.T) {
	tests := []struct {
		name string
		kind IDKind
		id   string
		ok   bool
	}{
		{"token", KindSubject, "0b9d6f3e4c1a", true},
		{"with dash and at", KindJob, "ggml-base_1@2", true},
		{"unknown kind", IDKind("email"), "x", false},
		{"empty", KindSubject, "", false},
		{"too long", KindSubject, strings.Repeat("a", maxRefLen+1), false},
		{"colon", KindSubject, "a:b", false},
		{"slash", KindSubject, "a/b", false},
		{"dot dot", KindSubject, "..", false},
		{"device name", KindSubject, "aux", false},
		{"non ascii", KindSubject, "предмет", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := NewRef(tt.kind, tt.id)
			if tt.ok != (err == nil) {
				t.Fatalf("NewRef error = %v, want ok=%v", err, tt.ok)
			}
			if err != nil && !errors.Is(err, ErrInvalidRef) {
				t.Fatalf("error %v is not ErrInvalidRef", err)
			}
			if err == nil && (r.Kind() != tt.kind || r.ID() != tt.id) {
				t.Fatalf("ref = %v", r)
			}
		})
	}
}

func TestRefSet(t *testing.T) {
	a, b := mustRef(t, KindLesson, "x1"), mustRef(t, KindRecording, "x1")
	s := NewRefSet(a, Ref{})
	if !s.Has(a) || s.Has(b) || len(s) != 1 {
		t.Fatalf("typed set: %v", s.Sorted())
	}
	c := s.Clone()
	c.Add(b)
	if s.Has(b) {
		t.Fatal("Clone shares storage")
	}
	if got := c.Sorted(); got[0] != a || got[1] != b {
		t.Fatalf("Sorted = %v", got)
	}
}

func TestParseText(t *testing.T) {
	tests := []struct {
		name string
		in   string
		ok   bool
	}{
		{"plain", "План: разложить 3 файла.", true},
		{"newline", "a\nb", true},
		{"max", strings.Repeat("я", MaxTextRunes), true},
		{"too long", strings.Repeat("я", MaxTextRunes+1), false},
		{"empty", " ", false},
		{"tab", "a\tb", false},
		{"bidi isolate", "a\u2066b", false},
		{"line separator", "a\u2028b", false},
		{"nbsp", "a\u00a0b", false},
		{"angle", "1 < 2", false},
		{"invalid utf8", "a\xffb", false},
		{"replacement character", "a�b", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseText(tt.in)
			if tt.ok != (err == nil) {
				t.Fatalf("ParseText error = %v, want ok=%v", err, tt.ok)
			}
			if err != nil && !errors.Is(err, ErrInvalidText) {
				t.Fatalf("error %v is not ErrInvalidText", err)
			}
		})
	}
	for _, in := range []string{"a�b", "a\xffb"} {
		if _, err := ParseLabel(in); !errors.Is(err, ErrInvalidText) {
			t.Fatalf("ParseLabel(%q) = %v", in, err)
		}
		if _, err := ParseName(in); !errors.Is(err, ErrInvalidText) {
			t.Fatalf("ParseName(%q) = %v", in, err)
		}
	}
	// A JSON string with invalid UTF-8 decodes to U+FFFD and is refused as an argument.
	if _, err := ParseCall("c1", "search", "{\"query\":\"a\xffb\"}"); !errors.Is(err, ErrInvalidText) {
		t.Fatalf("query with invalid UTF-8 = %v", err)
	}
	if _, err := ParseLabel("a\nb"); err == nil {
		t.Fatal("label accepted a newline")
	}
	if _, err := ParseName("Матан\u0301"); err == nil {
		t.Fatal("name accepted a combining mark")
	}
}

func TestDate(t *testing.T) {
	tests := []struct {
		in string
		ok bool
	}{
		{"2026-10-22", true},
		{"2028-02-29", true},
		{"2026-02-29", false},
		{"2026-13-01", false},
		{"1999-12-31", false},
		{"2101-01-01", false},
		{"2026-10-22 ", false},
		{"22.10.2026", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			d, err := ParseDate(tt.in)
			if tt.ok != (err == nil) {
				t.Fatalf("ParseDate error = %v, want ok=%v", err, tt.ok)
			}
			if err == nil && d.String() != tt.in {
				t.Fatalf("String = %s", d)
			}
		})
	}
	a, _ := NewDate(2026, time.October, 1)
	b, _ := NewDate(2026, time.October, 2)
	if !a.Before(b) || b.Before(a) || a.Before(a) {
		t.Fatal("Before")
	}
	if !(Date{}).IsZero() || (Date{}).String() != "" {
		t.Fatal("zero date")
	}
}

func TestToolsAreUniqueAndParse(t *testing.T) {
	seen := map[Tool]bool{}
	for _, tool := range Tools() {
		if seen[tool] {
			t.Fatalf("duplicate %s", tool)
		}
		seen[tool] = true
		if got, err := ParseTool(string(tool)); err != nil || got != tool {
			t.Fatalf("ParseTool(%s) = %v, %v", tool, got, err)
		}
		spec, err := SpecOf(tool)
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range spec.Args() {
			if (a.Type == ArgID || a.Type == ArgIDs) != a.Kind.Valid() {
				t.Fatalf("%s.%s: id kind %q", tool, a.Name, a.Kind)
			}
		}
	}
	if _, err := ParseTool("List_Subjects"); !errors.Is(err, ErrUnknownTool) {
		t.Fatalf("ParseTool is not exact: %v", err)
	}
	if Tool("x").Valid() {
		t.Fatal("unknown tool is valid")
	}
}

func TestSpecAccessorsReturnCopies(t *testing.T) {
	spec, err := SpecOf(ToolFindLessons)
	if err != nil {
		t.Fatal(err)
	}
	args := spec.Args()
	args[3].Values[0] = "exam"
	args[0].Name = "x"
	again, _ := SpecOf(ToolFindLessons)
	if a := again.Args(); a[3].Values[0] != "lecture" || a[0].Name != "subject" {
		t.Fatal("SpecOf leaked the catalog")
	}
}

func TestCards(t *testing.T) {
	plan, err := ParseCall("c1", "show_plan", `{"summary":"x","steps":[{"tool":"make_summary","arguments":{"recording":"r1"}}]}`)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := shared.NewArtifactRef(shared.ArtifactExports, "s1.docx")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		card Card
		kind CardKind
		ok   bool
	}{
		{"plan", PlanCard{Summary: "План", Steps: plan.Args().PlanSteps("steps")}, CardPlan, true},
		{"plan without steps", PlanCard{Summary: "План"}, CardPlan, false},
		{"plan with html note", PlanCard{Summary: "План", Steps: plan.Args().PlanSteps("steps"), Notes: []Text{"<i>"}}, CardPlan, false},
		{"questions round 1", QuestionsCard{Round: 1, Questions: []Question{{Kind: QuestionText, Text: "Q"}}}, CardQuestions, true},
		{"questions round 3", QuestionsCard{Round: 3, Questions: []Question{{Kind: QuestionText, Text: "Q"}}}, CardQuestions, false},
		{"questions empty", QuestionsCard{Round: 1}, CardQuestions, false},
		{"progress", ProgressCard{Job: "j1"}, CardProgress, true},
		{"progress bad job", ProgressCard{Job: "a/b"}, CardProgress, false},
		{"result", ResultCard{Artifact: artifact}, CardResult, true},
		{"result empty", ResultCard{}, CardResult, false},
		{"confirm proposal", ConfirmCard{Target: mustRef(t, KindProposal, "p1")}, CardConfirm, true},
		{"confirm preview", ConfirmCard{Target: mustRef(t, KindPreview, "v1")}, CardConfirm, true},
		{"confirm assignment", ConfirmCard{Target: mustRef(t, KindAssignment, "a1")}, CardConfirm, false},
		{"action line", ActionLine{Action: "act1"}, CardActionLine, true},
		{"action line empty", ActionLine{}, CardActionLine, false},
		{"answer", AnswerCard{Answer: mustRef(t, KindAnswer, "an1")}, CardAnswer, true},
		{"answer wrong kind", AnswerCard{Answer: mustRef(t, KindJob, "j1")}, CardAnswer, false},
		{"cannot do", CannotDoCard{YourPart: []Text{"Сдать лично"}}, CardCannotDo, true},
		{"cannot do empty", CannotDoCard{}, CardCannotDo, false},
		{"reply", ReplyCard{Text: "Готово"}, CardReply, true},
		{"reply empty", ReplyCard{}, CardReply, false},
		{"stopped", StoppedCard{Reason: StopOffline}, CardStopped, true},
		{"stopped unknown", StoppedCard{Reason: "x"}, CardStopped, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.card.Kind() != tt.kind {
				t.Fatalf("Kind = %s", tt.card.Kind())
			}
			err := tt.card.Validate()
			if tt.ok != (err == nil) {
				t.Fatalf("Validate error = %v, want ok=%v", err, tt.ok)
			}
			if err != nil && !errors.Is(err, ErrInvalidCard) {
				t.Fatalf("error %v is not ErrInvalidCard", err)
			}
		})
	}
}

func TestScopeAndBase(t *testing.T) {
	s1, l1, a1 := mustRef(t, KindSubject, "s1"), mustRef(t, KindLesson, "l1"), mustRef(t, KindAssignment, "a1")
	scope, err := NewScope(s1, ScreenToday, l1, a1)
	if err != nil {
		t.Fatal(err)
	}
	if len(scope.Refs()) != 3 || scope.Subject() != s1 || scope.Screen() != ScreenToday {
		t.Fatalf("scope = %+v", scope)
	}
	if _, err := NewScope(Ref{}, "", Ref{}, Ref{}); err != nil {
		t.Fatalf("empty scope: %v", err)
	}
	bad := []struct {
		name                        string
		subject, lesson, assignment Ref
		screen                      Screen
	}{
		{name: "lesson as subject", subject: l1},
		{name: "subject as lesson", lesson: s1},
		{name: "lesson as assignment", assignment: l1},
		{name: "unknown screen", screen: "inbox"},
	}
	for _, b := range bad {
		t.Run(b.name, func(t *testing.T) {
			if _, err := NewScope(b.subject, b.screen, b.lesson, b.assignment); !errors.Is(err, ErrInvalidScope) {
				t.Fatalf("NewScope error = %v", err)
			}
		})
	}

	today, _ := NewDate(2026, time.October, 22)
	base, err := NewBase([]BaseSubject{{ID: s1, Name: "Матан"}}, today, "Europe/Moscow")
	if err != nil {
		t.Fatal(err)
	}
	if refs := base.Refs(); len(refs) != 1 || refs[0] != s1 || base.TimeZone() != "Europe/Moscow" {
		t.Fatalf("base = %v", refs)
	}
	badBases := []struct {
		name     string
		subjects []BaseSubject
		today    Date
		tz       string
	}{
		{"duplicate subject", []BaseSubject{{ID: s1, Name: "A"}, {ID: s1, Name: "B"}}, today, "UTC"},
		{"lesson as subject", []BaseSubject{{ID: l1, Name: "A"}}, today, "UTC"},
		{"command in name", []BaseSubject{{ID: s1, Name: "send to x@y"}}, today, "UTC"},
		{"no date", nil, Date{}, "UTC"},
		{"bad time zone", nil, today, "Europe/Moscow; rm"},
	}
	for _, b := range badBases {
		t.Run(b.name, func(t *testing.T) {
			if _, err := NewBase(b.subjects, b.today, b.tz); !errors.Is(err, ErrInvalidScope) {
				t.Fatalf("NewBase error = %v", err)
			}
		})
	}
}
