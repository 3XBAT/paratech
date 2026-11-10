package library

import (
	"context"
	"errors"
	"fmt"
	"time"

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

// CreateSubjectWithCallLink creates a subject that keeps link as its call link («Создать предмет» with the link typed
// on a lesson card of «Сегодня»). The link is checked first: a bad one is ErrInvalidCallLink and nothing is created or
// erased (CreateSubject may erase an expired removed subject that holds the name). Then the subject is created with
// the link in one save, so there is never a subject without the link it was asked with. Otherwise it answers as
// CreateSubject. Nothing is logged: the link may hold a meeting password.
func (s *Service) CreateSubjectWithCallLink(ctx context.Context, req NewSubject, link string) (*subject.Subject, error) {
	if _, err := subject.ParseCallLink(link); err != nil {
		return nil, domainError(err)
	}
	return s.createSubject(ctx, req, func(sub *subject.Subject, now time.Time) error {
		_, err := sub.SetCallLink(link, now)
		return err
	})
}

// OpenCallLink opens the call link of an active subject («Подключиться»). The caller sends only the subject: the link
// is read again from storage at the click and handed to the CallOpener, which checks it once more (ADR-0032 «Opening
// and copying»). A subject without one is ErrNoCallLink, a missing or removed one ErrSubjectNotFound, a refusal or a
// failure of the opener ErrCallLinkNotOpened. Recording does not start.
func (s *Service) OpenCallLink(ctx context.Context, id shared.SubjectID) error {
	if s.calls == nil {
		return fmt.Errorf("%w: no opener is configured", ErrCallLinkNotOpened)
	}
	l, err := s.CallLink(ctx, id)
	if err != nil {
		return err
	}
	return s.openCallLink(ctx, l)
}

// OpenJoinLink opens the link a lesson of lessonKind of an active subject opens (subject.CallLinks.Join: the link of
// the lesson's kind, else the general one), read again from storage at the click. It answers as OpenCallLink:
// ErrNoCallLink when Join finds none.
func (s *Service) OpenJoinLink(ctx context.Context, id shared.SubjectID, lessonKind string) error {
	if s.calls == nil {
		return fmt.Errorf("%w: no opener is configured", ErrCallLinkNotOpened)
	}
	sub, err := s.subject(ctx, id)
	if err != nil {
		return err
	}
	l, _ := sub.CallLinks().Join(lessonKind)
	return s.openCallLink(ctx, l)
}

func (s *Service) openCallLink(ctx context.Context, l subject.CallLink) error {
	if l.IsZero() {
		return ErrNoCallLink
	}
	if err := s.calls.OpenSubjectCallLink(ctx, l); err != nil {
		return fmt.Errorf("%w: %w", ErrCallLinkNotOpened, err)
	}
	return nil
}

// SetCallLinkOf sets the link of kind k of a subject (ADR-0032 addendum 2026-11-10) as SetCallLink sets the general
// one: an unknown kind is ErrInvalidCallKind and a bad link ErrInvalidCallLink, both checked before the subject is
// read; no version check, the last save wins; the same link again changes nothing. Nothing is logged.
func (s *Service) SetCallLinkOf(ctx context.Context, id shared.SubjectID, k subject.CallKind,
	link string) (*subject.Subject, error) {
	if !k.Valid() {
		return nil, ErrInvalidCallKind
	}
	if _, err := subject.ParseCallLink(link); err != nil {
		return nil, domainError(err)
	}
	return s.changeSubject(ctx, id, func(sub *subject.Subject) (bool, error) {
		changed, err := sub.SetCallLinkOf(k, link, s.clock.Now())
		return changed, callLinkError(err)
	})
}

// ClearCallLinkOf removes the link of kind k of a subject; an unknown kind is ErrInvalidCallKind (before the subject
// is read), a kind without a link returns the subject unchanged.
func (s *Service) ClearCallLinkOf(ctx context.Context, id shared.SubjectID, k subject.CallKind) (*subject.Subject, error) {
	if !k.Valid() {
		return nil, ErrInvalidCallKind
	}
	return s.changeSubject(ctx, id, func(sub *subject.Subject) (bool, error) {
		changed, err := sub.ClearCallLinkOf(k, s.clock.Now())
		return changed, callLinkError(err)
	})
}

// callLinkError is domainError with the call kind as well.
func callLinkError(err error) error {
	if errors.Is(err, subject.ErrInvalidCallKind) {
		return fmt.Errorf("%w: %w", ErrInvalidCallKind, err)
	}
	return domainError(err)
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

// CallLinkSets returns the links (general and per kind) of each active subject of ids that has at least one (the
// lesson cards of «Сегодня» and the popup).
func (s *Service) CallLinkSets(ctx context.Context, ids []shared.SubjectID) (map[shared.SubjectID]subject.CallLinks, error) {
	out := map[shared.SubjectID]subject.CallLinks{}
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
		if want[sub.ID()] && !sub.CallLinks().IsZero() {
			out[sub.ID()] = sub.CallLinks()
		}
	}
	return out, nil
}
