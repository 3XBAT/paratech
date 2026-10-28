package recording

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"
	"time"
)

// riffChunk is a synthetic RIFF chunk: the id, the size n and n zero bytes padded to an even size.
func riffChunk(id string, n int) []byte {
	b := binary.LittleEndian.AppendUint32([]byte(id), uint32(n))
	return append(b, make([]byte, n+n%2)...)
}

// riffWAV is a synthetic WAV: the RIFF header, the fmt chunk of wavHeader (32000 bytes a second), then chunks.
func riffWAV(chunks ...[]byte) []byte {
	b := wavHeader(0)[:36]
	for _, c := range chunks {
		b = append(b, c...)
	}
	return b
}

// sparseFile is a file of size bytes: data, then zeros. It counts the bytes read.
type sparseFile struct {
	data []byte
	size int64
	read int
}

func (f *sparseFile) ReadAt(p []byte, off int64) (int, error) {
	if off >= f.size {
		return 0, io.EOF
	}
	n := int(min(int64(len(p)), f.size-off))
	for i := range n {
		p[i] = 0
		if off+int64(i) < int64(len(f.data)) {
			p[i] = f.data[off+int64(i)]
		}
	}
	f.read += n
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

// detectWAV detects f as the use case does: its head, no tail unless the head is the whole file, then the chunk walk
// past the head over f. It returns the number of headers read.
func detectWAV(f *sparseFile) (ImportedAudio, int, error) {
	head := make([]byte, min(f.size, ImportHeadBytes))
	n, _ := f.ReadAt(head, 0)
	f.read = 0
	a, err := DetectAudio(head[:n], nil, f.size)
	reads := 0
	for err == nil {
		off, ok := a.WAVChunkAt()
		if !ok {
			break
		}
		hdr := make([]byte, WAVChunkHeaderBytes)
		k, _ := f.ReadAt(hdr, off)
		reads++
		a, err = a.WAVChunk(hdr[:k])
	}
	return a, reads, err
}

func TestWAVDataPastTheHead(t *testing.T) {
	const second = 32000
	junk := riffChunk("JUNK", 100<<10)
	dataHeader := func(n uint32) []byte { return binary.LittleEndian.AppendUint32([]byte("data"), n) }
	many := func(k int) []byte { return bytes.Repeat(riffChunk("note", 3), k) } // odd-sized, padded to 4
	tests := []struct {
		name  string
		file  []byte
		size  int64 // 0: len(file)
		want  time.Duration
		err   error
		reads int
	}{
		{"junk of 100 KiB before the data", riffWAV(junk, riffChunk("data", 2*second)), 0, 2 * time.Second, nil, 1},
		{"list and junk before the data", riffWAV(riffChunk("LIST", 80<<10), riffChunk("JUNK", 101), riffChunk("data", 3*second)), 0,
			3 * time.Second, nil, 2},
		{"the data header across the end of the head", riffWAV(riffChunk("JUNK", ImportHeadBytes-36-8-4), riffChunk("data", second)), 0,
			time.Second, nil, 1},
		{"streamed data past the head", riffWAV(junk, dataHeader(0xFFFFFFFF)), int64(36+len(junk)+8) + 5*second, 5 * time.Second, nil, 1},
		{"zero data size past the head", riffWAV(junk, dataHeader(0)), int64(36+len(junk)+8) + 4*second, 4 * time.Second, nil, 1},
		{"data cut by the file size", riffWAV(junk, dataHeader(60*second)), int64(36+len(junk)+8) + 10*second, 10 * time.Second, nil, 1},
		{"over four hours past the head", riffWAV(junk, dataHeader(0xFFFFFFFF)), 1 << 31, 0, ErrAudioTooLong, 1},
		{"63 chunks past the head then the data", riffWAV(junk, many(63), riffChunk("data", second)), 0, time.Second, nil, maxWAVChunkReads},
		{"64 chunks past the head before the data", riffWAV(junk, many(64), riffChunk("data", second)), 0, 0, ErrAudioDurationUnknown,
			maxWAVChunkReads},
		{"zeros after the junk", riffWAV(junk), 1 << 30, 0, ErrAudioDurationUnknown, maxWAVChunkReads},
		{"no data after a few chunks", riffWAV(junk, many(5)), 0, 0, ErrAudioDurationUnknown, 5},
		{"a chunk past the end of the file", riffWAV(junk, binary.LittleEndian.AppendUint32([]byte("LIST"), 1<<31)), 0, 0, ErrAudioDurationUnknown, 1},
		{"the junk overshoots the file", riffWAV(junk)[:ImportHeadBytes+10], 0, 0, ErrAudioDurationUnknown, 0},
		{"data without a body", riffWAV(junk, dataHeader(second)), 0, 0, ErrAudioDurationUnknown, 1},
		{"no fmt chunk", append([]byte("RIFF\x00\x00\x00\x00WAVE"), append(junk, riffChunk("data", second)...)...), 0, 0, ErrAudioDurationUnknown, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := &sparseFile{data: tc.file, size: tc.size}
			if f.size == 0 {
				f.size = int64(len(tc.file))
			}
			got, reads, err := detectWAV(f)
			if reads != tc.reads || f.read > reads*WAVChunkHeaderBytes {
				t.Fatalf("%d headers read (%d bytes past the head), want %d", reads, f.read, tc.reads)
			}
			if tc.err != nil {
				var e AudioDurationUnknownError
				if !errors.Is(err, tc.err) || (errors.Is(err, ErrAudioDurationUnknown) && (!errors.As(err, &e) || e.Format != FormatWAV)) {
					t.Fatalf("detect = %+v, %v; want %v", got, err, tc.err)
				}
				return
			}
			if err != nil || got != (ImportedAudio{Format: FormatWAV, Duration: tc.want}) {
				t.Fatalf("detect = %+v, %v; want %v", got, err, tc.want)
			}
		})
	}
}

