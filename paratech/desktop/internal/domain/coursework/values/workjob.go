package values

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"paratech/desktop/internal/domain/shared"
)

// WorkWritingJobType is the job that writes a turnkey work (TASK-131): the outline, one section per step, then the
// file. Defined here for the reason of the post-processing types (ARCH-11).
const WorkWritingJobType = "work_writing"

// ErrInvalidWorkJob means a work_writing payload is malformed.
var ErrInvalidWorkJob = errors.New("invalid work_writing job")

// WorkJob is the payload of a work_writing job: the work and, for a work made from an assignment, the assignment, so
// that deleting the assignment (with its recording) finds and cancels the job (TASK-095). Texts are read when the job
// runs, never copied into it.
type WorkJob struct {
	work       shared.WorkID
	assignment shared.AssignmentID
}

// NewWorkJob validates the ids; assignment may be empty (a work made from an uploaded file).
func NewWorkJob(w shared.WorkID, a shared.AssignmentID) (WorkJob, error) {
	if err := w.Validate(); err != nil {
		return WorkJob{}, fmt.Errorf("%w: %w", ErrInvalidWorkJob, err)
	}
	if a != "" {
		if err := a.Validate(); err != nil {
			return WorkJob{}, fmt.Errorf("%w: %w", ErrInvalidWorkJob, err)
		}
	}
	return WorkJob{work: w, assignment: a}, nil
}

// Work returns the work to write.
func (j WorkJob) Work() shared.WorkID { return j.work }

// Assignment returns the assignment of the work; "" for a work of an uploaded file.
func (j WorkJob) Assignment() shared.AssignmentID { return j.assignment }

type wireWorkJob struct {
	WorkID       *string `json:"work_id"`
	AssignmentID *string `json:"assignment_id,omitempty"`
}

// Payload renders the job payload, for example {"work_id":"w1","assignment_id":"a1"}.
func (j WorkJob) Payload() string {
	w := string(j.work)
	wire := wireWorkJob{WorkID: &w}
	if j.assignment != "" {
		a := string(j.assignment)
		wire.AssignmentID = &a
	}
	b, _ := json.Marshal(wire) // string pointers only: encoding cannot fail
	return string(b)
}

// ParseWorkJob reads a payload strictly: one object with work_id and an optional assignment_id, nothing else.
func ParseWorkJob(s string) (WorkJob, error) {
	dec := json.NewDecoder(bytes.NewReader([]byte(s)))
	dec.DisallowUnknownFields()
	var w wireWorkJob
	if err := dec.Decode(&w); err != nil {
		return WorkJob{}, fmt.Errorf("%w: not a payload object", ErrInvalidWorkJob)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return WorkJob{}, fmt.Errorf("%w: data after the payload", ErrInvalidWorkJob)
	}
	if w.WorkID == nil {
		return WorkJob{}, fmt.Errorf("%w: no work_id", ErrInvalidWorkJob)
	}
	var a shared.AssignmentID
	if w.AssignmentID != nil {
		if *w.AssignmentID == "" {
			return WorkJob{}, fmt.Errorf("%w: empty assignment_id", ErrInvalidWorkJob)
		}
		a = shared.AssignmentID(*w.AssignmentID)
	}
	return NewWorkJob(shared.WorkID(*w.WorkID), a)
}

// WorkCheckpoint is the job checkpoint of a work_writing job: how many sections are written of how many, "3/8" — the
// «Раздел N из M» of the progress card. Counts only, never text; the format is that of LectureTextCheckpoint.
func WorkCheckpoint(done, total int) string { return LectureTextCheckpoint(done, total) }
