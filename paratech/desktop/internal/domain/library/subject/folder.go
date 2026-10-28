package subject

import "paratech/desktop/internal/domain/shared"

// System is the code of a system folder; "" for an own folder.
type System string

// System folders, in the order they are shown.
const (
	SystemLectures  System = "lectures"
	SystemSeminars  System = "seminars"
	SystemLabs      System = "labs"
	SystemMaterials System = "materials"
	SystemHomework  System = "homework"
	SystemTickets   System = "tickets"
)

// systemNames are the Russian names of the system folders, in the order they are shown.
var systemNames = []struct {
	code System
	name string
}{
	{SystemLectures, "Лекции"},
	{SystemSeminars, "Семинары"},
	{SystemLabs, "Лабораторные"},
	{SystemMaterials, "Материалы"},
	{SystemHomework, "ДЗ"},
	{SystemTickets, "Билеты"},
}

// Systems returns the codes of the system folders in the order they are shown.
func Systems() []System {
	out := make([]System, len(systemNames))
	for i, s := range systemNames {
		out[i] = s.code
	}
	return out
}

// systemName returns the name of a system folder; ok is false for an unknown code.
func systemName(code System) (FolderName, bool) {
	for _, s := range systemNames {
		if s.code == code {
			return FolderName{text: s.name}, true
		}
	}
	return FolderName{}, false
}

// SystemFolderName returns the name of a system folder, for a folder of a subject that does not exist yet; ok is false
// for an unknown code.
func SystemFolderName(code System) (FolderName, bool) { return systemName(code) }

// SystemFolderID is the id of a system folder of a subject: derived, so the six folders need no generated ids.
func SystemFolderID(subject shared.SubjectID, code System) shared.FolderID {
	return shared.FolderID(string(subject) + "-" + string(code))
}

// Folder is a folder of a subject, an entity of the aggregate. It is a copy: change it through the subject.
type Folder struct {
	id     shared.FolderID
	name   FolderName
	system System
}

// ID returns the folder id.
func (f Folder) ID() shared.FolderID { return f.id }

// Name returns the folder name.
func (f Folder) Name() FolderName { return f.name }

// System returns the code of a system folder; "" for an own folder.
func (f Folder) System() System { return f.system }

// IsSystem reports whether the folder is a system folder.
func (f Folder) IsSystem() bool { return f.system != "" }

// FolderState is the stored form of a folder.
type FolderState struct {
	ID     shared.FolderID
	Name   string
	System System
}

func (f Folder) state() FolderState {
	return FolderState{ID: f.id, Name: f.name.text, System: f.system}
}
