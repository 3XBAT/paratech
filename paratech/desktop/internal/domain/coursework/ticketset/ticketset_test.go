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
	if err := s.Finish([]File{{Material: "m1", Artifact: "materials/" + strings.Repeat("a", 64)}}, t0); !errors.Is(err, ErrStaleStep) {
		t.Fatalf("finish before every ticket = %v", err)
	}
	if err := s.Record(second, Result{Coverage: CoverageNotCovered}, t0); err != nil {
		t.Fatal(err)
	}
	if done, total := s.Progress(); done != 2 || total != 2 || s.Next().Kind != StepAssemble {
		t.Fatalf("progress %d/%d, next %+v", done, total, s.Next())
	}
	if err := s.Finish([]File{{Material: "m1", Artifact: "not a ref"}}, t0); !errors.Is(err, ErrInvalidSet) {
		t.Fatalf("bad artifact = %v", err)
	}
	if err := s.Finish(nil, t0); !errors.Is(err, ErrInvalidSet) {
		t.Fatalf("finish without a file = %v", err)
	}
	ref := validArtifact(t)
	if err := s.Finish([]File{{Material: "m1", Artifact: ref}}, t0); err != nil || s.Status() != StatusDone {
		t.Fatalf("finish = %v, %s", err, s.Status())
	}
	if f := s.Files(); len(f) != 1 || f[0].Material != "m1" {
		t.Fatalf("files = %+v", f)
	}
	if r := s.Report(); r != (Report{Partial: 1, NotCovered: 1}) {
		t.Fatalf("report = %+v", r)
	}
	if err := s.Cancel(t0); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("cancel a done set = %v", err)
	}
}

