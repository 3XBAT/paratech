package agent

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/cases/library"
	"paratech/desktop/internal/cases/notes"
	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/note"
	"paratech/desktop/internal/domain/shared"
)

// note (catalog v10, ADR-0033 §8, §10): «Запиши в конспект» and the changes of «Мой конспект» through the use case of
// the notes page. The text, the title and the file names are the student's: stored as the page stores them, never
// logged and never sent back to the planner, which gets the note ID and counters only (ADR-0022). The text and the
// file names are never journaled; a rename keeps the old and the new title in the local journal (RetitleNote) for Undo
// and Recover.
//
// The subject of a note: an add to a note takes the note's; to a lesson, the library subject the lesson names, else the
// subject of the request; to a date, the subject of the request (the context chip), none is refused
// (errNoteNeedsSubject: the model asks the student to pick one). A move keeps the note's own subject.

// Notes is «Мой конспект» (cases/notes) as list_notes, note and their Undo use it: the use cases of the notes page
// (NotesService) plus the reads of one note, an anchor and the subject of a lesson, the add of an upload and the
// removal of one part (the Undo of an add into a note that existed).
type Notes interface {
	Note(ctx context.Context, id shared.NoteID) (notes.View, error)
	ForLesson(ctx context.Context, l notes.Lecture) ([]notes.View, error)
	ForSubject(ctx context.Context, sub shared.SubjectID) ([]notes.View, error)
	Anchor(ctx context.Context, ref notes.AnchorRef) (note.Anchor, error)
	LessonSubject(ctx context.Context, id shared.LessonID) (string, error)
	Paste(ctx context.Context, req notes.PasteRequest) (notes.Added, error)
	AddMaterial(ctx context.Context, req notes.MaterialRequest) (notes.Added, error)
	Reanchor(ctx context.Context, id shared.NoteID, ref notes.AnchorRef) (notes.View, error)
	Retitle(ctx context.Context, id shared.NoteID, title string) (notes.View, error)
	Remove(ctx context.Context, id shared.NoteID) (notes.View, error)
	Restore(ctx context.Context, id shared.NoteID) (notes.View, error)
	RemovePart(ctx context.Context, id shared.NoteID, part shared.MaterialID) (notes.View, error)
}

// errNoteNeedsSubject: an add to a date (or to a lesson that names no subject of the library) in a request without a
// subject; the planner relays «выбери предмет».
var errNoteNeedsSubject = errors.New("agent: a note of a day needs the subject of the request")

// note prepares one action of note.
func (a *Actions) note(ctx context.Context, at origin, call values.Call) (prepared, error) {
	if a.notes == nil {
		return prepared{}, errUnavailable
	}
	act, _ := call.Args().Enum(values.ArgAction)
	switch act {
	case values.NoteAddText, values.NoteAddUpload:
		return a.addToNote(ctx, at, call, act)
	case values.NoteMove:
		return a.moveNote(ctx, call)
	case values.NoteRename:
		return a.renameNote(ctx, call)
	case values.NoteRemove:
		return a.removeNote(ctx, call)
	}
	return prepared{}, errUnavailable
}

// noteTarget is where an add goes: the note it names or the active note of the lecture (note), else a new note of
// subject at anchor; key names that lecture in the intent, for Recover.
type noteTarget struct {
	note    *note.Note
	subject shared.SubjectID
	anchor  notes.AnchorRef
	key     string
}

