package recordingimport

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
)

var now = time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)

// wavFile is a synthetic 16 kHz mono 16-bit WAV of the given seconds of silence plus a marker, so files differ.
func wavFile(seconds int, marker string) []byte {
	data := make([]byte, 32000*seconds)
	copy(data, marker)
	b := append([]byte("RIFF"), binary.LittleEndian.AppendUint32(nil, uint32(36+len(data)))...)
	b = append(b, "WAVEfmt "...)
	b = binary.LittleEndian.AppendUint32(b, 16)
	b = binary.LittleEndian.AppendUint16(b, 1)
	b = binary.LittleEndian.AppendUint16(b, 1)
	b = binary.LittleEndian.AppendUint32(b, 16000)
	b = binary.LittleEndian.AppendUint32(b, 32000)
	b = binary.LittleEndian.AppendUint16(b, 2)
	b = binary.LittleEndian.AppendUint16(b, 16)
	b = append(b, "data"...)
	b = binary.LittleEndian.AppendUint32(b, uint32(len(data)))
	return append(b, data...)
}

// mp3File is a synthetic MPEG frame header followed by the marker.
func mp3File(marker string) []byte { return append([]byte{0xFF, 0xFB, 0x90, 0x64}, marker...) }

// original is a file the fake Originals serves: its bytes, the size it reports and a read error after some bytes.
type original struct {
	data    []byte
	size    int64 // reported size; -1: len(data)
	failAt  int   // read error once this many bytes were read; 0: none
	openErr error
}

type fakeOriginals struct {
	mu     sync.Mutex
	files  map[string]*original
	opened []string
	closed int
}

func newOriginals() *fakeOriginals { return &fakeOriginals{files: map[string]*original{}} }

func (f *fakeOriginals) add(path string, data []byte) *original {
	o := &original{data: data, size: -1}
	f.files[path] = o
	return o
}

func (f *fakeOriginals) Open(_ context.Context, path string) (io.ReadCloser, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.opened = append(f.opened, path)
	o, ok := f.files[path]
	if !ok {
		return nil, 0, fmt.Errorf("import file: open: %w", shared.ErrNotFound)
	}
	if o.openErr != nil {
		return nil, 0, o.openErr
	}
	size := o.size
	if size < 0 {
		size = int64(len(o.data))
	}
	var r io.Reader = bytes.NewReader(o.data)
	if o.failAt > 0 {
		r = io.MultiReader(bytes.NewReader(o.data[:o.failAt]), errReader{})
	}
	return &closer{r: r, f: f}, size, nil
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("device not ready") }

type closer struct {
	r io.Reader
	f *fakeOriginals
}

func (c *closer) Read(p []byte) (int, error) { return c.r.Read(p) }
func (c *closer) Close() error {
	c.f.mu.Lock()
	defer c.f.mu.Unlock()
	c.f.closed++
	return nil
}

// memStore is the Storage Manager in memory: a write is stored only when its reader ended without an error.
type memStore struct {
	mu       sync.Mutex
	files    map[shared.ArtifactRef][]byte
	deleted  []shared.ArtifactRef
	writeErr error
	onWrite  func() // runs before the content is read
}

func newStore() *memStore { return &memStore{files: map[shared.ArtifactRef][]byte{}} }

func (s *memStore) Write(ctx context.Context, ref shared.ArtifactRef, content io.Reader) error {
	if s.onWrite != nil {
		s.onWrite()
	}
	if s.writeErr != nil {
		return s.writeErr
	}
	data, err := io.ReadAll(content)
	if err != nil {
		return fmt.Errorf("read content of %s: %w", ref, err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[ref] = data
	return nil
}

func (s *memStore) Delete(_ context.Context, ref shared.ArtifactRef) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.files, ref)
	s.deleted = append(s.deleted, ref)
	return nil
}

type fakeSpace struct {
	free int64
	err  error
}

func (f fakeSpace) FreeBytes(context.Context) (int64, error) { return f.free, f.err }

// memRecordings keeps saved recordings by id with the unique digest of the schema.
type memRecordings struct {
	mu      sync.Mutex
	byID    map[shared.RecordingID]*recording.Recording
	saveErr error
	// beforeSave runs inside Save before the insert, to store a rival import.
	beforeSave func()
}

func newRecordings() *memRecordings {
	return &memRecordings{byID: map[shared.RecordingID]*recording.Recording{}}
}

func (m *memRecordings) GetBySourceSHA256(_ context.Context, sha string) (*recording.Recording, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.byID {
		if sha != "" && r.SourceSHA256() == sha {
			return r, nil
		}
	}
	return nil, fmt.Errorf("recording of %s: %w", sha, shared.ErrNotFound)
}

func (m *memRecordings) Save(_ context.Context, r *recording.Recording) error {
	if m.beforeSave != nil {
		m.beforeSave()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.saveErr != nil {
		return m.saveErr
	}
	for id, other := range m.byID {
		if id != r.ID() && other.SourceSHA256() != "" && other.SourceSHA256() == r.SourceSHA256() {
			return fmt.Errorf("unique source_sha256: %w", shared.ErrDuplicate)
		}
	}
	m.byID[r.ID()] = r
	r.Persisted(r.Version() + 1)
	return nil
}

func (m *memRecordings) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.byID)
}

type fakeLessons map[shared.LessonID]*lesson.Lesson

func (f fakeLessons) Get(_ context.Context, id shared.LessonID) (*lesson.Lesson, error) {
	if l, ok := f[id]; ok {
		return l, nil
	}
	return nil, fmt.Errorf("lesson %s: %w", id, shared.ErrNotFound)
}

type fakeCompletions struct {
	mu  sync.Mutex
	ids []shared.RecordingID
	err error
}

func (f *fakeCompletions) RecordingCompleted(_ context.Context, id shared.RecordingID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ids = append(f.ids, id)
	return f.err
}

type fixedClock struct{}

func (fixedClock) Now() time.Time { return now }

type seqIDs struct {
	mu sync.Mutex
	n  int
}

func (s *seqIDs) NewRecordingID() shared.RecordingID {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.n++
	return shared.RecordingID(fmt.Sprintf("imp-%d", s.n))
}

type passTx struct{}

func (passTx) Do(ctx context.Context, fn func(ctx context.Context) error) error { return fn(ctx) }

// world is a Service on fakes.
type world struct {
	svc         *Service
	originals   *fakeOriginals
	store       *memStore
	recordings  *memRecordings
	completions *fakeCompletions
	lessons     fakeLessons
	space       *fakeSpace
}

func newWorld(t interface {
	Helper()
	Fatal(...any)
}) *world {
	t.Helper()
	w := &world{
		originals: newOriginals(), store: newStore(), recordings: newRecordings(), completions: &fakeCompletions{},
		lessons: fakeLessons{}, space: &fakeSpace{free: 100 << 30},
	}
	svc, err := New(Deps{
		Originals: w.originals, Store: w.store, Space: spaceOf{w.space}, Recordings: w.recordings, Lessons: w.lessons,
		Completions: w.completions, Clock: fixedClock{}, IDs: &seqIDs{}, Tx: passTx{},
	})
	if err != nil {
		t.Fatal(err)
	}
	w.svc = svc
	return w
}

// spaceOf reads the space the test sets after the world is built.
type spaceOf struct{ s *fakeSpace }

func (s spaceOf) FreeBytes(ctx context.Context) (int64, error) { return s.s.FreeBytes(ctx) }
