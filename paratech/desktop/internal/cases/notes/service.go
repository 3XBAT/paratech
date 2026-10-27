package notes

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/note"
	"paratech/desktop/internal/domain/shared"
)

// Operation names of the log.
const opNoteBusy = "notes: note changed while saved"

// saveAttempts bounds the saves of one change that lose against another save of the same note.
const saveAttempts = 3

// Pasted text.
const (
	pastedExt       = ".txt"
	maxPastedName   = 100 // runes of the first line in the file name of a pasted text
	maxTitleScanned = 4096
)

// Service is the use case of the student's own notes. Everything is local: no port talks to the network.
type Service struct {
	notes      NoteRepository
	materials  Materials
	recordings Recordings
	lessons    Lessons
	clock      Clock
	ids        IDs
	log        *slog.Logger
}

// New checks that every required port is given (see Deps).
func New(d Deps) (*Service, error) {
	if d.Notes == nil || d.Materials == nil || d.Recordings == nil || d.Lessons == nil || d.Clock == nil || d.IDs == nil {
		return nil, fmt.Errorf("%w: every port except the logger is required", ErrInvalidConfig)
	}
	log := d.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Service{
		notes: d.Notes, materials: d.Materials, recordings: d.Recordings, lessons: d.Lessons, clock: d.Clock, ids: d.IDs,
		log: log,
	}, nil
}

// target is the note files go to: an existing one, or nil for a new note of subject at anchor.
type target struct {
	note    *note.Note
	subject shared.SubjectID
	anchor  note.Anchor
}

// Add stores the files in the library and adds them to the notes of a lecture (see AddRequest), each file on its own:
// the result tells per file what happened. It fails as a whole for a bad request or anchor, for more files than the
// note has room for (ErrTooManyParts, nothing stored), or when no file could be
// stored (with the error of the first one); otherwise the files that failed carry their error in their result.
func (s *Service) Add(ctx context.Context, req AddRequest) (Added, error) {
	switch {
	case len(req.Uploads) == 0:
		return Added{}, ErrNoFiles
	case len(req.Uploads) > note.MaxParts:
		return Added{}, ErrTooManyParts
	}
	t, err := s.target(ctx, req)
	if err != nil {
		return Added{}, err
	}
	// Refused before anything is filed, so a full note leaves no new files in the library. Files that are parts
	// already count too: the bound is checked without storing them.
	if t.note != nil && len(t.note.Parts())+len(req.Uploads) > note.MaxParts {
		return Added{}, ErrTooManyParts
	}

	res := Added{Parts: make([]PartResult, 0, len(req.Uploads))}
	var (
		ids      []shared.MaterialID
		firstErr error
		title    = req.Title
		stored   bool
	)
	for _, up := range req.Uploads {
		r := s.filePart(ctx, t.subject, t.anchor.LessonKind(), up)
		switch {
		case r.Err != nil:
			firstErr = cmp.Or(firstErr, r.Err)
		case slices.Contains(ids, r.Material.ID()) || t.note != nil && slices.Contains(t.note.Parts(), r.Material.ID()):
			r.AlreadyAdded, stored = true, true
		default:
			ids, stored = append(ids, r.Material.ID()), true
			if title == "" {
				title = up.Name
			}
		}
		res.Parts = append(res.Parts, r)
	}
	if !stored {
		return res, firstErr
	}

	var n *note.Note
	switch {
	case len(ids) == 0: // every file is a part already
		n = t.note
	case t.note == nil:
		n, err = s.create(ctx, t, title, ids)
		res.Created = true
	default:
		n, err = s.change(ctx, t.note.ID(), func(n *note.Note, now time.Time) (bool, error) {
			return n.AddParts(ids, now)
		})
	}
	if err != nil {
		return res, err
	}
	res.View, err = s.view(ctx, n)
	return res, err
}

// Paste stores the text as a TXT material of at most MaxPastedBytes and adds it as Add adds a file. The same text
// pasted again is the same material, so it is «уже добавлено».
func (s *Service) Paste(ctx context.Context, req PasteRequest) (Added, error) {
	switch {
	case strings.TrimSpace(req.Text) == "":
		return Added{}, ErrEmptyText
	case len(req.Text) > MaxPastedBytes:
		return Added{}, ErrTextTooLarge
	}
	data := []byte(req.Text)
	if typ, err := material.DetectType(data); err != nil || typ != material.TypeTXT {
		return Added{}, ErrInvalidText
	}
	first := firstLine(req.Text)
	title := cmp.Or(req.Title, first)
	return s.Add(ctx, AddRequest{
		Note: req.Note, Anchor: req.Anchor, Subject: req.Subject, Title: title,
		Uploads: []Upload{{Name: cutRunes(first, maxPastedName) + pastedExt, Data: data}},
	})
}