// addToNote is «Запиши в конспект» (add_text: the text as a TXT part, as «Вставить текст» of the page) and the add of
// an upload of the panel (add_upload). Into a note that existed Undo takes the part out again (RemoveNotePart); an add
// that made the note removes it (RemoveNote). The same upload again is «уже добавлено»: nothing is journaled.
func (a *Actions) addToNote(ctx context.Context, at origin, call values.Call, act string) (prepared, error) {
	args := call.Args()
	t, err := a.noteTarget(ctx, at, args)
	if err != nil {
		return prepared{}, err
	}
	var title string
	if l, ok := args.Label("title"); ok {
		title = string(l)
	}

	var (
		in   = action.Intent{Name: t.key}
		into shared.NoteID
		add  func(ctx context.Context) (notes.Added, error)
		line = values.LineResult{Kind: values.LineNoteUpload}
	)
	if t.note != nil {
		ref, err := noteRef(t.note.ID())
		if err != nil {
			return prepared{}, err
		}
		into = t.note.ID()
		in = action.Intent{Target: ref, Before: t.note.Version()}
	}
	// Refused before anything is journaled, as the use case refuses it; an upload that is a part already is a no-op first.
	full := t.note != nil && len(t.note.Parts())+1 > note.MaxParts
	if act == values.NoteAddText {
		if full {
			return prepared{}, notes.ErrTooManyParts
		}
		text, _ := args.Text("text")
		// Only the action line of the live update shows the start of the text: never the intent, the result or a log.
		line = values.LineResult{Kind: values.LineNoteText, Excerpt: values.NoteExcerpt(string(text))}
		add = func(ctx context.Context) (notes.Added, error) {
			return a.notes.Paste(ctx, notes.PasteRequest{Note: into, Anchor: t.anchor, Subject: t.subject, Title: title, Text: string(text)})
		}
	} else {
		up, _ := args.ID("upload")
		id := shared.MaterialID(up.ID())
		// Refused before anything is journaled; the use case checks the same again.
		m, err := a.library.Material(ctx, id)
		switch {
		case err != nil:
			return prepared{}, err
		case !m.Active():
			return prepared{}, notes.ErrMaterialRemoved
		case !m.InInbox() && m.Subject() != t.subject:
			return prepared{}, notes.ErrOtherSubject
		}
		if t.note != nil {
			if slices.Contains(t.note.Parts(), id) {
				return prepared{noopNote: &values.NoteWriteRow{Note: in.Target}}, nil
			}
			if full {
				return prepared{}, notes.ErrTooManyParts
			}
			// The upload is the one part this add can make: Recover looks for it.
			part, err := values.NewRef(values.KindMaterial, string(id))
			if err != nil {
				return prepared{}, err
			}
			in.Next = action.RemoveNotePart{Note: in.Target, Part: part}
		} else {
			in.Name = withUpload(t.key, id) // Recover takes a new note as this add's only by this part
		}
		add = func(ctx context.Context) (notes.Added, error) {
			return a.notes.AddMaterial(ctx, notes.MaterialRequest{Note: into, Anchor: t.anchor, Subject: t.subject, Title: title, Material: id})
		}
	}
	return prepared{
		intent: in,
		run: func(ctx context.Context) (effect, error) {
			// Not in one transaction with the add: it files a material and saves the note (two aggregates, ARCH-23).
			if into != "" {
				if _, err := a.noteAt(ctx, into, in.Before); err != nil {
					return effect{}, err
				}
			}
			res, err := add(ctx)
			if errors.Is(err, notes.ErrAlreadyAdded) && into != "" {
				// Another add made it a part meanwhile: «уже добавлено», as for a part that was there before.
				return a.alreadyAdded(ctx, in.Target), nil
			}
			if err != nil {
				return effect{}, err
			}
			eff, err := addEffect(res)
			eff.line = line
			return eff, err
		},
	}, nil
}

