package library

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"

	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/services/subjectmatch"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/library/values"
	"paratech/desktop/internal/domain/shared"
)

// Operation names of the log.
const (
	opSubjectBusy  = "library: subject changed while saved"
	opMaterialBusy = "library: material changed while saved"
	opBlobRewrite  = "library: stored file missing or damaged, written again"
	opBlobDamaged  = "library: stored file does not match its sha256"
	opTextStatus   = "library: text status not saved"
	opInspectSave  = "library: inspection not saved"
	opInspectAsked = "library: material inspected"
)

// saveAttempts bounds the saves of one change that lose against another save of the same aggregate.
const saveAttempts = 3

// Service is the library (TASK-121). Everything is local: no port talks to the network.
type Service struct {
	subjects    SubjectRepository
	materials   MaterialRepository
	blobs       BlobStore
	lessons     LessonReader
	strings     SubjectStrings
	clock       Clock
	ids         IDs
	texts       TextExtractor
	reader      MaterialReader
	inspections InspectionRepository
	log         *slog.Logger
}

// New checks that every required port is given (see Deps).
func New(d Deps) (*Service, error) {
	if d.Subjects == nil || d.Materials == nil || d.Blobs == nil || d.Lessons == nil || d.Strings == nil ||
		d.Clock == nil || d.IDs == nil {
		return nil, fmt.Errorf("%w: every port except the logger is required", ErrInvalidConfig)
	}
	log := d.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Service{
		subjects: d.Subjects, materials: d.Materials, blobs: d.Blobs, lessons: d.Lessons, strings: d.Strings,
		clock: d.Clock, ids: d.IDs, texts: d.Texts, reader: d.Reader, inspections: d.Inspections, log: log,
	}, nil
}

// Subjects lists every subject by name.
func (s *Service) Subjects(ctx context.Context) ([]*subject.Subject, error) {
	list, err := s.subjects.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return list, nil
}

// CreateSubject creates a subject with its system folders. A name or alias that another subject has (by key) is
// ErrNameTaken; aliases with the key of the name are dropped.
func (s *Service) CreateSubject(ctx context.Context, req NewSubject) (*subject.Subject, error) {
	now := s.clock.Now()
	sem := values.SemesterOf(now)
	if req.Semester != "" {
		var err error
		if sem, err = values.ParseSemester(req.Semester); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalidRequest, err)
		}
	}
	sub, err := subject.New(subject.Spec{ID: s.ids.NewSubjectID(), Name: req.Name, Aliases: req.Aliases, Semester: sem}, now)
	if err != nil {
		return nil, domainError(err)
	}

	ix, err := s.index(ctx)
	if err != nil {
		return nil, err
	}
	names := []string{sub.Name().String()}
	for _, a := range sub.Aliases() {
		names = append(names, a.String())
	}
	for _, n := range names {
		if ix.TakenByOther(n, "") {
			return nil, ErrNameTaken
		}
	}

	switch err := s.subjects.Save(ctx, sub); {
	case errors.Is(err, shared.ErrDuplicate):
		return nil, ErrNameTaken // another subject took it meanwhile
	case errors.Is(err, shared.ErrConflict):
		return nil, ErrBusy
	case err != nil:
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return sub, nil
}

// AddAlias adds another spelling to the subject, for example a string SuggestSubjects listed. A spelling another
// subject has is ErrNameTaken; one the subject has changes nothing.
func (s *Service) AddAlias(ctx context.Context, id shared.SubjectID, alias string) (*subject.Subject, error) {
	return s.changeSubject(ctx, id, func(sub *subject.Subject) (bool, error) {
		ix, err := s.index(ctx)
		if err != nil {
			return false, err
		}
		if ix.TakenByOther(alias, id) {
			return false, ErrNameTaken
		}
		changed, err := sub.AddAlias(alias, s.clock.Now())
		return changed, domainError(err)
	})
}

// CreateFolder adds an own folder to the subject.
func (s *Service) CreateFolder(ctx context.Context, id shared.SubjectID, name string) (subject.Folder, error) {
	fn, err := subject.NewFolderName(name)
	if err != nil {
		return subject.Folder{}, domainError(err)
	}
	var folder subject.Folder
	_, err = s.changeSubject(ctx, id, func(sub *subject.Subject) (bool, error) {
		f, err := sub.AddFolder(s.ids.NewFolderID(), fn, s.clock.Now())
		folder = f
		return err == nil, domainError(err)
	})
	if err != nil {
		return subject.Folder{}, err
	}
	return folder, nil
}

