package work

import (
	"fmt"
	"maps"
	"slices"
	"time"

	"paratech/desktop/internal/domain/shared"
)

// Source is what the work is made from: an assignment of the homework list, or an uploaded file of the library that
// holds the assignment. Exactly one is set.
type Source struct {
	Assignment shared.AssignmentID
	Material   shared.MaterialID
}

func (s Source) valid() bool {
	switch {
	case s.Assignment != "" && s.Material == "":
		return s.Assignment.Validate() == nil
	case s.Material != "" && s.Assignment == "":
		return s.Material.Validate() == nil
	}
	return false
}

// Round is one answered reader call: its checked answer and, once the student answered or skipped its questions,
// the resolved replies (one per question).
type Round struct {
	Number  int
	Answer  Answer
	Replies []Reply // nil until answered; never set on a round without questions
}

// Work is a turnkey work. Fields change only through the methods (ARCH-14).
type Work struct {
	version int64 // persisted version for optimistic concurrency; 0 = never stored (ADR-0004)

	id      shared.WorkID
	source  Source
	status  Status
	round   int // the round of the pending or last reader call, 1..FinalRound
	attempt int // the attempt of the pending call of round, 1..MaxAttempts
	rounds  []Round
	edits   map[Field]string // the student's card where it differs from the reader's

	createdAt time.Time
	updatedAt time.Time
}

// New starts a work: the reader call of round 1 is due. Error: ErrInvalidWork.
func New(id shared.WorkID, src Source, now time.Time) (*Work, error) {
	w := &Work{
		id: id, source: src, status: StatusTriage, round: 1, attempt: 1, edits: map[Field]string{},
		createdAt: now, updatedAt: now,
	}
	if err := w.validate(); err != nil {
		return nil, err
	}
	return w, nil
}

// Request builds the reader call that is due: this round and attempt, with the questions and answers of the earlier
// rounds. The texts are the assignment and the teacher's material (empty if none); longer ones are cut to their
// limits. Errors: ErrInvalidTransition when no call is due, ErrInvalidRequest.
func (w *Work) Request(assignment, material string) (Request, error) {
	if w.status != StatusTriage {
		return Request{}, fmt.Errorf("%w: no reader call is due in %s", ErrInvalidTransition, w.status)
	}
	return newRequest(w.id, w.round, w.attempt, assignment, material, w.previous())
}

// previous are the questions of every answered round with the student's answers, in order.
func (w *Work) previous() []Previous {
	var out []Previous
	for _, r := range w.rounds {
		for i, q := range r.Answer.Questions {
			out = append(out, Previous{Question: q.Text, Answer: AnswerText(q, r.Replies[i])})
		}
	}
	return out
}

// Record keeps the checked answer of round: an impossible work goes to the helper mode; questions in round 1 or 2 go
// to the student; otherwise the card «Как я понял задание» is ready. Questions in the final round are refused
// (ErrTooManyRounds): there is no third question round. Errors: ErrInvalidTransition, ErrStaleRound,
// ErrTooManyRounds, ErrInvalidAnswer.
func (w *Work) Record(round int, a Answer, now time.Time) error {
	if w.status != StatusTriage {
		return fmt.Errorf("%w: no reader call is due in %s", ErrInvalidTransition, w.status)
	}
	if round != w.round {
		return fmt.Errorf("%w: answer of round %d, due %d", ErrStaleRound, round, w.round)
	}
	if len(a.Questions) > 0 && round > QuestionRounds {
		return ErrTooManyRounds
	}
	if err := a.check(round); err != nil {
		return err
	}
	to := StatusReview
	switch {
	case a.Feasibility == FeasibleImpossible:
		to = StatusHelper // questions are moot: the helper mode does not need them
		a.Questions = []Question{}
	case len(a.Questions) > 0:
		to = StatusQuestions
	}
	next := w.clone()
	next.rounds = append(next.rounds, Round{Number: round, Answer: a})
	return w.move(next, to, now)
}

// Refuse records a refused answer of round (the Hub's schema refusal or this desktop's): the next call is a new
// attempt; after the last one the work fails. Errors: ErrInvalidTransition, ErrStaleRound.
func (w *Work) Refuse(round int, now time.Time) error {
	if w.status != StatusTriage {
		return fmt.Errorf("%w: no reader call is due in %s", ErrInvalidTransition, w.status)
	}
	if round != w.round {
		return fmt.Errorf("%w: refusal of round %d, due %d", ErrStaleRound, round, w.round)
	}
	next := w.clone()
	if w.attempt == MaxAttempts {
		return w.move(next, StatusFailed, now)
	}
	next.attempt++
	return w.move(next, StatusTriage, now)
}

