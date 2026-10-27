package recordingimport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
)

const path = `C:\Users\student\Music\lecture.wav` // a fake path: the fake Originals serves it from memory

func digest(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func mustRef(t *testing.T, s string) shared.ArtifactRef {
	t.Helper()
	ref, err := shared.ParseArtifactRef(s)
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

func pastLesson(t *testing.T, w *world) *lesson.Lesson {
	t.Helper()
	start := now.Add(-72 * time.Hour)
	l, err := lesson.New(lesson.Spec{ID: "lesson-1", Content: lesson.Content{
		Title: "Матанализ (лекция)", Start: start, End: start.Add(90 * time.Minute), TimeZone: "Europe/Moscow",
		Subject: "Матанализ", Kind: lesson.KindLecture, Teacher: "Иванов И. И.",
	}}, now.Add(-100*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	w.lessons[l.ID()] = l
	return l
}

func TestImportOfAPastLesson(t *testing.T) {
	w := newWorld(t)
	l := pastLesson(t, w)
	audio := wavFile(3, "first lecture")
	w.originals.add(path, audio)

	res, err := w.svc.Import(context.Background(), Request{Path: path, LessonID: l.ID()})
	if err != nil {
		t.Fatal(err)
	}
	r := res.Recording
	if res.Existing || r.ID() != "imp-1" || r.Status() != recording.StatusCompleted || r.Origin() != recording.OriginImported {
		t.Fatalf("result %+v: %s %s %s", res, r.ID(), r.Status(), r.Origin())
	}
	if r.SourceSHA256() != digest(audio) || !r.StartedAt().Equal(l.Content().Start) || r.LessonID() != "lesson-1" {
		t.Fatalf("digest %s start %v lesson %s", r.SourceSHA256(), r.StartedAt(), r.LessonID())
	}
	snap := r.LessonSnapshot()
	if snap.Subject() != "Матанализ" || snap.Kind() != "lecture" || snap.Title() != "Матанализ (лекция)" || snap.Teacher() != "Иванов И. И." {
		t.Fatalf("snapshot %+v", snap)
	}
	if d, ok := r.Duration(); !ok || d != 3*time.Second {
		t.Fatalf("Duration = %v, %v; want 3s from the header", d, ok)
	}

	track := mustRef(t, "recordings/imp-1.wav")
	if r.Audio() != track || !bytes.Equal(w.store.files[track], audio) || len(w.store.files) != 1 {
		t.Fatalf("copy %s: %d files, same bytes %v", r.Audio(), len(w.store.files), bytes.Equal(w.store.files[track], audio))
	}
	if w.recordings.count() != 1 || r.Version() != 1 {
		t.Fatalf("%d recordings stored, version %d", w.recordings.count(), r.Version())
	}
	if len(w.completions.ids) != 1 || w.completions.ids[0] != r.ID() {
		t.Fatalf("completions %v, want the transcription enqueued once", w.completions.ids)
	}
	if w.originals.closed != 1 {
		t.Fatalf("the original was closed %d times", w.originals.closed)
	}
}

// A date and a subject without a lesson; the duration of an MP3 is its frames counted during the copy and the
// extension follows the content.
func TestImportOfADateAndSubject(t *testing.T) {
	w := newWorld(t)
	audio := mp3File(cbr128, 417, 20, "seminar")
	w.originals.add(`C:\Users\student\Music\seminar.ogg`, audio) // the name lies, the content decides
	at := now.Add(-24 * time.Hour)

	res, err := w.svc.Import(context.Background(), Request{
		Path: `C:\Users\student\Music\seminar.ogg`, StartedAt: at, Subject: " Физика ", Kind: "Seminar", Title: "Оптика",
	})
	if err != nil {
		t.Fatal(err)
	}
	r := res.Recording
	if r.LessonID() != "" || !r.StartedAt().Equal(at) || r.Audio() != mustRef(t, "recordings/imp-1.mp3") {
		t.Fatalf("lesson %q start %v audio %s", r.LessonID(), r.StartedAt(), r.Audio())
	}
	if s := r.LessonSnapshot(); s.Subject() != "Физика" || s.Kind() != "seminar" || s.Title() != "Оптика" {
		t.Fatalf("snapshot %+v", s)
	}
	if d, ok := r.Duration(); !ok || d != 20*1152*time.Second/44100 {
		t.Fatalf("Duration = %v, %v; want 20 frames of 1152 samples at 44.1 kHz", d, ok)
	}
}

// Every format has a duration cap: a file too long, or whose headers do not tell its duration, is refused. The headers
// of WAV, FLAC and Ogg (and the size of a CBR MP3) refuse it before the copy; the frames of any other MP3 are counted
// during the copy, which is then deleted.
func TestImportDurationCap(t *testing.T) {
	const big = int64(1) << 30
	pairs := bytes.Repeat(append(mp3File(low8, 72, 1, ""), mp3File(low16, 144, 1, "")...), 300) // VBR, 43.2 s
	tests := []struct {
		name   string
		file   original
		copied bool // the refusal comes after the copy
		want   error
		dur    time.Duration // of an accepted file
	}{
		{name: "flac over 4 hours", file: original{data: flacFile(8000, 8000*(4*3600+1), "x"), length: big}, want: recording.ErrAudioTooLong},
		{name: "flac without total samples", file: original{data: flacFile(48000, 0, "x"), length: big}, want: recording.ErrAudioDurationUnknown},
		{name: "ogg over 4 hours", file: original{data: oggHead(8000), end: oggPage(8000*(4*3600+1), []byte("x")), length: big},
			want: recording.ErrAudioTooLong},
		{name: "ogg without a last page", file: original{data: oggHead(8000), length: big}, want: recording.ErrAudioDurationUnknown},
		{name: "cbr mp3 over 4 hours", file: original{data: mp3File(low8, 72, 900, "x"), end: mp3File(low8, 72, 1800, ""), length: 16_000_000},
			want: recording.ErrAudioTooLong},
		{name: "vbr mp3 over 4 hours", file: original{data: append(pairs, mp3File(low8, 72, 199_401, "")...)}, copied: true,
			want: recording.ErrAudioTooLong},
		{name: "mp3 without countable frames", file: original{data: append([]byte{0xFF, 0xFB, 0x04, 0x64}, make([]byte, 1000)...)}, copied: true,
			want: recording.ErrAudioDurationUnknown},
		{name: "flac within", file: original{data: flacFile(48000, 48000*3600, "x")}, dur: time.Hour},
		{name: "ogg within", file: original{data: append(oggHead(44100), oggPage(44100*90, []byte("audio"))...)}, dur: 90 * time.Second},
		{name: "vbr mp3 within", file: original{data: append(pairs, mp3File(low8, 72, 100, "")...)}, dur: 50400 * time.Millisecond},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			o := w.originals.add(path, nil)
			*o = tc.file
			o.size = -1
			writes := 0
			w.store.onWrite = func() { writes++ }

			res, err := w.svc.Import(context.Background(), Request{Path: path, StartedAt: now.Add(-time.Hour)})
			if tc.want == nil {
				if err != nil {
					t.Fatal(err)
				}
				if d, ok := res.Recording.Duration(); !ok || d != tc.dur {
					t.Fatalf("Duration = %v, %v; want %v", d, ok, tc.dur)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("Import = %+v, %v; want %v", res, err, tc.want)
			}
			if len(w.store.files) != 0 || w.recordings.count() != 0 || len(w.completions.ids) != 0 {
				t.Fatalf("after a refusal: %d files, %d recordings, completions %v", len(w.store.files), w.recordings.count(),
					w.completions.ids)
			}
			if copied := writes > 0; copied != tc.copied || len(w.store.deleted) != writes {
				t.Fatalf("%d copies written, %d deleted; want a copy %v", writes, len(w.store.deleted), tc.copied)
			}
		})
	}
}

// The same bytes twice are the same recording: the second copy is deleted, nothing new is stored, and the
// transcription is handed the same id again (the enqueuer is idempotent).
func TestImportOfTheSameBytesTwice(t *testing.T) {
	w := newWorld(t)
	l := pastLesson(t, w)
	audio := wavFile(1, "same")
	w.originals.add(path, audio)
	w.originals.add(`D:\copy of lecture.wav`, append([]byte(nil), audio...))
	ctx := context.Background()

	first, err := w.svc.Import(ctx, Request{Path: path, LessonID: l.ID()})
	if err != nil {
		t.Fatal(err)
	}
	second, err := w.svc.Import(ctx, Request{Path: `D:\copy of lecture.wav`, StartedAt: now.Add(-time.Hour), Subject: "Другое"})
	if err != nil {
		t.Fatal(err)
	}
	if !second.Existing || second.Recording.ID() != first.Recording.ID() {
		t.Fatalf("second import %+v, want the first recording", second)
	}
	if w.recordings.count() != 1 || len(w.store.files) != 1 {
		t.Fatalf("%d recordings, %d files; want one of each", w.recordings.count(), len(w.store.files))
	}
	if len(w.store.deleted) != 1 || w.store.deleted[0] != mustRef(t, "recordings/imp-2.wav") {
		t.Fatalf("deleted %v, want the second copy", w.store.deleted)
	}
	if len(w.completions.ids) != 2 || w.completions.ids[1] != first.Recording.ID() {
		t.Fatalf("completions %v", w.completions.ids)
	}
}

// Another import of the same bytes is stored between the lookup and the insert: its recording is returned.
func TestImportRaceOnTheSameBytes(t *testing.T) {
	w := newWorld(t)
	audio := wavFile(1, "race")
	w.originals.add(path, audio)
	rival, err := recording.NewImported(recording.ImportSpec{
		ID: "rival", StartedAt: now.Add(-time.Hour), Audio: recording.ImportedAudio{Format: recording.FormatWAV},
		SHA256: digest(audio),
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	w.recordings.beforeSave = func() {
		w.recordings.beforeSave = nil
		if err := w.recordings.Save(context.Background(), rival); err != nil {
			t.Error(err)
		}
	}

	res, err := w.svc.Import(context.Background(), Request{Path: path, StartedAt: now.Add(-time.Hour)})
	if err != nil || !res.Existing || res.Recording.ID() != "rival" {
		t.Fatalf("Import = %+v, %v; want the rival recording", res, err)
	}
	if len(w.store.files) != 0 {
		t.Fatalf("files %v, want the copy deleted", w.store.files)
	}
}

func TestImportRefusesBeforeTheCopy(t *testing.T) {
	ok := wavFile(1, "x")
	tests := []struct {
		name string
		set  func(w *world) Request
		want error
	}{
		{"neither lesson nor time", func(w *world) Request {
			w.originals.add(path, ok)
			return Request{Path: path}
		}, ErrInvalidRequest},
		{"bad lesson id", func(w *world) Request {
			w.originals.add(path, ok)
			return Request{Path: path, LessonID: "a b"}
		}, ErrInvalidRequest},
		{"lesson gone", func(w *world) Request {
			w.originals.add(path, ok)
			return Request{Path: path, LessonID: "gone"}
		}, ErrLessonNotFound},
		{"future lecture", func(w *world) Request {
			w.originals.add(path, ok)
			return Request{Path: path, StartedAt: now.Add(time.Minute)}
		}, recording.ErrFutureLecture},
		{"missing file", func(*world) Request { return Request{Path: path, StartedAt: now} }, ErrUnreadable},
		{"not a regular file", func(w *world) Request {
			w.originals.add(path, ok).openErr = errors.New("import file: a directory")
			return Request{Path: path, StartedAt: now}
		}, ErrUnreadable},
		{"empty", func(w *world) Request {
			w.originals.add(path, nil)
			return Request{Path: path, StartedAt: now}
		}, recording.ErrEmptyAudio},
		{"over 2 GiB", func(w *world) Request {
			w.originals.add(path, ok).size = recording.MaxImportedBytes + 1
			return Request{Path: path, StartedAt: now}
		}, recording.ErrAudioTooLarge},
		{"text named .wav", func(w *world) Request {
			w.originals.add(path, []byte("these are my notes, not audio"))
			return Request{Path: path, StartedAt: now}
		}, recording.ErrUnsupportedAudio},
		{"m4a", func(w *world) Request {
			w.originals.add(path, append([]byte{0, 0, 0, 0x20}, "ftypM4A isom"...))
			return Request{Path: path, StartedAt: now}
		}, recording.ErrUnsupportedAudio},
		{"wav over 4 hours", func(w *world) Request {
			head := wavFile(0, "")
			binary.LittleEndian.PutUint32(head[40:44], 32000*(4*3600+1)) // the data chunk says 4 h 1 s
			o := w.originals.add(path, head)
			o.length = 1 << 30 // zeros after the header; only the head and the tail are read
			return Request{Path: path, StartedAt: now}
		}, recording.ErrAudioTooLong},
		{"header cut short", func(w *world) Request {
			w.originals.add(path, ok[:10]).size = 1000
			return Request{Path: path, StartedAt: now}
		}, ErrChanged},
		{"no disk space", func(w *world) Request {
			w.originals.add(path, ok)
			w.space.free = int64(len(ok)) + recording.ImportSpaceReserve - 1
			return Request{Path: path, StartedAt: now}
		}, recording.ErrNoDiskSpace},
		{"free space unknown", func(w *world) Request {
			w.originals.add(path, ok)
			w.space.err = errors.New("volume gone")
			return Request{Path: path, StartedAt: now}
		}, ErrStorage},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			req := tc.set(w)
			res, err := w.svc.Import(context.Background(), req)
			if !errors.Is(err, tc.want) {
				t.Fatalf("Import = %+v, %v; want %v", res, err, tc.want)
			}
			if len(w.store.files) != 0 || w.recordings.count() != 0 || len(w.completions.ids) != 0 {
				t.Fatalf("after a refusal: %d files, %d recordings, completions %v", len(w.store.files), w.recordings.count(),
					w.completions.ids)
			}
			if strings.Contains(err.Error(), path) {
				t.Fatalf("the error names the path: %v", err)
			}
		})
	}
}

// A refusal during or after the copy leaves no file and no recording.
func TestImportFailuresLeaveNothing(t *testing.T) {
	audio := wavFile(8, "fail") // longer than the head and the tail, so the read error is met during the copy
	tests := []struct {
		name string
		set  func(w *world)
		want error
	}{
		{"file shrank", func(w *world) { w.originals.add(path, audio).size = int64(len(audio)) + 100 }, ErrChanged},
		{"file grew", func(w *world) { w.originals.add(path, audio).size = int64(len(audio)) - 100 }, ErrChanged},
		{"read error", func(w *world) { w.originals.add(path, audio).failAt = recording.ImportHeadBytes + 10 }, ErrUnreadable},
		{"write error", func(w *world) {
			w.originals.add(path, audio)
			w.store.writeErr = errors.New("disk full")
		}, ErrStorage},
		{"save error", func(w *world) {
			w.originals.add(path, audio)
			w.recordings.saveErr = errors.New("database is locked")
		}, ErrStorage},
		{"being deleted", func(w *world) {
			w.originals.add(path, audio)
			old, err := recording.NewImported(recording.ImportSpec{
				ID: "old", StartedAt: now.Add(-time.Hour), Audio: recording.ImportedAudio{Format: recording.FormatWAV},
				SHA256: digest(audio),
			}, now)
			if err == nil {
				err = old.MarkDeleting(now, nil)
			}
			if err == nil {
				err = w.recordings.Save(context.Background(), old)
			}
			if err != nil {
				panic(err)
			}
		}, ErrDeleting},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			tc.set(w)
			before := w.recordings.count()
			res, err := w.svc.Import(context.Background(), Request{Path: path, StartedAt: now.Add(-time.Hour)})
			if !errors.Is(err, tc.want) {
				t.Fatalf("Import = %+v, %v; want %v", res, err, tc.want)
			}
			if len(w.store.files) != 0 || w.recordings.count() != before || len(w.completions.ids) != 0 {
				t.Fatalf("after a failure: %d files, %d recordings, completions %v", len(w.store.files),
					w.recordings.count(), w.completions.ids)
			}
		})
	}
}

// A cancelled import stops the copy and keeps nothing; the next import of the file works.
func TestImportCancelled(t *testing.T) {
	w := newWorld(t)
	w.originals.add(path, wavFile(2, "cancel"))
	ctx, cancel := context.WithCancel(context.Background())
	w.store.onWrite = cancel
	if _, err := w.svc.Import(ctx, Request{Path: path, StartedAt: now}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Import = %v, want context.Canceled", err)
	}
	if len(w.store.files) != 0 || w.recordings.count() != 0 {
		t.Fatalf("after a cancel: %d files, %d recordings", len(w.store.files), w.recordings.count())
	}

	w.store.onWrite = nil
	if _, err := w.svc.Import(context.Background(), Request{Path: path, StartedAt: now}); err != nil {
		t.Fatal(err)
	}
}

// A failed enqueue is only logged: the recording stays and the startup sweep enqueues it.
func TestImportKeepsTheRecordingWhenTheEnqueueFails(t *testing.T) {
	w := newWorld(t)
	w.originals.add(path, wavFile(1, "enqueue"))
	w.completions.err = errors.New("no model installed")
	if _, err := w.svc.Import(context.Background(), Request{Path: path, StartedAt: now}); err != nil {
		t.Fatal(err)
	}
	if w.recordings.count() != 1 || len(w.store.files) != 1 {
		t.Fatalf("%d recordings, %d files", w.recordings.count(), len(w.store.files))
	}
}

// The original is only opened and read: nothing is written to it, it is closed, and only the copy goes to storage.
func TestImportOnlyReadsTheOriginal(t *testing.T) {
	w := newWorld(t)
	audio := wavFile(1, "original")
	keep := append([]byte(nil), audio...)
	w.originals.add(path, audio)
	if _, err := w.svc.Import(context.Background(), Request{Path: path, StartedAt: now}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(audio, keep) || len(w.originals.opened) != 1 || w.originals.closed != 1 {
		t.Fatalf("original changed %v, opened %d, closed %d", !bytes.Equal(audio, keep), len(w.originals.opened), w.originals.closed)
	}
	for ref := range w.store.files {
		if ref.Kind() != shared.ArtifactRecordings {
			t.Fatalf("a file outside recordings/: %s", ref)
		}
	}
}

func TestNewRequiresEveryPort(t *testing.T) {
	w := newWorld(t)
	full := Deps{
		Originals: w.originals, Store: w.store, Space: spaceOf{w.space}, Recordings: w.recordings, Lessons: w.lessons,
		Completions: w.completions, Clock: fixedClock{}, IDs: &seqIDs{}, Tx: passTx{},
	}
	drop := []func(*Deps){
		func(d *Deps) { d.Originals = nil }, func(d *Deps) { d.Store = nil }, func(d *Deps) { d.Space = nil },
		func(d *Deps) { d.Recordings = nil }, func(d *Deps) { d.Lessons = nil }, func(d *Deps) { d.Completions = nil },
		func(d *Deps) { d.Clock = nil }, func(d *Deps) { d.IDs = nil }, func(d *Deps) { d.Tx = nil },
	}
	for i, f := range drop {
		d := full
		f(&d)
		if _, err := New(d); !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("port %d missing: %v", i, err)
		}
	}
	if _, err := New(full); err != nil {
		t.Fatal(err)
	}
}
