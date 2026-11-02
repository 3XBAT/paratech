package library

import (
	"context"

	"paratech/desktop/internal/domain/library/services/placement"
	"paratech/desktop/internal/domain/library/services/schedulesubjects"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
)

// Placement is where a file of «Входящие» may go (PlacementHint). Kind placement.KindExisting: Subject and its Folder
// (zero only if the subject had no «Материалы», which its invariants rule out). placement.KindCreate: CreateName and
// CreateAliases, the subject to create as the schedule-subjects card proposes it, and FolderName of FolderKind.
// placement.KindNone: nothing.
type Placement struct {
	Kind          placement.Kind
	Subject       *subject.Subject
	Folder        subject.Folder
	FolderKind    subject.System
	FolderName    subject.FolderName
	CreateName    string
	CreateAliases []string
}

// PlacementHint proposes where a file of «Входящие» goes, from its name, the subjects and the subjects the
// schedule-subjects card proposes to create (ScheduleSubjects, placement.For). It only proposes and writes nothing: the
// student places the file (Place) or creates the subject first. A file that is filed already or removed has no hint
// (KindNone); a missing one is ErrMaterialNotFound.
func (s *Service) PlacementHint(ctx context.Context, id shared.MaterialID) (Placement, error) {
	m, err := s.Material(ctx, id)
	switch {
	case err != nil:
		return Placement{}, err
	case !m.Active() || !m.InInbox():
		return Placement{}, nil
	}

	rows, _, subjects, err := s.scheduleState(ctx)
	if err != nil {
		return Placement{}, err
	}
	var schedule []placement.Proposed
	for _, g := range schedulesubjects.Propose(rows, entriesOf(subjects)).Groups {
		if g.Subject == "" { // spellings for an existing subject create nothing
			schedule = append(schedule, placement.Proposed{Name: g.Name, Spellings: g.Aliases})
		}
	}
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

	h := placement.For(m.Name(), cands, schedule)
	out := Placement{Kind: h.Kind, FolderKind: h.FolderKind, CreateName: h.CreateName, CreateAliases: h.CreateAliases}
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