// noteTarget finds where an add of args goes (see the subject rule at the top of the file).
func (a *Actions) noteTarget(ctx context.Context, at origin, args values.Args) (noteTarget, error) {
	if r, ok := args.ID("note"); ok {
		n, err := a.activeNote(ctx, shared.NoteID(r.ID()))
		if err != nil {
			return noteTarget{}, err
		}
		return noteTarget{note: n, subject: n.Subject()}, nil
	}
	var (
		t       noteTarget
		lecture notes.Lecture
	)
	if r, ok := args.ID("lesson"); ok {
		id := shared.LessonID(r.ID())
		sub, err := a.lessonSubject(ctx, id, at.subject)
		if err != nil {
			return noteTarget{}, err
		}
		t = noteTarget{
			subject: sub, anchor: notes.AnchorRef{Kind: note.AnchorLesson, Lesson: id},
			key: lessonKey + string(id) + ":" + string(sub),
		}
		lecture = notes.Lecture{Lesson: id}
	} else {
		d, _ := args.Date("date")
		day, err := noteDate(d)
		if err != nil {
			return noteTarget{}, err
		}
		if at.subject == "" {
			return noteTarget{}, errNoteNeedsSubject
		}
		t = noteTarget{
			subject: at.subject, anchor: notes.AnchorRef{Kind: note.AnchorDay, Date: day},
			key: dayKey + day.String() + ":" + string(at.subject),
		}
		lecture = notes.Lecture{Subject: at.subject, Date: day}
	}
	// A subject that is not in the library (removed meanwhile, TASK-288/308) takes no notes. Its ErrSubjectNotFound
	// answers unknown_id (noteRefusal), also for the subject of the request: the planner holds an ID that is gone.
	if _, err := a.subject(ctx, t.subject); err != nil {
		return noteTarget{}, err
	}
	n, err := a.lectureNote(ctx, lecture, t.subject)
	if err != nil {
		return noteTarget{}, err
	}
	t.note = n
	return t, nil
}

// Keys of the lecture of an add that makes a note, in Intent.Name: the lesson or the date, ':' and the subject, then
// for an upload '|' and its ID. IDs hold neither ':' nor '|' (shared.validateID).
const (
	lessonKey = "note:lesson:"
	dayKey    = "note:day:"
	uploadSep = "|"
)

// newNoteKey is a key read back.
type newNoteKey struct {
	lecture notes.Lecture
	subject shared.SubjectID
	upload  shared.MaterialID // "": an add of a text
}

// withUpload is the key of an add of the upload id.
func withUpload(key string, id shared.MaterialID) string { return key + uploadSep + string(id) }

// parseNoteKey reads a key back; ok is false for anything else.
func parseNoteKey(key string) (k newNoteKey, ok bool) {
	key, up, hasUpload := strings.Cut(key, uploadSep)
	if hasUpload {
		if up == "" {
			return newNoteKey{}, false
		}
		k.upload = shared.MaterialID(up)
	}
	if rest, found := strings.CutPrefix(key, lessonKey); found {
		id, s, found := strings.Cut(rest, ":")
		if !found || id == "" || s == "" {
			return newNoteKey{}, false
		}
		k.lecture, k.subject = notes.Lecture{Lesson: shared.LessonID(id)}, shared.SubjectID(s)
		return k, true
	}
	rest, found := strings.CutPrefix(key, dayKey)
	if !found {
		return newNoteKey{}, false
	}
	date, s, found := strings.Cut(rest, ":")
	d, err := note.ParseDate(date)
	if !found || err != nil || s == "" {
		return newNoteKey{}, false
	}
	k.lecture, k.subject = notes.Lecture{Subject: shared.SubjectID(s), Date: d}, shared.SubjectID(s)
	return k, true
}

// lessonSubject is the library subject a lesson names, else the subject of the request.
func (a *Actions) lessonSubject(ctx context.Context, id shared.LessonID, scope shared.SubjectID) (shared.SubjectID, error) {
	name, err := a.notes.LessonSubject(ctx, id)
	if err != nil {
		return "", err
	}
	if name != "" {
		found, err := a.library.ResolveSubjects(ctx, []string{name})
		if err != nil {
			return "", err
		}
		if m, ok := found[name]; ok {
			return m.ID, nil
		}
	}
	if scope == "" {
		return "", errNoteNeedsSubject
	}
	return scope, nil
}

