package tickets

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"paratech/desktop/internal/domain/coursework/ticketset"
	cvalues "paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
)

// JobType is the job type of a run (cvalues.TicketAnswersJobType).
const JobType job.Type = cvalues.TicketAnswersJobType

// DefaultJobAttempts is the attempt limit of a ticket_answers job when Deps.MaxAttempts is 0.
const DefaultJobAttempts = 5

// runRounds bounds how often the same set starts a new job after a failed or cancelled one.
const runRounds = 10

// saveAttempts bounds the saves of one change that lose against another save of the same set.
const saveAttempts = 3

// Operation names of the log. The log never holds the list, a title or an answer.
const (
	opProposed  = "tickets: set proposed"
	opConfirmed = "tickets: run confirmed"
	opCancelled = "tickets: set cancelled"
	opJobCancel = "tickets: run job not cancelled"
	opSaveLost  = "tickets: set changed while saved"
	opUnfiled   = "tickets: file moved back to the inbox"
)

// Service is the tickets use case.
type Service struct {
	sets      Repository
	materials MaterialTexts
	answerer  Answerer
	shelf     Shelf
	jobs      Jobs
	canceller Canceller
	profiles  Profiles
	renderer  Renderer
	clock     Clock
	ids       IDs
	log       *slog.Logger

	maxAttempts int
}

// New checks that every required port is given (see Deps).
func New(d Deps) (*Service, error) {
	if d.Sets == nil || d.Materials == nil || d.Answerer == nil || d.Shelf == nil || d.Jobs == nil ||
		d.Canceller == nil || d.Profiles == nil || d.Renderer == nil || d.Clock == nil || d.IDs == nil ||
		d.MaxAttempts < 0 {
		return nil, fmt.Errorf("%w: every port except the logger is required", ErrInvalidConfig)
	}
	log := d.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	attempts := d.MaxAttempts
	if attempts == 0 {
		attempts = DefaultJobAttempts
	}
	return &Service{
		sets: d.Sets, materials: d.Materials, answerer: d.Answerer, shelf: d.Shelf, jobs: d.Jobs,
		canceller: d.Canceller, profiles: d.Profiles, renderer: d.Renderer, clock: d.Clock, ids: d.IDs, log: log,
		maxAttempts: attempts,
	}, nil
}

// Propose parses a pasted list for a subject and stores the proposed set: «Найдено N билетов — подтвердить». Nothing
// is paid. Errors: ErrInvalidRequest, ErrSubjectNotFound, ErrNoTickets, ErrTooManyTickets, ErrListTooLong,
// ErrTicketTooLong, ErrUnavailable.
func (s *Service) Propose(ctx context.Context, sub shared.SubjectID, text string) (View, error) {
	if sub.Validate() != nil {
		return View{}, fmt.Errorf("%w: subject id", ErrInvalidRequest)
	}
	if _, ok, err := s.subject(ctx, sub); err != nil {
		return View{}, err
	} else if !ok {
		return View{}, ErrSubjectNotFound
	}
	return s.propose(ctx, sub, "", text)
}

