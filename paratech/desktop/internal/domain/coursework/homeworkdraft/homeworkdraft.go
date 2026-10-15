package homeworkdraft

import (
	"fmt"
	"slices"
	"time"

	"paratech/desktop/internal/domain/shared"
)

// MaxRevisions bounds the versions of one draft, so that loading a draft stays cheap.
const MaxRevisions = 100

// Draft is the draft history of one assignment (PRD section 10). Fields change only through the methods below.
type Draft struct {
	version    int64 // persisted version for optimistic concurrency; 0 = never stored (ADR-0004)
	assignment shared.AssignmentID
	revisions  []Revision // numbered 1..n in order
}

// New starts the empty history of an assignment, never stored (Version() == 0). It is stored once it has a version.
func New(a shared.AssignmentID) (*Draft, error) {
	if err := a.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidDraft, err)
	}
	return &Draft{assignment: a}, nil
}

// RevisionState is the stored form of a version.
type RevisionState struct {
	Number        int
	Origin        Origin
	Title         string
	Body          string
	Instruction   string       // generated only
	PromptVersion int          // generated only, else 0
	Job           shared.JobID // generated only, else empty
	CreatedAt     time.Time
}

// State is the stored form of a draft, for Restore and the repositories.
type State struct {
	Version    int64
	Assignment shared.AssignmentID
	Revisions  []RevisionState // in version order
}

// Restore rebuilds a stored draft without side effects; it only checks that the state is consistent: at least one
// and at most MaxRevisions versions numbered 1..n, each valid, and at most one generated version per job.
func Restore(st State) (*Draft, error) {
	if st.Version < 1 {
		return nil, fmt.Errorf("%w: stored version %d", ErrInvalidDraft, st.Version)
	}
	if err := st.Assignment.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidDraft, err)
	}
	if len(st.Revisions) == 0 || len(st.Revisions) > MaxRevisions {
		return nil, fmt.Errorf("%w: %d versions, want 1-%d", ErrInvalidDraft, len(st.Revisions), MaxRevisions)
	}

	d := &Draft{version: st.Version, assignment: st.Assignment, revisions: make([]Revision, 0, len(st.Revisions))}
	for i, rs := range st.Revisions {
		if rs.Number != i+1 {
			return nil, fmt.Errorf("%w: version %d at position %d", ErrInvalidDraft, rs.Number, i+1)
		}
		if d.ProducedBy(rs.Job) {
			return nil, fmt.Errorf("%w: two versions of one job", ErrInvalidDraft)
		}
		r, err := Revision{
			number: rs.Number, origin: rs.Origin, title: rs.Title, body: rs.Body, instruction: rs.Instruction,
			promptVersion: rs.PromptVersion, job: rs.Job, createdAt: rs.CreatedAt,
		}.validate()
		if err != nil {
			return nil, err
		}
		d.revisions = append(d.revisions, r)
	}
	return d, nil
}

// Generation is the validated answer of a homework_draft job, applied with AddGenerated.
type Generation struct {
	Base          int          // the version the job started from; 0 when there was no draft
	Job           shared.JobID // the job, so that a retry does not add the answer twice
	Title         string
	Body          string
	Instruction   string // the student's instructions of the generation; may be empty
	PromptVersion int    // at least 1
}

// CanAppend reports whether a change that starts from version base can still be added: base is the latest version
// (0 for an empty history), else ErrStaleBase, and there is room for one more version, else ErrFull. A job checks it
// before it calls the LLM, so a generation that cannot be applied costs nothing.
func (d *Draft) CanAppend(base int) error {
	if base != d.LatestNumber() {
		return fmt.Errorf("%w: base %d, latest %d", ErrStaleBase, base, d.LatestNumber())
	}
	if len(d.revisions) >= MaxRevisions {
		return ErrFull
	}
	return nil
}

// AddGenerated appends the answer of a generation job. A job whose answer is already a version adds nothing (added is
// false): the job ran again after a crash. Otherwise it is refused as CanAppend says, so a generation never becomes
// the latest version over a change the student made meanwhile.
func (d *Draft) AddGenerated(g Generation, now time.Time) (added bool, err error) {
	if d.ProducedBy(g.Job) {
		return false, nil
	}
	if err := d.CanAppend(g.Base); err != nil {
		return false, err
	}
	r, err := Revision{
		number: len(d.revisions) + 1, origin: OriginGenerated, title: g.Title, body: g.Body, instruction: g.Instruction,
		promptVersion: g.PromptVersion, job: g.Job, createdAt: now,
	}.validate()
	if err != nil {
		return false, fmt.Errorf("%w: %w", ErrInvalidContent, err)
	}
	d.revisions = append(d.revisions, r)
	return true, nil
}

// Edit appends the title and body the student wrote, starting from version base (0: the first version is written by
// hand). It is refused as CanAppend says. A text equal to the latest version changes nothing (changed is false).
func (d *Draft) Edit(base int, title, body string, now time.Time) (changed bool, err error) {
	title, body, err = checkContent(title, body)
	if err != nil {
		return false, err
	}
	if latest, ok := d.Latest(); ok && base == latest.number && latest.title == title && latest.body == body {
		return false, nil
	}
	if err := d.CanAppend(base); err != nil {
		return false, err
	}
	r, err := Revision{number: len(d.revisions) + 1, origin: OriginEdited, title: title, body: body, createdAt: now}.validate()
	if err != nil {
		return false, fmt.Errorf("%w: %w", ErrInvalidContent, err)
	}
	d.revisions = append(d.revisions, r)
	return true, nil
}

// ProducedBy reports whether the answer of job is already a version.
func (d *Draft) ProducedBy(job shared.JobID) bool {
	return job != "" && slices.ContainsFunc(d.revisions, func(r Revision) bool { return r.job == job })
}

// Assignment returns the assignment the draft is for; it also identifies the draft.
func (d *Draft) Assignment() shared.AssignmentID { return d.assignment }

// LatestNumber returns the number of the latest version; 0 for an empty history.
func (d *Draft) LatestNumber() int { return len(d.revisions) }

// Latest returns the latest version; ok is false for an empty history.
func (d *Draft) Latest() (r Revision, ok bool) {
	if len(d.revisions) == 0 {
		return Revision{}, false
	}
	return d.revisions[len(d.revisions)-1], true
}

// Revision returns version n; ok is false if there is none.
func (d *Draft) Revision(n int) (r Revision, ok bool) {
	if n < 1 || n > len(d.revisions) {
		return Revision{}, false
	}
	return d.revisions[n-1], true
}

// Revisions returns a copy of the versions in order.
func (d *Draft) Revisions() []Revision { return slices.Clone(d.revisions) }

// State returns the stored form of the draft, for the repositories.
func (d *Draft) State() State {
	st := State{Version: d.version, Assignment: d.assignment, Revisions: make([]RevisionState, len(d.revisions))}
	for i, r := range d.revisions {
		st.Revisions[i] = RevisionState{
			Number: r.number, Origin: r.origin, Title: r.title, Body: r.body, Instruction: r.instruction,
			PromptVersion: r.promptVersion, Job: r.job, CreatedAt: r.createdAt,
		}
	}
	return st
}

// Version returns the version this instance was last loaded or saved with: 0 for a draft that was never stored,
// otherwise the stored version, which is at least 1 (optimistic concurrency, ADR-0004). It is not a draft version
// number (LatestNumber).
func (d *Draft) Version() int64 { return d.version }

// Persisted records the version the store assigned after a successful Save. It is called by the repository adapter
// only, after the write succeeded (ADR-0004); use cases and handlers never call it.
func (d *Draft) Persisted(version int64) { d.version = version }