// lectureNote is the active note of sub at lecture l an add goes to, as the use case picks it; nil for none.
func (a *Actions) lectureNote(ctx context.Context, l notes.Lecture, sub shared.SubjectID) (*note.Note, error) {
	views, err := a.notes.ForLesson(ctx, l)
	if err != nil {
		return nil, err
	}
	for _, v := range views {
		if v.Note.Subject() == sub {
			return v.Note, nil
		}
	}
	return nil, nil
}

// addEffect is the effect of an add of one part: the note it made, or the part it added to a note that existed, or
// nothing for a part that was there already (no «Отменить»).
func addEffect(res notes.Added) (effect, error) {
	if len(res.Parts) != 1 || res.View.Note == nil {
		return effect{}, fmt.Errorf("agent: note: %d parts added", len(res.Parts))
	}
	n, p := res.View.Note, res.Parts[0]
	ref, err := noteRef(n.ID())
	if err != nil {
		return effect{}, err
	}
	row := values.NoteWriteRow{Note: ref, Parts: len(n.Parts())}
	switch {
	case p.Err != nil:
		return effect{}, p.Err
	case res.Created:
		row.Changed, row.Created = true, true
		return effect{comp: action.RemoveNote{Note: ref}, version: n.Version(), note: &row}, nil
	case p.AlreadyAdded || p.Material == nil:
		return effect{note: &row, failed: true}, nil
	}
	row.Changed = true
	return effect{comp: action.RemoveNotePart{Note: ref, Part: materialRef(p.Material)}, version: n.Version(), note: &row}, nil
}

// alreadyAdded is the effect of an add whose part the note got meanwhile: nothing changed, no «Отменить»; the counter
// is the note's now, when it can be read.
func (a *Actions) alreadyAdded(ctx context.Context, ref values.Ref) effect {
	row := values.NoteWriteRow{Note: ref}
	if v, err := a.notes.Note(ctx, shared.NoteID(ref.ID())); err == nil {
		row.Parts = len(v.Note.Parts())
	}
	return effect{note: &row, failed: true}
}

// moveNote attaches the note to a lesson or to a day of its own subject; Undo puts back the anchor read here.
func (a *Actions) moveNote(ctx context.Context, call values.Call) (prepared, error) {
	args := call.Args()
	ref, _ := args.ID("note")
	id := shared.NoteID(ref.ID())
	n, err := a.activeNote(ctx, id)
	if err != nil {
		return prepared{}, err
	}
	var to notes.AnchorRef
	if l, ok := args.ID("lesson"); ok {
		to = notes.AnchorRef{Kind: note.AnchorLesson, Lesson: shared.LessonID(l.ID())}
	} else {
		d, _ := args.Date("date")
		day, err := noteDate(d)
		if err != nil {
			return prepared{}, err
		}
		to = notes.AnchorRef{Kind: note.AnchorDay, Date: day}
	}
	next, err := a.notes.Anchor(ctx, to)
	if err != nil {
		return prepared{}, err
	}
	prior, err := anchorOf(n.Anchor())
	if err != nil {
		return prepared{}, err
	}
	wanted, err := anchorOf(next)
	if err != nil {
		return prepared{}, err
	}
	if wanted == prior {
		return prepared{noopNote: &values.NoteWriteRow{Note: ref}}, nil
	}
	undo := action.ReanchorNote{Note: ref, Prior: prior}
	return a.changeNote(ref, n.Version(), values.LineNoteMove, undo, action.ReanchorNote{Note: ref, Prior: wanted},
		func(ctx context.Context) (notes.View, error) { return a.notes.Reanchor(ctx, id, to) }), nil
}

