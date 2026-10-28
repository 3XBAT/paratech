package settings

import (
	"fmt"
	"time"
)

// NoteRecognition is who recognises the text of photo pages of a note («ИИ распознаёт фото конспектов», ADR-0027).
// A later engine (a vision model through the Hub, TASK-168) is one more value here.
type NoteRecognition string

// Note recognition modes.
const (
	NoteRecognitionOff   NoteRecognition = "off"   // photo pages are shown, never recognised
	NoteRecognitionLocal NoteRecognition = "local" // Windows OCR on this computer; the photo never leaves it
)

// DefaultNoteRecognition is the mode of a fresh installation and of every database from before the setting: the
// student turns recognition on (ADR-0027, Open 14).
const DefaultNoteRecognition = NoteRecognitionOff

// Valid reports whether m is a known mode.
func (m NoteRecognition) Valid() bool { return m == NoteRecognitionOff || m == NoteRecognitionLocal }

// OrDefault returns m when it is known, otherwise DefaultNoteRecognition.
func (m NoteRecognition) OrDefault() NoteRecognition {
	if m.Valid() {
		return m
	}
	return DefaultNoteRecognition
}

// String returns the stored form of the mode.
func (m NoteRecognition) String() string { return string(m) }

// NoteRecognition returns who recognises photo pages of notes.
func (s *Settings) NoteRecognition() NoteRecognition { return s.noteRecognition }

// SetNoteRecognition sets who recognises photo pages of notes; it applies to the pages added from now on.
func (s *Settings) SetNoteRecognition(m NoteRecognition, now time.Time) error {
	if !m.Valid() {
		return fmt.Errorf("%w: unknown note recognition %q", ErrInvalidSettings, m)
	}
	return change(s, &s.noteRecognition, m, now)
}
