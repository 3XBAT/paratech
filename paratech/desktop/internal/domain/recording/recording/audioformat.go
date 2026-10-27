package recording

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"time"
)

// AudioFormat is a container of an imported file that whisper-cli v1.9.5 decodes itself (miniaudio:
// docs/refs/whisper.md). It is also the extension of the copy.
type AudioFormat string

// Formats of imported files. Video and M4A/AAC/WMA need a local decoder first (ADR-0027 Open 4).
const (
	FormatWAV  AudioFormat = "wav"
	FormatFLAC AudioFormat = "flac"
	FormatMP3  AudioFormat = "mp3"
	FormatOGG  AudioFormat = "ogg" // Ogg Vorbis only: miniaudio has no Opus decoder
)

// Valid reports whether f is one of the formats above.
func (f AudioFormat) Valid() bool {
	switch f {
	case FormatWAV, FormatFLAC, FormatMP3, FormatOGG:
		return true
	}
	return false
}

// ImportHeadBytes is how much of the start of a file DetectAudio needs: enough for the WAV chunks before the data and
// for a usual ID3 tag.
const ImportHeadBytes = 64 << 10

// ImportTailBytes is how much of the end of a file DetectAudio needs: more than the largest Ogg page (65307 bytes).
const ImportTailBytes = 128 << 10

// ImportedAudio is what the content of a file says: its format and its duration from the headers. An MP3 learns its
// duration only when its frames are counted during the copy (NeedsFrameCount, Counted).
type ImportedAudio struct {
	Format   AudioFormat
	Duration time.Duration

	counting  bool  // an MP3 whose frames are still to be counted
	countFrom int64 // the offset of its first frame: after the ID3 tag
}

// NeedsFrameCount reports an MP3 whose duration is known only after an MP3FrameCounter has seen all its bytes.
func (a ImportedAudio) NeedsFrameCount() bool { return a.counting }

// DetectAudio tells the format of a file from its first bytes (head, at most ImportHeadBytes are read), its last
// bytes (tail, at most ImportTailBytes; nil when the head holds the whole file) and its size, never from its name:
// RIFF/WAVE, fLaC, an ID3 tag or an MPEG audio frame, OggS with Vorbis. Anything else is ErrUnsupportedAudio. The
// duration comes from the headers without decoding: the WAV data chunk, the FLAC STREAMINFO, the granule position of
// the last Ogg page; one longer than MaxImportedDuration is ErrAudioTooLong, one the headers do not tell is
// ErrAudioDurationUnknown. An MP3 is refused here only when its Xing/VBRI header, or the size over a bitrate that is
// the same at the start and at the end, says it is too long; otherwise its frames are counted during the copy.
func DetectAudio(head, tail []byte, size int64) (ImportedAudio, error) {
	if len(head) > ImportHeadBytes {
		head = head[:ImportHeadBytes]
	}
	if len(tail) == 0 && int64(len(head)) >= size {
		tail = head
	}
	if len(tail) > ImportTailBytes {
		tail = tail[len(tail)-ImportTailBytes:]
	}

	switch {
	case len(head) >= 12 && string(head[:4]) == "RIFF" && string(head[8:12]) == "WAVE":
		d, ok := wavDuration(head, size)
		return knownAudio(FormatWAV, d, ok)
	case bytes.HasPrefix(head, []byte("fLaC")):
		d, ok := flacDuration(head)
		return knownAudio(FormatFLAC, d, ok)
	case bytes.HasPrefix(head, []byte("OggS")):
		if !oggVorbis(head) {
			return ImportedAudio{}, fmt.Errorf("%w: an Ogg stream that is not Vorbis", ErrUnsupportedAudio)
		}
		d, ok := oggDuration(head, tail)
		return knownAudio(FormatOGG, d, ok)
	case bytes.HasPrefix(head, []byte("ID3")):
		return id3Audio(head, tail, size)
	case mpegFrame(head):
		return mp3Audio(head, tail, size, 0)
	}
	return ImportedAudio{}, ErrUnsupportedAudio
}

