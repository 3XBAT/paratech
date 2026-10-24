package ticketset

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/answer"
	"paratech/desktop/internal/domain/shared"
)

var t0 = time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)

func items(n int) []Item {
	out := make([]Item, n)
	for i := range out {
		out[i] = Item{Number: i + 1, Title: fmt.Sprintf("Вопрос %d", i+1)}
	}
	return out
}

func newSet(t *testing.T, n int) *Set {
	t.Helper()
	s, err := New("ts1", "sub1", "", items(n), t0)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// answering returns a set of n tickets started.
func answering(t *testing.T, n int) *Set {
	t.Helper()
	s := newSet(t, n)
	if err := s.Confirm(s.Budget().OverCap, t0); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(t0); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestTransitionTable(t *testing.T) {
	allowed := map[[2]Status]bool{
		{StatusProposed, StatusConfirmed}: true, {StatusProposed, StatusCancelled}: true,
		{StatusConfirmed, StatusAnswering}: true, {StatusConfirmed, StatusFailed}: true, {StatusConfirmed, StatusCancelled}: true,
		{StatusAnswering, StatusDone}: true, {StatusAnswering, StatusFailed}: true, {StatusAnswering, StatusCancelled}: true,
	}
	for _, from := range Statuses() {
		for _, to := range Statuses() {
			if got := CanTransition(from, to); got != allowed[[2]Status{from, to}] {
				t.Errorf("%s -> %s = %v", from, to, got)
			}
		}
	}
	for _, s := range []Status{StatusDone, StatusFailed, StatusCancelled} {
		if !s.IsTerminal() {
			t.Errorf("%s is not terminal", s)
		}
	}
	if Status("x").Valid() || StatusProposed.IsTerminal() {
		t.Fatal("status checks")
	}
}

func TestNewRefusesBadInput(t *testing.T) {
	tests := []struct {
		name  string
		id    shared.TicketSetID
		sub   shared.SubjectID
		items []Item
	}{
		{"no tickets", "ts1", "sub1", nil},
		{"too many", "ts1", "sub1", items(MaxTickets + 1)},
		{"no subject", "ts1", "", items(1)},
		{"bad id", "", "sub1", items(1)},
		{"numbers not rising", "ts1", "sub1", []Item{{2, "а"}, {1, "б"}}},
		{"title on two lines", "ts1", "sub1", []Item{{1, "а\nб"}}},
		{"title with a format character", "ts1", "sub1", []Item{{1, "а" + string(rune(0x202E)) + "б"}}},
		{"title too long", "ts1", "sub1", []Item{{1, strings.Repeat("я", MaxTitleRunes+1)}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := New(tt.id, tt.sub, "", tt.items, t0); !errors.Is(err, ErrInvalidSet) {
				t.Fatalf("New = %v", err)
			}
		})
	}
}

func TestConfirmAndBudget(t *testing.T) {
	cheap := newSet(t, 3)
	if b := cheap.Budget(); b.Tickets != 3 || b.EstimateMicroUSD != 3*TicketCostMicroUSD || b.OverCap {
		t.Fatalf("budget = %+v", b)
	}
	if err := cheap.Confirm(false, t0); err != nil || cheap.Status() != StatusConfirmed || cheap.CostAccepted() {
		t.Fatalf("confirm = %v, %s", err, cheap.Status())
	}
	if err := cheap.Confirm(false, t0); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("confirm twice = %v", err)
	}

	costly := newSet(t, MaxTickets)
	if !costly.Budget().OverCap {
		t.Fatalf("budget = %+v", costly.Budget())
	}
	if err := costly.Confirm(false, t0); !errors.Is(err, ErrCostNotAccepted) || costly.Status() != StatusProposed {
		t.Fatalf("confirm without the cost = %v", err)
	}
	if err := costly.Confirm(true, t0); err != nil || !costly.CostAccepted() {
		t.Fatalf("confirm with the cost = %v", err)
	}
}

func TestEstimate(t *testing.T) {
	tests := []struct {
		n       int
		tickets int
		over    bool
	}{
		{0, 0, false}, {-1, 0, false}, {1, 1, false},
		{int(RunCapMicroUSD / TicketCostMicroUSD), int(RunCapMicroUSD / TicketCostMicroUSD), false},
		{int(RunCapMicroUSD/TicketCostMicroUSD) + 1, int(RunCapMicroUSD/TicketCostMicroUSD) + 1, true},
		{MaxTickets + 50, MaxTickets, true},
	}
	for _, tt := range tests {
		b := Estimate(tt.n)
		if b.Tickets != tt.tickets || b.OverCap != tt.over || b.EstimateMicroUSD != int64(tt.tickets)*TicketCostMicroUSD ||
			b.CapMicroUSD != RunCapMicroUSD {
			t.Errorf("Estimate(%d) = %+v", tt.n, b)
		}
	}
}

func TestRunStepByStep(t *testing.T) {
	s := newSet(t, 2)
	if st := s.Next(); st.Kind != StepNone {
		t.Fatalf("a proposed set has a step: %+v", st)
	}
	if err := s.Start(t0); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("start before confirm = %v", err)
	}
	s = answering(t, 2)

	first := s.Next()
	if first.Kind != StepTicket || first.Ticket != 1 || !strings.HasPrefix(first.Key, stepKeyPrefix) {
		t.Fatalf("first step = %+v", first)
	}
	if err := s.Record(first, Result{Coverage: CoverageCovered}, t0); !errors.Is(err, ErrInvalidSet) {
		t.Fatalf("covered without an answer = %v", err)
	}
	if err := s.Record(first, Result{Answer: "a1", Coverage: CoverageFailed}, t0); !errors.Is(err, ErrInvalidSet) {
		t.Fatalf("failed with an answer = %v", err)
	}
	if err := s.Record(first, Result{Answer: "a1", Coverage: CoveragePartial}, t0); err != nil {
		t.Fatal(err)
	}
	if err := s.Record(first, Result{Coverage: CoverageNotCovered}, t0); !errors.Is(err, ErrStaleStep) {
		t.Fatalf("the same step again = %v", err)
	}
	second := s.Next()
	if second.Ticket != 2 || second.Key == first.Key {
		t.Fatalf("second step = %+v", second)
	}
	if err := s.Finish(File{Material: "m1", Artifact: "materials/" + strings.Repeat("a", 64)}, t0); !errors.Is(err, ErrStaleStep) {
		t.Fatalf("finish before every ticket = %v", err)
	}
	if err := s.Record(second, Result{Coverage: CoverageNotCovered}, t0); err != nil {
		t.Fatal(err)
	}
	if done, total := s.Progress(); done != 2 || total != 2 || s.Next().Kind != StepAssemble {
		t.Fatalf("progress %d/%d, next %+v", done, total, s.Next())
	}
	if err := s.Finish(File{Material: "m1", Artifact: "not a ref"}, t0); !errors.Is(err, ErrInvalidSet) {
		t.Fatalf("bad artifact = %v", err)
	}
	ref := validArtifact(t)
	if err := s.Finish(File{Material: "m1", Artifact: ref}, t0); err != nil || s.Status() != StatusDone {
		t.Fatalf("finish = %v, %s", err, s.Status())
	}
	if r := s.Report(); r != (Report{Partial: 1, NotCovered: 1}) {
		t.Fatalf("report = %+v", r)
	}
	if err := s.Cancel(t0); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("cancel a done set = %v", err)
	}
}

