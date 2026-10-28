package wails

import (
	"context"

	"paratech/desktop/internal/cases/library"
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/services/placement"
	"paratech/desktop/internal/domain/shared"
)

// Operation names of the log.
const (
	opLibraryHint     = "ui: placement hint"
	opLibraryImportTo = "ui: import a file into a subject"
)

// Kinds of LibraryPlacementHint.
const (
	PlacementNone     = "none"
	PlacementExisting = "existing"
	PlacementCreate   = "create"
)

// LibraryPlacementHint is where a file of «Входящие» may go, from its name (no AI). Nothing is placed: the page offers
// it and the student confirms.
//   - Kind "existing": «Похоже на: <SubjectName> → <FolderName>»; the page calls Place(materialID, SubjectID, FolderID).
//   - Kind "create": no such subject yet, the schedule names it: «Создать предмет „<CreateName>“ и положить»; the page
//     calls CreateSubject(CreateName, [], "") and then Place(materialID, <new subject id>, <the folder of FolderKind
//     among its folders>). SubjectID and FolderID are "".
//   - Kind "none": no hint (no match, an ambiguous one, a file filed already or removed); all else "".
//
// FolderKind is the code of a system folder ("lectures", "seminars", "labs", "materials", "homework", "tickets").
// SubjectName, FolderName and CreateName are plain text; CreateName is spelled by the schedule, never by the file name.
type LibraryPlacementHint struct {
	Kind        string `json:"kind"`
	SubjectID   string `json:"subjectId"`
	SubjectName string `json:"subjectName"`
	FolderID    string `json:"folderId"`
	FolderName  string `json:"folderName"`
	FolderKind  string `json:"folderKind"`
	CreateName  string `json:"createName"`
}

// PlacementHint proposes where a file of «Входящие» goes. It changes nothing.
func (s *LibraryService) PlacementHint(ctx context.Context, materialID string) (LibraryPlacementHint, error) {
	id := shared.MaterialID(materialID)
	if id.Validate() != nil {
		s.log.Info(opLibraryHint, "result", "invalid_id")
		return LibraryPlacementHint{}, ErrLibraryInvalidID
	}
	p, err := s.uc.PlacementHint(ctx, id)
	if err != nil {
		return LibraryPlacementHint{}, s.fail(opLibraryHint, err)
	}
	return libraryPlacementHint(p), nil
}

// ImportFileTo is ImportFile into a folder of a subject («Добавить файл» on the page of a subject); folderID "" is
// «Материалы». The same file filed elsewhere already is not moved: Existing is true and the material tells where it
// lies (subjectId, folderId differ from the target). A subject or folder that is gone is refused, and nothing is kept.
func (s *LibraryService) ImportFileTo(ctx context.Context, subjectID, folderID string) (LibraryImport, error) {
	sub, folder := shared.SubjectID(subjectID), shared.FolderID(folderID)
	if sub.Validate() != nil || (folder != "" && folder.Validate() != nil) {
		s.log.Info(opLibraryImportTo, "result", "invalid_id")
		return LibraryImport{}, ErrLibraryInvalidID
	}
	path, err := s.picker.PickMaterialFile(ctx)
	switch {
	case err != nil:
		s.log.Warn(opLibraryPick, "err", err)
		return LibraryImport{}, ErrLibraryFileUnreadable
	case path == "":
		return LibraryImport{Cancelled: true}, nil
	}
	return s.importPathTo(ctx, path, sub, folder)
}

// importPathTo reads the file at path and imports it into folder of sub; both "" is «Входящие».
func (s *LibraryService) importPathTo(ctx context.Context, path string, sub shared.SubjectID, folder shared.FolderID) (LibraryImport, error) {
	data, err := s.files.ReadFile(ctx, path, material.MaxBytes)
	switch {
	case err != nil:
		s.log.Warn(opLibraryRead, "err", err) // the reader's errors never carry the path
		return LibraryImport{}, ErrLibraryFileUnreadable
	case len(data) > material.MaxBytes:
		return LibraryImport{}, ErrLibraryFileTooLarge
	}
	op := opLibraryImport
	if sub != "" {
		op = opLibraryImportTo
	}
	res, err := s.uc.Import(ctx, library.Upload{Name: baseName(path), Data: data, Subject: sub, Folder: folder})
	if err != nil {
		return LibraryImport{}, s.fail(op, err)
	}
	m := libraryMaterial(res.Material)
	return LibraryImport{Existing: res.Existing, Material: &m}, nil
}

func libraryPlacementHint(p library.Placement) LibraryPlacementHint {
	switch p.Kind {
	case placement.KindExisting:
		if p.Subject == nil {
			return LibraryPlacementHint{Kind: PlacementNone}
		}
		return LibraryPlacementHint{
			Kind: PlacementExisting, SubjectID: string(p.Subject.ID()), SubjectName: p.Subject.Name().String(),
			FolderID: string(p.Folder.ID()), FolderName: p.FolderName.String(), FolderKind: string(p.FolderKind),
		}
	case placement.KindCreate:
		return LibraryPlacementHint{
			Kind: PlacementCreate, FolderName: p.FolderName.String(), FolderKind: string(p.FolderKind), CreateName: p.CreateName,
		}
	}
	return LibraryPlacementHint{Kind: PlacementNone}
}