// A header cut short (the file changed) ends the walk; a walk not finished is no duration; audio without a walk is
// left as it is.
func TestWAVChunkWalkEnds(t *testing.T) {
	file := riffWAV(riffChunk("JUNK", 100<<10), riffChunk("data", 32000))
	a, err := DetectAudio(file[:ImportHeadBytes], nil, int64(len(file)))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := a.WAVChunkAt(); !ok {
		t.Fatalf("DetectAudio = %+v, want a walk past the head", a)
	}
	if _, err := a.WAVChunk([]byte("data")); !errors.Is(err, ErrAudioDurationUnknown) {
		t.Fatalf("a short header: %v", err)
	}
	if _, err := a.Counted(nil); !errors.Is(err, ErrAudioDurationUnknown) {
		t.Fatalf("Counted before the walk ended: %v", err)
	}

	done := ImportedAudio{Format: FormatWAV, Duration: time.Minute}
	if got, err := done.WAVChunk(nil); err != nil || got != done {
		t.Fatalf("WAVChunk without a walk = %+v, %v", got, err)
	}
	if _, ok := done.WAVChunkAt(); ok {
		t.Fatal("a walk on audio with a duration")
	}
}

// Fuzz the chunk walk past a head of any length over the whole input: no panic, a bounded number of header reads, and
// a result within the limits.
func FuzzWAVChunkWalk(f *testing.F) {
	f.Add(riffWAV(riffChunk("JUNK", 300), riffChunk("data", 64000)), uint16(64))
	f.Add(riffWAV(riffChunk("LIST", 7), riffChunk("JUNK", 9), riffChunk("data", 100)), uint16(40))
	f.Add(riffWAV(bytes.Repeat(riffChunk("pad ", 0), 100)), uint16(44))
	f.Add(riffWAV(binary.LittleEndian.AppendUint32([]byte("data"), 0xFFFFFFFF)), uint16(36))
	f.Fuzz(func(t *testing.T, file []byte, headLen uint16) {
		sf := &sparseFile{data: file, size: int64(len(file))}
		head := file[:min(int(headLen), len(file))]
		a, err := DetectAudio(head, nil, sf.size)
		reads := 0
		for err == nil {
			off, ok := a.WAVChunkAt()
			if !ok {
				break
			}
			if off < 0 || off+WAVChunkHeaderBytes > sf.size || reads >= maxWAVChunkReads {
				t.Fatalf("read %d of a header at %d of %d bytes", reads+1, off, sf.size)
			}
			hdr := make([]byte, WAVChunkHeaderBytes)
			k, _ := sf.ReadAt(hdr, off)
			reads++
			a, err = a.WAVChunk(hdr[:k])
		}
		if err != nil || a.Format != FormatWAV {
			return
		}
		if a.Duration <= 0 || a.Duration > MaxImportedDuration || a.NeedsFrameCount() {
			t.Fatalf("detect = %+v", a)
		}
	})
}