func TestCancelAndFail(t *testing.T) {
	for _, st := range []func(*testing.T) *Set{
		func(t *testing.T) *Set { return newSet(t, 1) },
		func(t *testing.T) *Set { return answering(t, 1) },
	} {
		s := st(t)
		if err := s.Cancel(t0); err != nil || s.Status() != StatusCancelled || s.Next().Kind != StepNone {
			t.Fatalf("cancel = %v, %s", err, s.Status())
		}
		if err := s.Confirm(false, t0); !errors.Is(err, ErrInvalidTransition) {
			t.Fatalf("confirm a cancelled set = %v", err)
		}
	}
	s := answering(t, 1)
	if err := s.Fail(t0); err != nil || s.Status() != StatusFailed {
		t.Fatalf("fail = %v", err)
	}
	if err := newSet(t, 1).Fail(t0); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("fail a proposed set = %v", err)
	}
}

func TestContentRoundTripAndRestore(t *testing.T) {
	s := answering(t, MaxTickets)
	if err := s.Record(s.Next(), Result{Answer: "a1", Coverage: CoverageCovered}, t0); err != nil {
		t.Fatal(err)
	}
	s.Persisted(3)
	st := s.State()
	c, err := ParseContent(st.Content.Encode())
	if err != nil {
		t.Fatal(err)
	}
	st.Content = c
	got, err := Restore(st)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version() != 3 || got.Status() != StatusAnswering || !got.CostAccepted() || got.Next().Ticket != 2 ||
		got.Tickets()[0].Result.Answer != "a1" {
		t.Fatalf("restored %+v", got.State())
	}

	for name, raw := range map[string]string{
		"unknown field": `{"tickets":[],"cost_accepted":false,"file":null,"x":1}`,
		"data after":    st.Content.Encode() + `{}`,
		"not json":      `[`,
	} {
		if _, err := ParseContent(raw); !errors.Is(err, ErrInvalidSet) {
			t.Errorf("%s: %v", name, err)
		}
	}

	bad := []func(st *State){
		func(st *State) { st.Version = 0 },
		func(st *State) { st.Content.Tickets[1].Title = "два\nряда" },
		func(st *State) { st.Content.Tickets[1].Number = 1 },
		func(st *State) { st.Content.Tickets[1].Result = &Result{Coverage: "maybe"} },
		func(st *State) { st.Content.CostAccepted = false },
		func(st *State) { st.Status = StatusProposed },
		func(st *State) { st.Status = StatusDone },
		func(st *State) { st.Content.File = &File{Material: "m1", Artifact: validArtifact(t)} },
	}
	for i, edit := range bad {
		st := s.State()
		edit(&st)
		if _, err := Restore(st); !errors.Is(err, ErrInvalidSet) {
			t.Errorf("edit %d restored: %v", i, err)
		}
	}
}

