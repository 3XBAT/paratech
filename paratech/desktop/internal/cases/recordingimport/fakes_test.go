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

// mp3File is a synthetic MP3: frames of the header (silent bodies), the marker in the first body.
func mp3File(hdr [4]byte, frameLen, frames int, marker string) []byte {
	frame := append(hdr[:], make([]byte, frameLen-4)...)
	b := bytes.Repeat(frame, frames)
	copy(b[4:], marker)
	return b
}

// Synthetic MPEG audio frames.
var (
	cbr128 = [4]byte{0xFF, 0xFB, 0x90, 0x64} // MPEG-1 layer III, 128 kbit/s, 44.1 kHz: 417 bytes, 1152 samples
	low8   = [4]byte{0xFF, 0xE3, 0x18, 0xC4} // MPEG-2.5 layer III, 8 kbit/s, 8 kHz: 72 bytes, 576 samples (72 ms)
	low16  = [4]byte{0xFF, 0xE3, 0x28, 0xC4} // the same at 16 kbit/s: 144 bytes
)

// flacFile is a synthetic FLAC head: STREAMINFO of mono 16-bit at rate with total samples, then the marker.
func flacFile(rate uint32, total uint64, marker string) []byte {
	s := make([]byte, 34)
	s[10], s[11], s[12] = byte(rate>>12), byte(rate>>4), byte(rate<<4)
	s[13] = 0xF0 | byte(total>>32)&0x0F
	binary.BigEndian.PutUint32(s[14:18], uint32(total))
	b := append([]byte("fLaC\x80\x00\x00\x22"), s...)
	return append(b, marker...)
}

// oggPage is a synthetic Ogg page of one short packet; the CRC is not checked.
func oggPage(granule int64, packet []byte) []byte {
	b := append([]byte("OggS"), 0, 0)
	b = binary.LittleEndian.AppendUint64(b, uint64(granule))
	b = binary.LittleEndian.AppendUint32(b, 7) // serial
	b = binary.LittleEndian.AppendUint64(b, 0) // sequence and CRC
	b = append(b, 1, byte(len(packet)))
	return append(b, packet...)
}

// oggHead is the first page of a synthetic Ogg Vorbis stream at rate.
func oggHead(rate uint32) []byte {
	id := binary.LittleEndian.AppendUint32([]byte("\x01vorbis"), 0)
	id = append(id, 1)
	id = binary.LittleEndian.AppendUint32(id, rate)
	return oggPage(0, append(id, make([]byte, 14)...))
}

// original is a file the fake Originals serves: its bytes (data, then zeros, then end, length bytes in all), the size
// it reports and a read error at a position.
type original struct {
	data    []byte
	end     []byte
	length  int64 // bytes of the file; 0: len(data)+len(end)
	size    int64 // reported size; -1: the length
	failAt  int   // a read reaching this position fails; 0: none
	openErr error
	read    int64 // bytes read from it
}

func (o *original) len() int64 {
	if o.length > 0 {
		return o.length
	}
	return int64(len(o.data) + len(o.end))
}

func (o *original) at(i int64) byte {
	switch n := o.len(); {
	case i < int64(len(o.data)):
		return o.data[i]
	case i >= n-int64(len(o.end)):
		return o.end[i-(n-int64(len(o.end)))]
	}
	return 0
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

func (f *fakeOriginals) Open(_ context.Context, path string) (io.ReadSeekCloser, int64, error) {
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
		size = o.len()
	}
	return &closer{o: o, f: f}, size, nil
}

type closer struct {
	o   *original
	pos int64
	f   *fakeOriginals
}

func (c *closer) Read(p []byte) (int, error) {
	fail := int64(c.o.failAt)
	switch {
	case fail > 0 && c.pos == fail:
		return 0, errors.New("device not ready")
	case c.pos >= c.o.len():
		return 0, io.EOF
	}
	k := min(int64(len(p)), c.o.len()-c.pos)
	if fail > c.pos {
		k = min(k, fail-c.pos)
	}
	for i := range k {
		p[i] = c.o.at(c.pos + i)
	}
	c.pos += k
	c.o.read += k
	return int(k), nil
}

func (c *closer) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekCurrent:
		offset += c.pos
	case io.SeekEnd:
		offset += c.o.len()
	}
	if offset < 0 {
		return 0, errors.New("seek before the start")
	}
	c.pos = offset
	return offset, nil
}

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