// knownAudio is the audio of a format whose headers told the duration d (ok), within MaxImportedDuration.
func knownAudio(f AudioFormat, d time.Duration, ok bool) (ImportedAudio, error) {
	switch {
	case !ok || d <= 0:
		return ImportedAudio{}, fmt.Errorf("%w: a %s file", ErrAudioDurationUnknown, f)
	case d > MaxImportedDuration:
		return ImportedAudio{}, fmt.Errorf("%w: %v, at most %v", ErrAudioTooLong, d.Round(time.Second), MaxImportedDuration)
	}
	return ImportedAudio{Format: f, Duration: d}, nil
}

// wavDuration reads the fmt and data chunks; not ok when the header does not tell (no byte rate, chunks past the
// head). A streamed WAV (a zero or all-ones data size, as a recorder that crashed leaves it) lasts what the rest of
// the file holds; the data size is capped by the file size, so a cut file is not longer than what it holds.
func wavDuration(head []byte, size int64) (time.Duration, bool) {
	var byteRate uint32
	for off := 12; off+8 <= len(head); {
		id, n := string(head[off:off+4]), int64(binary.LittleEndian.Uint32(head[off+4:off+8]))
		body := off + 8
		switch id {
		case "fmt ":
			if body+12 <= len(head) {
				byteRate = binary.LittleEndian.Uint32(head[body+8 : body+12])
			}
		case "data":
			if byteRate == 0 {
				return 0, false
			}
			rest := size - int64(body)
			if n == 0 || n == 0xFFFFFFFF {
				n = rest
			}
			n = min(n, rest)
			if n <= 0 {
				return 0, false
			}
			return ratio(uint64(n), uint64(byteRate)), true
		}
		next := int64(body) + n + n%2 // chunks are padded to an even size
		if next > int64(len(head)) {
			return 0, false
		}
		off = int(next)
	}
	return 0, false
}

// id3Audio looks past an ID3v2 tag: FLAC or an MPEG frame after it. A tag longer than the head is taken for MP3, the
// usual case (a large cover picture), whose frames are counted from the end of the tag.
func id3Audio(head, tail []byte, size int64) (ImportedAudio, error) {
	if len(head) < 10 {
		return ImportedAudio{}, ErrUnsupportedAudio
	}
	n := 0
	for _, b := range head[6:10] {
		if b&0x80 != 0 {
			return ImportedAudio{}, fmt.Errorf("%w: a broken ID3 tag", ErrUnsupportedAudio)
		}
		n = n<<7 | int(b)
	}
	end := 10 + n
	if head[5]&0x10 != 0 {
		end += 10 // footer
	}
	if end >= len(head) {
		return ImportedAudio{Format: FormatMP3, counting: true, countFrom: int64(end)}, nil
	}
	rest := head[end:]
	switch {
	case bytes.HasPrefix(rest, []byte("fLaC")):
		d, ok := flacDuration(rest)
		return knownAudio(FormatFLAC, d, ok)
	case mpegFrame(rest):
		return mp3Audio(head, tail, size, end)
	}
	return ImportedAudio{}, fmt.Errorf("%w: an ID3 tag before something that is not MP3", ErrUnsupportedAudio)
}

// mpegFrame reports an MPEG audio frame header: the 11-bit sync, a known version, layer I-III (AAC in ADTS has layer
// 0 and is refused), a bitrate index that is not "bad" and a known sample rate.
func mpegFrame(b []byte) bool {
	if len(b) < 4 || b[0] != 0xFF || b[1]&0xE0 != 0xE0 {
		return false
	}
	version, layer := b[1]>>3&0x3, b[1]>>1&0x3
	bitrate, rate := b[2]>>4, b[2]>>2&0x3
	return version != 1 && layer != 0 && bitrate != 0xF && rate != 3
}

// oggVorbis reports whether the first page of an Ogg stream starts a Vorbis stream (identification header).
func oggVorbis(head []byte) bool {
	if len(head) < 27 {
		return false
	}
	body := 27 + int(head[26])
	return body+7 <= len(head) && string(head[body:body+7]) == "\x01vorbis"
}
