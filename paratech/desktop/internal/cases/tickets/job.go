package tickets

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"paratech/desktop/internal/domain/coursework/answer"
	"paratech/desktop/internal/domain/coursework/services/homeworkdoc"
	"paratech/desktop/internal/domain/coursework/ticketset"
	cvalues "paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/jobs/services/backoff"
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/shared"
)

// Log lines of a run; none holds a title or an answer.
const (
	opStep     = "tickets: run step failed"
	opRefused  = "tickets: ticket answer refused, recorded as failed"
	opAnswered = "tickets: ticket answered"
	opStale    = "tickets: step result dropped, the step is not due any more"
	opStopped  = "tickets: run stopped"
	opFiled    = "tickets: answers filed"
	opNoAnswer = "tickets: a stored answer is gone, the ticket is shown as not covered"
)

// Handle runs one step of a ticket_answers job (jobrunner.Handler): the first ticket without its result, or the file.
// After each ticket the job yields (job.ErrContinueLater, «Билет N из M»), so other jobs run in between; a stored
// result is never asked again, and a repeated call of the same ticket is answered from the stored answer or replayed
// by the Hub free (answering's key). A refused answer is recorded as failed for that ticket and the run goes on; no
// connection or a used-up quota leaves the ticket due and lets the runner wait (the attempt given back). The answers
// are data (hard rule 3): only rendered into the file. A set gone, cancelled or finished ends the job.
func (s *Service) Handle(ctx context.Context, snap job.Snapshot, report func(ctx context.Context, pr job.Progress) error) error {
	if report == nil {
		report = func(context.Context, job.Progress) error { return nil }
	}
	if snap.Type() != JobType {
		return s.permanent(snap, ErrJobPayload, nil)
	}
	p, err := cvalues.ParseTicketJob(snap.Payload())
	if err != nil {
		return s.permanent(snap, ErrJobPayload, err)
	}
	set, err := s.sets.Get(ctx, p.Set())
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return s.permanent(snap, ErrJobGone, err)
	case err != nil:
		return s.retry(snap, ErrJobStorage, err)
	}
	switch set.Status() {
	case ticketset.StatusDone, ticketset.StatusCancelled, ticketset.StatusFailed:
		return nil
	case ticketset.StatusConfirmed:
		if set, err = s.saveStep(ctx, set, func(set *ticketset.Set) error { return set.Start(s.clock.Now()) }); err != nil {
			return s.retry(snap, ErrJobStorage, err)
		}
		if set.Status() != ticketset.StatusAnswering {
			return nil // cancelled meanwhile
		}
	case ticketset.StatusAnswering:
	default:
		return s.permanent(snap, ErrJobGone, fmt.Errorf("set in %s", set.Status()))
	}

	switch st := set.Next(); st.Kind {
	case ticketset.StepTicket:
		return s.answerTicket(ctx, snap, report, set, st)
	case ticketset.StepAssemble:
		return s.assemble(ctx, snap, report, set)
	}
	return nil
}

// answerTicket asks the answer of ticket st and records its result.
func (s *Service) answerTicket(ctx context.Context, snap job.Snapshot, report func(context.Context, job.Progress) error,
	set *ticketset.Set, st ticketset.Step) error {
	var title string
	for _, t := range set.Tickets() {
		if t.Number == st.Ticket {
			title = t.Title
		}
	}
	a, err := s.answerer.Ask(ctx, title, set.Subject())
	var r ticketset.Result
	switch {
	case err == nil:
		r = ticketset.Result{Coverage: ticketset.Classify(a)}
		if a != nil {
			r.Answer = a.ID()
		}
	case ctx.Err() != nil:
		return fail(ErrJobStopped, ctx.Err())
	case errors.Is(err, answer.ErrInvalidAnswer), errors.Is(err, answer.ErrInvalidQuestion):
		// The same question repeats the refusal free: asking again would not help.
		s.log.WarnContext(ctx, opRefused, slog.String("set", string(set.ID())), slog.Int("ticket", st.Ticket))
		r = ticketset.Result{Coverage: ticketset.CoverageFailed}
	case errors.Is(err, shared.ErrQuotaExceeded), errors.Is(err, shared.ErrNetworkUnavailable):
		return s.retry(snap, ErrJobAnswer, err)
	default:
		return s.retry(snap, ErrJobStorage, err)
	}

	// The unchanged progress first: a job cancelled during the call stores nothing.
	if err := s.progress(ctx, snap, report, set); err != nil {
		return err
	}
	got, err := s.saveStep(ctx, set, func(set *ticketset.Set) error { return set.Record(st, r, s.clock.Now()) })
	if err != nil {
		return s.retry(snap, ErrJobStorage, err)
	}
	if got.Status() != ticketset.StatusAnswering {
		return nil // cancelled meanwhile
	}
	s.log.InfoContext(ctx, opAnswered, slog.String("set", string(set.ID())), slog.Int("ticket", st.Ticket),
		slog.String("coverage", string(r.Coverage)))
	if err := s.progress(ctx, snap, report, got); err != nil {
		return err
	}
	return job.ErrContinueLater
}