// Answer takes the student's replies to the questions of the current round (unanswered questions take their
// default) and makes the next round's reader call due. Errors: ErrInvalidTransition, ErrInvalidReply.
func (w *Work) Answer(replies []Reply, now time.Time) error {
	if w.status != StatusQuestions {
		return fmt.Errorf("%w: no questions wait in %s", ErrInvalidTransition, w.status)
	}
	last := w.rounds[len(w.rounds)-1]
	resolved, err := Resolve(last.Answer.Questions, replies)
	if err != nil {
		return err
	}
	next := w.clone()
	next.rounds[len(next.rounds)-1].Replies = resolved
	next.round, next.attempt = w.round+1, 1
	return w.move(next, StatusTriage, now)
}

// Skip is «Пропустить — сделай как считаешь»: every question takes its default.
func (w *Work) Skip(now time.Time) error { return w.Answer(nil, now) }

// Edit keeps the student's card «Как я понял задание» while it can still change the work: with the questions or the
// card itself. A field equal to the reader's value is no edit. Errors: ErrInvalidTransition, ErrInvalidReply.
func (w *Work) Edit(u Understanding, now time.Time) error {
	if w.status != StatusQuestions && w.status != StatusReview {
		return fmt.Errorf("%w: the card cannot be edited in %s", ErrInvalidTransition, w.status)
	}
	edits, err := cleanEdits(w.readerUnderstanding(), u)
	if err != nil {
		return err
	}
	next := w.clone()
	next.edits = edits
	return w.move(next, w.status, now)
}

// ConfirmRequirements is «Подтвердить» on the card: the requirements are settled.
func (w *Work) ConfirmRequirements(now time.Time) error { return w.moveTo(StatusConfirmed, now) }

// StartWriting marks the work written (TASK-105).
func (w *Work) StartWriting(now time.Time) error { return w.moveTo(StatusWriting, now) }

// Finish marks the work written.
func (w *Work) Finish(now time.Time) error { return w.moveTo(StatusDone, now) }

func (w *Work) moveTo(to Status, now time.Time) error { return w.move(w.clone(), to, now) }

// move checks the transition and the state next would have, then takes it.
func (w *Work) move(next *Work, to Status, now time.Time) error {
	if to != w.status && !CanTransition(w.status, to) {
		return fmt.Errorf("%w: %s to %s", ErrInvalidTransition, w.status, to)
	}
	next.status, next.updatedAt = to, now
	if err := next.validate(); err != nil {
		return err
	}
	*w = *next
	return nil
}

func (w *Work) clone() *Work {
	c := *w
	c.rounds = slices.Clone(w.rounds)
	c.edits = maps.Clone(w.edits)
	return &c
}