// validArtifact is the artifact ref of a stored material file.
func validArtifact(t *testing.T) string {
	t.Helper()
	ref, err := shared.NewArtifactRef(shared.ArtifactMaterials, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	return ref.String()
}

func classified(t *testing.T, reply string) *answer.Answer {
	t.Helper()
	req, err := answer.NewRequest("Когда ряд сходится абсолютно?", []answer.Excerpt{{
		Text:   "Ряд сходится абсолютно, если сходится ряд модулей.",
		Source: answer.Source{Recording: "r1", Section: answer.SectionSummary},
	}})
	if err != nil {
		t.Fatal(err)
	}
	rep, err := req.Parse(reply)
	if err != nil {
		t.Fatal(err)
	}
	a, err := answer.New("a1", "sub1", req, rep, t0)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestClassify(t *testing.T) {
	const (
		good = `{"text":"Если сходится ряд модулей.","cites":[{"chunk_id":"f1","quote":"если сходится ряд модулей"}]}`
		bad  = `{"text":"Всегда.","cites":[{"chunk_id":"f1","quote":"ряд всегда сходится абсолютно"}]}`
	)
	tests := []struct {
		name  string
		reply string
		want  Coverage
	}{
		{"every claim supported", `{"claims":[` + good + `],"missing":[]}`, CoverageCovered},
		{"a claim not supported", `{"claims":[` + good + `,` + bad + `],"missing":[]}`, CoveragePartial},
		{"a part missing", `{"claims":[` + good + `],"missing":["Пример"]}`, CoveragePartial},
		{"no claim supported", `{"claims":[` + bad + `],"missing":[]}`, CoverageNotCovered},
		{"only missing", `{"claims":[],"missing":["Нет в лекциях"]}`, CoverageNotCovered},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Classify(classified(t, tt.reply)); got != tt.want {
				t.Fatalf("Classify = %s, want %s", got, tt.want)
			}
		})
	}
	if Classify(nil) != CoverageNotCovered {
		t.Fatal("no answer is not covered")
	}
}
