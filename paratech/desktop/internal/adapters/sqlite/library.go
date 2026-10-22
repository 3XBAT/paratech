package sqlite

import (
	"context"
	"fmt"
	"sort"

	"paratech/desktop/internal/adapters/sqlite/sqlcgen"
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/services/subjectmatch"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/library/values"
	"paratech/desktop/internal/domain/shared"
)

// Operation names of the library repositories, used in their error messages.
const (
	opListSubjects     = "sqlite: list subjects"
	opGetSubject       = "sqlite: get subject"
	opSaveSubject      = "sqlite: save subject"
	opDeleteSubject    = "sqlite: delete subject"
	opGetMaterial      = "sqlite: get material"
	opListMaterials    = "sqlite: list materials"
	opCountMaterials   = "sqlite: count materials"
	opSaveMaterial     = "sqlite: save material"
	opSaveMaterialText = "sqlite: save material text status"
	opSubjectStrings   = "sqlite: list subject strings"
	opRestoreSubject   = "stored subject"
	opRestoreMaterial  = "stored material"
)

// SubjectRepository stores the subjects of the library with their aliases and folders (the library.SubjectRepository
// port, TASK-121). Validation stays in subject.Subject: the repository only converts rows.
type SubjectRepository struct{ db *DB }

// NewSubjectRepository returns the subject repository of db.
func NewSubjectRepository(db *DB) *SubjectRepository { return &SubjectRepository{db: db} }

// List returns every subject by name key. A row that cannot be restored fails the list: the library is small and a
// broken subject must not silently vanish from the choices.
func (r *SubjectRepository) List(ctx context.Context) ([]*subject.Subject, error) {
	q := r.db.Queries(ctx)
	rows, err := q.ListSubjects(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", opListSubjects, MapError(ctx, err))
	}
	aliases, err := q.ListAllSubjectAliases(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", opListSubjects, MapError(ctx, err))
	}
	folders, err := q.ListAllSubjectFolders(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", opListSubjects, MapError(ctx, err))
	}
	aliasesOf := map[string][]sqlcgen.SubjectAlias{}
	for _, a := range aliases {
		aliasesOf[a.SubjectID] = append(aliasesOf[a.SubjectID], a)
	}
	foldersOf := map[string][]sqlcgen.SubjectFolder{}
	for _, f := range folders {
		foldersOf[f.SubjectID] = append(foldersOf[f.SubjectID], f)
	}

	out := make([]*subject.Subject, 0, len(rows))
	for _, row := range rows {
		s, err := subjectFromRows(row, aliasesOf[row.ID], foldersOf[row.ID])
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

// Get loads a subject; a missing one wraps shared.ErrNotFound.
func (r *SubjectRepository) Get(ctx context.Context, id shared.SubjectID) (*subject.Subject, error) {
	q := r.db.Queries(ctx)
	row, err := q.GetSubject(ctx, string(id))
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", opGetSubject, id, MapError(ctx, err))
	}
	aliases, err := q.ListSubjectAliases(ctx, row.ID)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", opGetSubject, id, MapError(ctx, err))
	}
	folders, err := q.ListSubjectFolders(ctx, row.ID)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", opGetSubject, id, MapError(ctx, err))
	}
	return subjectFromRows(row, aliases, folders)
}