// renameNote sets the title to the student's words; Undo puts back the title read here.
func (a *Actions) renameNote(ctx context.Context, call values.Call) (prepared, error) {
	args := call.Args()
	ref, _ := args.ID("note")
	id := shared.NoteID(ref.ID())
	l, _ := args.Label("title")
	title := note.CleanTitle(string(l))
	if title == "" {
		return prepared{}, notes.ErrEmptyTitle
	}
	n, err := a.activeNote(ctx, id)
	if err != nil {
		return prepared{}, err
	}
	if n.Title() == title {
		return prepared{noopNote: &values.NoteWriteRow{Note: ref}}, nil
	}
	undo := action.RetitleNote{Note: ref, OldTitle: n.Title()}
	return a.changeNote(ref, n.Version(), values.LineNoteRename, undo, action.RetitleNote{Note: ref, OldTitle: title},
		func(ctx context.Context) (notes.View, error) { return a.notes.Retitle(ctx, id, title) }), nil
}

// removeNote removes the note softly (its files stay in the library); Undo restores it.
func (a *Actions) removeNote(ctx context.Context, call values.Call) (prepared, error) {
	ref, _ := call.Args().ID("note")
	id := shared.NoteID(ref.ID())
	v, err := a.notes.Note(ctx, id)
	if err != nil {
		return prepared{}, err
	}
	if !v.Note.Active() {
		return prepared{noopNote: &values.NoteWriteRow{Note: ref}}, nil
	}
	return a.changeNote(ref, v.Note.Version(), values.LineNoteRemove, action.RestoreNote{Note: ref}, nil,
		func(ctx context.Context) (notes.View, error) { return a.notes.Remove(ctx, id) }), nil
}

// changeNote runs change on the note ref while it is still at version before, in one transaction with that check; the
// action line words it as kind.
func (a *Actions) changeNote(ref values.Ref, before int64, kind values.LineKind, undo, next action.Compensation,
	change func(ctx context.Context) (notes.View, error)) prepared {
	id := shared.NoteID(ref.ID())
	return prepared{
		intent: action.Intent{Target: ref, Before: before, Prior: undo, Next: next},
		run: func(ctx context.Context) (effect, error) {
			var e effect
			err := a.tx.Do(ctx, func(ctx context.Context) error {
				if _, err := a.noteAt(ctx, id, before); err != nil {
					return err
				}
				v, err := change(ctx)
				if err != nil {
					return err
				}
				e = effect{
					comp: undo, version: v.Note.Version(), note: &values.NoteWriteRow{Note: ref, Changed: true},
					line: values.LineResult{Kind: kind},
				}
				return nil
			})
			return e, err
		},
	}
}

// activeNote reads a note that is not removed.
func (a *Actions) activeNote(ctx context.Context, id shared.NoteID) (*note.Note, error) {
	v, err := a.notes.Note(ctx, id)
	if err != nil {
		return nil, err
	}
	if !v.Note.Active() {
		return nil, notes.ErrRemoved
	}
	return v.Note, nil
}

// noteAt reads the note id and checks it is still at version before.
func (a *Actions) noteAt(ctx context.Context, id shared.NoteID, before int64) (*note.Note, error) {
	v, err := a.notes.Note(ctx, id)
	if err != nil {
		return nil, err
	}
	if v.Note.Version() != before {
		return nil, errMovedOn
	}
	return v.Note, nil
}

// noteRefusal maps the errors of note the generic map does not know. A note, upload or subject that is not in the
// library (a note removed with its subject, TASK-288/308) is unknown, as a deleted assignment is.
func noteRefusal(tool values.Tool, err error) (values.Refusal, bool) {
	if tool != values.ToolNote {
		return "", false
	}
	switch {
	case errors.Is(err, notes.ErrNoteNotFound), errors.Is(err, library.ErrSubjectNotFound),
		errors.Is(err, notes.ErrMaterialNotFound), errors.Is(err, library.ErrMaterialNotFound):
		return values.RefuseUnknownID, true
	case errors.Is(err, notes.ErrRemoved), errors.Is(err, notes.ErrMaterialRemoved), errors.Is(err, notes.ErrLessonNotFound),
		errors.Is(err, notes.ErrRecordingUnavailable):
		return values.RefuseNotFound, true
	case errors.Is(err, errNoteNeedsSubject), errors.Is(err, notes.ErrEmptyTitle), errors.Is(err, notes.ErrEmptyText),
		errors.Is(err, notes.ErrInvalidText), errors.Is(err, notes.ErrTextTooLarge), errors.Is(err, notes.ErrOtherSubject),
		errors.Is(err, notes.ErrInvalidRequest), errors.Is(err, notes.ErrTooManyParts):
		return values.RefuseInvalidArgs, true
	case errors.Is(err, notes.ErrBusy):
		return values.RefuseNotNow, true // the note kept changing under the save: later
	}
	return "", false
}