// ReplacePart stores the file and puts it in the place of the part old (a changed file is a new material). The old
// material stays in the library, so ReplacePartWith(next, old) undoes it.
func (s *Service) ReplacePart(ctx context.Context, id shared.NoteID, old shared.MaterialID, up Upload) (Replaced, error) {
	n, err := s.activeNote(ctx, id)
	if err != nil {
		return Replaced{}, err
	}
	if !slices.Contains(n.Parts(), old) {
		return Replaced{}, ErrPartNotFound // checked before anything is stored
	}
	r := s.filePart(ctx, n.Subject(), n.Anchor().LessonKind(), up)
	if r.Err != nil {
		return Replaced{}, r.Err
	}
	return s.replace(ctx, id, old, r.Material, r.Existing)
}

// ReplacePartWith puts a material of the library in the place of the part old: the undo of ReplacePart. The material
// id comes from the page, so it must be an active material of the note's subject.
func (s *Service) ReplacePartWith(ctx context.Context, id shared.NoteID, old, next shared.MaterialID) (Replaced, error) {
	if next.Validate() != nil {
		return Replaced{}, ErrInvalidRequest
	}
	n, err := s.activeNote(ctx, id)
	if err != nil {
		return Replaced{}, err
	}
	m, err := s.materials.Material(ctx, next)
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return Replaced{}, ErrMaterialNotFound
	case err != nil:
		return Replaced{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	if err := partOf(m, n.Subject()); err != nil {
		return Replaced{}, err
	}
	return s.replace(ctx, id, old, m, true)
}

func (s *Service) replace(ctx context.Context, id shared.NoteID, old shared.MaterialID, m *material.Material, existing bool) (Replaced, error) {
	n, err := s.change(ctx, id, func(n *note.Note, now time.Time) (bool, error) {
		return n.ReplacePart(old, m.ID(), now)
	})
	if err != nil {
		return Replaced{}, err
	}
	v, err := s.view(ctx, n)
	if err != nil {
		return Replaced{}, err
	}
	return Replaced{View: v, Material: m, Existing: existing, Previous: old}, nil
}

// Reorder sets the order of the parts; order holds every part exactly once.
func (s *Service) Reorder(ctx context.Context, id shared.NoteID, order []shared.MaterialID) (View, error) {
	return s.changeView(ctx, id, func(n *note.Note, now time.Time) (bool, error) { return n.Reorder(order, now) })
}

// Reanchor attaches the note to another lecture, checked as in Add.
func (s *Service) Reanchor(ctx context.Context, id shared.NoteID, ref AnchorRef) (View, error) {
	a, err := s.anchorFor(ctx, ref)
	if err != nil {
		return View{}, err
	}
	return s.changeView(ctx, id, func(n *note.Note, now time.Time) (bool, error) { return n.Reanchor(a, now) })
}

// Retitle sets the display title.
func (s *Service) Retitle(ctx context.Context, id shared.NoteID, title string) (View, error) {
	return s.changeView(ctx, id, func(n *note.Note, now time.Time) (bool, error) { return n.Retitle(title, now) })
}

// Remove removes the note softly; Restore brings it back as it was. Its files stay in the library.
func (s *Service) Remove(ctx context.Context, id shared.NoteID) (View, error) {
	return s.changeView(ctx, id, func(n *note.Note, now time.Time) (bool, error) { return n.Remove(now) })
}

// Restore brings a removed note back.
func (s *Service) Restore(ctx context.Context, id shared.NoteID) (View, error) {
	return s.changeView(ctx, id, func(n *note.Note, now time.Time) (bool, error) { return n.Restore(now) })
}

// target finds the note the files of req go to.
func (s *Service) target(ctx context.Context, req AddRequest) (target, error) {
	if req.Note != "" {
		n, err := s.activeNote(ctx, req.Note)
		if err != nil {
			return target{}, err
		}
		return target{note: n, subject: n.Subject(), anchor: n.Anchor()}, nil
	}
	if req.Subject.Validate() != nil {
		return target{}, ErrInvalidRequest
	}
	a, err := s.anchorFor(ctx, req.Anchor)
	if err != nil {
		return target{}, err
	}
	n, err := s.lectureNote(ctx, req.Subject, a)
	if err != nil {
		return target{}, err
	}
	return target{note: n, subject: req.Subject, anchor: a}, nil
}

// lectureNote is the first active note of sub at the lecture of a, as ForLesson reads it; nil for none or for a
// lecture without a date.
func (s *Service) lectureNote(ctx context.Context, sub shared.SubjectID, a note.Anchor) (*note.Note, error) {
	var l Lecture
	switch a.Kind() {
	case note.AnchorRecording:
		l.Recording = a.Recording()
	case note.AnchorLesson:
		l.Lesson = a.Lesson()
	case note.AnchorDay:
		l.Subject, l.Date = sub, a.Date()
	default:
		return nil, nil
	}
	list, keep, err := s.lectureList(ctx, l)
	if err != nil {
		return nil, err
	}
	r := s.newResolver()
	for _, n := range list {
		if !n.Active() || n.Subject() != sub {
			continue
		}
		cur, err := r.current(ctx, n.Anchor())
		if err != nil {
			return nil, err
		}
		if keep == nil || keep(cur) {
			return n, nil
		}
	}
	return nil, nil
}

// filePart stores one file for a note of sub and checks the material it gives.
func (s *Service) filePart(ctx context.Context, sub shared.SubjectID, lessonKind string, up Upload) PartResult {
	r := PartResult{Name: up.Name}
	m, existing, err := s.materials.FileNotePart(ctx, sub, lessonKind, up.Name, up.Data)
	if err == nil {
		err = partOf(m, sub)
	}
	if err != nil {
		r.Err = err
		return r
	}
	r.Material, r.Existing = m, existing
	return r
}

// partOf checks that m may be a part of a note of sub: an active material filed under sub.
func partOf(m *material.Material, sub shared.SubjectID) error {
	switch {
	case m.Subject() != sub:
		return ErrOtherSubject
	case !m.Active():
		return ErrMaterialRemoved
	}
	return nil
}

// create saves a new note of t.
func (s *Service) create(ctx context.Context, t target, title string, parts []shared.MaterialID) (*note.Note, error) {
	n, err := note.New(note.Spec{ID: s.ids.NewNoteID(), Subject: t.subject, Anchor: t.anchor, Title: title, Parts: parts},
		s.clock.Now())
	if err != nil {
		return nil, domainError(err)
	}
	if err := s.notes.Save(ctx, n); err != nil {
		return nil, s.saveError(n.ID(), err)
	}
	return n, nil
}

// activeNote loads a note that is not removed.
func (s *Service) activeNote(ctx context.Context, id shared.NoteID) (*note.Note, error) {
	n, err := s.get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !n.Active() {
		return nil, ErrRemoved
	}
	return n, nil
}

// get loads a note; a missing one is ErrNoteNotFound.
func (s *Service) get(ctx context.Context, id shared.NoteID) (*note.Note, error) {
	if id.Validate() != nil {
		return nil, ErrInvalidRequest
	}
	n, err := s.notes.Get(ctx, id)
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return nil, ErrNoteNotFound
	case err != nil:
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return n, nil
}

// changeView is change and then the view of the note.
func (s *Service) changeView(ctx context.Context, id shared.NoteID, fn func(*note.Note, time.Time) (bool, error)) (View, error) {
	n, err := s.change(ctx, id, fn)
	if err != nil {
		return View{}, err
	}
	return s.view(ctx, n)
}

// change loads the note, applies fn and saves it when fn changed it, again from the start when another save came first.
func (s *Service) change(ctx context.Context, id shared.NoteID, fn func(*note.Note, time.Time) (bool, error)) (*note.Note, error) {
	for attempt := 1; ; attempt++ {
		n, err := s.get(ctx, id)
		if err != nil {
			return nil, err
		}
		changed, err := fn(n, s.clock.Now())
		if err != nil {
			return nil, domainError(err)
		}
		if !changed {
			return n, nil
		}
		err = s.notes.Save(ctx, n)
		if errors.Is(err, shared.ErrVersionConflict) && attempt < saveAttempts {
			continue
		}
		if err != nil {
			return nil, s.saveError(id, err)
		}
		return n, nil
	}
}

// saveError maps an error of NoteRepository.Save.
func (s *Service) saveError(id shared.NoteID, err error) error {
	switch {
	case errors.Is(err, shared.ErrVersionConflict):
		s.log.Warn(opNoteBusy, slog.String("note", string(id)))
		return ErrBusy
	case errors.Is(err, shared.ErrConflict):
		return fmt.Errorf("%w: %w", ErrChanged, err)
	}
	return fmt.Errorf("%w: %w", ErrUnavailable, err)
}

// domainError maps an error of the note aggregate to the error of the use case; others pass as they are.
func domainError(err error) error {
	var to error
	switch {
	case errors.Is(err, note.ErrInvalidNote), errors.Is(err, note.ErrInvalidAnchor), errors.Is(err, note.ErrInvalidDate),
		errors.Is(err, note.ErrInvalidTransition):
		to = ErrInvalidRequest
	case errors.Is(err, note.ErrEmptyTitle):
		to = ErrEmptyTitle
	case errors.Is(err, note.ErrNoParts):
		to = ErrNoFiles
	case errors.Is(err, note.ErrTooManyParts):
		to = ErrTooManyParts
	case errors.Is(err, note.ErrDuplicatePart):
		to = ErrAlreadyAdded
	case errors.Is(err, note.ErrPartNotFound):
		to = ErrPartNotFound
	case errors.Is(err, note.ErrInvalidOrder):
		to = ErrInvalidOrder
	case errors.Is(err, note.ErrRemoved):
		to = ErrRemoved
	default:
		return err
	}
	return fmt.Errorf("%w: %w", to, err)
}

// firstLine is the first line of text with something in it, trimmed; only the start of a long text is looked at.
func firstLine(text string) string {
	if len(text) > maxTitleScanned {
		text = text[:maxTitleScanned]
	}
	for line := range strings.Lines(text) {
		if line = strings.TrimSpace(line); line != "" {
			return strings.ToValidUTF8(line, "")
		}
	}
	return ""
}

// cutRunes cuts s to at most n runes.
func cutRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}
