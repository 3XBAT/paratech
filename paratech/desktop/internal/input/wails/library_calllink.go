package wails

import (
	"context"
	"errors"

	"paratech/desktop/internal/cases/library"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
)

// Operation names of the log.
const (
	opLibrarySetCallLink    = "ui: set subject call link"
	opLibraryClearCallLink  = "ui: clear subject call link"
	opLibraryOpenCallLink   = "ui: open subject call link"
	opLibraryCreateCallLink = "ui: create subject with a call link"
)

// Problems of the call link calls as the page reads them. The page owns the Russian texts.
const (
	callLinkInvalid  = "invalid" // not an http or https link the app may open
	callLinkNone     = "no_link" // OpenSubjectCallLink: the subject has no link (any more)
	callLinkNotFound = "not_found"
	callLinkFailed   = "failed"

	// CreateSubjectWithCallLink only; "invalid" there is the link, and then nothing is created.
	callLinkInvalidName = "invalid_name" // the name is empty or too long
	callLinkNameTaken   = "name_taken"   // another subject has the name; RemovedNameTaken when a removed one has it
)

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

// LibraryCreateWithCallLink answers CreateSubjectWithCallLink: Problem "" with Subject, the new subject with its link;
// or a problem with Subject null and nothing created: "invalid" (the link), "invalid_name", "name_taken",
// "removed_name_taken" (a removed subject holds the name: «Вернуть» is RestoreSubject(RemovedSubjectID,
// RemovedVersion)) or "failed".
type LibraryCreateWithCallLink struct {
	Problem          string          `json:"problem"`
	Subject          *LibrarySubject `json:"subject"`
	RemovedSubjectID string          `json:"removedSubjectId"`
	RemovedVersion   int64           `json:"removedVersion"`
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

// CreateSubjectWithCallLink creates a subject of the current semester that keeps link as its call link (a lesson card
// of «Сегодня» whose subject string matches no subject). The link is checked before anything is created, and the
// subject is stored with it in one save: either both exist or nothing does. The link is never logged.
func (s *LibraryService) CreateSubjectWithCallLink(ctx context.Context, name, link string) LibraryCreateWithCallLink {
	sub, err := s.uc.CreateSubjectWithCallLink(ctx, library.NewSubject{Name: name}, link)
	if err == nil {
		out := librarySubject(sub)
		return LibraryCreateWithCallLink{Subject: &out}
	}

	var gone *library.RemovedNameError
	switch {
	case errors.As(err, &gone):
		var named *LibraryRemovedNameError
		if errors.As(s.removedName(ctx, gone.Subject), &named) {
			return LibraryCreateWithCallLink{
				Problem: RemovedNameTaken, RemovedSubjectID: named.SubjectID, RemovedVersion: named.Version,
			}
		}
		// The removed subject was restored or purged meanwhile, or could not be read: a repeat answers anew.
		return LibraryCreateWithCallLink{Problem: callLinkFailed}
	case errors.Is(err, library.ErrNameTaken):
		s.log.Info(opLibraryCreateCallLink, "result", callLinkNameTaken)
		return LibraryCreateWithCallLink{Problem: callLinkNameTaken}
	case errors.Is(err, library.ErrInvalidName):
		s.log.Info(opLibraryCreateCallLink, "result", callLinkInvalidName)
		return LibraryCreateWithCallLink{Problem: callLinkInvalidName}
	}
	problem := s.callLinkProblem(opLibraryCreateCallLink, err)
	if problem == callLinkNotFound { // no subject is looked up here: anything of the kind is a fault
		problem = callLinkFailed
	}
	return LibraryCreateWithCallLink{Problem: problem}
}

// OpenSubjectCallLink opens the link kept on a subject («Подключиться»). The page sends only the subject; the use case
// reads the link again and the opener checks it once more (ADR-0032 «Opening and copying»). Recording does not start.
func (s *LibraryService) OpenSubjectCallLink(ctx context.Context, subjectID string) LibraryCallLinkOpen {
	err := s.uc.OpenCallLink(ctx, shared.SubjectID(subjectID))
	switch {
	case err == nil:
		return LibraryCallLinkOpen{}
	case errors.Is(err, library.ErrNoCallLink):
		s.log.Info(opLibraryOpenCallLink, "result", callLinkNone)
		return LibraryCallLinkOpen{Problem: callLinkNone}
	case errors.Is(err, library.ErrCallLinkNotOpened):
		// The opener's errors name the rule or the step, never the link.
		s.log.Error(opLibraryOpenCallLink, "result", callLinkFailed, "err", err)
		return LibraryCallLinkOpen{Problem: callLinkFailed}
	}
	return LibraryCallLinkOpen{Problem: s.callLinkProblem(opLibraryOpenCallLink, err)}
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