// --- Undo and Recover ----------------------------------------------------------------------------------------

// noteOf is the note a note compensation acts on.
func noteOf(c action.Compensation) (shared.NoteID, bool) {
	switch v := c.(type) {
	case action.RemoveNotePart:
		return shared.NoteID(v.Note.ID()), true
	case action.RemoveNote:
		return shared.NoteID(v.Note.ID()), true
	case action.ReanchorNote:
		return shared.NoteID(v.Note.ID()), true
	case action.RetitleNote:
		return shared.NoteID(v.Note.ID()), true
	case action.RestoreNote:
		return shared.NoteID(v.Note.ID()), true
	}
	return "", false
}

// checkNote reads the target of a note compensation: its version (the store's, bumped by every save) and whether it is
// already as the Undo leaves it. A note that is gone, also with its removed subject, is a changed target.
func (a *Actions) checkNote(ctx context.Context, c action.Compensation) (action.Target, error) {
	id, ok := noteOf(c)
	if !ok || a.notes == nil {
		return action.Target{}, fmt.Errorf("%w: %T", action.ErrInvalidCompensation, c)
	}
	v, err := a.notes.Note(ctx, id)
	if errors.Is(err, notes.ErrNoteNotFound) {
		return deletedTarget, nil
	}
	if err != nil {
		return action.Target{}, err
	}
	n := v.Note
	t := action.Target{Version: n.Version()}
	switch c := c.(type) {
	case action.RemoveNotePart:
		t.Compensated = !slices.Contains(n.Parts(), shared.MaterialID(c.Part.ID()))
	case action.RemoveNote:
		t.Compensated = !n.Active()
	case action.ReanchorNote:
		cur, err := anchorOf(n.Anchor())
		t.Compensated = err == nil && cur == c.Prior
	case action.RetitleNote:
		t.Compensated = n.Title() == c.OldTitle
	case action.RestoreNote:
		t.Compensated = n.Active()
	}
	return t, nil
}

// compensateNote applies a note compensation to a note Apply found at its expected version. A refusal of the use case
// that means the note is not as the action left it (removed, its last part, an anchor that is gone) is a change.
func (a *Actions) compensateNote(ctx context.Context, c action.Compensation) error {
	id, _ := noteOf(c)
	var err error
	switch v := c.(type) {
	case action.RemoveNotePart:
		_, err = a.notes.RemovePart(ctx, id, shared.MaterialID(v.Part.ID()))
	case action.RemoveNote:
		_, err = a.notes.Remove(ctx, id)
	case action.ReanchorNote:
		_, err = a.notes.Reanchor(ctx, id, anchorRefOf(v.Prior))
	case action.RetitleNote:
		_, err = a.notes.Retitle(ctx, id, v.OldTitle)
	case action.RestoreNote:
		_, err = a.notes.Restore(ctx, id)
	default:
		return fmt.Errorf("%w: %T", action.ErrInvalidCompensation, c)
	}
	switch {
	case errors.Is(err, notes.ErrRemoved), errors.Is(err, notes.ErrNoFiles), errors.Is(err, notes.ErrNoteNotFound),
		errors.Is(err, notes.ErrRecordingUnavailable), errors.Is(err, notes.ErrLessonNotFound), errors.Is(err, notes.ErrBusy),
		errors.Is(err, notes.ErrChanged):
		return fmt.Errorf("%w: %w", errMovedOn, err)
	}
	return err
}

