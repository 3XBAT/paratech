package library

import (
	"context"

	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
)

// SetCallLink sets the link to the online class of a subject (ADR-0032, TASK-364). It is the student's action only: no
// agent tool reaches it. The «Сегодня» card saves it without knowing the subject's version, so the last save wins (as
// AddAlias). A bad link is ErrInvalidCallLink, checked before the subject is read; the same link again changes
// nothing. Nothing is logged: the link may hold a meeting password.
func (s *Service) SetCallLink(ctx context.Context, id shared.SubjectID, link string) (*subject.Subject, error) {
	if _, err := subject.ParseCallLink(link); err != nil {
		return nil, domainError(err)
	}
	return s.changeSubject(ctx, id, func(sub *subject.Subject) (bool, error) {
		changed, err := sub.SetCallLink(link, s.clock.Now())
		return changed, domainError(err)
	})
}

// ClearCallLink removes the link to the online class of a subject; a subject without one is returned unchanged.
func (s *Service) ClearCallLink(ctx context.Context, id shared.SubjectID) (*subject.Subject, error) {
	return s.changeSubject(ctx, id, func(sub *subject.Subject) (bool, error) {
		changed, err := sub.ClearCallLink(s.clock.Now())
		return changed, domainError(err)
	})
}

// CallLink returns the link to the online class of an active subject, read again at the click (ADR-0032 «Opening and
// copying»); the zero CallLink when it has none. A missing or removed subject is ErrSubjectNotFound.
func (s *Service) CallLink(ctx context.Context, id shared.SubjectID) (subject.CallLink, error) {
	sub, err := s.subject(ctx, id)
	if err != nil {
		return subject.CallLink{}, err
	}
	return sub.CallLink(), nil
}

// CallLinks returns the link of each active subject of ids that has one (the lesson cards of «Сегодня»).
func (s *Service) CallLinks(ctx context.Context, ids []shared.SubjectID) (map[shared.SubjectID]subject.CallLink, error) {
	out := map[shared.SubjectID]subject.CallLink{}
	if len(ids) == 0 {
		return out, nil
	}
	list, err := s.Subjects(ctx)
	if err != nil {
		return nil, err
	}
	want := make(map[shared.SubjectID]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	for _, sub := range list {
		if want[sub.ID()] && !sub.CallLink().IsZero() {
			out[sub.ID()] = sub.CallLink()
		}
	}
	return out, nil
}
