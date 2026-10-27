package recordingimport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
)

// Operation names used in errors and logs. The OS path of the file is never part of them.
const (
	opImport  = "recordingimport: import a recording"
	opDiscard = "recordingimport: delete the copy"
	opEnqueue = "recordingimport: enqueue the transcription"
)

// Service imports old recordings (see the package doc). One import runs at a time: the copy of a 2 GiB file is enough
// disk work, and the batch import (TASK-163) calls it file by file.
type Service struct {
	originals   Originals
	store       ArtifactStore
	space       DiskSpace
	recordings  RecordingRepository
	lessons     LessonReader
	completions CompletionListener
	clock       Clock
	ids         IDGenerator
	tx          Tx
	log         *slog.Logger

	mu sync.Mutex
}

// New checks that every port is given.
func New(d Deps) (*Service, error) {
	if d.Originals == nil || d.Store == nil || d.Space == nil || d.Recordings == nil || d.Lessons == nil ||
		d.Completions == nil || d.Clock == nil || d.IDs == nil || d.Tx == nil {
		return nil, fmt.Errorf("%w: every port except the logger is required", ErrInvalidConfig)
	}
	s := &Service{
		originals: d.Originals, store: d.Store, space: d.Space, recordings: d.Recordings, lessons: d.Lessons,
		completions: d.Completions, clock: d.Clock, ids: d.IDs, tx: d.Tx, log: d.Log,
	}
	if s.log == nil {
		s.log = slog.New(slog.DiscardHandler)
	}
	return s, nil
}

// Import makes the file of req a COMPLETED recording of its lecture and enqueues its transcription. The same bytes
// again return the recording made from them (Existing), whatever the lecture named now. Refusals: ErrInvalidRequest,
// ErrLessonNotFound, ErrUnreadable, ErrChanged, ErrDeleting, ErrStorage and the domain's errors of the file and the
// lecture (see errors.go). On any refusal after the copy was written the copy is deleted.
func (s *Service) Import(ctx context.Context, req Request) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	res, err := s.importFile(ctx, req)
	if err != nil {
		return Result{}, fmt.Errorf("%s: %w", opImport, err)
	}
	s.enqueue(ctx, res.Recording.ID())
	return res, nil
}

// anchor is the lecture of an import.
type anchor struct {
	lesson shared.LessonID
	snap   values.LessonSnapshot
	start  time.Time
}

func (s *Service) importFile(ctx context.Context, req Request) (Result, error) {
	a, err := s.anchor(ctx, req)
	if err != nil {
		return Result{}, err
	}
	if err := recording.CheckLectureTime(a.start, s.clock.Now()); err != nil {
		return Result{}, err
	}

	src, size, err := s.originals.Open(ctx, req.Path)
	if err != nil {
		return Result{}, unreadable(ctx, err)
	}
	defer func() { _ = src.Close() }()

	if err := recording.CheckImportSize(size); err != nil {
		return Result{}, err
	}
	head := make([]byte, min(size, recording.ImportHeadBytes))
	if _, err := io.ReadFull(src, head); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return Result{}, fmt.Errorf("%w: the file ended before its header", ErrChanged)
		}
		return Result{}, unreadable(ctx, err)
	}
	audio, err := recording.DetectAudio(head, size)
	if err != nil {
		return Result{}, err
	}
	free, err := s.space.FreeBytes(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %w", ErrStorage, err)
	}
	if err := recording.CheckImportSpace(size, free); err != nil {
		return Result{}, err
	}

	id := s.ids.NewRecordingID()
	ref, err := recording.ImportedTrackRef(id, audio.Format)
	if err != nil {
		return Result{}, err
	}
	h := sha256.New()
	content := io.TeeReader(exactReader(ctx, io.MultiReader(bytes.NewReader(head), sourceReader{src}), size), h)
	if err := s.store.Write(ctx, ref, content); err != nil {
		return Result{}, copyError(ctx, err)
	}

	res, err := s.save(ctx, recording.ImportSpec{
		ID: id, LessonID: a.lesson, Lesson: a.snap, StartedAt: a.start, Audio: audio, SHA256: hex.EncodeToString(h.Sum(nil)),
	})
	if err != nil || res.Existing {
		s.discard(ctx, ref)
	}
	return res, err
}

// anchor reads the lecture of req: the past lesson, or the date and names the student gave.
func (s *Service) anchor(ctx context.Context, req Request) (anchor, error) {
	if req.LessonID == "" {
		if req.StartedAt.IsZero() {
			return anchor{}, fmt.Errorf("%w: neither a lesson nor a lecture time", ErrInvalidRequest)
		}
		snap := values.NewLessonSnapshot(values.LessonSnapshotSpec{
			Title: req.Title, Subject: req.Subject, Kind: req.Kind, Start: req.StartedAt,
		})
		return anchor{snap: snap, start: req.StartedAt}, nil
	}

	if err := req.LessonID.Validate(); err != nil {
		return anchor{}, fmt.Errorf("%w: %w", ErrInvalidRequest, err)
	}
	l, err := s.lessons.Get(ctx, req.LessonID)
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return anchor{}, fmt.Errorf("%w: %w", ErrLessonNotFound, err)
	case err != nil:
		return anchor{}, fmt.Errorf("%w: %w", ErrStorage, err)
	}
	return anchor{lesson: req.LessonID, snap: snapshotOf(l), start: l.Content().Start}, nil
}