// observeNote settles a PENDING note entry after a crash: move, rename and remove by the state their intent leads to;
// an add into a note that existed by its upload (an added text cannot be told from a change of the student); an add
// that made a note by the note of its lecture made since the entry.
func (a *Actions) observeNote(ctx context.Context, in action.Intent, since time.Time) (action.Observation, error) {
	if a.notes == nil {
		return action.Observation{}, nil
	}
	if in.Target.IsZero() {
		return a.observeNewNote(ctx, in, since)
	}
	v, err := a.notes.Note(ctx, shared.NoteID(in.Target.ID()))
	if errors.Is(err, notes.ErrNoteNotFound) {
		return action.Observation{Known: true}, nil
	}
	if err != nil {
		return action.Observation{}, err
	}
	n := v.Note
	switch p := in.Prior.(type) {
	case action.ReanchorNote:
		next, ok := in.Next.(action.ReanchorNote)
		cur, err := anchorOf(n.Anchor())
		return observed(n.Version(), in, ok && err == nil && cur == next.Prior), nil
	case action.RetitleNote:
		next, ok := in.Next.(action.RetitleNote)
		return observed(n.Version(), in, ok && n.Title() == next.OldTitle), nil
	case action.RestoreNote:
		return observed(n.Version(), in, !n.Active()), nil
	case nil:
		next, ok := in.Next.(action.RemoveNotePart)
		switch {
		case n.Version() == in.Before:
			return action.Observation{Known: true, Version: n.Version()}, nil
		case ok && slices.Contains(n.Parts(), shared.MaterialID(next.Part.ID())):
			return action.Observation{Known: true, Version: n.Version(), Effect: true, Compensation: next}, nil
		}
		return action.Observation{}, nil
	default:
		return action.Observation{}, fmt.Errorf("%w: %T", action.ErrInvalidCompensation, p)
	}
}

// observeNewNote finds the note an add made: the one active note of its lecture and subject made at or after the entry,
// and only when this add made it (madeBy). Another note of the lecture changed since, or a new note the student may
// have made, is unknown: Undo would remove the student's note.
func (a *Actions) observeNewNote(ctx context.Context, in action.Intent, since time.Time) (action.Observation, error) {
	k, ok := parseNoteKey(in.Name)
	if !ok {
		return action.Observation{}, nil
	}
	views, err := a.notes.ForLesson(ctx, k.lecture)
	if err != nil {
		return action.Observation{}, err
	}
	var made []*note.Note
	for _, v := range views {
		n := v.Note
		switch {
		case n.Subject() != k.subject:
		case !n.CreatedAt().Before(since):
			made = append(made, n)
		case !n.UpdatedAt().Before(since):
			return action.Observation{}, nil
		}
	}
	switch len(made) {
	case 0:
		return action.Observation{Known: true}, nil
	case 1:
		mine, err := a.madeBy(ctx, made[0], k, since)
		if err != nil || !mine {
			return action.Observation{}, err
		}
		ref, err := noteRef(made[0].ID())
		if err != nil {
			return action.Observation{}, err
		}
		return action.Observation{Known: true, Version: made[0].Version(), Effect: true, Compensation: action.RemoveNote{Note: ref}}, nil
	}
	return action.Observation{}, nil
}

// madeBy reports whether the add of key k made the note n: its only part is the upload of the add, or, for a text, a
// TXT material stored at or after the entry (a text the library had before cannot be told from the student's).
func (a *Actions) madeBy(ctx context.Context, n *note.Note, k newNoteKey, since time.Time) (bool, error) {
	parts := n.Parts()
	if len(parts) != 1 {
		return false, nil
	}
	if k.upload != "" {
		return parts[0] == k.upload, nil
	}
	m, err := a.library.Material(ctx, parts[0])
	switch {
	case errors.Is(err, library.ErrMaterialNotFound), errors.Is(err, shared.ErrNotFound):
		return false, nil
	case err != nil:
		return false, err
	}
	return m.Type() == material.TypeTXT && !m.CreatedAt().Before(since), nil
}

