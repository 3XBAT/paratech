package homeworkflow

import (
	"time"

	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/shared"
)

// Deps are the ports the Service needs; all are required.
type Deps struct {
	Proposals   ProposalRepository
	Assignments AssignmentRepository
	Artifacts   ArtifactStore
	Recipients  RecipientSource
	Mailer      HomeworkMailer
	Exporter    HomeworkExporter
	Calendar    DeadlineCalendar
	Clock       Clock
	IDs         IDGenerator
	Tx          Tx
}

// ProposalView is a proposal as the confirmation dialog shows it (PRD section 13.3, AC-14): what will happen, to which
// assignment, with which data and to which trusted target. Title and Body are model text: show them as data.
type ProposalView struct {
	ID           proposal.ID
	Kind         proposal.Kind
	Status       proposal.Status
	Reason       proposal.RejectReason // set when REJECTED
	AssignmentID shared.AssignmentID
	Title        string
	Body         string
	Due          time.Time
	Recipient    string // the trusted address for send_homework_email, set once validated
	CreatedAt    time.Time
	ConfirmedAt  time.Time
}

func viewOf(p *proposal.Proposal) ProposalView {
	return ProposalView{
		ID: p.ID(), Kind: p.Kind(), Status: p.Status(), Reason: p.Reason(), AssignmentID: p.AssignmentID(),
		Title: p.Title(), Body: p.Body(), Due: p.Due(), Recipient: p.Recipient().Address(),
		CreatedAt: p.CreatedAt(), ConfirmedAt: p.ConfirmedAt(),
	}
}
