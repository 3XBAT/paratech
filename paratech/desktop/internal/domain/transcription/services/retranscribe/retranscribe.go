package retranscribe

import (
	"errors"
	"strconv"
	"time"

	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/values"
)

// ErrNotAllowed means the recording may not be transcribed again now: it has a transcript, an STT job of it is
// unfinished, it has no STT job at all (the automatic enqueue makes the first one) or its newest STT job was not
// cancelled.
var ErrNotAllowed = errors.New("retranscribe: the transcription cannot be started again")

// State is what matters of the status of an STT job; contexts do not import each other (ARCH-11), so the use case
// maps the job status to it.
type State int

// States.
const (
	StateUnfinished State = iota + 1 // queued, waiting or running
	StateCancelled                   // cancelled by the user
	StateFinished                    // completed or failed
)

// Job is one STT job of a recording as the decision sees it.
type Job struct {
	ID      shared.JobID
	Created time.Time
	State   State
}

// Decision allows a new STT job of a recording; Key names it.
type Decision struct {
	rec shared.RecordingID
	n   int
}

// Key is the idempotency key of the new job pinned to model: "stt:<recording>:<model>:r<n>", n the number of STT jobs
// of the recording plus one. The same jobs give the same key, so a repeated request makes one job.
func (d Decision) Key(model shared.SttModelID) string {
	return values.STTJobKeyPrefix(d.rec) + string(model) + ":r" + strconv.Itoa(d.n)
}

// Policy holds the rule. It has no state.
type Policy struct{}

// New returns the policy.
func New() Policy { return Policy{} }

// Decide allows a new transcription of rec only when it has no transcript, none of its STT jobs is unfinished and
// the newest one is cancelled; otherwise it returns ErrNotAllowed.
func (Policy) Decide(rec shared.RecordingID, jobs []Job, hasTranscript bool) (Decision, error) {
	if hasTranscript {
		return Decision{}, ErrNotAllowed
	}
	if _, ok := Unfinished(jobs); ok {
		return Decision{}, ErrNotAllowed
	}
	last, ok := Newest(jobs)
	if !ok || last.State != StateCancelled {
		return Decision{}, ErrNotAllowed
	}
	return Decision{rec: rec, n: len(jobs) + 1}, nil
}

// Unfinished returns the newest unfinished job: the one a cancel stops.
func Unfinished(jobs []Job) (Job, bool) {
	var open []Job
	for _, j := range jobs {
		if j.State == StateUnfinished {
			open = append(open, j)
		}
	}
	return Newest(open)
}

// Newest returns the latest created job, the greater id on a tie (as the recordings screen orders them).
func Newest(jobs []Job) (Job, bool) {
	var out Job
	found := false
	for _, j := range jobs {
		if !found || j.Created.After(out.Created) || (j.Created.Equal(out.Created) && j.ID > out.ID) {
			out, found = j, true
		}
	}
	return out, found
}
