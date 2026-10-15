package audiocapture

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
)

const (
	tagPCM        = 0x0001 // WAVE_FORMAT_PCM
	tagFloat      = 0x0003 // WAVE_FORMAT_IEEE_FLOAT
	tagExtensible = 0xFFFE // WAVE_FORMAT_EXTENSIBLE

	pcmHeaderLen   = 44 // RIFF(12) + fmt(8+16) + data(8)
	floatHeaderLen = 58 // RIFF(12) + fmt(8+18) + fact(8+4) + data(8); non-PCM WAV needs cbSize and a fact chunk

	extensibleFmtLen = 40 // WAVEFORMATEXTENSIBLE
	maxFmtLen        = 1024
	maxChunksScanned = 64
)

// subFormatTail is the GUID tail of KSDATAFORMAT_SUBTYPE_PCM and _IEEE_FLOAT after Data1 (the base tag) in a
// WAVEFORMATEXTENSIBLE (mmreg.h: DEFINE_WAVEFORMATEX_GUID).
func subFormatTail() []byte {
	return []byte{0x00, 0x00, 0x10, 0x00, 0x80, 0x00, 0x00, 0xaa, 0x00, 0x38, 0x9b, 0x71}
}

// errNotAudio means a file cannot be read as a complete chunk: not WAV, an unsupported format, or a header that does
// not describe the bytes on disk (a placeholder header of a killed writer, a truncated file).
var errNotAudio = errors.New("not a complete WAV file")

func headerLen(f Format) int {
	if f.Float {
		return floatHeaderLen
	}
	return pcmHeaderLen
}

// maxDataBytes is the largest data size whose file still fits the 32-bit RIFF size.
func maxDataBytes(f Format) int64 { return math.MaxUint32 - int64(headerLen(f)) - 1 }

// encodeHeader returns the WAV header for frames frames. final == false yields the placeholder written when a chunk
// starts: its RIFF size is 0, which never matches a real file, so a chunk cut short is never valid. An odd data size
// is followed by a pad byte (RIFF rule), which the RIFF size counts.
func encodeHeader(f Format, frames int64, final bool) []byte {
	n := headerLen(f)
	data := frames * int64(f.BlockAlign())
	riff := int64(n-8) + data + data&1
	if !final {
		riff, data, frames = 0, 0, 0
	}

	le := binary.LittleEndian
	b := make([]byte, 0, n)
	b = append(b, "RIFF"...)
	b = le.AppendUint32(b, uint32(riff)) //nolint:gosec // the writers bound data by maxDataBytes
	b = append(b, "WAVE"...)

	b = append(b, "fmt "...)
	tag, fmtLen := uint16(tagPCM), uint32(16)
	if f.Float {
		tag, fmtLen = tagFloat, 18
	}
	b = le.AppendUint32(b, fmtLen)
	b = le.AppendUint16(b, tag)
	b = le.AppendUint16(b, f.Channels)
	b = le.AppendUint32(b, f.SampleRate)
	b = le.AppendUint32(b, f.ByteRate())
	b = le.AppendUint16(b, uint16(f.BlockAlign())) //nolint:gosec // BlockAlign <= 32 after Validate
	b = le.AppendUint16(b, f.BitsPerSample)
	if f.Float {
		b = le.AppendUint16(b, 0) // cbSize
		b = append(b, "fact"...)
		b = le.AppendUint32(b, 4)
		b = le.AppendUint32(b, uint32(frames)) //nolint:gosec // bounded with data
	}

	b = append(b, "data"...)
	b = le.AppendUint32(b, uint32(data)) //nolint:gosec // bounded by maxDataBytes
	return b
}

// chunkInfo describes a complete chunk file.
type chunkInfo struct {
	Format Format
	Frames int64
}

