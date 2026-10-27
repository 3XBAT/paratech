package tickets

import (
	"strings"
	"testing"

	"paratech/desktop/internal/domain/coursework/answer"
	"paratech/desktop/internal/domain/coursework/ticketset"
)

// A ticket whose answer rests on the student's note is covered: answering.Ask counts notes as lessons, the rule of
// coverage is unchanged (ADR-0027). The file names no note, file or recording id.
func TestTicketCoveredByOwnNote(t *testing.T) {
	e := newEnv(t)
	e.answerer.on("Сходимость рядов", outcome{reply: covered, note: true}).on("Пределы", outcome{none: true}).
		on("Интегралы", outcome{none: true})
	v := e.proposed(t, threeTickets)
	st, err := e.svc.Confirm(t.Context(), v.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, last := e.run(t, st.Job); last != nil {
		t.Fatalf("run = %v", last)
	}
	got, err := e.svc.Set(t.Context(), v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Tickets[0].Coverage != ticketset.CoverageCovered ||
		got.Report != (ticketset.Report{Covered: 1, NotCovered: 2}) {
		t.Fatalf("set = %+v", got)
	}
	a, err := e.answerer.Answer(t.Context(), got.Tickets[0].Answer)
	if err != nil || a.Sources()[0].Section != answer.SectionOwnNote {
		t.Fatalf("answer = %v, %v", a, err)
	}

	var lines []string
	for _, b := range e.renderer.docs[0].Blocks {
		lines = append(lines, strings.Join(b.Lines, "|"))
	}
	all := strings.Join(lines, "\n")
	if !strings.Contains(all, "Если сходится ряд модулей. [источник: лекция «Ряды», 20.10.2026]") {
		t.Fatalf("the claim of the note is not in the file:\n%s", all)
	}
	for _, id := range []string{"n1", "m1", "r1"} {
		if strings.Contains(all, id) {
			t.Fatalf("the id %s is shown:\n%s", id, all)
		}
	}
}