// snapshotOf is what a recording keeps of l, as the recording session keeps it at a capture (TASK-088).
func snapshotOf(l *lesson.Lesson) values.LessonSnapshot {
	c := l.Content()
	kind := ""
	if c.Kind != lesson.KindUnknown {
		kind = c.Kind.String()
	}
	return values.NewLessonSnapshot(values.LessonSnapshotSpec{
		Title: c.Title, Subject: c.Subject, Kind: kind, Teacher: c.Teacher, Start: c.Start, End: c.End,
	})
}

// save stores the new recording in one transaction, or returns the one of the same digest.
func (s *Service) save(ctx context.Context, spec recording.ImportSpec) (Result, error) {
	var res Result
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		existing, err := s.recordings.GetBySourceSHA256(ctx, spec.SHA256)
		switch {
		case err == nil:
			res = Result{Recording: existing, Existing: true}
			return nil
		case !errors.Is(err, shared.ErrNotFound):
			return err
		}

		rec, err := recording.NewImported(spec, s.clock.Now())
		if err != nil {
			return err
		}
		if err := s.recordings.Save(ctx, rec); err != nil {
			return err
		}
		res = Result{Recording: rec}
		return nil
	})
	if errors.Is(err, shared.ErrDuplicate) {
		// Another import of the same bytes was stored meanwhile.
		existing, gerr := s.recordings.GetBySourceSHA256(ctx, spec.SHA256)
		if gerr != nil {
			return Result{}, fmt.Errorf("%w: %w", ErrStorage, errors.Join(err, gerr))
		}
		res, err = Result{Recording: existing, Existing: true}, nil
	}
	switch {
	case errors.Is(err, recording.ErrInvalidRecording):
		return Result{}, err
	case err != nil:
		return Result{}, fmt.Errorf("%w: %w", ErrStorage, err)
	case res.Existing && res.Recording.Deleting():
		return Result{}, fmt.Errorf("%w: %s", ErrDeleting, res.Recording.ID())
	}
	return res, nil
}

// discard deletes the copy; a cancelled ctx must not keep it on disk. A failure is only logged: the copy is a file
// no recording names.
func (s *Service) discard(ctx context.Context, ref shared.ArtifactRef) {
	if err := s.store.Delete(context.WithoutCancel(ctx), ref); err != nil {
		s.log.Warn(opDiscard, slog.String("file", ref.String()), slog.Any("error", err))
	}
}

// enqueue hands the stored recording to the transcription; idempotent, so an existing one is handed again in case a
// crash lost its first enqueue.
func (s *Service) enqueue(ctx context.Context, id shared.RecordingID) {
	if err := s.completions.RecordingCompleted(context.WithoutCancel(ctx), id); err != nil {
		s.log.Warn(opEnqueue, slog.String("recording", id.String()), slog.Any("error", err))
	}
}

// unreadable wraps an error of the original file; a cancellation stays itself.
func unreadable(ctx context.Context, err error) error {
	if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
		return err
	}
	return fmt.Errorf("%w: %w", ErrUnreadable, err)
}

// copyError classifies a failed copy: the original changed or could not be read, or the Storage Manager failed.
func copyError(ctx context.Context, err error) error {
	var src *sourceError
	switch {
	case ctx.Err() != nil && errors.Is(err, ctx.Err()):
		return err
	case errors.Is(err, errSizeChanged):
		return fmt.Errorf("%w: %w", ErrChanged, err)
	case errors.As(err, &src):
		return fmt.Errorf("%w: %w", ErrUnreadable, err)
	}
	return fmt.Errorf("%w: %w", ErrStorage, err)
}

// errSizeChanged means the original had more or fewer bytes than its size when it was opened.
var errSizeChanged = errors.New("the file changed size")

// sourceError marks a read error of the original, so it is told apart from a write error of the copy.
type sourceError struct{ err error }

func (e *sourceError) Error() string { return "read the file: " + e.err.Error() }
func (e *sourceError) Unwrap() error { return e.err }

// sourceReader marks the read errors of the original.
type sourceReader struct{ r io.Reader }

func (s sourceReader) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		return n, &sourceError{err: err}
	}
	return n, err
}

// exactReader yields the size bytes of r and fails with errSizeChanged when r has more or fewer. It never reads more
// than size+1 bytes and stops when ctx ends.
func exactReader(ctx context.Context, r io.Reader, size int64) io.Reader {
	return &boundedReader{ctx: ctx, r: io.LimitReader(r, size+1), size: size}
}

type boundedReader struct {
	ctx  context.Context
	r    io.Reader
	size int64
	n    int64
}

func (b *boundedReader) Read(p []byte) (int, error) {
	if err := b.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := b.r.Read(p)
	b.n += int64(n)
	switch {
	case b.n > b.size:
		return 0, fmt.Errorf("%w: it grew beyond %d bytes while it was copied", errSizeChanged, b.size)
	case errors.Is(err, io.EOF) && b.n < b.size:
		return n, fmt.Errorf("%w: it ended at %d of %d bytes", errSizeChanged, b.n, b.size)
	}
	return n, err
}