// ProposeFromMaterial parses the text of a library material (a list the student filed under a subject) and stores
// the proposed set for the material's subject. Errors: those of Propose, ErrNoSubject, ErrSourceNotFound, ErrNoText.
func (s *Service) ProposeFromMaterial(ctx context.Context, id shared.MaterialID) (View, error) {
	if id.Validate() != nil {
		return View{}, fmt.Errorf("%w: material id", ErrInvalidRequest)
	}
	m, text, err := s.materials.MaterialText(ctx, id)
	switch {
	case errors.Is(err, shared.ErrNotFound), errors.Is(err, material.ErrRemoved):
		return View{}, fmt.Errorf("%w: %w", ErrSourceNotFound, err)
	case errors.Is(err, material.ErrTextNotExtracted), errors.Is(err, material.ErrTextUnreadable),
		errors.Is(err, material.ErrDamaged):
		return View{}, fmt.Errorf("%w: %w", ErrNoText, err)
	case err != nil:
		return View{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	case m.Subject() == "":
		return View{}, ErrNoSubject
	case text.Empty():
		return View{}, ErrNoText
	}
	return s.propose(ctx, m.Subject(), m.ID(), text.String())
}

func (s *Service) propose(ctx context.Context, sub shared.SubjectID, source shared.MaterialID, text string) (View, error) {
	items, err := ticketset.ParseList(text)
	if err != nil {
		return View{}, listError(err)
	}
	set, err := ticketset.New(s.ids.NewTicketSetID(), sub, source, items, s.clock.Now())
	if err != nil {
		return View{}, fmt.Errorf("%w: %w", ErrInvalidRequest, err)
	}
	if err := s.sets.Save(ctx, set); err != nil {
		return View{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	s.log.InfoContext(ctx, opProposed, slog.String("set", string(set.ID())), slog.Int("tickets", len(items)))
	return viewOf(set), nil
}

// Set returns a set. Errors: ErrNotFound, ErrUnavailable.
func (s *Service) Set(ctx context.Context, id shared.TicketSetID) (View, error) {
	set, err := s.get(ctx, id)
	if err != nil {
		return View{}, err
	}
	return viewOf(set), nil
}

// Confirm is the student's «Подтвердить» (input/wails only, never the agent): the paid run starts as a ticket_answers
// job. acceptCost is the student's acceptance of an estimate above the cap. A set confirmed or answering already gets
// its job back, or a new one after a failed or cancelled job, which continues from the tickets answered (none is
// asked again). Errors: ErrNotFound, ErrCostNotAccepted, ErrAlreadyDone, ErrInvalidRequest, ErrRetries,
// ErrUnavailable.
func (s *Service) Confirm(ctx context.Context, id shared.TicketSetID, acceptCost bool) (Started, error) {
	set, err := s.get(ctx, id)
	if err != nil {
		return Started{}, err
	}
	switch set.Status() {
	case ticketset.StatusProposed:
		set, err = s.change(ctx, id, func(set *ticketset.Set) error { return set.Confirm(acceptCost, s.clock.Now()) })
		if err != nil {
			return Started{}, err
		}
		s.log.InfoContext(ctx, opConfirmed, slog.String("set", string(id)), slog.Int("tickets", len(set.Tickets())))
	case ticketset.StatusConfirmed, ticketset.StatusAnswering:
	case ticketset.StatusDone:
		return Started{View: viewOf(set)}, ErrAlreadyDone
	default:
		return Started{View: viewOf(set)}, fmt.Errorf("%w: the set is %s", ErrInvalidRequest, set.Status())
	}
	return s.enqueue(ctx, set)
}

// Cancel is the student's «Отменить»: the set stops and its running job is cancelled. The answers already paid for
// stay stored. Errors: ErrNotFound, ErrInvalidRequest (a finished set), ErrUnavailable.
func (s *Service) Cancel(ctx context.Context, id shared.TicketSetID) (View, error) {
	set, err := s.change(ctx, id, func(set *ticketset.Set) error { return set.Cancel(s.clock.Now()) })
	if err != nil {
		return View{}, err
	}
	s.log.InfoContext(ctx, opCancelled, slog.String("set", string(id)))
	// The set is cancelled first: a step that runs meanwhile finds it so and stores nothing.
	if jobID, running, err := s.ActiveJob(ctx, id); err != nil {
		return viewOf(set), err
	} else if running {
		if err := s.canceller.Cancel(ctx, jobID); err != nil {
			s.log.WarnContext(ctx, opJobCancel, slog.String("job", string(jobID)), slog.Any("cause", err))
		}
	}
	return viewOf(set), nil
}

// Unfile is the Undo of the filing: the file of a done set goes back from «Билеты» to «Входящие», the file kept. A
// file already in «Входящие» is no error; one the student moved or removed is left alone (ErrChanged). Errors:
// ErrNotFound, ErrInvalidRequest (no file), ErrChanged, ErrUnavailable.
func (s *Service) Unfile(ctx context.Context, id shared.TicketSetID) (View, error) {
	set, err := s.get(ctx, id)
	if err != nil {
		return View{}, err
	}
	f, ok := set.File()
	if !ok {
		return viewOf(set), fmt.Errorf("%w: no file", ErrInvalidRequest)
	}
	m, _, err := s.materials.MaterialText(ctx, f.Material)
	switch {
	case m == nil && errors.Is(err, shared.ErrNotFound):
		return viewOf(set), fmt.Errorf("%w: %w", ErrChanged, err)
	case m == nil && err != nil:
		return viewOf(set), fmt.Errorf("%w: %w", ErrUnavailable, err)
	case m.Active() && m.InInbox():
		return viewOf(set), nil
	case !m.Active() || m.Folder() != subject.SystemFolderID(set.Subject(), subject.SystemTickets):
		return viewOf(set), ErrChanged
	}
	if _, err := s.shelf.MoveToInbox(ctx, f.Material); err != nil {
		return viewOf(set), fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	s.log.InfoContext(ctx, opUnfiled, slog.String("set", string(id)), slog.String("material", string(f.Material)))
	return viewOf(set), nil
}

// ActiveJob returns the job of a set that is pending or running; ok is false when there is none.
func (s *Service) ActiveJob(ctx context.Context, id shared.TicketSetID) (shared.JobID, bool, error) {
	j, err := s.jobs.LatestByKeyPrefix(ctx, JobType, keyPrefix(id))
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return "", false, nil
	case err != nil:
		return "", false, fmt.Errorf("%w: %w", ErrUnavailable, err)
	case j.Status().IsTerminal():
		return "", false, nil
	}
	return j.ID(), true, nil
}

// keyPrefix starts the key of every job of a set; a set id holds no ':'.
func keyPrefix(id shared.TicketSetID) string { return fmt.Sprintf("%s:%s:", JobType, id) }

// enqueue creates the job of the set under the first round of its key whose job did not end without a result, or
// returns the live or finished job of that round. The key is ticket_answers:<set>:run[:r<round>].
func (s *Service) enqueue(ctx context.Context, set *ticketset.Set) (Started, error) {
	payload, err := cvalues.NewTicketJob(set.ID())
	if err != nil {
		return Started{}, fmt.Errorf("%w: %w", ErrInvalidRequest, err)
	}
	for round := range runRounds {
		key := keyPrefix(set.ID()) + "run"
		if round > 0 {
			key += fmt.Sprintf(":r%d", round)
		}
		j, err := job.New(job.Spec{
			ID: s.ids.NewJobID(), Type: JobType, Key: key, Payload: payload.Payload(), MaxAttempts: s.maxAttempts,
		}, s.clock.Now())
		if err != nil {
			return Started{}, fmt.Errorf("%w: %w", ErrInvalidRequest, err)
		}
		id, created, err := s.jobs.Enqueue(ctx, j)
		if err != nil {
			return Started{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
		}
		if created {
			return Started{View: viewOf(set), Job: id, Created: true}, nil
		}
		existing, err := s.jobs.Get(ctx, id)
		if err != nil {
			return Started{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
		}
		if st := existing.Status(); !st.IsTerminal() || st == job.StatusCompleted {
			return Started{View: viewOf(set), Job: id}, nil
		}
	}
	return Started{}, ErrRetries
}

// change loads a set and applies a student's move to it; a move the set does not allow is ErrInvalidRequest.
func (s *Service) change(ctx context.Context, id shared.TicketSetID, fn func(*ticketset.Set) error) (*ticketset.Set, error) {
	for range saveAttempts {
		set, err := s.get(ctx, id)
		if err != nil {
			return nil, err
		}
		if err := fn(set); err != nil {
			if errors.Is(err, ticketset.ErrCostNotAccepted) {
				return nil, fmt.Errorf("%w: %w", ErrCostNotAccepted, err)
			}
			return nil, fmt.Errorf("%w: %w", ErrInvalidRequest, err)
		}
		err = s.sets.Save(ctx, set)
		if err == nil {
			return set, nil
		}
		if !errors.Is(err, shared.ErrVersionConflict) {
			return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
		}
		s.log.InfoContext(ctx, opSaveLost, slog.String("set", string(id)))
	}
	return nil, fmt.Errorf("%w: the set keeps changing", ErrUnavailable)
}

func (s *Service) get(ctx context.Context, id shared.TicketSetID) (*ticketset.Set, error) {
	if id.Validate() != nil {
		return nil, fmt.Errorf("%w: %w", ErrNotFound, shared.ErrNotFound)
	}
	set, err := s.sets.Get(ctx, id)
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return nil, fmt.Errorf("%w: %w", ErrNotFound, err)
	case err != nil:
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return set, nil
}

// subject returns the library subject sub; ok is false when it does not exist.
func (s *Service) subject(ctx context.Context, sub shared.SubjectID) (*subject.Subject, bool, error) {
	list, err := s.shelf.Subjects(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	for _, x := range list {
		if x.ID() == sub {
			return x, true, nil
		}
	}
	return nil, false, nil
}

// listError sorts a refusal of the list.
func listError(err error) error {
	switch {
	case errors.Is(err, ticketset.ErrNoTickets):
		return fmt.Errorf("%w: %w", ErrNoTickets, err)
	case errors.Is(err, ticketset.ErrTooManyTickets):
		return fmt.Errorf("%w: %w", ErrTooManyTickets, err)
	case errors.Is(err, ticketset.ErrListTooLong):
		return fmt.Errorf("%w: %w", ErrListTooLong, err)
	case errors.Is(err, ticketset.ErrTitleTooLong):
		return fmt.Errorf("%w: %w", ErrTicketTooLong, err)
	}
	return fmt.Errorf("%w: %w", ErrInvalidRequest, err)
}
