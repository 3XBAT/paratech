package agent

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"paratech/desktop/internal/cases/agentjournal"
	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/cases/library"
	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/agent/services/actionpolicy"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/library/services/schedulesubjects"
)

var (
	schedTokA = strings.Repeat("a", 64)
	schedTokB = strings.Repeat("b", 64)
)

// fakeScheduleProposals is the proposal of subjects from the schedule.
type fakeScheduleProposals struct {
	mu    sync.Mutex
	p     library.ScheduleProposal
	err   error
	calls int
}

func (f *fakeScheduleProposals) ScheduleSubjects(context.Context) (library.ScheduleProposal, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.p, f.err
}

func (f *fakeScheduleProposals) set(p library.ScheduleProposal) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.p = p
}

// scheduleText is every schedule string of twoGroups: none may reach the planner.
var scheduleText = []string{"Математический анализ", "Матан (лек.)", "матанализ", "Физика", "физика лаб", "физика"}

func twoGroups(tok string) library.ScheduleProposal {
	return library.ScheduleProposal{Token: tok, Groups: []schedulesubjects.Group{
		{Key: "матанализ", Name: "Математический анализ", Aliases: []string{"Матан (лек.)"}, Doubtful: true, Members: []schedulesubjects.Member{
			{Key: "матанализ", Name: "Математический анализ"}, {Key: "матан", Name: "Матан (лек.)", Doubtful: true},
		}},
		{Key: "физика", Name: "Физика", Subject: "s-phys", Aliases: []string{"физика лаб"}},
	}}
}

func subjectsCall(t *testing.T, id string) agentturn.Call {
	t.Helper()
	c, err := values.ParseCallAt(id, string(values.ToolProposeSubjectsFromSchedule), `{}`, values.CatalogWave10)
	if err != nil {
		t.Fatal(err)
	}
	return agentturn.Call{Call: c, Class: actionpolicy.ClassC, PlanStep: -1, Card: values.CardScheduleSubjects}
}

func subjectsWorld(t *testing.T, p library.ScheduleProposal) (*world, tools, *fakeScheduleProposals) {
	t.Helper()
	w := newWorld(t)
	f := &fakeScheduleProposals{p: p}
	tl := w.tools(t)
	tl.subjects = f
	return w, tl, f
}

func TestProposeSubjectsMakesTheCard(t *testing.T) {
	w, tl, _ := subjectsWorld(t, twoGroups(schedTokA))
	r := tl.run(t.Context(), req, subjectsCall(t, "c1"))

	if r.out.Result.IsError || r.out.Result.ResultJSON != `{"groups":2,"proposed":true,"waits_for":"student"}` {
		t.Fatalf("result = %+v", r.out.Result)
	}
	if len(r.out.Seen) != 0 {
		t.Errorf("the planner sees %v; it gets no ID", r.out.Seen)
	}
	if len(r.cards) != 1 {
		t.Fatalf("cards = %v", r.cards)
	}
	card, ok := r.cards[0].(values.ScheduleSubjectsCard)
	if !ok || card.Validate() != nil {
		t.Fatalf("card = %#v", r.cards[0])
	}
	if card.Proposal.Kind() != values.KindProposal || card.Proposal.ID() != schedTokA || len(card.Groups) != 2 {
		t.Fatalf("card = %+v", card)
	}
	for i, key := range []string{"матанализ", "физика"} {
		if card.Groups[i].Kind() != values.KindScheduleGroup || card.Groups[i].ID() != scheduleGroupID(key) {
			t.Errorf("group %d = %v", i, card.Groups[i])
		}
	}

	e, err := w.journal.Find(t.Context(), req.Thread, req.Turn, "c1")
	if err != nil {
		t.Fatal(err)
	}
	if e.Status != action.StatusProposed || e.Tool != values.ToolProposeSubjectsFromSchedule || e.Proposal != card.Proposal {
		t.Fatalf("journal entry = %+v", e)
	}
}

// TestProposeSubjectsTellsThePlannerOnlyACount: the result JSON (what goes towards the Hub) holds no schedule string,
// no token and no group or subject ID, whatever the groups are named.
func TestProposeSubjectsTellsThePlannerOnlyACount(t *testing.T) {
	p := twoGroups(schedTokA)
	_, tl, _ := subjectsWorld(t, p)
	r := tl.run(t.Context(), req, subjectsCall(t, "c1"))

	got := r.out.Result.ResultJSON
	forbidden := append([]string{schedTokA, "s-phys", scheduleGroupPrefix, "sched-"}, scheduleText...)
	for _, g := range p.Groups {
		forbidden = append(forbidden, g.Key, scheduleGroupID(g.Key))
	}
	for _, s := range forbidden {
		if strings.Contains(strings.ToLower(got), strings.ToLower(s)) {
			t.Errorf("the result %s holds %q", got, s)
		}
	}
}