// Save writes the subject in one transaction: an INSERT for version 0, otherwise a version compare-and-swap
// (ADR-0004(d)); aliases are rewritten, folders inserted and deleted (they are never renamed, and a folder that holds
// a file must not be deleted even for a moment). A taken name or alias key wraps shared.ErrDuplicate; an id that
// exists or a stale version shared.ErrVersionConflict; a removed folder that holds a file shared.ErrConflict.
func (r *SubjectRepository) Save(ctx context.Context, s *subject.Subject) error {
	st := s.State()
	if st.Version < 0 {
		return fmt.Errorf("%s: negative version %d", opSaveSubject, st.Version)
	}
	err := r.db.Do(ctx, func(ctx context.Context) error {
		q := r.db.Queries(ctx)
		var stored []sqlcgen.SubjectFolder
		if st.Version == 0 {
			if err := DuplicateAsVersionConflict(MapError(ctx, q.InsertSubject(ctx, sqlcgen.InsertSubjectParams{
				ID: string(st.ID), Name: st.Name, NameKey: values.Key(st.Name), Semester: st.Semester.Code(),
				CreatedAt: jobMs(st.CreatedAt), UpdatedAt: jobMs(st.UpdatedAt),
			}))); err != nil {
				return err
			}
		} else {
			n, err := q.UpdateSubject(ctx, sqlcgen.UpdateSubjectParams{
				Name: st.Name, NameKey: values.Key(st.Name), Semester: st.Semester.Code(), UpdatedAt: jobMs(st.UpdatedAt),
				ID: string(st.ID), ExpectedVersion: st.Version,
			})
			if err := VersionCAS(ctx, n, err); err != nil {
				return err
			}
			if err := q.DeleteSubjectAliases(ctx, string(st.ID)); err != nil {
				return MapError(ctx, err)
			}
			if stored, err = q.ListSubjectFolders(ctx, string(st.ID)); err != nil {
				return MapError(ctx, err)
			}
		}

		for i, a := range st.Aliases {
			if err := q.InsertSubjectAlias(ctx, sqlcgen.InsertSubjectAliasParams{
				AliasKey: values.Key(a), SubjectID: string(st.ID), Alias: a, Position: int64(i),
			}); err != nil {
				return MapError(ctx, err)
			}
		}
		return saveFolders(ctx, q, st, stored)
	})
	if err != nil {
		return fmt.Errorf("%s %s: %w", opSaveSubject, st.ID, err)
	}
	s.Persisted(st.Version + 1)
	return nil
}

// saveFolders deletes the stored folders the subject no longer has and inserts the new ones in order.
func saveFolders(ctx context.Context, q *sqlcgen.Queries, st subject.State, stored []sqlcgen.SubjectFolder) error {
	keep := map[string]bool{}
	for _, f := range st.Folders {
		keep[string(f.ID)] = true
	}
	have := map[string]bool{}
	for _, f := range stored {
		have[f.ID] = true
		if keep[f.ID] {
			continue
		}
		if err := q.DeleteSubjectFolder(ctx, sqlcgen.DeleteSubjectFolderParams{SubjectID: string(st.ID), ID: f.ID}); err != nil {
			return MapError(ctx, err)
		}
	}
	for _, f := range st.Folders {
		if have[string(f.ID)] {
			continue
		}
		if err := q.InsertSubjectFolder(ctx, sqlcgen.InsertSubjectFolderParams{
			ID: string(f.ID), SubjectID: string(st.ID), Name: f.Name, NameKey: values.Key(f.Name), System: string(f.System),
		}); err != nil {
			return MapError(ctx, err)
		}
	}
	return nil
}

// Delete removes a stored subject with its aliases and folders under its version. A stale version wraps
// shared.ErrVersionConflict; a subject that still holds a file (active or removed) shared.ErrConflict.
func (r *SubjectRepository) Delete(ctx context.Context, s *subject.Subject) error {
	n, err := r.db.Queries(ctx).DeleteSubject(ctx, sqlcgen.DeleteSubjectParams{ID: string(s.ID()), ExpectedVersion: s.Version()})
	if err := VersionCAS(ctx, n, err); err != nil {
		return fmt.Errorf("%s %s: %w", opDeleteSubject, s.ID(), err)
	}
	return nil
}

func subjectFromRows(row sqlcgen.Subject, aliases []sqlcgen.SubjectAlias, folders []sqlcgen.SubjectFolder) (*subject.Subject, error) {
	sem, err := values.ParseSemester(row.Semester)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", opRestoreSubject, row.ID, err)
	}
	st := subject.State{
		Version: row.Version, ID: shared.SubjectID(row.ID), Name: row.Name, Semester: sem,
		CreatedAt: jobFromMs(row.CreatedAt), UpdatedAt: jobFromMs(row.UpdatedAt),
	}
	for _, a := range aliases {
		st.Aliases = append(st.Aliases, a.Alias)
	}
	for _, f := range folders {
		st.Folders = append(st.Folders, subject.FolderState{ID: shared.FolderID(f.ID), Name: f.Name, System: subject.System(f.System)})
	}
	s, err := subject.Restore(st)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", opRestoreSubject, row.ID, err)
	}
	return s, nil
}