// RemoveFolder removes an own folder that holds no file (active or removed). A system folder is ErrSystemFolder, a
// folder with files ErrNotEmpty.
func (s *Service) RemoveFolder(ctx context.Context, id shared.SubjectID, folder shared.FolderID) error {
	if folder.Validate() != nil {
		return ErrInvalidRequest
	}
	_, err := s.changeSubject(ctx, id, func(sub *subject.Subject) (bool, error) {
		n, err := s.materials.Count(ctx, id, folder)
		if err != nil {
			return false, fmt.Errorf("%w: %w", ErrUnavailable, err)
		}
		return true, domainError(sub.RemoveFolder(folder, n, s.clock.Now()))
	})
	return err
}

// RemoveSubject removes a subject that holds no file (active or removed), with its own folders. One with files is
// ErrNotEmpty.
func (s *Service) RemoveSubject(ctx context.Context, id shared.SubjectID) error {
	for attempt := 1; ; attempt++ {
		sub, err := s.subject(ctx, id)
		if err != nil {
			return err
		}
		n, err := s.materials.Count(ctx, id, "")
		if err != nil {
			return fmt.Errorf("%w: %w", ErrUnavailable, err)
		}
		if err := sub.Removable(n); err != nil {
			return domainError(err)
		}
		err = s.subjects.Delete(ctx, sub)
		switch {
		case errors.Is(err, shared.ErrVersionConflict) && attempt < saveAttempts:
			continue
		case errors.Is(err, shared.ErrVersionConflict):
			s.log.Warn(opSubjectBusy, slog.String("subject", string(id)))
			return ErrBusy
		case errors.Is(err, shared.ErrConflict):
			return ErrNotEmpty // a file was filed under it after the count
		case err != nil:
			return fmt.Errorf("%w: %w", ErrUnavailable, err)
		}
		return nil
	}
}

// Materials lists the files of a subject, or of «Входящие» for "", in any status, newest first.
func (s *Service) Materials(ctx context.Context, sub shared.SubjectID) ([]*material.Material, error) {
	if sub != "" && sub.Validate() != nil {
		return nil, ErrInvalidRequest
	}
	list, err := s.materials.ListIn(ctx, sub)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return list, nil
}

// Material returns one file of the library in any status; a missing one is ErrMaterialNotFound.
func (s *Service) Material(ctx context.Context, id shared.MaterialID) (*material.Material, error) {
	if id.Validate() != nil {
		return nil, ErrInvalidRequest
	}
	m, err := s.materials.Get(ctx, id)
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return nil, ErrMaterialNotFound
	case err != nil:
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return m, nil
}

