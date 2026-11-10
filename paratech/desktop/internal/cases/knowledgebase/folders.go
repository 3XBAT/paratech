package knowledgebase

import (
	"context"
	"fmt"

	kb "paratech/desktop/internal/domain/coursework/services/knowledgebase"
	"paratech/desktop/internal/domain/library/services/filing"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
)

// FolderLessons lists the recorded lessons of library subject sub with the system folder of sub each shows in
// (ADR-0034 amendment 2026-11-10): the lessons Lessons lists that are matched to sub, newest first, each in the
// folder filing.NoteFolder names for its kind, the user's correction winning. A lesson whose folder sub does not have
// is left out. Nothing is stored. An id that does not validate is ErrInvalidRequest; an unknown or removed subject
// gives an empty list.
func (s *Service) FolderLessons(ctx context.Context, sub shared.SubjectID) ([]FolderLesson, error) {
	if err := sub.Validate(); err != nil {
		return nil, ErrInvalidRequest
	}
	out := []FolderLesson{}
	if s.subjects == nil {
		return out, nil // no library wired: no subject has folders
	}

	subs, err := s.subjects.Subjects(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	var lib *subject.Subject
	for _, x := range subs {
		if x.ID() == sub && x.Active() {
			lib = x
		}
	}
	if lib == nil {
		return out, nil
	}

	lessons, err := s.Lessons(ctx, kb.LibraryKey(sub))
	if err != nil {
		return nil, err
	}
	for _, l := range lessons {
		if l.LibraryMatch() != kb.LibraryMatched || l.Library.ID != sub {
			continue
		}
		system := filing.NoteFolder(l.Kind)
		folder, ok := filing.Target(sub, lib, system)
		if !ok {
			continue
		}
		out = append(out, FolderLesson{Folder: folder, System: system, Lesson: l})
	}
	return out, nil
}