// MaterialRepository stores the materials of the library with their lesson links (the library.MaterialRepository
// port, TASK-121). Validation stays in material.Material.
type MaterialRepository struct{ db *DB }

// NewMaterialRepository returns the material repository of db.
func NewMaterialRepository(db *DB) *MaterialRepository { return &MaterialRepository{db: db} }

// Get loads a material; a missing one wraps shared.ErrNotFound.
func (r *MaterialRepository) Get(ctx context.Context, id shared.MaterialID) (*material.Material, error) {
	q := r.db.Queries(ctx)
	row, err := q.GetMaterial(ctx, string(id))
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", opGetMaterial, id, MapError(ctx, err))
	}
	return materialWithLessons(ctx, q, row)
}

// GetByDigest loads the material with the sha256 d (one per sha256); none wraps shared.ErrNotFound.
func (r *MaterialRepository) GetByDigest(ctx context.Context, d material.Digest) (*material.Material, error) {
	q := r.db.Queries(ctx)
	row, err := q.GetMaterialBySHA256(ctx, string(d))
	if err != nil {
		return nil, fmt.Errorf("%s by sha256: %w", opGetMaterial, MapError(ctx, err))
	}
	return materialWithLessons(ctx, q, row)
}

// ListIn returns the materials filed under subject, or those in «Входящие» for "", in any status, newest first.
func (r *MaterialRepository) ListIn(ctx context.Context, sub shared.SubjectID) ([]*material.Material, error) {
	q := r.db.Queries(ctx)
	var rows []sqlcgen.Material
	var err error
	if sub == "" {
		rows, err = q.ListInboxMaterials(ctx)
	} else {
		rows, err = q.ListMaterialsOfSubject(ctx, jobNullString(string(sub)))
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", opListMaterials, MapError(ctx, err))
	}
	out := make([]*material.Material, 0, len(rows))
	for _, row := range rows {
		m, err := materialWithLessons(ctx, q, row)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

// Count returns how many materials (active or removed) are filed in folder of subject, or under subject at all when
// folder is "".
func (r *MaterialRepository) Count(ctx context.Context, sub shared.SubjectID, folder shared.FolderID) (int, error) {
	q := r.db.Queries(ctx)
	var n int64
	var err error
	if folder == "" {
		n, err = q.CountMaterialsOfSubject(ctx, jobNullString(string(sub)))
	} else {
		n, err = q.CountMaterialsInFolder(ctx, sqlcgen.CountMaterialsInFolderParams{
			SubjectID: jobNullString(string(sub)), FolderID: jobNullString(string(folder)),
		})
	}
	if err != nil {
		return 0, fmt.Errorf("%s: %w", opCountMaterials, MapError(ctx, err))
	}
	return int(n), nil
}

// Save writes the material and its lesson links in one transaction: an INSERT for version 0, otherwise a version
// compare-and-swap (ADR-0004(d)). A sha256 that is stored wraps shared.ErrDuplicate; an id that exists or a stale
// version shared.ErrVersionConflict; a subject, folder or lesson that does not exist shared.ErrConflict.
func (r *MaterialRepository) Save(ctx context.Context, m *material.Material) error {
	st := m.State()
	if st.Version < 0 {
		return fmt.Errorf("%s: negative version %d", opSaveMaterial, st.Version)
	}
	err := r.db.Do(ctx, func(ctx context.Context) error {
		q := r.db.Queries(ctx)
		if st.Version == 0 {
			if err := DuplicateAsVersionConflict(MapError(ctx, q.InsertMaterial(ctx, sqlcgen.InsertMaterialParams{
				ID: string(st.ID), Sha256: string(st.Digest), SizeBytes: st.Size, Type: string(st.Type),
				OriginalName: st.Name, SubjectID: jobNullString(string(st.Subject)), FolderID: jobNullString(string(st.Folder)),
				Status: string(st.Status), TextStatus: string(st.Text), CreatedAt: jobMs(st.CreatedAt),
				UpdatedAt: jobMs(st.UpdatedAt),
			}))); err != nil {
				return err
			}
		} else {
			n, err := q.UpdateMaterial(ctx, sqlcgen.UpdateMaterialParams{
				SubjectID: jobNullString(string(st.Subject)), FolderID: jobNullString(string(st.Folder)),
				Status: string(st.Status), UpdatedAt: jobMs(st.UpdatedAt), ID: string(st.ID), ExpectedVersion: st.Version,
			})
			if err := VersionCAS(ctx, n, err); err != nil {
				return err
			}
			if err := q.DeleteMaterialLessons(ctx, string(st.ID)); err != nil {
				return MapError(ctx, err)
			}
		}
		for i, l := range st.Lessons {
			if err := q.InsertMaterialLesson(ctx, sqlcgen.InsertMaterialLessonParams{
				MaterialID: string(st.ID), LessonID: string(l), Position: int64(i),
			}); err != nil {
				return MapError(ctx, err)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("%s %s: %w", opSaveMaterial, st.ID, err)
	}
	m.Persisted(st.Version + 1)
	return nil
}

// SaveText writes only the text status of a stored material, checked against its version but keeping it (see
// material.SetText); a stale version or a missing material wraps shared.ErrVersionConflict.
func (r *MaterialRepository) SaveText(ctx context.Context, m *material.Material) error {
	st := m.State()
	if st.Version < 1 {
		return fmt.Errorf("%s %s: text of a material never stored", opSaveMaterialText, st.ID)
	}
	n, err := r.db.Queries(ctx).UpdateMaterialText(ctx, sqlcgen.UpdateMaterialTextParams{
		TextStatus: string(st.Text), ID: string(st.ID), ExpectedVersion: st.Version,
	})
	if err := VersionCAS(ctx, n, err); err != nil {
		return fmt.Errorf("%s %s: %w", opSaveMaterialText, st.ID, err)
	}
	return nil
}

func materialWithLessons(ctx context.Context, q *sqlcgen.Queries, row sqlcgen.Material) (*material.Material, error) {
	lessons, err := q.ListMaterialLessons(ctx, row.ID)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", opGetMaterial, row.ID, MapError(ctx, err))
	}
	st := material.State{
		Version: row.Version, ID: shared.MaterialID(row.ID), Digest: material.Digest(row.Sha256), Size: row.SizeBytes,
		Type: material.Type(row.Type), Name: row.OriginalName, Subject: shared.SubjectID(row.SubjectID.String),
		Folder: shared.FolderID(row.FolderID.String), Status: material.Status(row.Status),
		Text: material.TextStatus(row.TextStatus), CreatedAt: jobFromMs(row.CreatedAt), UpdatedAt: jobFromMs(row.UpdatedAt),
	}
	for _, l := range lessons {
		st.Lessons = append(st.Lessons, shared.LessonID(l))
	}
	m, err := material.Restore(st)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", opRestoreMaterial, row.ID, err)
	}
	return m, nil
}

// SubjectStrings reads the free subject strings of the schedule and the recordings (the library.SubjectStrings
// port), for the subjects to suggest. Stored rows are only read, never rewritten (ADR-0024).
type SubjectStrings struct{ db *DB }

// NewSubjectStrings returns the subject string reads of db.
func NewSubjectStrings(db *DB) *SubjectStrings { return &SubjectStrings{db: db} }

// ListSubjectStrings returns every distinct string as stored, with how many live lessons of the schedule and
// recordings not marked for deletion name it, sorted by string.
func (s *SubjectStrings) ListSubjectStrings(ctx context.Context) ([]subjectmatch.Candidate, error) {
	q := s.db.Queries(ctx)
	lessons, err := q.ListScheduleSubjectCounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", opSubjectStrings, MapError(ctx, err))
	}
	recs, err := q.ListRecordingSubjectCounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", opSubjectStrings, MapError(ctx, err))
	}
	byName := map[string]*subjectmatch.Candidate{}
	get := func(name string) *subjectmatch.Candidate {
		c, ok := byName[name]
		if !ok {
			c = &subjectmatch.Candidate{Name: name}
			byName[name] = c
		}
		return c
	}
	for _, l := range lessons {
		get(l.Subject).Lessons += int(l.Lessons)
	}
	for _, r := range recs {
		get(r.Subject).Recordings += int(r.Recordings)
	}
	out := make([]subjectmatch.Candidate, 0, len(byName))
	for _, c := range byName {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