func TestProposeSubjectsNothingToCreate(t *testing.T) {
	w, tl, _ := subjectsWorld(t, library.ScheduleProposal{Token: schedTokA})
	r := tl.run(t.Context(), req, subjectsCall(t, "c1"))

	if r.out.Result.IsError || r.out.Result.ResultJSON != `{"nothing_to_create":true}` || len(r.cards) != 0 {
		t.Fatalf("got %+v, cards %v", r.out.Result, r.cards)
	}
	if _, err := w.journal.Find(t.Context(), req.Thread, req.Turn, "c1"); err == nil {
		t.Error("nothing was proposed, yet the journal has an entry")
	}
}

func TestProposeSubjectsRefusals(t *testing.T) {
	tests := []struct {
		name  string
		p     library.ScheduleProposal
		err   error
		unset bool
		want  values.Refusal
	}{
		{"not wired", twoGroups(schedTokA), nil, true, values.RefuseUnavailable},
		{"the use case fails", library.ScheduleProposal{}, errors.New("disk: Матанализ"), false, values.RefuseFailed},
		{"a token that is no ID", twoGroups("not a token/../x"), nil, false, values.RefuseFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, tl, f := subjectsWorld(t, tt.p)
			f.err = tt.err
			if tt.unset {
				tl.subjects = nil
			}
			r := tl.run(t.Context(), req, subjectsCall(t, "c1"))
			if why, ok := values.RefusalOf(r.out.Result); !ok || why != tt.want || len(r.cards) != 0 {
				t.Fatalf("got %+v, cards %v; want %s", r.out.Result, r.cards, tt.want)
			}
			if _, err := w.journal.Find(t.Context(), req.Thread, req.Turn, "c1"); err == nil {
				t.Error("a refused call has a journal entry")
			}
		})
	}
}

// TestProposeSubjectsReplay: the same call again answers from its journal entry without a second card; once the
// proposal changed it no longer tells a count.
func TestProposeSubjectsReplay(t *testing.T) {
	_, tl, f := subjectsWorld(t, twoGroups(schedTokA))
	first := tl.run(t.Context(), req, subjectsCall(t, "c1"))

	again := tl.run(t.Context(), req, subjectsCall(t, "c1"))
	if again.out.Result != first.out.Result || len(again.cards) != 0 {
		t.Fatalf("replay = %+v, cards %v; want %+v without a card", again.out.Result, again.cards, first.out.Result)
	}

	f.set(twoGroups(schedTokB))
	changed := tl.run(t.Context(), req, subjectsCall(t, "c1"))
	if changed.out.Result.ResultJSON != `{"proposed":true,"waits_for":"student"}` || len(changed.cards) != 0 {
		t.Fatalf("replay after a change = %+v, cards %v", changed.out.Result, changed.cards)
	}

	// A call ID journaled for another tool, or an entry without a proposal, is refused.
	target, err := values.NewRef(values.KindProposal, schedTokA)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range []agentjournal.Entry{
		{Tool: values.ToolProposeDeadline, Status: action.StatusProposed, Proposal: target},
		{Tool: values.ToolProposeSubjectsFromSchedule, Status: action.StatusDone},
	} {
		r := tl.replaySubjects(t.Context(), subjectsCall(t, "c1"), e)
		if why, ok := values.RefusalOf(r.out.Result); !ok || why != values.RefuseInvalidArgs || len(r.cards) != 0 {
			t.Errorf("entry %+v: %+v", e, r.out.Result)
		}
	}
}

// TestAgentCannotCreateSubjects: the port of the agent only proposes, and no file of the package names the use case
// methods that create or take back subjects; only the student's «Да» in input/wails calls them (hard rule 3).
func TestAgentCannotCreateSubjects(t *testing.T) {
	port := reflect.TypeFor[ScheduleSubjects]()
	if port.NumMethod() != 1 || port.Method(0).Name != "ScheduleSubjects" {
		t.Fatalf("the agent's schedule port has %d methods", port.NumMethod())
	}

	forbidden := map[string]bool{"CreateFromSchedule": true, "UndoFromSchedule": true}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && forbidden[id.Name] {
				t.Errorf("%s names %s", name, id.Name)
			}
			return true
		})
	}
}

// TestSubjectsProblemIsAClass: the log gets a class of the error, never its text.
func TestSubjectsProblemIsAClass(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{context.DeadlineExceeded, "timeout"},
		{context.Canceled, "canceled"},
		{errors.New("Матанализ: disk"), "unavailable"},
		{library.ErrUnavailable, "unavailable"},
	}
	for _, tt := range tests {
		if got := subjectsProblem(tt.err); got != tt.want {
			t.Errorf("subjectsProblem(%v) = %q, want %q", tt.err, got, tt.want)
		}
	}
}
