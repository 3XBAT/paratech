package schedulesync

import (
	"context"
	"errors"
	"fmt"

	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/schedule/services/importplan"
	"paratech/desktop/internal/domain/shared"
)

// PreviewImport parses and validates a file and shows what a commit would change (PRD section 11.2, AC-16). It
// stores nothing. Row errors are part of the preview, not an error; an unreadable file is an error wrapping
// importplan.ErrUnreadableFile.
func (s *Service) PreviewImport(ctx context.Context, req ImportRequest) (Preview, error) {
	parsed, err := s.parse(ctx, req.Source, req.Data)
	if err != nil {
		return Preview{}, err
	}
	current, err := s.lessons.ListLive(ctx)
	if err != nil {
		return Preview{}, fmt.Errorf("schedulesync: load schedule: %w", err)
	}
	plan, err := importplan.Build(req.Source, parsed, current)
	if err != nil {
		return Preview{}, err
	}
	return Preview{Plan: plan, ConfirmToken: plan.Fingerprint}, nil
}

// CommitImport writes a confirmed preview in one transaction (PRD sections 11.2 and 14 case 9, AC-16). The plan is
// rebuilt from the same bytes against the schedule as it is now; if it has errors (ErrInvalidImport) or is not the
// confirmed one (ErrStalePreview), nothing is written. The calendar sync job is not enqueued yet (TASK-051): the
// written lessons are pending by state and the job finds them.
func (s *Service) CommitImport(ctx context.Context, req CommitRequest) (CommitResult, error) {
	if req.ConfirmToken == "" {
		return CommitResult{}, ErrNotConfirmed
	}
	parsed, err := s.parse(ctx, req.Source, req.Data)
	if err != nil {
		return CommitResult{}, err
	}

	var res CommitResult
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		current, err := s.lessons.ListLive(ctx)
		if err != nil {
			return fmt.Errorf("schedulesync: load schedule: %w", err)
		}
		plan, err := importplan.Build(req.Source, parsed, current)
		switch {
		case err != nil:
			return err
		case !plan.Valid():
			return fmt.Errorf("%w: %d row errors", ErrInvalidImport, len(plan.Errors))
		case plan.Fingerprint != req.ConfirmToken:
			return ErrStalePreview
		case !plan.HasChanges():
			return nil
		}

		upserts, deleted, err := s.applyPlan(plan, current)
		if err != nil {
			return err
		}
		if err := s.lessons.ApplyChanges(ctx, upserts, deleted); err != nil {
			return fmt.Errorf("schedulesync: commit import: %w", err)
		}
		res = CommitResult{Added: len(plan.Added), Changed: len(plan.Changed), Removed: len(plan.Removed)}
		return nil
	})
	if errors.Is(err, shared.ErrVersionConflict) {
		return CommitResult{}, fmt.Errorf("%w: %w", ErrStalePreview, err)
	}
	if err != nil {
		return CommitResult{}, err
	}
	return res, nil
}

func (s *Service) parse(ctx context.Context, src importplan.Source, data []byte) (importplan.Parsed, error) {
	p, ok := s.parsers[src]
	if !ok {
		return importplan.Parsed{}, fmt.Errorf("%w: %q", ErrUnsupportedSource, string(src))
	}
	parsed, err := p.Parse(ctx, data)
	if err != nil {
		return importplan.Parsed{}, fmt.Errorf("schedulesync: parse %s file: %w", src, err)
	}
	return parsed, nil
}

// applyPlan turns the plan into aggregate changes; the plan was built from current, so every lesson it names is there.
func (s *Service) applyPlan(plan importplan.Plan, current []*lesson.Lesson) (upserts, deleted []*lesson.Lesson, err error) {
	byID := make(map[shared.LessonID]*lesson.Lesson, len(current))
	for _, l := range current {
		byID[l.ID()] = l
	}
	now := s.clock.Now()

	for _, a := range plan.Added {
		l, err := lesson.New(lesson.Spec{ID: s.ids.NewLessonID(), Content: a.Content, SourceUID: a.UID}, now)
		if err != nil {
			return nil, nil, fmt.Errorf("schedulesync: line %d: %w", a.Line, err)
		}
		upserts = append(upserts, l)
	}
	for _, c := range plan.Changed {
		l := byID[c.Lesson]
		if _, err := l.ChangeContent(c.After, now); err != nil {
			return nil, nil, fmt.Errorf("schedulesync: line %d: %w", c.Line, err)
		}
		upserts = append(upserts, l)
	}
	for _, r := range plan.Removed {
		l := byID[r.Lesson]
		if err := l.MarkDeleted(now); err != nil {
			return nil, nil, fmt.Errorf("schedulesync: remove %s: %w", r.Lesson, err)
		}
		deleted = append(deleted, l)
	}
	return upserts, deleted, nil
}
