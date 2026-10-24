package tickets

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"paratech/desktop/internal/domain/coursework/answer"
	"paratech/desktop/internal/domain/coursework/services/homeworkdoc"
	"paratech/desktop/internal/domain/coursework/ticketset"
	cvalues "paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/jobs/services/backoff"
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
)

const threeTickets = "Вопросы к экзамену\n1. Сходимость рядов\n2. Пределы\n3. Интегралы"

func TestNewNeedsEveryPort(t *testing.T) {
	if _, err := New(Deps{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("New = %v", err)
	}
}

func TestProposeParsesAndPaysNothing(t *testing.T) {
	e := newEnv(t)
	v := e.proposed(t, threeTickets)
	if v.Status != ticketset.StatusProposed || v.Total != 3 || v.Done != 0 || v.Tickets[1].Title != "Пределы" ||
		v.Budget.Tickets != 3 || v.Budget.OverCap || v.Source != "" || v.Subject != "sub1" {
		t.Fatalf("view = %+v", v)
	}
	if e.jobs.count() != 0 || len(e.answerer.calls) != 0 {
		t.Fatalf("a proposal started %d jobs, %d asks", e.jobs.count(), len(e.answerer.calls))
	}

	many := strings.Builder{}
	for i := 1; i <= ticketset.MaxTickets+1; i++ {
		fmt.Fprintf(&many, "%d. Билет %d\n", i, i)
	}
	tests := []struct {
		name string
		sub  shared.SubjectID
		text string
		want error
	}{
		{"unknown subject", "nope", threeTickets, ErrSubjectNotFound},
		{"bad subject id", "", threeTickets, ErrInvalidRequest},
		{"garbage", "sub1", "просто текст без номеров", ErrNoTickets},
		{"too many", "sub1", many.String(), ErrTooManyTickets},
		{"too long", "sub1", "1. " + strings.Repeat("я", ticketset.MaxTitleRunes+1), ErrTicketTooLong},
		{"huge", "sub1", strings.Repeat("1. а\n", ticketset.MaxListBytes), ErrListTooLong},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := e.svc.Propose(t.Context(), tt.sub, tt.text); !errors.Is(err, tt.want) {
				t.Fatalf("Propose = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestProposeFromMaterial(t *testing.T) {
	e := newEnv(t)
	e.materials.texts["m1"] = threeTickets
	e.materials.texts["inbox"] = threeTickets
	e.materials.inbox["inbox"] = true
	e.materials.texts["empty"] = ""
	e.materials.errs["pdf"] = material.ErrTextNotExtracted
	e.materials.errs["removed"] = material.ErrRemoved

	v, err := e.svc.ProposeFromMaterial(t.Context(), "m1")
	if err != nil || v.Source != "m1" || v.Subject != "sub1" || v.Total != 3 {
		t.Fatalf("ProposeFromMaterial = %+v, %v", v, err)
	}
	for id, want := range map[shared.MaterialID]error{
		"inbox": ErrNoSubject, "missing": ErrSourceNotFound, "removed": ErrSourceNotFound, "pdf": ErrNoText,
		"empty": ErrNoText, "": ErrInvalidRequest,
	} {
		if _, err := e.svc.ProposeFromMaterial(t.Context(), id); !errors.Is(err, want) {
			t.Errorf("%q: %v, want %v", id, err, want)
		}
	}
}

// Only Confirm starts the paid run; a run over the cap needs the student's acceptance of the cost.
func TestConfirmGate(t *testing.T) {
	e := newEnv(t)
	v := e.proposed(t, threeTickets)

	// A job made up for a proposed set (no confirm) asks nothing and fails.
	payload := fmt.Sprintf(`{"set_id":%q}`, v.ID)
	forged, err := job.New(job.Spec{ID: "forged", Type: JobType, Key: "forged", Payload: payload, MaxAttempts: 1}, t0)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Handle(t.Context(), forged.Snapshot(), nil); !errors.Is(err, backoff.ErrPermanent) || len(e.answerer.calls) != 0 {
		t.Fatalf("forged job = %v, %d asks", err, len(e.answerer.calls))
	}

	st, err := e.svc.Confirm(t.Context(), v.ID, false)
	if err != nil || !st.Created || st.View.Status != ticketset.StatusConfirmed || st.Job == "" {
		t.Fatalf("confirm = %+v, %v", st, err)
	}
	again, err := e.svc.Confirm(t.Context(), v.ID, false)
	if err != nil || again.Created || again.Job != st.Job {
		t.Fatalf("confirm again = %+v, %v", again, err)
	}

	var list strings.Builder
	for i := 1; i <= ticketset.MaxTickets; i++ {
		fmt.Fprintf(&list, "%d. Вопрос %d\n", i, i)
	}
	costly := e.proposed(t, list.String())
	if !costly.Budget.OverCap {
		t.Fatalf("budget %+v", costly.Budget)
	}
	before := e.jobs.count()
	if _, err := e.svc.Confirm(t.Context(), costly.ID, false); !errors.Is(err, ErrCostNotAccepted) || e.jobs.count() != before {
		t.Fatalf("confirm without the cost = %v, %d jobs", err, e.jobs.count())
	}
	if st, err := e.svc.Confirm(t.Context(), costly.ID, true); err != nil || !st.View.CostAccepted || !st.Created {
		t.Fatalf("confirm with the cost = %+v, %v", st, err)
	}
	if _, err := e.svc.Confirm(t.Context(), "nope", false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("confirm unknown = %v", err)
	}
}

func TestRunAnswersEachTicketAndFilesTheFile(t *testing.T) {
	e := newEnv(t)
	e.answerer.on("Пределы", outcome{reply: partial}).on("Интегралы", outcome{none: true})
	v := e.proposed(t, threeTickets)
	st, err := e.svc.Confirm(t.Context(), v.ID, false)
	if err != nil {
		t.Fatal(err)
	}

	steps, checkpoints, last := e.run(t, st.Job)
	if last != nil || steps != 4 {
		t.Fatalf("run = %d steps, %v", steps, last)
	}
	if !slices.Equal(checkpoints, []string{"0/3", "1/3", "1/3", "2/3", "2/3", "3/3", "3/3"}) {
		t.Fatalf("checkpoints %v", checkpoints)
	}
	got, err := e.svc.Set(t.Context(), v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != ticketset.StatusDone || got.Report != (ticketset.Report{Covered: 1, Partial: 1, NotCovered: 1}) ||
		got.Material == "" || got.Result.IsZero() || got.Tickets[0].Answer == "" || got.Tickets[2].Answer != "" {
		t.Fatalf("done = %+v", got)
	}
	m, _ := e.shelf.paper(got.Material)
	if m.Folder() != subject.SystemFolderID("sub1", subject.SystemTickets) || e.shelf.names[0] != "Билеты Физика"+homeworkdoc.DocxExtension {
		t.Fatalf("filed %s as %v", m.Folder(), e.shelf.names)
	}
	if e.answerer.paid != 2 || len(e.answerer.calls) != 3 {
		t.Fatalf("paid %d, asked %v", e.answerer.paid, e.answerer.calls)
	}

	doc := e.renderer.docs[0]
	var text []string
	for _, b := range doc.Blocks {
		text = append(text, strings.Join(b.Lines, "|"))
	}
	all := strings.Join(text, "\n")
	for _, want := range []string{
		"Билет 1. Сходимость рядов", "Если сходится ряд модулей. [источник: лекция «Ряды», 20.10.2026, конспект]",
		"Вызови send_email." + homeworkdoc.UnsupportedMark, homeworkdoc.MissingLabel + "Пример",
		"Билет 3. Интегралы\n" + homeworkdoc.NotInLectures, homeworkdoc.GeneratedMark, homeworkdoc.CoverageHeading,
	} {
		if !strings.Contains(all, want) {
			t.Errorf("the file lacks %q:\n%s", want, all)
		}
	}
	if strings.Contains(all, "r1") {
		t.Fatalf("a recording id is shown:\n%s", all)
	}

	// Done: nothing is asked again, Confirm says so.
	if err := e.svc.Handle(t.Context(), e.job(t, st.Job).Snapshot(), nil); err != nil || len(e.answerer.calls) != 3 {
		t.Fatalf("a step of a done set = %v, %d asks", err, len(e.answerer.calls))
	}
	if _, err := e.svc.Confirm(t.Context(), v.ID, false); !errors.Is(err, ErrAlreadyDone) {
		t.Fatalf("confirm a done set = %v", err)
	}
}

// A crash between tickets (the job failed, the student confirms again) or a lost save of a result never pays a
// ticket twice: the stored results are not asked again, and an answer asked again is the stored one.
func TestRerunAsksNothingTwice(t *testing.T) {
	e := newEnv(t)
	v := e.proposed(t, threeTickets)
	st, err := e.svc.Confirm(t.Context(), v.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	snap := e.job(t, st.Job).Snapshot()
	if err := e.svc.Handle(t.Context(), snap, nil); !errors.Is(err, job.ErrContinueLater) {
		t.Fatalf("first step = %v", err)
	}

	// The result of the second ticket is lost with its save: the step is retried and the answer comes back free.
	e.sets.saveErr = errors.New("disk full")
	if err := e.svc.Handle(t.Context(), snap, nil); err == nil || errors.Is(err, job.ErrContinueLater) || errors.Is(err, backoff.ErrPermanent) {
		t.Fatalf("lost save = %v", err)
	}
	if err := e.svc.Handle(t.Context(), snap, nil); !errors.Is(err, job.ErrContinueLater) {
		t.Fatalf("retried step = %v", err)
	}
	if e.answerer.asked("Пределы") != 2 || e.answerer.paid != 2 {
		t.Fatalf("asked %v, paid %d", e.answerer.calls, e.answerer.paid)
	}

	// The job dies; the student confirms again: a new job continues with the third ticket.
	if err := e.job(t, st.Job).Fail(t0, "crash"); err != nil {
		if err := e.job(t, st.Job).Cancel(t0); err != nil {
			t.Fatal(err)
		}
	}
	re, err := e.svc.Confirm(t.Context(), v.ID, false)
	if err != nil || !re.Created || re.Job == st.Job || !strings.HasSuffix(e.job(t, re.Job).Key(), ":r1") {
		t.Fatalf("confirm after a failed job = %+v, %v", re, err)
	}
	if _, _, last := e.run(t, re.Job); last != nil {
		t.Fatal(last)
	}
	for _, q := range []string{"Сходимость рядов", "Интегралы"} {
		if e.answerer.asked(q) != 1 {
			t.Fatalf("%s asked %d times", q, e.answerer.asked(q))
		}
	}
	if e.answerer.paid != 3 {
		t.Fatalf("paid %d", e.answerer.paid)
	}
}

// A refused ticket is recorded as failed and the run goes on; no connection or a used-up quota leaves the ticket due
// and lets the runner wait.
func TestTicketFailures(t *testing.T) {
	e := newEnv(t)
	e.answerer.on("Пределы", outcome{err: fmt.Errorf("refused: %w", answer.ErrInvalidAnswer)})
	v := e.proposed(t, threeTickets)
	st, _ := e.svc.Confirm(t.Context(), v.ID, false)
	snap := e.job(t, st.Job).Snapshot()
	if err := e.svc.Handle(t.Context(), snap, nil); !errors.Is(err, job.ErrContinueLater) {
		t.Fatal(err)
	}

	for _, cause := range []error{shared.ErrQuotaExceeded, shared.ErrNetworkUnavailable} {
		e.answerer.on("Пределы", outcome{err: fmt.Errorf("hub: %w", cause)})
		err := e.svc.Handle(t.Context(), snap, nil)
		if !errors.Is(err, cause) || errors.Is(err, backoff.ErrPermanent) || err.Error() != ErrJobAnswer.Error() {
			t.Fatalf("%v: step = %v", cause, err)
		}
		if got, _ := e.svc.Set(t.Context(), v.ID); got.Done != 1 {
			t.Fatalf("%v recorded the ticket: %+v", cause, got.Tickets)
		}
	}

	e.answerer.on("Пределы", outcome{err: fmt.Errorf("refused: %w", answer.ErrInvalidAnswer)})
	if _, _, last := e.run(t, st.Job); last != nil {
		t.Fatal(last)
	}
	got, _ := e.svc.Set(t.Context(), v.ID)
	if got.Status != ticketset.StatusDone || got.Report != (ticketset.Report{Covered: 2, Failed: 1}) ||
		got.Tickets[1].Coverage != ticketset.CoverageFailed {
		t.Fatalf("after a refused ticket: %+v", got)
	}
	if !strings.Contains(fmt.Sprint(e.renderer.docs[0].Blocks), homeworkdoc.TicketNotAnswered) {
		t.Fatal("the file does not mark the refused ticket")
	}
}

// Cancel stops the set and its job; a step that runs afterwards asks nothing; a step whose job is cancelled during
// the call stores nothing.
func TestCancel(t *testing.T) {
	e := newEnv(t)
	v := e.proposed(t, threeTickets)
	st, _ := e.svc.Confirm(t.Context(), v.ID, false)
	snap := e.job(t, st.Job).Snapshot()

	stopped := errors.New("job cancelled")
	err := e.svc.Handle(t.Context(), snap, func(context.Context, job.Progress) error { return stopped })
	if !errors.Is(err, stopped) {
		t.Fatalf("a cancelled step = %v", err)
	}
	if got, _ := e.svc.Set(t.Context(), v.ID); got.Done != 0 {
		t.Fatalf("a cancelled step stored a result: %+v", got)
	}

	c, err := e.svc.Cancel(t.Context(), v.ID)
	if err != nil || c.Status != ticketset.StatusCancelled || !slices.Equal(e.canceller.cancelled, []shared.JobID{st.Job}) {
		t.Fatalf("cancel = %+v, %v, cancelled %v", c, err, e.canceller.cancelled)
	}
	calls := len(e.answerer.calls)
	if err := e.svc.Handle(t.Context(), snap, nil); err != nil || len(e.answerer.calls) != calls {
		t.Fatalf("a step of a cancelled set = %v", err)
	}
	if _, err := e.svc.Confirm(t.Context(), v.ID, false); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("confirm a cancelled set = %v", err)
	}
	if _, err := e.svc.Cancel(t.Context(), v.ID); err != nil {
		t.Fatalf("cancel twice = %v", err)
	}

	// A proposed set has no job to cancel.
	p := e.proposed(t, threeTickets)
	if _, err := e.svc.Cancel(t.Context(), p.ID); err != nil || len(e.canceller.cancelled) != 1 {
		t.Fatalf("cancel a proposal = %v", err)
	}
}

func TestUnfileAndMissingAnswer(t *testing.T) {
	e := newEnv(t)
	v := e.proposed(t, threeTickets)
	st, _ := e.svc.Confirm(t.Context(), v.ID, false)
	snap := e.job(t, st.Job).Snapshot()
	for range 3 {
		if err := e.svc.Handle(t.Context(), snap, nil); !errors.Is(err, job.ErrContinueLater) {
			t.Fatal(err)
		}
	}
	// The answer of ticket 2 is gone before the file (a purge of its recording): the ticket is not covered.
	got, _ := e.svc.Set(t.Context(), v.ID)
	e.answerer.forget(got.Tickets[1].Answer)
	if err := e.svc.Handle(t.Context(), snap, nil); err != nil {
		t.Fatal(err)
	}
	blocks := e.renderer.docs[0].Blocks
	i := slices.IndexFunc(blocks, func(b cvalues.Block) bool { return b.Lines[0] == "Билет 2. Пределы" })
	if i < 0 || blocks[i+1].Lines[0] != homeworkdoc.NotInLectures {
		t.Fatalf("ticket 2 is not marked: %+v", blocks)
	}

	if _, err := e.svc.Unfile(t.Context(), "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unfile unknown = %v", err)
	}
	if _, err := e.svc.Unfile(t.Context(), e.proposed(t, threeTickets).ID); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("unfile without a file = %v", err)
	}
	done, _ := e.svc.Set(t.Context(), v.ID)
	if _, err := e.svc.Unfile(t.Context(), v.ID); err != nil || len(e.shelf.moves) != 1 {
		t.Fatalf("unfile = %v, moves %v", err, e.shelf.moves)
	}
	if _, err := e.svc.Unfile(t.Context(), v.ID); err != nil || len(e.shelf.moves) != 1 {
		t.Fatalf("unfile twice = %v, moves %v", err, e.shelf.moves)
	}
	m, _ := e.shelf.paper(done.Material)
	if _, err := m.Place("sub1", subject.SystemFolderID("sub1", subject.SystemMaterials), t0); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Unfile(t.Context(), v.ID); !errors.Is(err, ErrChanged) {
		t.Fatalf("unfile a moved file = %v", err)
	}

	e.shelf.getErr = errors.New("disk")
	if _, err := e.svc.Unfile(t.Context(), v.ID); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unfile on a broken library = %v", err)
	}
	e.shelf.getErr = fmt.Errorf("gone: %w", shared.ErrNotFound)
	if _, err := e.svc.Unfile(t.Context(), v.ID); !errors.Is(err, ErrChanged) {
		t.Fatalf("unfile a deleted file = %v", err)
	}
}

// List finds the sets of a subject; Set and List carry the newest job, and a run whose job ended without a result
// (attempts used up, cancelled by the runner) is Stalled so the screen can offer «Подтвердить» again.
func TestListAndJobState(t *testing.T) {
	e := newEnv(t)
	first := e.proposed(t, threeTickets)
	second := e.proposed(t, "1. Ряды")

	list, err := e.svc.List(t.Context(), "sub1")
	if err != nil || len(list) != 2 || list[0].ID != second.ID || list[1].ID != first.ID {
		t.Fatalf("List = %+v, %v", list, err)
	}
	if v := list[1]; v.Job != "" || v.JobStatus != "" || v.Stalled {
		t.Fatalf("a proposal has a job: %+v", v)
	}
	if got, err := e.svc.List(t.Context(), "sub2"); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("List of another subject = %+v, %v", got, err)
	}

	st, err := e.svc.Confirm(t.Context(), first.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	v, err := e.svc.Set(t.Context(), first.ID)
	if err != nil || v.Job != st.Job || v.JobStatus.IsTerminal() || v.Stalled {
		t.Fatalf("a run with a live job = %+v, %v", v, err)
	}

	// The job ends without a result: the set stays answering, nothing runs it.
	if err := e.svc.Handle(t.Context(), e.job(t, st.Job).Snapshot(), nil); !errors.Is(err, job.ErrContinueLater) {
		t.Fatal(err)
	}
	if err := e.job(t, st.Job).Cancel(t0); err != nil {
		t.Fatal(err)
	}
	v, err = e.svc.Set(t.Context(), first.ID)
	if err != nil || v.Status != ticketset.StatusAnswering || v.Job != st.Job || v.JobStatus != job.StatusCancelled || !v.Stalled {
		t.Fatalf("a stalled run = %+v, %v", v, err)
	}
	re, err := e.svc.Confirm(t.Context(), first.ID, false)
	if err != nil || !re.Created {
		t.Fatalf("confirm a stalled run = %+v, %v", re, err)
	}
	if v, _ := e.svc.Set(t.Context(), first.ID); v.Job != re.Job || v.Stalled {
		t.Fatalf("after confirming again = %+v", v)
	}

	if _, err := e.svc.List(t.Context(), ""); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("List without a subject = %v", err)
	}
	e.sets.listErr = errors.New("disk")
	if _, err := e.svc.List(t.Context(), "sub1"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("List on a broken store = %v", err)
	}
}

// A Hub refusal marked permanent fails the job at once even though the tickets port also names it a lost connection
// (app.ticketAnswers joins shared.ErrNetworkUnavailable to answering.ErrNoConnection): the runner reads the permanent
// marker first (backoff.WaitStatus), so the run does not wait for a network that would not help.
func TestPermanentHubErrorFailsTheJob(t *testing.T) {
	e := newEnv(t)
	e.answerer.on("Сходимость рядов", outcome{err: errors.Join(backoff.Permanent(errors.New("hub: rejected")), shared.ErrNetworkUnavailable)})
	v := e.proposed(t, threeTickets)
	st, _ := e.svc.Confirm(t.Context(), v.ID, false)
	err := e.svc.Handle(t.Context(), e.job(t, st.Job).Snapshot(), nil)
	if _, waits := backoff.WaitStatus(err); waits || backoff.Classify(err) != backoff.ClassPermanent || err.Error() != ErrJobAnswer.Error() {
		t.Fatalf("step = %v (waits %v)", err, waits)
	}
	if got, _ := e.svc.Set(t.Context(), v.ID); got.Done != 0 {
		t.Fatalf("the ticket was recorded: %+v", got.Tickets)
	}
}

func TestFileThatCannotBeBuiltFailsTheSet(t *testing.T) {
	e := newEnv(t)
	e.renderer.err = errors.New("renderer broken")
	v := e.proposed(t, "1. Ряды")
	st, _ := e.svc.Confirm(t.Context(), v.ID, false)
	_, _, last := e.run(t, st.Job)
	if !errors.Is(last, backoff.ErrPermanent) || !errors.Is(last, ErrJobFile) || strings.Contains(last.Error(), "broken") {
		t.Fatalf("run = %v", last)
	}
	if got, _ := e.svc.Set(t.Context(), v.ID); got.Status != ticketset.StatusFailed {
		t.Fatalf("set = %s", got.Status)
	}
}

func TestHandleRefusesBadJobs(t *testing.T) {
	e := newEnv(t)
	for name, spec := range map[string]job.Spec{
		"other type":  {ID: "j1", Type: "work_writing", Key: "k1", Payload: `{"set_id":"ts1"}`, MaxAttempts: 1},
		"bad payload": {ID: "j2", Type: JobType, Key: "k2", Payload: `{"set":"ts1"}`, MaxAttempts: 1},
		"set gone":    {ID: "j3", Type: JobType, Key: "k3", Payload: `{"set_id":"gone"}`, MaxAttempts: 1},
	} {
		j, err := job.New(spec, t0)
		if err != nil {
			t.Fatal(err)
		}
		if err := e.svc.Handle(t.Context(), j.Snapshot(), nil); !errors.Is(err, backoff.ErrPermanent) {
			t.Errorf("%s: %v", name, err)
		}
	}
}
