package values

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"paratech/desktop/internal/domain/shared"
)

// TicketAnswersJobType is the job that answers the tickets of a confirmed set (TASK-133): one ticket per step, then
// the file. Defined here for the reason of the post-processing types (ARCH-11).
const TicketAnswersJobType = "ticket_answers"

// ErrInvalidTicketJob means a ticket_answers payload is malformed.
var ErrInvalidTicketJob = errors.New("invalid ticket_answers job")

// TicketJob is the payload of a ticket_answers job: the set only. Titles are read when the job runs, never copied
// into it.
type TicketJob struct {
	set shared.TicketSetID
}

// NewTicketJob validates the id.
func NewTicketJob(s shared.TicketSetID) (TicketJob, error) {
	if err := s.Validate(); err != nil {
		return TicketJob{}, fmt.Errorf("%w: %w", ErrInvalidTicketJob, err)
	}
	return TicketJob{set: s}, nil
}

// Set returns the set to answer.
func (j TicketJob) Set() shared.TicketSetID { return j.set }

type wireTicketJob struct {
	SetID *string `json:"set_id"`
}

// Payload renders the job payload, for example {"set_id":"ts1"}.
func (j TicketJob) Payload() string {
	s := string(j.set)
	b, _ := json.Marshal(wireTicketJob{SetID: &s}) // a string pointer only: encoding cannot fail
	return string(b)
}

// ParseTicketJob reads a payload strictly: one object with set_id, nothing else.
func ParseTicketJob(s string) (TicketJob, error) {
	dec := json.NewDecoder(bytes.NewReader([]byte(s)))
	dec.DisallowUnknownFields()
	var w wireTicketJob
	if err := dec.Decode(&w); err != nil {
		return TicketJob{}, fmt.Errorf("%w: not a payload object", ErrInvalidTicketJob)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return TicketJob{}, fmt.Errorf("%w: data after the payload", ErrInvalidTicketJob)
	}
	if w.SetID == nil {
		return TicketJob{}, fmt.Errorf("%w: no set_id", ErrInvalidTicketJob)
	}
	return NewTicketJob(shared.TicketSetID(*w.SetID))
}

// TicketCheckpoint is the job checkpoint of a ticket_answers job: how many tickets are answered of how many, "3/8" —
// the «Билет N из M» of the progress card. Counts only, never text.
func TicketCheckpoint(done, total int) string { return LectureTextCheckpoint(done, total) }
