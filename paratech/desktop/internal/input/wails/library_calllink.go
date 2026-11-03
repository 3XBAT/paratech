package wails

import (
	"context"
	"errors"

	"paratech/desktop/internal/cases/library"
	"paratech/desktop/internal/domain/library/subject"
	schedvalues "paratech/desktop/internal/domain/schedule/values"
	"paratech/desktop/internal/domain/shared"
)

// Operation names of the log.
const (
	opLibrarySetCallLink   = "ui: set subject call link"
	opLibraryClearCallLink = "ui: clear subject call link"
	opLibraryOpenCallLink  = "ui: open subject call link"
)

// Problems of the call link calls as the page reads them. The page owns the Russian texts.
const (
	callLinkInvalid  = "invalid" // not an http or https link the app may open
	callLinkNone     = "no_link" // OpenSubjectCallLink: the subject has no link (any more)
	callLinkNotFound = "not_found"
	callLinkFailed   = "failed"
)

// CallLinkOpener opens a call link outside the app (ARCH-42; the app wires adapters/weblink, which re-checks it and
// opens it through a .url file and explorer.exe, ADR-0032). The page never sends the URL to open.
type CallLinkOpener interface {
	OpenCallLink(ctx context.Context, l schedvalues.CallLink) error
}

// LibraryCallLink is the link to the online class kept on a subject (ADR-0032, TASK-364): the student's own typed
// link in canonical form, and its host, shown in full. Plain text only.
type LibraryCallLink struct {
	Link string `json:"link"`
	Host string `json:"host"`
}

// LibraryCallLinkResult answers SetSubjectCallLink and ClearSubjectCallLink: Problem "" with Subject set, or a problem
// code ("invalid", "not_found", "failed") with Subject null.
type LibraryCallLinkResult struct {
	Problem string          `json:"problem"`
	Subject *LibrarySubject `json:"subject"`
}

// LibraryCallLinkOpen answers OpenSubjectCallLink: Problem "" when the link was handed to the system, else
// "no_link", "not_found" or "failed".
type LibraryCallLinkOpen struct {
	Problem string `json:"problem"`
}

// withCallLinkOpener adds «Подключиться».
func (s *LibraryService) withCallLinkOpener(o CallLinkOpener) *LibraryService {
	s.callLinks = o
	return s
}

// SetSubjectCallLink keeps link as the link to the online class of a subject (the subject page, and «Добавить ссылку»
// of a lesson card on «Сегодня»). It is the student's action only; nothing is opened or sent, and the link is never
// logged.
func (s *LibraryService) SetSubjectCallLink(ctx context.Context, subjectID, link string) LibraryCallLinkResult {
	sub, err := s.uc.SetCallLink(ctx, shared.SubjectID(subjectID), link)
	if err != nil {
		return LibraryCallLinkResult{Problem: s.callLinkProblem(opLibrarySetCallLink, err)}
	}
	out := librarySubject(sub)
	return LibraryCallLinkResult{Subject: &out}
}

// ClearSubjectCallLink removes the link to the online class of a subject.
func (s *LibraryService) ClearSubjectCallLink(ctx context.Context, subjectID string) LibraryCallLinkResult {
	sub, err := s.uc.ClearCallLink(ctx, shared.SubjectID(subjectID))
	if err != nil {
		return LibraryCallLinkResult{Problem: s.callLinkProblem(opLibraryClearCallLink, err)}
	}
	out := librarySubject(sub)
	return LibraryCallLinkResult{Subject: &out}
}

// OpenSubjectCallLink opens the link kept on a subject («Подключиться»). The page sends only the subject: the link is
// read again here and opened only when it passes the call link rule of the schedule, the one the opener re-checks
// (ADR-0032 «Opening and copying»). Recording does not start.
func (s *LibraryService) OpenSubjectCallLink(ctx context.Context, subjectID string) LibraryCallLinkOpen {
	if s.callLinks == nil {
		s.log.Error(opLibraryOpenCallLink, "result", callLinkFailed, "cause", "no opener")
		return LibraryCallLinkOpen{Problem: callLinkFailed}
	}
	l, err := s.uc.CallLink(ctx, shared.SubjectID(subjectID))
	if err != nil {
		return LibraryCallLinkOpen{Problem: s.callLinkProblem(opLibraryOpenCallLink, err)}
	}
	if l.IsZero() {
		s.log.Info(opLibraryOpenCallLink, "result", callLinkNone)
		return LibraryCallLinkOpen{Problem: callLinkNone}
	}
	open, err := schedvalues.ParseCallLink(l.String())
	if err != nil || open.String() != l.String() {
		s.log.Error(opLibraryOpenCallLink, "result", callLinkFailed, "cause", "the call link rules differ")
		return LibraryCallLinkOpen{Problem: callLinkFailed}
	}
	if err := s.callLinks.OpenCallLink(ctx, open); err != nil {
		// The opener's errors name the rule or the step, never the link.
		s.log.Error(opLibraryOpenCallLink, "result", callLinkFailed, "err", err)
		return LibraryCallLinkOpen{Problem: callLinkFailed}
	}
	return LibraryCallLinkOpen{}
}

// callLinkProblem maps a use case error to a code of the page. The cause is never logged: a storage error may carry
// row data, and the link may hold a meeting password.
func (s *LibraryService) callLinkProblem(op string, err error) string {
	switch {
	case errors.Is(err, library.ErrInvalidCallLink):
		return callLinkInvalid
	case errors.Is(err, library.ErrSubjectNotFound), errors.Is(err, library.ErrInvalidRequest):
		s.log.Info(op, "result", callLinkNotFound)
		return callLinkNotFound
	case errors.Is(err, library.ErrUnavailable), errors.Is(err, library.ErrBusy):
		s.log.Error(op, "result", callLinkFailed, "cause", "local storage")
		return callLinkFailed
	}
	s.log.Error(op, "result", callLinkFailed, "cause", "unexpected")
	return callLinkFailed
}

// libraryCallLink is nil for no link.
func libraryCallLink(l subject.CallLink) *LibraryCallLink {
	if l.IsZero() {
		return nil
	}
	return &LibraryCallLink{Link: l.String(), Host: l.Host()}
}
