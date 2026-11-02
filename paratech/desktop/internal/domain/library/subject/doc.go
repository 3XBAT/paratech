// Package subject is the aggregate of a subject of the library (ADR-0024): its name, aliases, semester and folders.
// The six system folders exist in every subject and cannot be removed; own folders are entities of the aggregate,
// removed only while no file is in them. Whether files are in a folder or the subject is known to the material
// repository, so the use case passes the count to RemoveFolder and Removable. A subject may have a Teacher (ADR-0030),
// set only by the student and never shown to the agent. The student removes a subject softly with everything filed
// under it (Remove, ADR-0027) and may Restore it within RestoreWindow; a removed subject keeps its name and refuses
// every other change (ErrRemoved).
package subject
