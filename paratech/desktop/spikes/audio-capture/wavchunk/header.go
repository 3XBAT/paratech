package wavchunk

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"time"
)

const (
	tagPCM        = 1
	tagFloat      = 3
	tagExtensible = 0xFFFE

	pcmHeaderLen   = 44 // RIFF(12) + fmt(8+16) + data(8)
	floatHeaderLen = 58 // RIFF(12) + fmt(8+18) + fact(8+4) + data(8); non-PCM WAV needs cbSize and a fact chunk

	maxChunksScanned = 64
)

var (
	// ErrNotWAV means the file is not a RIFF/WAVE file at all.
	ErrNotWAV = errors.New("wavchunk: not a WAV file")

	// ErrIncomplete means the file looks like WAV but its header does not describe the bytes on disk:
	// the writer was killed before the header was final, or the file was truncated.
	ErrIncomplete = errors.New("wavchunk: incomplete WAV file")
)

func headerLen(f Format) int {
	if f.Float {
		return floatHeaderLen
	}
	return pcmHeaderLen
}

// encodeHeader returns the WAV header for frames frames. final == false yields the placeholder written when a
// chunk starts: its RIFF size is 0, which never matches a real file, so a chunk cut short is never valid.
func encodeHeader(f Format, frames uint32, final bool) []byte {
	n := headerLen(f)
	data := frames * uint32(f.BlockAlign()) //nolint:gosec // the writer bounds frames so that data fits in uint32
	riff := uint32(n-8) + data              //nolint:gosec // n is 44 or 58
	if !final {
		riff, data, frames = 0, 0, 0
	}

	b := make([]byte, 0, n)
	le := binary.LittleEndian
	b = append(b, "RIFF"...)
	b = le.AppendUint32(b, riff)
	b = append(b, "WAVE"...)

	b = append(b, "fmt "...)
	tag := uint16(tagPCM)
	fmtLen := uint32(16)
	if f.Float {
		tag, fmtLen = tagFloat, 18
	}
	b = le.AppendUint32(b, fmtLen)
	b = le.AppendUint16(b, tag)
	b = le.AppendUint16(b, f.Channels)
	b = le.AppendUint32(b, f.SampleRate)
	b = le.AppendUint32(b, f.ByteRate())
	b = le.AppendUint16(b, uint16(f.BlockAlign())) //nolint:gosec // BlockAlign <= 256 after Validate
	b = le.AppendUint16(b, f.BitsPerSample)
	if f.Float {
		b = le.AppendUint16(b, 0) // cbSize
		b = append(b, "fact"...)
		b = le.AppendUint32(b, 4)
		b = le.AppendUint32(b, frames)
	}

	b = append(b, "data"...)
	b = le.AppendUint32(b, data)
	return b
}

// Info describes a complete chunk.
type Info struct {
	Format Format
	Frames int64
}

// Duration is the audio length counted by frames.
func (i Info) Duration() time.Duration { return i.Format.Duration(i.Frames) }

// Read checks that r (size bytes) is a complete WAV file whose header describes exactly the bytes present,
// and returns its format and frame count. It returns errors wrapping ErrNotWAV or ErrIncomplete.
func Read(r io.ReaderAt, size int64) (Info, error) {
	le := binary.LittleEndian
	head := make([]byte, 12)
	if size < int64(len(head)) {
		return Info{}, fmt.Errorf("%w: %d bytes", ErrIncomplete, size)
	}
	if _, err := r.ReadAt(head, 0); err != nil {
		return Info{}, fmt.Errorf("%w: %v", ErrIncomplete, err)
	}
	if string(head[0:4]) != "RIFF" || string(head[8:12]) != "WAVE" {
		return Info{}, ErrNotWAV
	}
	if riff := int64(le.Uint32(head[4:8])); riff+8 != size {
		return Info{}, fmt.Errorf("%w: RIFF size %d does not match file size %d", ErrIncomplete, riff, size)
	}

	var (
		f      Format
		hasFmt bool
		off    = int64(12)
		hdr    = make([]byte, 8)
	)
	for range maxChunksScanned {
		if off+8 > size {
			return Info{}, fmt.Errorf("%w: no data chunk", ErrIncomplete)
		}
		if _, err := r.ReadAt(hdr, off); err != nil {
			return Info{}, fmt.Errorf("%w: %v", ErrIncomplete, err)
		}
		id, n, body := string(hdr[0:4]), int64(le.Uint32(hdr[4:8])), off+8
		if body+n > size {
			return Info{}, fmt.Errorf("%w: chunk %q runs past the end of the file", ErrIncomplete, id)
		}

		switch id {
		case "fmt ":
			b := make([]byte, n)
			if _, err := r.ReadAt(b, body); err != nil {
				return Info{}, fmt.Errorf("%w: %v", ErrIncomplete, err)
			}
			parsed, err := parseFmt(b)
			if err != nil {
				return Info{}, err
			}
			f, hasFmt = parsed, true
		case "data":
			if !hasFmt {
				return Info{}, fmt.Errorf("%w: data before fmt", ErrNotWAV)
			}
			if n%int64(f.BlockAlign()) != 0 {
				return Info{}, fmt.Errorf("%w: %d data bytes is not a whole number of frames", ErrIncomplete, n)
			}
			return Info{Format: f, Frames: n / int64(f.BlockAlign())}, nil
		}
		off = body + n + n&1 // RIFF chunks are padded to an even size
	}
	return Info{}, fmt.Errorf("%w: too many chunks before data", ErrNotWAV)
}

// parseFmt decodes a fmt chunk body (WAVEFORMATEX, optionally WAVEFORMATEXTENSIBLE).
func parseFmt(b []byte) (Format, error) {
	le := binary.LittleEndian
	if len(b) < 16 {
		return Format{}, fmt.Errorf("%w: fmt chunk of %d bytes", ErrNotWAV, len(b))
	}
	tag := le.Uint16(b[0:2])
	if tag == tagExtensible && len(b) >= 40 {
		tag = le.Uint16(b[24:26]) // first bytes of SubFormat carry the base tag
	}

	f := Format{
		Channels:      le.Uint16(b[2:4]),
		SampleRate:    le.Uint32(b[4:8]),
		BitsPerSample: le.Uint16(b[14:16]),
	}
	switch tag {
	case tagPCM:
	case tagFloat:
		f.Float = true
	default:
		return Format{}, fmt.Errorf("%w: format tag 0x%04x", ErrNotWAV, tag)
	}
	if err := f.Validate(); err != nil {
		return Format{}, fmt.Errorf("%w: %v", ErrNotWAV, err)
	}
	if int(le.Uint16(b[12:14])) != f.BlockAlign() || le.Uint32(b[8:12]) != f.ByteRate() {
		return Format{}, fmt.Errorf("%w: inconsistent block align or byte rate", ErrNotWAV)
	}
	return f, nil
}
