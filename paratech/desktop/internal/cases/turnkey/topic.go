package turnkey

import (
	"context"
	"fmt"

	"paratech/desktop/internal/domain/coursework/work"
	"paratech/desktop/internal/domain/shared"
)

// TriageTopic starts a work on a topic without an assignment (TASK-187) and asks the reader, as Triage does: the topic
// is the assignment text, data and never an instruction (ADR-0022). request is the key of the explicit action that
// asks for it (the agent's call, the student's press): the same key gets its work back, a new key a new work even for
// the same text. subject is the library subject whose «ДЗ» gets the file ("" none: «Входящие»). The work is written
// and exported like any other, never sent to a teacher (work.ErrNoAssignment). Errors: ErrInvalidRequest for a topic
// work.NewTopic refuses or a key already used for another topic, else those of Triage.
func (s *Service) TriageTopic(ctx context.Context, request, text string, subject shared.SubjectID) (Started, error) {
	t, err := work.NewTopic(request, text, subject)
	if err != nil {
		return Started{}, fmt.Errorf("%w: %w", ErrInvalidRequest, err)
	}
	return s.Triage(ctx, work.Source{Topic: t})
}

// sameSource returns the active work found for src. A topic is found by its request key only: the same key with
// another text or subject is a caller's mistake, never a merge of two requests.
func sameSource(w *work.Work, src work.Source) (*work.Work, bool, error) {
	if w.Source() != src {
		return nil, false, fmt.Errorf("%w: the request key names another topic", ErrInvalidRequest)
	}
	return w, false, nil
}

// topicSnapshot is what the paper of a topic work takes: the topic as its title and the subject the student chose,
// named as the library names it now; a subject gone since files the paper into «Входящие».
func (s *Service) topicSnapshot(ctx context.Context, t work.Topic) (work.Snapshot, error) {
	snap := work.Snapshot{Title: t.Text()}
	if t.Subject() != "" {
		list, err := s.writing.Shelf.Subjects(ctx)
		if err != nil {
			return work.Snapshot{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
		}
		for _, sub := range list {
			if sub.ID() == t.Subject() {
				snap.Subject, snap.SubjectName = sub.ID(), sub.Name().String()
			}
		}
	}
	return cleanSnapshot(snap), nil
}
