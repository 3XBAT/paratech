package tickets

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"paratech/desktop/internal/domain/coursework/services/homeworkdoc"
	"paratech/desktop/internal/domain/coursework/ticketset"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
)

// longReply is a covered answer of claims claims of 400 characters, each citing the lesson.
func longReply(claims int) string {
	claim := `{"text":"` + strings.Repeat("у", 400) + `","cites":[{"chunk_id":"f1","quote":"если сходится ряд модулей"}]}`
	return `{"claims":[` + strings.TrimSuffix(strings.Repeat(claim+",", claims), ",") + `],"missing":[]}`
}

// bigSet proposes and confirms MaxTickets tickets whose answers are too large for one document.
func bigSet(t *testing.T, e *env) (View, shared.JobID) {
	t.Helper()
	var list strings.Builder
	for i := 1; i <= ticketset.MaxTickets; i++ {
		q := fmt.Sprintf("Вопрос %d", i)
		fmt.Fprintf(&list, "%d. %s\n", i, q)
		e.answerer.on(q, outcome{reply: longReply(30)})
	}
	v := e.proposed(t, list.String())
	st, err := e.svc.Confirm(t.Context(), v.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	return v, st.Job
}

// answerAll runs the ticket steps of a job, so its next step is the file.
func answerAll(t *testing.T, e *env, id shared.JobID) job.Snapshot {
	t.Helper()
	snap := e.job(t, id).Snapshot()
	for range ticketset.MaxTickets {
		if err := e.svc.Handle(t.Context(), snap, nil); !errors.Is(err, job.ErrContinueLater) {
			t.Fatal(err)
		}
	}
	return snap
}

func TestRunSplitsALargeSetIntoParts(t *testing.T) {
	e := newEnv(t)
	v, id := bigSet(t, e)
	if _, _, last := e.run(t, id); last != nil {
		t.Fatal(last)
	}
	got, err := e.svc.Set(t.Context(), v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != ticketset.StatusDone || len(got.Files) < 2 || len(e.renderer.docs) != len(got.Files) {
		t.Fatalf("done = %s, %d files, %d rendered", got.Status, len(got.Files), len(e.renderer.docs))
	}
	if got.Material != got.Files[0].Material || got.Result != got.Files[0].Result {
		t.Fatalf("the first file is not the set's: %+v", got)
	}
	next := 1
	for i, f := range got.Files {
		rng := fmt.Sprintf("%d–%d", f.From, f.To)
		if f.From != next || f.To < f.From || f.Name != "Билеты Физика "+rng+homeworkdoc.DocxExtension ||
			e.shelf.names[i] != f.Name || f.Result.IsZero() {
			t.Fatalf("file %d = %+v after %d", i, f, next-1)
		}
		next = f.To + 1
		if err := e.renderer.docs[i].Validate(); err != nil {
			t.Fatalf("file %s: %v", rng, err)
		}
		m, ok := e.shelf.paper(f.Material)
		if !ok || m.Folder() != subject.SystemFolderID("sub1", subject.SystemTickets) {
			t.Fatalf("file %s is not in «Билеты»", rng)
		}
	}
	if next != ticketset.MaxTickets+1 {
		t.Fatalf("the files end at %d", next-1)
	}
}

func TestRunKeepsASmallSetInOneFile(t *testing.T) {
	e := newEnv(t)
	var list strings.Builder
	for i := 1; i <= 10; i++ {
		fmt.Fprintf(&list, "%d. Вопрос %d\n", i, i)
	}
	v := e.proposed(t, list.String())
	st, err := e.svc.Confirm(t.Context(), v.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, last := e.run(t, st.Job); last != nil {
		t.Fatal(last)
	}
	got, _ := e.svc.Set(t.Context(), v.ID)
	want := FileView{Material: got.Material, Result: got.Result, From: 1, To: 10, Name: "Билеты Физика" + homeworkdoc.DocxExtension}
	if len(got.Files) != 1 || got.Files[0] != want || len(e.shelf.names) != 1 {
		t.Fatalf("files %+v, filed %v", got.Files, e.shelf.names)
	}
}

// A lost save of the finished set repeats the file step: the same bytes find the same materials, nothing is filed
// twice.
func TestRepeatedFileStepFilesNothingTwice(t *testing.T) {
	e := newEnv(t)
	v, id := bigSet(t, e)
	snap := answerAll(t, e, id)
	e.sets.saveErr = errors.New("disk full")
	if err := e.svc.Handle(t.Context(), snap, nil); err == nil || errors.Is(err, job.ErrContinueLater) {
		t.Fatalf("lost save = %v", err)
	}
	filed := len(e.shelf.names)
	if err := e.svc.Handle(t.Context(), snap, nil); err != nil {
		t.Fatal(err)
	}
	got, _ := e.svc.Set(t.Context(), v.ID)
	if got.Status != ticketset.StatusDone || len(got.Files) < 2 || filed != len(got.Files) ||
		len(e.shelf.names) != 2*filed || len(e.shelf.papers) != filed {
		t.Fatalf("%s, %d files, filed %d then %d calls, %d materials", got.Status, len(got.Files), filed,
			len(e.shelf.names), len(e.shelf.papers))
	}
}

// A set cancelled while its parts are filed takes every part just filed back to «Входящие».
func TestCancelWhileFilingUnfilesEveryPart(t *testing.T) {
	e := newEnv(t)
	v, id := bigSet(t, e)
	snap := answerAll(t, e, id)
	e.shelf.onFile = func(n int) {
		if n == 2 {
			if _, err := e.svc.Cancel(t.Context(), v.ID); err != nil {
				t.Error(err)
			}
		}
	}
	if err := e.svc.Handle(t.Context(), snap, nil); err != nil {
		t.Fatal(err)
	}
	got, _ := e.svc.Set(t.Context(), v.ID)
	if got.Status != ticketset.StatusCancelled || len(got.Files) != 0 {
		t.Fatalf("set = %s, files %+v", got.Status, got.Files)
	}
	if len(e.shelf.papers) < 2 || len(e.shelf.moves) != len(e.shelf.papers) {
		t.Fatalf("%d filed, moved %v", len(e.shelf.papers), e.shelf.moves)
	}
	for mid := range e.shelf.papers {
		if m, _ := e.shelf.paper(mid); !m.InInbox() {
			t.Fatalf("%s left in %s", mid, m.Folder())
		}
	}
}

// Undo takes every part back; a part the student moved is left alone while the others move.
func TestUnfileEveryPart(t *testing.T) {
	e := newEnv(t)
	v, id := bigSet(t, e)
	if _, _, last := e.run(t, id); last != nil {
		t.Fatal(last)
	}
	done, _ := e.svc.Set(t.Context(), v.ID)
	moved, _ := e.shelf.paper(done.Files[0].Material)
	if _, err := moved.Place("sub1", subject.SystemFolderID("sub1", subject.SystemMaterials), t0); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Unfile(t.Context(), v.ID); err != nil {
		t.Fatalf("unfile = %v", err)
	}
	if len(e.shelf.moves) != len(done.Files)-1 || moved.InInbox() {
		t.Fatalf("moved %v of %d files", e.shelf.moves, len(done.Files))
	}
	for _, f := range done.Files[1:] {
		if m, _ := e.shelf.paper(f.Material); !m.InInbox() {
			t.Fatalf("%s is not in «Входящие»", f.Material)
		}
	}
	// Again: the others are in «Входящие» already, the moved one is left alone; nothing moved, so it is reported.
	if _, err := e.svc.Unfile(t.Context(), v.ID); !errors.Is(err, ErrChanged) || len(e.shelf.moves) != len(done.Files)-1 {
		t.Fatalf("unfile again = %v, moves %v", err, e.shelf.moves)
	}
}

// A set filed before TASK-402 has one file without a range: the view gives it every ticket.
func TestLegacyFileViewHoldsEveryTicket(t *testing.T) {
	e := newEnv(t)
	v := e.proposed(t, threeTickets)
	st, _ := e.svc.Confirm(t.Context(), v.ID, false)
	if _, _, last := e.run(t, st.Job); last != nil {
		t.Fatal(last)
	}
	// Store it as before TASK-402: one "file" without a range or a name.
	e.sets.mu.Lock()
	state := e.sets.states[v.ID]
	f := state.Content.Files[0]
	state.Content.Files = []ticketset.File{{Material: f.Material, Artifact: f.Artifact}}
	e.sets.states[v.ID] = state
	e.sets.mu.Unlock()

	got, err := e.svc.Set(t.Context(), v.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := FileView{Material: f.Material, Result: f.ArtifactRef(), From: 1, To: 3}
	if len(got.Files) != 1 || got.Files[0] != want || got.Material != f.Material {
		t.Fatalf("legacy files %+v", got.Files)
	}
}