// assemble builds the file of the answered tickets, files it into the subject's «Билеты» and marks the set done. The
// same answers give the same bytes, so a repeated step finds the material it filed.
func (s *Service) assemble(ctx context.Context, snap job.Snapshot, report func(context.Context, job.Progress) error,
	set *ticketset.Set) error {
	prefs, err := s.profiles.Load(ctx)
	if err != nil {
		return s.retry(snap, ErrJobStorage, err)
	}
	sub, _, err := s.subject(ctx, set.Subject())
	if err != nil {
		return s.retry(snap, ErrJobStorage, err)
	}
	subjectName := ""
	if sub != nil {
		subjectName = sub.Name().String()
	}
	entries, err := s.entries(ctx, set)
	if err != nil {
		return s.retry(snap, ErrJobStorage, err)
	}
	p := prefs.Profile().Fields()
	now := s.clock.Now()
	doc := homeworkdoc.BuildTickets(homeworkdoc.TicketsInput{
		Student: homeworkdoc.Student{
			FullName: p.FullName, Group: p.Group, University: p.University, Faculty: p.Faculty, City: p.City,
		},
		Subject: subjectName, Year: now.Year(), Tickets: entries,
	})
	if err := doc.Validate(); err != nil {
		return s.failRun(ctx, snap, set, err)
	}
	content, err := s.renderer.Render(ctx, doc)
	if err != nil {
		return s.failRun(ctx, snap, set, err)
	}
	if err := s.progress(ctx, snap, report, set); err != nil {
		return err
	}
	m, err := s.shelf.FileTickets(ctx, set.Subject(), homeworkdoc.TicketsFileName(subjectName), content)
	if err != nil {
		return s.retry(snap, ErrJobStorage, err)
	}
	ref, err := material.BlobRef(m.Digest())
	if err != nil {
		return s.permanent(snap, ErrJobStorage, err)
	}
	got, err := s.saveStep(ctx, set, func(set *ticketset.Set) error {
		return set.Finish(ticketset.File{Material: m.ID(), Artifact: ref.String()}, s.clock.Now())
	})
	if err != nil {
		return s.retry(snap, ErrJobStorage, err)
	}
	if got.Status() != ticketset.StatusDone {
		// Cancelled while the file was built: the file just filed is no file of the set.
		if err := s.unfile(ctx, got, m); err != nil {
			return s.retry(snap, ErrJobStorage, err)
		}
		return nil
	}
	s.log.InfoContext(ctx, opFiled, slog.String("set", string(set.ID())), slog.String("material", string(m.ID())))
	return nil
}

// entries are the tickets as the file shows them, from their stored answers. An answer gone since (a later purge) is
// shown as not covered: the file never names a lesson the store does not hold.
func (s *Service) entries(ctx context.Context, set *ticketset.Set) ([]homeworkdoc.TicketEntry, error) {
	loc := s.clock.Now().Location()
	var out []homeworkdoc.TicketEntry
	for _, t := range set.Tickets() {
		e := homeworkdoc.TicketEntry{Number: t.Number, Title: t.Title, Coverage: ticketset.CoverageNotCovered}
		if t.Result != nil {
			e.Coverage = t.Result.Coverage
		}
		if t.Result != nil && t.Result.Answer != "" {
			a, err := s.answerer.Answer(ctx, t.Result.Answer)
			switch {
			case errors.Is(err, shared.ErrNotFound):
				s.log.WarnContext(ctx, opNoAnswer, slog.String("set", string(set.ID())), slog.Int("ticket", t.Number))
				e.Coverage = ticketset.CoverageNotCovered
			case err != nil:
				return nil, err
			default:
				e.Claims, e.Missing = claimsOf(a, loc), a.Missing()
			}
		}
		out = append(out, e)
	}
	return out, nil
}

