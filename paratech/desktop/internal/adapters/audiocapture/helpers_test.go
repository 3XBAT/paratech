package audiocapture

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"

	"paratech/desktop/internal/domain/shared"
)

// dirStore is a minimal Storage Manager on a temporary directory for the tests of this package (the filestore
// adapter itself cannot be imported here, ARCH-30). Faults can be injected per operation.
type dirStore struct {
	root string

	mu      sync.Mutex
	openErr error
	sizeErr error
	listErr error
}

func newDirStore(t *testing.T) *dirStore {
	t.Helper()
	return &dirStore{root: t.TempDir()}
}

func (d *dirStore) path(ref shared.ArtifactRef) string {
	return filepath.Join(d.root, string(ref.Kind()), filepath.FromSlash(ref.Path()))
}

func (d *dirStore) fault(err *error) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return *err
}

func notFound(ref shared.ArtifactRef, err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: %s", shared.ErrNotFound, ref)
	}
	return err
}

func (d *dirStore) List(_ context.Context, dir shared.ArtifactRef) ([]shared.ArtifactRef, error) {
	if err := d.fault(&d.listErr); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(d.path(dir))
	if err != nil {
		return nil, notFound(dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	out := make([]shared.ArtifactRef, 0, len(names))
	for _, n := range names {
		if c, err := dir.Child(n); err == nil {
			out = append(out, c)
		}
	}
	return out, nil
}

func (d *dirStore) Open(_ context.Context, ref shared.ArtifactRef) (io.ReadCloser, error) {
	if err := d.fault(&d.openErr); err != nil {
		return nil, err
	}
	f, err := os.Open(d.path(ref))
	return f, notFound(ref, err)
}

func (d *dirStore) Size(_ context.Context, ref shared.ArtifactRef) (int64, error) {
	if err := d.fault(&d.sizeErr); err != nil {
		return 0, err
	}
	fi, err := os.Stat(d.path(ref))
	if err != nil {
		return 0, notFound(ref, err)
	}
	return fi.Size(), nil
}

func (d *dirStore) Write(ctx context.Context, ref shared.ArtifactRef, content io.Reader) error {
	p := d.path(ref)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	tmp := p + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, content)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, p)
}

func (d *dirStore) Create(_ context.Context, ref shared.ArtifactRef) (*os.File, error) {
	p := d.path(ref)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return nil, fmt.Errorf("%w: %s exists", shared.ErrConflict, ref)
	}
	return f, err
}

func (d *dirStore) Rename(_ context.Context, from, to shared.ArtifactRef) error {
	if _, err := os.Lstat(d.path(to)); err == nil {
		return fmt.Errorf("%w: %s exists", shared.ErrConflict, to)
	}
	return notFound(from, os.Rename(d.path(from), d.path(to)))
}

func (d *dirStore) put(t *testing.T, ref shared.ArtifactRef, content []byte) {
	t.Helper()
	p := d.path(ref)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

func (d *dirStore) get(t *testing.T, ref shared.ArtifactRef) []byte {
	t.Helper()
	b, err := os.ReadFile(d.path(ref))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func (d *dirStore) names(t *testing.T, dir shared.ArtifactRef) []string {
	t.Helper()
	refs, err := d.List(context.Background(), dir)
	if err != nil && !errors.Is(err, shared.ErrNotFound) {
		t.Fatal(err)
	}
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		out = append(out, baseName(r))
	}
	return out
}

func mustRef(t *testing.T, path string) shared.ArtifactRef {
	t.Helper()
	r, err := shared.NewArtifactRef(shared.ArtifactRecordings, path)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// tone returns frames interleaved frames of a sine (the same on every channel) starting at frame start.
func tone(f Format, freq, amp float64, start, frames int) []byte {
	le := binary.LittleEndian
	out := make([]byte, 0, frames*f.BlockAlign())
	for i := start; i < start+frames; i++ {
		v := amp * math.Sin(2*math.Pi*freq*float64(i)/float64(f.SampleRate))
		for range f.Channels {
			switch {
			case f.Float:
				out = le.AppendUint32(out, math.Float32bits(float32(v)))
			case f.BitsPerSample == 16:
				out = le.AppendUint16(out, uint16(int16(math.Round(v*math.MaxInt16))))
			case f.BitsPerSample == 24:
				x := int32(math.Round(v * (1<<23 - 1)))
				out = append(out, byte(x), byte(x>>8), byte(x>>16))
			default:
				out = le.AppendUint32(out, uint32(int32(math.Round(v*math.MaxInt32))))
			}
		}
	}
	return out
}

// wavFile builds a WAV file from a fmt body, optional extra chunks before data, and the data.
func wavFile(fmtBody []byte, extra []byte, data []byte) []byte {
	le := binary.LittleEndian
	var body []byte
	body = append(body, "WAVE"...)
	body = append(body, "fmt "...)
	body = le.AppendUint32(body, uint32(len(fmtBody)))
	body = append(body, fmtBody...)
	body = append(body, extra...)
	body = append(body, "data"...)
	body = le.AppendUint32(body, uint32(len(data)))
	body = append(body, data...)
	if len(data)%2 == 1 {
		body = append(body, 0)
	}
	out := append([]byte("RIFF"), le.AppendUint32(nil, uint32(len(body)))...)
	return append(out, body...)
}

// fmtBody is a WAVEFORMATEX body (16 bytes) or WAVEFORMATEXTENSIBLE (40 bytes) for f.
func fmtBody(f Format, extensible bool) []byte {
	le := binary.LittleEndian
	tag := uint16(tagPCM)
	if f.Float {
		tag = tagFloat
	}
	b := le.AppendUint16(nil, tag)
	if extensible {
		b = le.AppendUint16(nil, tagExtensible)
	}
	b = le.AppendUint16(b, f.Channels)
	b = le.AppendUint32(b, f.SampleRate)
	b = le.AppendUint32(b, f.ByteRate())
	b = le.AppendUint16(b, uint16(f.BlockAlign()))
	b = le.AppendUint16(b, f.BitsPerSample)
	if extensible {
		b = le.AppendUint16(b, 22)
		b = le.AppendUint16(b, f.BitsPerSample)
		b = le.AppendUint32(b, 0x3) // front left and right
		b = le.AppendUint32(b, uint32(tag))
		b = append(b, subFormatTail()...)
	}
	return b
}

// pcm16 decodes 16-bit mono samples to floats.
func pcm16(data []byte) []float64 {
	out := make([]float64, len(data)/2)
	for i := range out {
		out[i] = float64(int16(binary.LittleEndian.Uint16(data[2*i:]))) / math.MaxInt16
	}
	return out
}

// goertzel returns the power of frequency freq in samples at rate.
func goertzel(samples []float64, freq float64, rate int) float64 {
	w := 2 * math.Pi * freq / float64(rate)
	c := 2 * math.Cos(w)
	var s1, s2 float64
	for _, x := range samples {
		s1, s2 = x+c*s1-s2, s1
	}
	return s1*s1 + s2*s2 - c*s1*s2
}

func rms(samples []float64) float64 {
	var sum float64
	for _, x := range samples {
		sum += x * x
	}
	return math.Sqrt(sum / float64(len(samples)))
}
