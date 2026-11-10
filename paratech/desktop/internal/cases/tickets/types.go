package tickets

import (
	"log/slog"
	"time"

	"paratech/desktop/internal/domain/coursework/ticketset"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
)

// Deps are the ports of the service; every one except Log is required.
type Deps struct {
	Sets      Repository
	Materials MaterialTexts
	Answerer  Answerer
	Shelf     Shelf
	Jobs      Jobs
	Canceller Canceller
	Profiles  Profiles
	Renderer  Renderer
	Clock     Clock
	IDs       IDs
	Log       *slog.Logger

	MaxAttempts int // attempts of a job; 0: DefaultJobAttempts
}

// View is a set as its card and the tickets screen show it (TASK-134). Titles come from the list: untrusted, shown as
// plain text only.
type View struct {
	ID      shared.TicketSetID
	Subject shared.SubjectID
	Source  shared.MaterialID // the material of the list; "" pasted
	Status  ticketset.Status

	Tickets      []TicketView
	Done, Total  int // «Билет N из M»
	Budget       ticketset.Budget
	CostAccepted bool
	Report       ticketset.Report

	Material shared.MaterialID  // the (first) file in «Билеты», once done
	Result   shared.ArtifactRef // the artifact of that file, once done
	Files    []FileView         // every file in the order of its tickets, once done; one unless the set was split

	// The newest ticket_answers job of the set and its status (Set and List only; "" when it has none). Stalled: the
	// run is confirmed or answering but no job is live (the last one failed after its attempts or was cancelled), so
	// the screen offers «Подтвердить» again; computed, never stored.
	Job       shared.JobID
	JobStatus job.Status
	Stalled   bool

	Version   int64
	UpdatedAt time.Time
}

// TicketView is one ticket: its result once answered. Answer opens the answer card (cases/answering) when set.
type TicketView struct {
	Number   int
	Title    string
	Answered bool
	Coverage ticketset.Coverage
	Answer   shared.AnswerID
}

// FileView is one file of a done set: the whole answers or a part of them (TASK-402), with the numbers of its first
// and last ticket. Name is the file name as filed; "" for a set filed before TASK-402.
type FileView struct {
	Material shared.MaterialID
	Result   shared.ArtifactRef
	From, To int
	Name     string
}

// Started is the outcome of Confirm: the set and its job; Created is false when a job of the run runs (or ran)
// already.
type Started struct {
	View    View
	Job     shared.JobID
	Created bool
}

// viewOf builds the view of a set.
func viewOf(s *ticketset.Set) View {
	v := View{
		ID: s.ID(), Subject: s.Subject(), Source: s.Source(), Status: s.Status(), Budget: s.Budget(),
		CostAccepted: s.CostAccepted(), Report: s.Report(), Version: s.Version(), UpdatedAt: s.UpdatedAt(),
	}
	v.Done, v.Total = s.Progress()
	for _, t := range s.Tickets() {
		tv := TicketView{Number: t.Number, Title: t.Title}
		if r := t.Result; r != nil {
			tv.Answered, tv.Coverage, tv.Answer = true, r.Coverage, r.Answer
		}
		v.Tickets = append(v.Tickets, tv)
	}
	for _, f := range s.Files() {
		fv := FileView{Material: f.Material, Result: f.ArtifactRef(), From: f.From, To: f.To, Name: f.Name}
		if fv.From == 0 && len(v.Tickets) > 0 { // a set filed before TASK-402: its one file holds every ticket
			fv.From, fv.To = v.Tickets[0].Number, v.Tickets[len(v.Tickets)-1].Number
		}
		v.Files = append(v.Files, fv)
	}
	if len(v.Files) > 0 {
		v.Material, v.Result = v.Files[0].Material, v.Files[0].Result
	}
	return v
}