// claimsOf are the claims of an answer with the places their checked citations open, dated in the student's zone.
func claimsOf(a *answer.Answer, loc *time.Location) []homeworkdoc.TicketClaim {
	sources := a.Sources()
	var out []homeworkdoc.TicketClaim
	for _, c := range a.Claims() {
		tc := homeworkdoc.TicketClaim{Text: c.Text}
		for _, cite := range c.Cites {
			if cite.Source < 0 || cite.Source >= len(sources) {
				continue
			}
			src := sources[cite.Source]
			date := src.StartedAt
			if !date.IsZero() {
				date = date.In(loc)
			}
			tc.Sources = append(tc.Sources, homeworkdoc.TicketSource{Lesson: src.Title, Date: date, Section: src.Section})
		}
		out = append(out, tc)
	}
	return out
}

// failRun ends a run whose file cannot be built: the set fails, the job fails at once.
func (s *Service) failRun(ctx context.Context, snap job.Snapshot, set *ticketset.Set, cause error) error {
	if _, err := s.saveStep(ctx, set, func(set *ticketset.Set) error { return set.Fail(s.clock.Now()) }); err != nil {
		return s.retry(snap, ErrJobStorage, err)
	}
	return s.permanent(snap, ErrJobFile, cause)
}

// unfile takes a file of the set back from «Билеты» to «Входящие»; a file the student moved is left alone.
func (s *Service) unfile(ctx context.Context, set *ticketset.Set, m *material.Material) error {
	if !inTickets(set, m) {
		return nil
	}
	_, err := s.shelf.MoveToInbox(ctx, m.ID())
	return err
}

// saveStep applies fn to set and saves it, again on the stored set after a lost save. A step that is not due any
// more (the student cancelled meanwhile) is dropped: the stored set is returned as it is.
func (s *Service) saveStep(ctx context.Context, set *ticketset.Set, fn func(*ticketset.Set) error) (*ticketset.Set, error) {
	for range saveAttempts {
		if err := fn(set); err != nil {
			if errors.Is(err, ticketset.ErrStaleStep) || errors.Is(err, ticketset.ErrInvalidTransition) {
				s.log.InfoContext(ctx, opStale, slog.String("set", string(set.ID())))
				return set, nil
			}
			return nil, err
		}
		err := s.sets.Save(ctx, set)
		if err == nil {
			return set, nil
		}
		if !errors.Is(err, shared.ErrVersionConflict) {
			return nil, err
		}
		if set, err = s.sets.Get(ctx, set.ID()); err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("%w: the set keeps changing", ErrUnavailable)
}

// progress reports how many tickets are answered as the checkpoint «N/M» (cvalues.TicketCheckpoint); the runner
// refuses it when the job was cancelled or changed meanwhile.
func (s *Service) progress(ctx context.Context, snap job.Snapshot, report func(context.Context, job.Progress) error,
	set *ticketset.Set) error {
	done, total := set.Progress()
	pr, err := job.NewProgress(done*100/(total+1), cvalues.TicketCheckpoint(done, total)) // the file is one more step
	if err != nil {
		return s.permanent(snap, ErrJobStorage, err)
	}
	if err := report(ctx, pr); err != nil {
		s.log.InfoContext(ctx, opStopped, slog.String("job", snap.ID().String()), slog.Any("reason", err))
		return fail(ErrJobStopped, err)
	}
	return nil
}

// permanent fails the job at once; the cause goes to the log only.
func (s *Service) permanent(snap job.Snapshot, msg, cause error) error {
	s.warnStep(snap, msg, cause)
	return backoff.Permanent(fail(msg, cause))
}

// retry leaves the decision to the runner: a wait for the network or the quota, else a retry with backoff.
func (s *Service) retry(snap job.Snapshot, msg, cause error) error {
	s.warnStep(snap, msg, cause)
	return fail(msg, cause)
}

func (s *Service) warnStep(snap job.Snapshot, msg, cause error) {
	attrs := []any{slog.String("job", snap.ID().String()), slog.String("error", msg.Error())}
	if cause != nil {
		attrs = append(attrs, slog.Any("cause", cause))
	}
	s.log.Warn(opStep, attrs...)
}