// Import stores an upload in «Входящие»: the type is detected from the content, the bytes are kept once under their
// sha256. The same bytes uploaded again return the material that exists (Existing), a removed one back in
// «Входящие». A stored file that is missing or has another size is written again.
func (s *Service) Import(ctx context.Context, up Upload) (Imported, error) {
	typ, err := material.DetectType(up.Data)
	if err != nil {
		return Imported{}, domainError(err)
	}
	digest := material.DigestOf(up.Data)
	ref, err := material.BlobRef(digest)
	if err != nil {
		return Imported{}, fmt.Errorf("%w: %w", ErrInvalidRequest, err)
	}
	if err := s.ensureBlob(ctx, ref, up.Data); err != nil {
		return Imported{}, err
	}

	for attempt := 1; ; attempt++ {
		m, err := s.materials.GetByDigest(ctx, digest)
		switch {
		case errors.Is(err, shared.ErrNotFound):
			m, err = material.New(material.Spec{
				ID: s.ids.NewMaterialID(), Digest: digest, Size: int64(len(up.Data)), Type: typ, Name: up.Name,
			}, s.clock.Now())
			if err != nil {
				return Imported{}, domainError(err)
			}
			err = s.materials.Save(ctx, m)
			if err == nil {
				return Imported{Material: m}, nil
			}
		case err != nil:
			return Imported{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
		default:
			var changed bool
			if changed, err = m.Reupload(s.clock.Now()); err != nil {
				return Imported{}, domainError(err)
			}
			if !changed {
				return Imported{Material: m, Existing: true}, nil
			}
			if err = s.materials.Save(ctx, m); err == nil {
				return Imported{Material: m, Existing: true}, nil
			}
		}
		// The same bytes were uploaded concurrently, or the material changed: look again.
		switch {
		case errors.Is(err, shared.ErrConflict) && attempt < saveAttempts:
			continue
		case errors.Is(err, shared.ErrConflict):
			s.log.Warn(opMaterialBusy, slog.String("sha256", digest.String()))
			return Imported{}, ErrBusy
		}
		return Imported{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
}

// ensureBlob writes data to ref unless the same bytes are there: a stored file of another size, or of the same size
// but another sha256 (damaged), is written again. The write is atomic and the name is the content's sha256, so a write
// over an existing file changes nothing.
func (s *Service) ensureBlob(ctx context.Context, ref shared.ArtifactRef, data []byte) error {
	size, err := s.blobs.Size(ctx, ref)
	if err == nil && size == int64(len(data)) {
		stored, rerr := s.readBlob(ctx, ref)
		if rerr == nil && material.DigestOf(stored) == material.DigestOf(data) {
			return nil
		}
		err = errors.Join(errors.New("same size, other content"), rerr)
	}
	if err == nil || !errors.Is(err, shared.ErrNotFound) {
		s.log.Warn(opBlobRewrite, slog.String("file", ref.String()), slog.Any("cause", err))
	}
	if err := s.blobs.Write(ctx, ref, bytes.NewReader(data)); err != nil {
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return nil
}

// Place files a material in folder of subject; folder "" is the system folder «Материалы». A folder of another
// subject is ErrFolderNotFound.
func (s *Service) Place(ctx context.Context, id shared.MaterialID, sub shared.SubjectID, folder shared.FolderID) (*material.Material, error) {
	if sub.Validate() != nil || (folder != "" && folder.Validate() != nil) {
		return nil, ErrInvalidRequest
	}
	return s.changeMaterial(ctx, id, func(m *material.Material) (bool, error) {
		target, err := s.subject(ctx, sub)
		if err != nil {
			return false, err
		}
		f, ok := target.Folder(folder)
		if folder == "" {
			f, ok = target.SystemFolder(subject.SystemMaterials)
		}
		if !ok {
			return false, ErrFolderNotFound
		}
		changed, err := m.Place(sub, f.ID(), s.clock.Now())
		return changed, domainError(err)
	})
}

// FileHomework stores a file the app made for the student (a turnkey work, TASK-131) and files it in «ДЗ» of sub. With
// no subject, or one that is gone, it stays in «Входящие». The same bytes again give the material that exists, filed
// in «ДЗ» again, so a repeated job step files one material.
func (s *Service) FileHomework(ctx context.Context, sub shared.SubjectID, name string, data []byte) (*material.Material, error) {
	return s.fileInto(ctx, sub, subject.SystemHomework, name, data)
}

// FileTickets stores the answers to a ticket set (TASK-133) and files them in «Билеты» of sub, as FileHomework does
// for «ДЗ».
func (s *Service) FileTickets(ctx context.Context, sub shared.SubjectID, name string, data []byte) (*material.Material, error) {
	return s.fileInto(ctx, sub, subject.SystemTickets, name, data)
}

// fileInto imports a file the app made and places it in the system folder of sub; with no subject, or one that is
// gone, it stays in «Входящие».
func (s *Service) fileInto(ctx context.Context, sub shared.SubjectID, folder subject.System, name string, data []byte) (*material.Material, error) {
	im, err := s.Import(ctx, Upload{Name: name, Data: data})
	if err != nil || sub == "" {
		return im.Material, err
	}
	target, err := s.subject(ctx, sub)
	switch {
	case errors.Is(err, ErrSubjectNotFound):
		return im.Material, nil
	case err != nil:
		return nil, err
	}
	f, ok := target.SystemFolder(folder)
	if !ok {
		return im.Material, nil
	}
	return s.Place(ctx, im.Material.ID(), sub, f.ID())
}

// MoveToInbox returns a material to «Входящие» (the undo of Place); the file is kept.
func (s *Service) MoveToInbox(ctx context.Context, id shared.MaterialID) (*material.Material, error) {
	return s.changeMaterial(ctx, id, func(m *material.Material) (bool, error) {
		changed, err := m.MoveToInbox(s.clock.Now())
		return changed, domainError(err)
	})
}

// Link links a material to a lesson of the schedule.
func (s *Service) Link(ctx context.Context, id shared.MaterialID, lessonID shared.LessonID) (*material.Material, error) {
	if lessonID.Validate() != nil {
		return nil, ErrInvalidRequest
	}
	return s.changeMaterial(ctx, id, func(m *material.Material) (bool, error) {
		switch _, err := s.lessons.Get(ctx, lessonID); {
		case errors.Is(err, shared.ErrNotFound):
			return false, ErrLessonNotFound
		case err != nil:
			return false, fmt.Errorf("%w: %w", ErrUnavailable, err)
		}
		changed, err := m.Link(lessonID, s.clock.Now())
		return changed, domainError(err)
	})
}

// Unlink removes the link of a material to a lesson; one not linked changes nothing.
func (s *Service) Unlink(ctx context.Context, id shared.MaterialID, lessonID shared.LessonID) (*material.Material, error) {
	if lessonID.Validate() != nil {
		return nil, ErrInvalidRequest
	}
	return s.changeMaterial(ctx, id, func(m *material.Material) (bool, error) {
		changed, err := m.Unlink(lessonID, s.clock.Now())
		return changed, domainError(err)
	})
}

// RemoveMaterial removes a material softly: it keeps its place, links and file, so RestoreMaterial brings it back.
// It is the student's action, not an agent tool (ADR-0024).
func (s *Service) RemoveMaterial(ctx context.Context, id shared.MaterialID) (*material.Material, error) {
	return s.changeMaterial(ctx, id, func(m *material.Material) (bool, error) {
		changed, err := m.Remove(s.clock.Now())
		return changed, domainError(err)
	})
}

// RestoreMaterial brings a removed material back where it was.
func (s *Service) RestoreMaterial(ctx context.Context, id shared.MaterialID) (*material.Material, error) {
	return s.changeMaterial(ctx, id, func(m *material.Material) (bool, error) {
		changed, err := m.Restore(s.clock.Now())
		return changed, domainError(err)
	})
}

// SuggestSubjects lists the free subject strings of the schedule and the recordings that match no subject, with how
// many lessons and recordings name them. It never creates a subject: the student does (CreateSubject, or AddAlias to
// one that exists).
func (s *Service) SuggestSubjects(ctx context.Context) ([]subjectmatch.Suggestion, error) {
	ix, err := s.index(ctx)
	if err != nil {
		return nil, err
	}
	cands, err := s.strings.ListSubjectStrings(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return ix.Suggest(cands), nil
}

// ResolveSubjects maps free subject strings to the subjects they name (name or alias; an ambiguous string matches
// none). Strings that match nothing are absent from the result. Nothing stored is rewritten (ADR-0024).
func (s *Service) ResolveSubjects(ctx context.Context, names []string) (map[string]subjectmatch.Match, error) {
	out := map[string]subjectmatch.Match{}
	if len(names) == 0 {
		return out, nil
	}
	ix, err := s.index(ctx)
	if err != nil {
		return nil, err
	}
	for _, n := range names {
		if m, ok := ix.Resolve(n); ok {
			out[n] = m
		}
	}
	return out, nil
}

// index builds the matching index of every subject.
func (s *Service) index(ctx context.Context) (subjectmatch.Index, error) {
	list, err := s.subjects.List(ctx)
	if err != nil {
		return subjectmatch.Index{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	entries := make([]subjectmatch.Entry, 0, len(list))
	for _, sub := range list {
		e := subjectmatch.Entry{ID: sub.ID(), Name: sub.Name().String()}
		for _, a := range sub.Aliases() {
			e.Aliases = append(e.Aliases, a.String())
		}
		entries = append(entries, e)
	}
	return subjectmatch.NewIndex(entries), nil
}

// subject loads a subject; a missing one is ErrSubjectNotFound.
func (s *Service) subject(ctx context.Context, id shared.SubjectID) (*subject.Subject, error) {
	if id.Validate() != nil {
		return nil, ErrInvalidRequest
	}
	sub, err := s.subjects.Get(ctx, id)
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return nil, ErrSubjectNotFound
	case err != nil:
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return sub, nil
}

// changeSubject loads the subject, applies fn and saves it when fn changed it, again from the start when another save
// came first. A taken name is ErrNameTaken; a folder that got a file meanwhile ErrNotEmpty.
func (s *Service) changeSubject(ctx context.Context, id shared.SubjectID, fn func(*subject.Subject) (bool, error)) (*subject.Subject, error) {
	for attempt := 1; ; attempt++ {
		sub, err := s.subject(ctx, id)
		if err != nil {
			return nil, err
		}
		changed, err := fn(sub)
		if err != nil {
			return nil, err
		}
		if !changed {
			return sub, nil
		}
		err = s.subjects.Save(ctx, sub)
		switch {
		case errors.Is(err, shared.ErrVersionConflict) && attempt < saveAttempts:
			continue
		case errors.Is(err, shared.ErrVersionConflict):
			s.log.Warn(opSubjectBusy, slog.String("subject", string(id)))
			return nil, ErrBusy
		case errors.Is(err, shared.ErrDuplicate):
			return nil, ErrNameTaken
		case errors.Is(err, shared.ErrConflict):
			return nil, ErrNotEmpty
		case err != nil:
			return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
		}
		return sub, nil
	}
}

// changeMaterial loads the material, applies fn and saves it when fn changed it, again from the start when the save
// lost against another change (a newer version, or a subject, folder or lesson gone meanwhile: fn checks them again).
func (s *Service) changeMaterial(ctx context.Context, id shared.MaterialID, fn func(*material.Material) (bool, error)) (*material.Material, error) {
	return s.changeMaterialWith(ctx, id, fn, s.materials.Save)
}

// changeMaterialWith is changeMaterial with the save to use: Save, or SaveText for the text status.
func (s *Service) changeMaterialWith(ctx context.Context, id shared.MaterialID, fn func(*material.Material) (bool, error),
	save func(context.Context, *material.Material) error) (*material.Material, error) {
	if id.Validate() != nil {
		return nil, ErrInvalidRequest
	}
	for attempt := 1; ; attempt++ {
		m, err := s.materials.Get(ctx, id)
		switch {
		case errors.Is(err, shared.ErrNotFound):
			return nil, ErrMaterialNotFound
		case err != nil:
			return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
		}
		changed, err := fn(m)
		if err != nil {
			return nil, err
		}
		if !changed {
			return m, nil
		}
		err = save(ctx, m)
		switch {
		case errors.Is(err, shared.ErrConflict) && attempt < saveAttempts:
			continue
		case errors.Is(err, shared.ErrConflict):
			s.log.Warn(opMaterialBusy, slog.String("material", string(id)))
			return nil, ErrBusy
		case err != nil:
			return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
		}
		return m, nil
	}
}

// domainError maps an error of the library aggregates to the error of the use case; nil stays nil.
func domainError(err error) error {
	var to error
	switch {
	case err == nil:
		return nil
	case errors.Is(err, subject.ErrInvalidName):
		to = ErrInvalidName
	case errors.Is(err, subject.ErrInvalidFolderName):
		to = ErrInvalidFolderName
	case errors.Is(err, subject.ErrFolderExists):
		to = ErrFolderTaken
	case errors.Is(err, subject.ErrFolderNotFound):
		to = ErrFolderNotFound
	case errors.Is(err, subject.ErrSystemFolder):
		to = ErrSystemFolder
	case errors.Is(err, subject.ErrNotEmpty):
		to = ErrNotEmpty
	case errors.Is(err, subject.ErrTooMany), errors.Is(err, material.ErrTooManyLessons):
		to = ErrTooMany
	case errors.Is(err, material.ErrRemoved):
		to = ErrMaterialRemoved
	case errors.Is(err, material.ErrEmpty):
		to = ErrEmptyFile
	case errors.Is(err, material.ErrTooLarge):
		to = ErrFileTooLarge
	case errors.Is(err, material.ErrUnsupportedType):
		to = ErrUnsupportedFile
	case errors.Is(err, subject.ErrInvalidSubject), errors.Is(err, material.ErrInvalidMaterial),
		errors.Is(err, material.ErrInvalidTransition):
		to = ErrInvalidRequest
	default:
		return err // already an error of the use case
	}
	return fmt.Errorf("%w: %w", to, err)
}
