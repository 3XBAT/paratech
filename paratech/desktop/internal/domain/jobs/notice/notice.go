package notice

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
)

// Statuses returns the notifiable job statuses, mirrored by the CHECK of job_notices.status; a test keeps both in sync.
func Statuses() []job.Status { return []job.Status{job.StatusCompleted, job.StatusFailed} }

// Notifiable reports whether reaching s notifies the user: the processing finished, successfully or not.
func Notifiable(s job.Status) bool { return s == job.StatusCompleted || s == job.StatusFailed }

// ValidateType checks that t can be stored with a notice: not blank, no spaces or control characters.
func ValidateType(t job.Type) error {
	if t == "" || strings.ContainsFunc(string(t), func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
		return fmt.Errorf("%w: blank or malformed job type %q", ErrInvalidNotice, string(t))
	}
	return nil
}

// Notice records that the user was notified that a job reached a finished status. It never changes after New.
type Notice struct {
	job        shared.JobID
	typ        job.Type
	status     job.Status
	notifiedAt time.Time
}

// New creates the notice for a job of type typ that reached status at now.
func New(id shared.JobID, typ job.Type, status job.Status, now time.Time) (*Notice, error) {
	n := &Notice{job: id, typ: typ, status: status, notifiedAt: now}
	if err := n.validate(); err != nil {
		return nil, err
	}
	return n, nil
}

// State is the stored form of a notice, for Restore.
type State struct {
	Job        shared.JobID
	Type       job.Type
	Status     job.Status
	NotifiedAt time.Time
}

// Restore rebuilds a stored notice without side effects; it only checks that the state is consistent.
func Restore(s State) (*Notice, error) {
	return New(s.Job, s.Type, s.Status, s.NotifiedAt)
}

func (n *Notice) validate() error {
	if err := n.job.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidNotice, err)
	}
	if err := ValidateType(n.typ); err != nil {
		return err
	}
	if !Notifiable(n.status) {
		return fmt.Errorf("%w: status %q is not notifiable", ErrInvalidNotice, n.status)
	}
	if n.notifiedAt.IsZero() {
		return fmt.Errorf("%w: no notification time", ErrInvalidNotice)
	}
	return nil
}

// Job returns the job the notice is about.
func (n *Notice) Job() shared.JobID { return n.job }

// Type returns the type of that job, which selects the text shown.
func (n *Notice) Type() job.Type { return n.typ }

// Status returns the finished status the user was notified about.
func (n *Notice) Status() job.Status { return n.status }

// NotifiedAt returns when the notice was recorded.
func (n *Notice) NotifiedAt() time.Time { return n.notifiedAt }

// State returns the stored form of the notice.
func (n *Notice) State() State {
	return State{Job: n.job, Type: n.typ, Status: n.status, NotifiedAt: n.notifiedAt}
}