// readHeader checks that the file r of size bytes is a complete WAV file whose header describes exactly the bytes
// present, and leaves r at the first audio byte. A file that is not complete audio yields an error wrapping
// errNotAudio; a failing reader yields its own error (an I/O problem, not a broken chunk).
func readHeader(r io.Reader, size int64) (chunkInfo, error) {
	le := binary.LittleEndian
	bad := func(format string, args ...any) error {
		return fmt.Errorf("%w: %s", errNotAudio, fmt.Sprintf(format, args...))
	}

	if size < 12 {
		return chunkInfo{}, bad("%d bytes", size)
	}
	head := make([]byte, 12)
	if _, err := io.ReadFull(r, head); err != nil {
		return chunkInfo{}, err
	}
	if string(head[0:4]) != "RIFF" || string(head[8:12]) != "WAVE" {
		return chunkInfo{}, bad("no RIFF/WAVE signature")
	}
	if riff := int64(le.Uint32(head[4:8])); riff+8 != size {
		return chunkInfo{}, bad("RIFF size %d does not match the file size %d", riff, size)
	}

	var (
		f      Format
		hasFmt bool
		off    = int64(12)
		hdr    = make([]byte, 8)
	)
	for range maxChunksScanned {
		if off+8 > size {
			return chunkInfo{}, bad("no data chunk")
		}
		if _, err := io.ReadFull(r, hdr); err != nil {
			return chunkInfo{}, err
		}
		id, n, body := string(hdr[0:4]), int64(le.Uint32(hdr[4:8])), off+8
		if body+n > size {
			return chunkInfo{}, bad("chunk %q runs past the end of the file", sanitizeID(hdr[0:4]))
		}

		switch id {
		case "fmt ":
			if n < 16 || n > maxFmtLen {
				return chunkInfo{}, bad("fmt chunk of %d bytes", n)
			}
			b := make([]byte, n)
			if _, err := io.ReadFull(r, b); err != nil {
				return chunkInfo{}, err
			}
			parsed, err := parseFmt(b)
			if err != nil {
				return chunkInfo{}, err
			}
			f, hasFmt = parsed, true
		case "data":
			if !hasFmt {
				return chunkInfo{}, bad("data before fmt")
			}
			if n%int64(f.BlockAlign()) != 0 {
				return chunkInfo{}, bad("%d data bytes is not a whole number of frames", n)
			}
			return chunkInfo{Format: f, Frames: n / int64(f.BlockAlign())}, nil
		default:
			if _, err := io.CopyN(io.Discard, r, n); err != nil {
				return chunkInfo{}, err
			}
		}

		off = body + n
		if n&1 == 1 && off < size { // RIFF chunks are padded to an even size
			if _, err := io.CopyN(io.Discard, r, 1); err != nil {
				return chunkInfo{}, err
			}
			off++
		}
	}
	return chunkInfo{}, bad("too many chunks before data")
}

// parseFmt decodes a fmt chunk body: WAVEFORMATEX or WAVEFORMATEXTENSIBLE with integer PCM or IEEE float samples.
func parseFmt(b []byte) (Format, error) {
	f, err := decodeWaveFormat(b)
	if err != nil {
		return Format{}, fmt.Errorf("%w: %w", errNotAudio, err)
	}
	return f, nil
}

// decodeWaveFormat decodes a WAVEFORMATEX (the first 16 bytes suffice for PCM and float) or a WAVEFORMATEXTENSIBLE,
// as found in a WAV fmt chunk and in WASAPI. Its errors wrap ErrInvalidFormat.
func decodeWaveFormat(b []byte) (Format, error) {
	le := binary.LittleEndian
	if len(b) < 16 {
		return Format{}, fmt.Errorf("%w: WAVEFORMATEX of %d bytes", ErrInvalidFormat, len(b))
	}
	tag := uint32(le.Uint16(b[0:2]))
	if tag == tagExtensible {
		if len(b) < extensibleFmtLen || int(le.Uint16(b[16:18])) < extensibleFmtLen-18 {
			return Format{}, fmt.Errorf("%w: short WAVEFORMATEXTENSIBLE", ErrInvalidFormat)
		}
		if !bytes.Equal(b[28:40], subFormatTail()) {
			return Format{}, fmt.Errorf("%w: unknown sub-format", ErrInvalidFormat)
		}
		tag = le.Uint32(b[24:28])
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
		return Format{}, fmt.Errorf("%w: format tag 0x%04x", ErrInvalidFormat, tag)
	}
	if err := f.Validate(); err != nil {
		return Format{}, err
	}
	if int(le.Uint16(b[12:14])) != f.BlockAlign() || le.Uint32(b[8:12]) != f.ByteRate() {
		return Format{}, fmt.Errorf("%w: inconsistent block align or byte rate", ErrInvalidFormat)
	}
	return f, nil
}

// sanitizeID makes a RIFF chunk id printable for an error message.
func sanitizeID(id []byte) string {
	out := make([]byte, len(id))
	for i, c := range id {
		if c < 0x20 || c > 0x7e {
			c = '?'
		}
		out[i] = c
	}
	return string(out)
}