// replayNote rebuilds the result of a note call from its compensation; an add shows the parts the note has now.
func (t tools) replayNote(ctx context.Context, c agentturn.Call, comp action.Compensation) (ran, bool) {
	id, ok := noteOf(comp)
	if !ok {
		return ran{}, false
	}
	ref, err := noteRef(id)
	if err != nil {
		return ran{}, false
	}
	row := values.NoteWriteRow{Note: ref, Changed: true}
	_, created := comp.(action.RemoveNote)
	_, part := comp.(action.RemoveNotePart)
	if created || part {
		row.Created = created
		// Without the use case wired the replay answers without the counter.
		if t.actions.notes == nil {
			return t.noteResult(c, row, nil), true
		}
		if v, err := t.actions.notes.Note(ctx, id); err == nil {
			row.Parts = len(v.Note.Parts())
		}
	}
	return t.noteResult(c, row, nil), true
}

// noteResult is the result of note: the note's ID and counters.
func (t tools) noteResult(c agentturn.Call, row values.NoteWriteRow, cards []values.Card) ran {
	res, seen, err := values.NoteResult(c.Call.ID(), row)
	if err != nil {
		t.log.Warn(opAction, "tool", c.Call.Tool().String(), "err", err)
		return ran{out: agentturn.Outcome{Result: values.RefusalResult(c.Call.ID(), values.RefuseFailed)}, write: true}
	}
	return ran{out: agentturn.Outcome{Result: res, Seen: seen}, write: true, cards: cards}
}

// --- conversions ---------------------------------------------------------------------------------------------

func noteRef(id shared.NoteID) (values.Ref, error) { return values.NewRef(values.KindNote, string(id)) }

// noteDate is the date of an argument as a note date.
func noteDate(d values.Date) (note.Date, error) {
	nd, err := note.NewDate(d.Year(), d.Month(), d.Day())
	if err != nil {
		return note.Date{}, fmt.Errorf("%w: %w", notes.ErrInvalidRequest, err)
	}
	return nd, nil
}

// anchorOf is the stored anchor of a note as a compensation holds it: kind, recording or lesson and date.
func anchorOf(a note.Anchor) (action.NoteAnchor, error) {
	out := action.NoteAnchor{Kind: action.NoteAnchorKind(a.Kind())}
	var err error
	switch a.Kind() {
	case note.AnchorRecording:
		out.Target, err = values.NewRef(values.KindRecording, string(a.Recording()))
	case note.AnchorLesson:
		out.Target, err = values.NewRef(values.KindLesson, string(a.Lesson()))
	}
	if err != nil {
		return action.NoteAnchor{}, err
	}
	if d := a.Date(); !d.IsZero() {
		if out.Date, err = values.NewDate(d.Year(), d.Month(), d.Day()); err != nil {
			return action.NoteAnchor{}, err
		}
	}
	return out, nil
}

// anchorRefOf is the anchor of a compensation as the use case takes it; the snapshot is taken again.
func anchorRefOf(a action.NoteAnchor) notes.AnchorRef {
	ref := notes.AnchorRef{Kind: note.AnchorKind(a.Kind)}
	switch a.Kind {
	case action.AnchorRecording:
		ref.Recording = shared.RecordingID(a.Target.ID())
	case action.AnchorLesson:
		ref.Lesson = shared.LessonID(a.Target.ID())
	case action.AnchorDay:
		ref.Date, _ = note.NewDate(a.Date.Year(), a.Date.Month(), a.Date.Day())
	}
	return ref
}
