package recording

import (
	"encoding/binary"
	"time"
)

// WAVChunkHeaderBytes is the size of a RIFF chunk header: its id and its little-endian size.
const WAVChunkHeaderBytes = 8

// maxWAVChunkReads bounds the chunk headers read past the head of a WAV (big LIST or JUNK chunks before the data): a
// file of tiny chunks is refused instead of being walked header by header.
const maxWAVChunkReads = 64

// wavScan is the walk of the chunk headers of a WAV past the head: the offset of the next header, the byte rate of
// its fmt chunk and the file size. Zero when no walk is due.
type wavScan struct {
	next     int64
	byteRate uint32
	size     int64
	reads    int
}

// WAVChunkAt is the offset of the next chunk header (WAVChunkHeaderBytes) of a WAV whose data chunk lies past the
// head; ok is false when a holds no such walk. It always lies within the file.
func (a ImportedAudio) WAVChunkAt() (int64, bool) {
	return a.wav.next, a.wav.next > 0
}

// WAVChunk takes the header read at WAVChunkAt: the data chunk gives the duration as in the head; another chunk moves
// the walk past its body (padded to an even size). A header shorter than WAVChunkHeaderBytes, a chunk that overshoots
// the file, the end of the file or more than maxWAVChunkReads headers end the walk as ErrAudioDurationUnknown. Without
// a walk a is returned as it is.
func (a ImportedAudio) WAVChunk(header []byte) (ImportedAudio, error) {
	s := a.wav
	if s.next <= 0 {
		return a, nil
	}
	if len(header) < WAVChunkHeaderBytes {
		return knownAudio(FormatWAV, 0, false)
	}

	id, n := string(header[:4]), int64(binary.LittleEndian.Uint32(header[4:8]))
	body := s.next + WAVChunkHeaderBytes
	if id == "data" {
		d, ok := wavData(n, body, s.size, s.byteRate)
		return knownAudio(FormatWAV, d, ok)
	}
	s.reads++
	s.next = body + n + n%2
	if s.reads >= maxWAVChunkReads || s.next+WAVChunkHeaderBytes > s.size {
		return knownAudio(FormatWAV, 0, false)
	}
	a.wav = s
	return a, nil
}

// wavAudio reads the fmt and data chunks in the head. When the head ends before the data chunk, the walk goes on past
// it (WAVChunkAt) if the fmt chunk gave a byte rate and the next header lies within the file.
func wavAudio(head []byte, size int64) (ImportedAudio, error) {
	var byteRate uint32
	off := int64(12)
	for off+WAVChunkHeaderBytes <= int64(len(head)) {
		id, n := string(head[off:off+4]), int64(binary.LittleEndian.Uint32(head[off+4:off+8]))
		body := off + WAVChunkHeaderBytes
		switch id {
		case "fmt ":
			if body+12 <= int64(len(head)) {
				byteRate = binary.LittleEndian.Uint32(head[body+8 : body+12])
			}
		case "data":
			d, ok := wavData(n, body, size, byteRate)
			return knownAudio(FormatWAV, d, ok)
		}
		off = body + n + n%2 // chunks are padded to an even size
	}
	if byteRate == 0 || off+WAVChunkHeaderBytes > size {
		return knownAudio(FormatWAV, 0, false)
	}
	return ImportedAudio{Format: FormatWAV, wav: wavScan{next: off, byteRate: byteRate, size: size}}, nil
}

// wavData is the duration of the data chunk of n bytes from body; not ok without a byte rate or data. A streamed WAV
// (a zero or all-ones data size, as a recorder that crashed leaves it) lasts what the rest of the file holds; the data
// size is capped by the file size, so a cut file is not longer than what it holds.
func wavData(n, body, size int64, byteRate uint32) (time.Duration, bool) {
	if byteRate == 0 {
		return 0, false
	}
	rest := size - body
	if n == 0 || n == 0xFFFFFFFF {
		n = rest
	}
	n = min(n, rest)
	if n <= 0 {
		return 0, false
	}
	return ratio(uint64(n), uint64(byteRate)), true
}
