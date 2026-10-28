package library

import (
	"context"
	"fmt"

	"paratech/desktop/internal/domain/library/services/placement"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
)

// Placement is where a file of «Входящие» may go (PlacementHint). Kind placement.KindExisting: Subject and its Folder
// (zero only if the subject had no «Материалы», which its invariants rule out). placement.KindCreate: CreateName, the
// subject to create as the schedule spells it, and FolderName of FolderKind. placement.KindNone: nothing.
type Placement struct {
	Kind       placement.Kind
	Subject    *subject.Subject
	Folder     subject.Folder
	FolderKind subject.System
	FolderName subject.FolderName
	CreateName string
}

// PlacementHint proposes where a file of «Входящие» goes, from its name, the subjects and the free subject strings
// of the schedule and the recordings (placement.For). It only proposes and writes nothing: the student places the file
// (Place) or creates the subject first. A file that is filed already or removed has no hint (KindNone); a missing one
// is ErrMaterialNotFound.
func (s *Service) PlacementHint(ctx context.Context, id shared.MaterialID) (Placement, error) {
	m, err := s.Material(ctx, id)
	switch {
	case err != nil:
		return Placement{}, err
	case !m.Active() || !m.InInbox():
		return Placement{}, nil
	}

	subjects, err := s.subjects.List(ctx)
	if err != nil {
		return Placement{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	strs, err := s.strings.ListSubjectStrings(ctx)
	if err != nil {
		return Placement{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	suggestions := indexOf(subjects).Suggest(strs) // the strings that match no subject, as SuggestSubjects lists them
	cands := make([]placement.Candidate, 0, len(subjects))
	for _, sub := range subjects {
		c := placement.Candidate{ID: sub.ID(), Name: sub.Name().String()}
		for _, a := range sub.Aliases() {
			c.Aliases = append(c.Aliases, a.String())
		}
		for _, f := range sub.Folders() {
			c.Folders = append(c.Folders, placement.Folder{ID: f.ID(), System: f.System()})
		}
		cands = append(cands, c)
	}
	schedule := make([]string, 0, len(suggestions))
	for _, sg := range suggestions {
		schedule = append(schedule, sg.Name)
	}

	h := placement.For(m.Name(), cands, schedule)
	out := Placement{Kind: h.Kind, FolderKind: h.FolderKind, CreateName: h.CreateName}
	out.FolderName, _ = subject.SystemFolderName(h.FolderKind)
	if h.Kind != placement.KindExisting {
		return out, nil
	}
	for _, sub := range subjects {
		if sub.ID() == h.SubjectID {
			out.Subject = sub
			out.Folder, _ = sub.Folder(h.FolderID)
		}
	}
	return out, nil
}