// assembling returns a started set of the tickets numbered nums, every one answered: the file step is due.
func assembling(t *testing.T, nums ...int) *Set {
	t.Helper()
	var its []Item
	for _, n := range nums {
		its = append(its, Item{Number: n, Title: fmt.Sprintf("Вопрос %d", n)})
	}
	s, err := New("ts1", "sub1", "", its, t0)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Confirm(s.Budget().OverCap, t0); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(t0); err != nil {
		t.Fatal(err)
	}
	for s.Next().Kind == StepTicket {
		if err := s.Record(s.Next(), Result{Coverage: CoverageNotCovered}, t0); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

// part is a file of the tickets from..to.
func part(t *testing.T, id string, from, to int) File {
	t.Helper()
	ref, err := shared.NewArtifactRef(shared.ArtifactMaterials, strings.Repeat(id[len(id)-1:], 64))
	if err != nil {
		t.Fatal(err)
	}
	return File{Material: shared.MaterialID(id), Artifact: ref.String(), From: from, To: to, Name: fmt.Sprintf("Билеты %d–%d.docx", from, to)}
}

// The parts of a set too large for one document cover its tickets in order; anything else is refused.
func TestFinishWithParts(t *testing.T) {
	nums := []int{1, 2, 3, 5, 8, 9}
	long := part(t, "m1", 1, 3)
	long.Name = strings.Repeat("я", MaxFileNameBytes/2) + ".docx"
	tests := []struct {
		name  string
		files []File
		ok    bool
	}{
		{"one part", []File{part(t, "m1", 1, 9)}, true},
		{"two parts", []File{part(t, "m1", 1, 3), part(t, "m2", 5, 9)}, true},
		{"one ticket each", []File{part(t, "m1", 1, 2), part(t, "m2", 3, 3), part(t, "m3", 5, 5), part(t, "m4", 8, 9)}, true},
		{"a set filed before parts", []File{{Material: "m1", Artifact: validArtifact(t)}}, true},
		{"two parts without ranges", []File{{Material: "m1", Artifact: validArtifact(t)}, {Material: "m2", Artifact: validArtifact(t)}}, false},
		{"gap", []File{part(t, "m1", 1, 2), part(t, "m2", 5, 9)}, false},
		{"overlap", []File{part(t, "m1", 1, 3), part(t, "m2", 3, 9)}, false},
		{"starts late", []File{part(t, "m1", 2, 9)}, false},
		{"ends early", []File{part(t, "m1", 1, 3), part(t, "m2", 5, 8)}, false},
		{"ends past the last", []File{part(t, "m1", 1, 3), part(t, "m2", 5, 10)}, false},
		{"ends between tickets", []File{part(t, "m1", 1, 4), part(t, "m2", 5, 9)}, false},
		{"backwards", []File{part(t, "m2", 5, 9), part(t, "m1", 1, 3)}, false},
		{"to before from", []File{part(t, "m1", 1, 3), part(t, "m2", 5, 4)}, false},
		{"one part after the end", []File{part(t, "m1", 1, 9), part(t, "m2", 9, 9)}, false},
		{"material twice", []File{part(t, "m1", 1, 3), part(t, "m1", 5, 9)}, false},
		{"bad material", []File{part(t, "m1", 1, 3), {Artifact: validArtifact(t), From: 5, To: 9}}, false},
		{"name too long", []File{long, part(t, "m2", 5, 9)}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := assembling(t, nums...)
			err := s.Finish(tt.files, t0)
			if !tt.ok {
				if !errors.Is(err, ErrInvalidSet) || s.Status() != StatusAnswering {
					t.Fatalf("Finish = %v, %s", err, s.Status())
				}
				return
			}
			if err != nil || s.Status() != StatusDone || len(s.Files()) != len(tt.files) {
				t.Fatalf("Finish = %v, %s, %+v", err, s.Status(), s.Files())
			}
			got := s.Files()
			got[0].Material = "changed"
			if s.Files()[0].Material == "changed" {
				t.Fatal("Files is not a copy")
			}
		})
	}

	s := assembling(t, 1)
	many := make([]File, MaxTickets+1)
	if err := s.Finish(many, t0); !errors.Is(err, ErrInvalidSet) {
		t.Fatalf("too many files = %v", err)
	}
}

// Content keeps the files; content stored before TASK-402 has one "file", read as the only file without a range.
func TestContentFiles(t *testing.T) {
	s := assembling(t, 1, 2, 3)
	if err := s.Finish([]File{part(t, "m1", 1, 2), part(t, "m2", 3, 3)}, t0); err != nil {
		t.Fatal(err)
	}
	s.Persisted(4)
	st := s.State()
	raw := st.Content.Encode()
	if strings.Contains(raw, `"file":`) || !strings.Contains(raw, `"files":[`) || !strings.Contains(raw, `"name":"Билеты 1–2.docx"`) {
		t.Fatalf("encoded %s", raw)
	}
	c, err := ParseContent(raw)
	if err != nil {
		t.Fatal(err)
	}
	st.Content = c
	got, err := Restore(st)
	if err != nil {
		t.Fatal(err)
	}
	if f := got.Files(); len(f) != 2 || f[1] != part(t, "m2", 3, 3) {
		t.Fatalf("restored files %+v", f)
	}

	art := validArtifact(t)
	tickets := `{"tickets":[{"number":1,"title":"Вопрос 1","result":{"answer":"","coverage":"not_covered"}}],` +
		`"accepted":{"estimate_micro_usd":1,"cap_micro_usd":2},"cost_accepted":false,`
	old, err := ParseContent(tickets + `"file":{"material":"m1","artifact":"` + art + `"}}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(old.Files) != 1 || old.Files[0] != (File{Material: "m1", Artifact: art}) {
		t.Fatalf("old file read as %+v", old.Files)
	}
	done, err := Restore(State{Version: 1, ID: "ts1", Subject: "sub1", Status: StatusDone, Content: old, CreatedAt: t0, UpdatedAt: t0})
	if err != nil || len(done.Files()) != 1 {
		t.Fatalf("an old done set = %v", err)
	}
	if !strings.Contains(old.Encode(), `"files":[{"material":"m1","artifact":"`+art+`"}]`) {
		t.Fatalf("old content encoded as %s", old.Encode())
	}
	if c, err := ParseContent(tickets + `"file":null}`); err != nil || c.Files != nil {
		t.Fatalf("an old set without its file = %+v, %v", c, err)
	}

	for name, raw := range map[string]string{
		"file and files":          tickets + `"file":{"material":"m1","artifact":"` + art + `"},"files":[{"material":"m2","artifact":"` + art + `"}]}`,
		"unknown field":           tickets + `"files":[],"x":1}`,
		"unknown field of a file": tickets + `"files":[{"material":"m1","artifact":"` + art + `","size":1}]}`,
	} {
		if _, err := ParseContent(raw); !errors.Is(err, ErrInvalidSet) {
			t.Errorf("%s: %v", name, err)
		}
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
		"unknown field": `{"tickets":[],"accepted":null,"cost_accepted":false,"file":null,"x":1}`,
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
		func(st *State) { st.Content.Files = []File{{Material: "m1", Artifact: validArtifact(t)}} },
	}
	for i, edit := range bad {
		st := s.State()
		edit(&st)
		if _, err := Restore(st); !errors.Is(err, ErrInvalidSet) {
			t.Errorf("edit %d restored: %v", i, err)
		}
	}
}

// A stored set is checked against the figures the student confirmed, never the current constants: a set accepted
// above an old cap, or confirmed under it, restores after the pricing changed.
func TestRestoreUsesTheConfirmedFigures(t *testing.T) {
	tests := []struct {
		name     string
		tickets  int
		accepted Accepted
		cost     bool
		ok       bool
	}{
		// Under the current constants 3 tickets are under the cap; the stored figures were over it.
		{"accepted over an old cap", 3, Accepted{EstimateMicroUSD: 300_000, CapMicroUSD: 100_000}, true, true},
		// Under the current constants MaxTickets are over the cap; the stored figures were under it.
		{"confirmed under an old cap", MaxTickets, Accepted{EstimateMicroUSD: 1_000, CapMicroUSD: 50_000_000}, false, true},
		{"acceptance missing for the stored figures", 3, Accepted{EstimateMicroUSD: 300_000, CapMicroUSD: 100_000}, false, false},
		{"acceptance under the stored cap", MaxTickets, Accepted{EstimateMicroUSD: 1_000, CapMicroUSD: 50_000_000}, true, false},
		{"zero figures", 3, Accepted{}, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := answering(t, tt.tickets)
			s.Persisted(2)
			st := s.State()
			a := tt.accepted
			st.Content.Accepted, st.Content.CostAccepted = &a, tt.cost
			c, err := ParseContent(st.Content.Encode())
			if err != nil {
				t.Fatal(err)
			}
			st.Content = c
			got, err := Restore(st)
			if !tt.ok {
				if !errors.Is(err, ErrInvalidSet) {
					t.Fatalf("Restore = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if b := got.Budget(); b.EstimateMicroUSD != a.EstimateMicroUSD || b.CapMicroUSD != a.CapMicroUSD || b.OverCap != tt.cost {
				t.Fatalf("budget = %+v, want the stored figures", b)
			}
		})
	}

	// A proposed set holds no figures; a confirmed one keeps what the card showed.
	p := newSet(t, 3)
	st := p.State()
	st.Version, st.Content.Accepted = 1, &Accepted{EstimateMicroUSD: 1, CapMicroUSD: 2}
	if _, err := Restore(st); !errors.Is(err, ErrInvalidSet) {
		t.Fatalf("figures on a proposal = %v", err)
	}
	if err := p.Confirm(false, t0); err != nil {
		t.Fatal(err)
	}
	if a := p.State().Content.Accepted; a == nil || a.EstimateMicroUSD != 3*TicketCostMicroUSD || a.CapMicroUSD != RunCapMicroUSD {
		t.Fatalf("confirmed figures = %+v", a)
	}
	st = p.State()
	st.Version, st.Content.Accepted = 1, nil
	if _, err := Restore(st); !errors.Is(err, ErrInvalidSet) {
		t.Fatalf("a confirmed set without figures = %v", err)
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
	}}, "tk:v1")
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
