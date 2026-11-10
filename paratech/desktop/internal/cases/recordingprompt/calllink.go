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
// TASK-370) and the kind of its lesson: for the current lesson prompt while it is offered, and for the lesson prompt
// the latest recording was started from while that recording runs (TASK-405), when the lesson's subject string
// resolves to exactly one subject, as on «Сегодня», and only while the lesson's kind finds a link on that subject
// (subject.CallLinks.Join: the kind's link, else the general one; addendum 2026-11-10). Anything else, failures
// included, is the zero Call: the popup then simply offers no link. The link stays here; the popup opens it by the
// subject and the kind. Failures are logged without their cause: a storage error may carry row data, and a link a
// meeting password.
func (s *Service) CallSubject(ctx context.Context, id prompt.ID) Call {
	if s.subjects == nil || s.callLinks == nil {
		return Call{}
	}
	lessonID := s.promptLesson(id)
	if lessonID == "" {
		return Call{}
	}

	l, err := s.lessons.Get(ctx, lessonID)
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return Call{} // the lesson was removed from the schedule meanwhile
	case err != nil:
		s.callSubjectFailed(id, "lesson unread")
		return Call{}
	case l.Deleted():
		return Call{}
	}
	name := l.Content().Subject
	if name == "" {
		return Call{}
	}

	found, err := s.subjects.LookupSubjects(ctx, []string{name})
	if err != nil {
		s.callSubjectFailed(id, "subjects unread")
		return Call{}
	}
	r, ok := found[name]
	if !ok || r.Outcome != subjectmatch.Matched || r.Match.ID == "" {
		return Call{}
	}

	links, err := s.callLinks.CallLinkSets(ctx, []shared.SubjectID{r.Match.ID})
	if err != nil {
		s.callSubjectFailed(id, "call links unread")
		return Call{}
	}
	kind := l.Content().Kind.String()
	if link, _ := links[r.Match.ID].Join(kind); link.IsZero() {
		return Call{}
	}
	return Call{Subject: r.Match.ID, LessonKind: kind}
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
