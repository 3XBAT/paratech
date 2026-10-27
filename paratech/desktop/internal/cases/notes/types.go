package notes

import (
	"log/slog"

	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/note"
	"paratech/desktop/internal/domain/shared"
)

// MaxPastedBytes bounds a pasted text (TASK-151; ADR-0027 Open 10 may change it).
const MaxPastedBytes = 1 << 20

// Deps are the ports the Service needs; Log is optional.
type Deps struct {
	Notes      NoteRepository
	Materials  Materials
	Recordings Recordings
	Lessons    Lessons
	Clock      Clock
	IDs        IDs
	Log        *slog.Logger
}

// AnchorRef is the lecture a note is attached to, as the student chose it. Kind tells which field is read: Recording
// for note.AnchorRecording, Lesson for note.AnchorLesson, Date for note.AnchorDay, none for note.AnchorUndated. The
// snapshot (date, lesson kind, title) is taken from the recording or the lesson by the use case.
type AnchorRef struct {
	Kind      note.AnchorKind
	Recording shared.RecordingID
	Lesson    shared.LessonID
	Date      note.Date
}

// Lecture names the lecture whose notes ForLesson reads: exactly one of Recording, Lesson, or Subject with Date.
type Lecture struct {
	Recording shared.RecordingID
	Lesson    shared.LessonID
	Subject   shared.SubjectID
	Date      note.Date
}

// Upload is a file the student chose: its bytes and its name (shown only).
type Upload struct {
	Name string
	Data []byte
}

// AddRequest is what Add needs. With Note set the files go to that note, and Anchor and Subject are ignored;
// otherwise to the first active note of Subject at Anchor, or to a new one. Title is the title of a new note; "" takes
// the name of its first file.
type AddRequest struct {
	Note    shared.NoteID
	Anchor  AnchorRef
	Subject shared.SubjectID
	Title   string
	Uploads []Upload
}

// PasteRequest is what Paste needs; Note, Anchor and Subject as in AddRequest. Title "" takes the first line of the
// text.
type PasteRequest struct {
	Note    shared.NoteID
	Anchor  AnchorRef
	Subject shared.SubjectID
	Title   string
	Text    string
}

// PartResult is the outcome of one file of Add: Material when it is in the library, Err otherwise (a use case error
// of this package or of the library). Existing: the same bytes were in the library already. AlreadyAdded: the material
// was a part of the note already (or an earlier file of the request), so no second part was made.
type PartResult struct {
	Name         string
	Material     *material.Material
	Existing     bool
	AlreadyAdded bool
	Err          error
}

// Added is the outcome of Add and Paste: the note as it reads now and one result per file, in order. Created: the
// note is new.
type Added struct {
	View    View
	Parts   []PartResult
	Created bool
}

// Replaced is the outcome of ReplacePart: the note, the material now in the place of Previous, and whether the same
// bytes were in the library already.
type Replaced struct {
	View     View
	Material *material.Material
	Existing bool
	Previous shared.MaterialID
}

// View is a note as it reads now. Anchor is the anchor resolved at read time: a recording or lesson that is gone
// reads as the day of its snapshot. Parts holds the material of each part in order; nil for a material that is gone.
// NoTextPages counts the parts that are photos without text, for «N стр. без текста» (ADR-0027). Counts holds how the
// text of each part is cut, in the order of Parts: zero for a part without text (a photo, a PDF, one that is gone,
// removed or damaged) and for one not read within the bound of a listing (maxCountReads).
type View struct {
	Note        *note.Note
	Anchor      note.Anchor
	Parts       []*material.Material
	Counts      []material.TextCounts
	NoTextPages int
}

// Page is a photo page of a note: the material and its clean bytes (the type tells image/jpeg or image/png).
type Page struct {
	Material *material.Material
	Data     []byte
}
