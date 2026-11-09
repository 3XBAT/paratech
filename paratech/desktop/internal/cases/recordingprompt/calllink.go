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

// startedPrompt is the prompt a recording was last started from (TASK-405): the popup page keeps its id and still asks
// for the link while that recording runs. lesson is empty for an ad-hoc prompt.
type startedPrompt struct {
	id        prompt.ID
	lesson    shared.LessonID
	recording shared.RecordingID
}

// CallSubject returns the library subject whose call link the popup of prompt id may offer («Подключиться», ADR-0032,
// TASK-370): for the current lesson prompt while it is offered, and for the lesson prompt the latest recording was
// started from while that recording runs (TASK-405), when the lesson's subject string resolves to exactly one
// subject, as on «Сегодня», and only while that subject keeps a link. Anything else, failures included, is "": the
// popup then simply offers no link. The link stays here; the popup opens it by the subject. Failures are logged
// without their cause: a storage error may carry row data, and a link a meeting password.
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

// promptLesson is the lesson of prompt id when it may offer a link: the current prompt, an offered lesson prompt, or
// the lesson prompt the latest recording was started from while that recording runs; else "".
func (s *Service) promptLesson(id prompt.ID) shared.LessonID {
	s.mu.Lock()
	if p, err := s.lookup(id); err == nil {
		defer s.mu.Unlock()
		if p.Kind() != prompt.KindLesson || p.State() != prompt.StateOffered {
			return ""
		}
		return p.Lesson().ID
	}
	started := s.started
	s.mu.Unlock()

	// Asked outside s.mu: the recording session has locks of its own.
	if s.running == nil || started.id != id || started.lesson == "" || !s.running.IsRunning(started.recording) {
		return ""
	}
	return started.lesson
}

func (s *Service) callSubjectFailed(id prompt.ID, result string) {
	s.log.Warn(opCallSubject, slog.String("prompt", id.String()), slog.String("result", result))
}