// State is the stored form, for Restore and the repositories.
type State struct {
	Version   int64
	ID        shared.WorkID
	Source    Source
	Status    Status
	Round     int
	Attempt   int
	Rounds    []Round
	Edits     map[Field]string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Restore rebuilds a stored work without side effects; it only checks that the state is consistent. The answers of
// the rounds must have been read back with ParseAnswer.
func Restore(st State) (*Work, error) {
	if st.Version < 1 {
		return nil, fmt.Errorf("%w: stored version %d", ErrInvalidWork, st.Version)
	}
	w := &Work{
		version: st.Version, id: st.ID, source: st.Source, status: st.Status, round: st.Round, attempt: st.Attempt,
		rounds: slices.Clone(st.Rounds), edits: maps.Clone(st.Edits), createdAt: st.CreatedAt, updatedAt: st.UpdatedAt,
	}
	if w.edits == nil {
		w.edits = map[Field]string{}
	}
	if err := w.validate(); err != nil {
		return nil, err
	}
	return w, nil
}

// validate checks the invariants of the whole state.
func (w *Work) validate() error {
	bad := func(why string) error { return fmt.Errorf("%w: %s", ErrInvalidWork, why) }
	switch {
	case w.id.Validate() != nil:
		return bad("work id")
	case !w.source.valid():
		return bad("exactly one source")
	case !w.status.Valid():
		return bad("status")
	case w.round < 1 || w.round > FinalRound:
		return bad("round")
	case w.attempt < 1 || w.attempt > MaxAttempts:
		return bad("attempt")
	case w.createdAt.IsZero() || w.updatedAt.IsZero():
		return bad("missing time")
	}
	for i, r := range w.rounds {
		last := i == len(w.rounds)-1
		switch {
		case r.Number != i+1:
			return bad("rounds out of order")
		case r.Answer.check(r.Number) != nil:
			return bad("an answer breaks the schema")
		case !last && r.Replies == nil:
			return bad("an earlier round is not answered")
		case r.Replies != nil && (len(r.Answer.Questions) == 0 || !checkReplies(r.Answer.Questions, r.Replies)):
			return bad("the replies do not fit the questions")
		}
	}
	for f, v := range w.edits {
		if !validEdit(f, v) {
			return bad("an edit")
		}
	}

	// The rounds the status needs: a due call has the earlier ones answered, a shown answer is the current round.
	pending := w.status == StatusTriage || w.status == StatusFailed
	switch {
	case pending && len(w.rounds) != w.round-1:
		return bad("a due reader call follows the answered rounds")
	case !pending && len(w.rounds) != w.round:
		return bad("the last answer is of the current round")
	case w.status == StatusFailed && w.attempt != MaxAttempts:
		return bad("a failed work used every attempt")
	case pending && w.round > 1 && w.rounds[len(w.rounds)-1].Replies == nil:
		return bad("the next round follows answers")
	case len(w.edits) > 0 && len(w.rounds) == 0:
		return bad("an edit needs the reader's card")
	}
	if pending {
		return nil
	}
	last := w.rounds[len(w.rounds)-1]
	asked := len(last.Answer.Questions) > 0
	impossible := last.Answer.Feasibility == FeasibleImpossible
	switch {
	case w.status == StatusHelper && !impossible:
		return bad("the helper mode is for an impossible work")
	case w.status != StatusHelper && impossible:
		return bad("an impossible work is in the helper mode")
	case w.status == StatusQuestions && (!asked || last.Replies != nil || w.round > QuestionRounds):
		return bad("questions wait in round 1 or 2")
	case w.status != StatusQuestions && asked:
		return bad("questions are asked only in the questions status")
	}
	return nil
}

// --- reads ----------------------------------------------------------------------------------------------

// ID returns the identifier.
func (w *Work) ID() shared.WorkID { return w.id }

// Source returns what the work is made from.
func (w *Work) Source() Source { return w.source }

// Status returns the status.
func (w *Work) Status() Status { return w.status }

// Round returns the round of the pending or last reader call.
func (w *Work) Round() int { return w.round }

// Attempt returns the attempt of the pending reader call.
func (w *Work) Attempt() int { return w.attempt }

// Rounds returns the answered rounds, in order.
func (w *Work) Rounds() []Round {
	out := slices.Clone(w.rounds)
	for i := range out {
		out[i].Replies = slices.Clone(out[i].Replies)
	}
	return out
}

// Latest returns the answer of the last answered round; ok is false before the first answer.
func (w *Work) Latest() (Answer, bool) {
	if len(w.rounds) == 0 {
		return Answer{}, false
	}
	return w.rounds[len(w.rounds)-1].Answer, true
}

// Questions returns the questions waiting for the student, and their round (1 or 2); none outside StatusQuestions.
func (w *Work) Questions() (round int, questions []Question) {
	if w.status != StatusQuestions {
		return 0, nil
	}
	return w.round, slices.Clone(w.rounds[len(w.rounds)-1].Answer.Questions)
}

// Partial reports whether the reader said only a part can be done: the work is written with placeholders the
// student fills (TASK-107).
func (w *Work) Partial() bool {
	a, ok := w.Latest()
	return ok && a.Feasibility == FeasiblePartial
}

// readerUnderstanding is the card as the reader wrote it in the last answer.
func (w *Work) readerUnderstanding() Understanding {
	a, _ := w.Latest()
	return Understanding{WorkType: a.WorkType, Requirements: a.Requirements}
}

// Understanding returns the card «Как я понял задание»: the reader's last answer with the student's edits over it,
// and the fields the student edited.
func (w *Work) Understanding() (Understanding, []Field) {
	u := w.readerUnderstanding()
	var edited []Field
	for _, f := range Fields() {
		if v, ok := w.edits[f]; ok {
			u.set(f, v)
			edited = append(edited, f)
		}
	}
	return u, edited
}

// State returns the stored form.
func (w *Work) State() State {
	return State{
		Version: w.version, ID: w.id, Source: w.source, Status: w.status, Round: w.round, Attempt: w.attempt,
		Rounds: w.Rounds(), Edits: maps.Clone(w.edits), CreatedAt: w.createdAt, UpdatedAt: w.updatedAt,
	}
}

// CreatedAt returns when the work started.
func (w *Work) CreatedAt() time.Time { return w.createdAt }

// UpdatedAt returns when the work last changed.
func (w *Work) UpdatedAt() time.Time { return w.updatedAt }

// Version returns the version this instance was last loaded or saved with: 0 for one never stored.
func (w *Work) Version() int64 { return w.version }

// Persisted records the version the store assigned after a successful Save; called by the repository adapter only.
func (w *Work) Persisted(version int64) { w.version = version }
