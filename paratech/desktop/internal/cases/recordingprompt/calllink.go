package recordingprompt

import (
	"context"
	"errors"
	"log/slog"

	"paratech/desktop/internal/domain/library/services/subjectmatch"
	"paratech/desktop/internal/domain/recording/prompt"
	"paratech/desktop/internal/domain/shared"
)

// opCallSubject names the failures of CallSubject in the log.
const opCallSubject = "recording prompt: call subject unread"

// CallSubject returns the library subject whose call link the popup of prompt id may offer («Подключиться», ADR-0032,
// TASK-370): only for the current lesson prompt, while it is offered, whose lesson's subject string resolves to exactly one subject, as on
// «Сегодня», and only while that subject keeps a link. Anything else, failures included, is "": the popup then simply
// offers no link. The link stays here; the popup opens it by the subject. Failures are logged without their cause: a
// storage error may carry row data, and a link a meeting password.
func (s *Service) CallSubject(ctx context.Context, id prompt.ID) shared.SubjectID {
	if s.subjects == nil || s.callLinks == nil {
		return ""
	}
	lessonID := s.promptLesson(id)
	if lessonID == "" {
		return ""
	}

	l, err := s.lessons.Get(ctx, lessonID)
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return "" // the lesson was removed from the schedule meanwhile
	case err != nil:
		s.callSubjectFailed(id, "lesson unread")
		return ""
	case l.Deleted():
		return ""
	}
	name := l.Content().Subject
	if name == "" {
		return ""
	}

	found, err := s.subjects.LookupSubjects(ctx, []string{name})
	if err != nil {
		s.callSubjectFailed(id, "subjects unread")
		return ""
	}
	r, ok := found[name]
	if !ok || r.Outcome != subjectmatch.Matched || r.Match.ID == "" {
		return ""
	}

	links, err := s.callLinks.CallLinks(ctx, []shared.SubjectID{r.Match.ID})
	if err != nil {
		s.callSubjectFailed(id, "call links unread")
		return ""
	}
	if links[r.Match.ID].IsZero() {
		return ""
	}
	return r.Match.ID
}

// promptLesson is the lesson of the current prompt when it is id and is an offered lesson prompt, else "".
func (s *Service) promptLesson(id prompt.ID) shared.LessonID {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.lookup(id)
	if err != nil || p.Kind() != prompt.KindLesson || p.State() != prompt.StateOffered {
		return ""
	}
	return p.Lesson().ID
}

func (s *Service) callSubjectFailed(id prompt.ID, result string) {
	s.log.Warn(opCallSubject, slog.String("prompt", id.String()), slog.String("result", result))
}
