package recording

import (
	"encoding/binary"
	"errors"
	"testing"
	"time"
)

// wavHeader is a synthetic RIFF/WAVE header: 16 kHz mono 16-bit PCM (32000 bytes a second), extra chunks of the
// given sizes before the data, and a data chunk of dataBytes.
func wavHeader(dataBytes uint32, extra ...int) []byte {
	le32 := func(v uint32) []byte { return binary.LittleEndian.AppendUint32(nil, v) }
	le16 := func(v uint16) []byte { return binary.LittleEndian.AppendUint16(nil, v) }

	b := append([]byte("RIFF"), le32(36+dataBytes)...)
	b = append(b, "WAVE"...)
	b = append(b, "fmt "...)
	b = append(b, le32(16)...)
	b = append(b, le16(1)...)     // PCM
	b = append(b, le16(1)...)     // mono
	b = append(b, le32(16000)...) // sample rate
	b = append(b, le32(32000)...) // byte rate
	b = append(b, le16(2)...)     // block align
	b = append(b, le16(16)...)    // bits
	for _, n := range extra {
		b = append(b, "LIST"...)
		b = append(b, le32(uint32(n))...)
		b = append(b, make([]byte, n+n%2)...)
	}
	b = append(b, "data"...)
	return append(b, le32(dataBytes)...)
}

func TestDetectAudio(t *testing.T) {
	const big = int64(1) << 40 // the file size does not cap the header duration
	ogg := func(packet string) []byte {
		b := append([]byte("OggS"), make([]byte, 22)...)
		b = append(b, 1, byte(len(packet)))
		return append(b, packet...)
	}
	id3 := func(size int, after ...byte) []byte {
		b := []byte{'I', 'D', '3', 4, 0, 0, 0, 0, byte(size >> 7), byte(size & 0x7F)}
		b = append(b, make([]byte, size)...)
		return append(b, after...)
	}
	mpeg := []byte{0xFF, 0xFB, 0x90, 0x64} // MPEG-1 layer III, 128 kbit/s, 44.1 kHz
	tests := []struct {
		name string
		head []byte
		size int64
		want ImportedAudio
		err  error
	}{
		{"wav one minute", wavHeader(32000 * 60), big, ImportedAudio{FormatWAV, time.Minute}, nil},
		{"wav after a LIST chunk", wavHeader(32000*90, 101), big, ImportedAudio{FormatWAV, 90 * time.Second}, nil},
		{"wav cut short", wavHeader(32000 * 60), 44 + 32000*10, ImportedAudio{FormatWAV, 10 * time.Second}, nil},
		{"wav exactly four hours", wavHeader(32000 * 4 * 3600), big, ImportedAudio{FormatWAV, 4 * time.Hour}, nil},
		{"wav over four hours", wavHeader(32000*4*3600 + 32000), big, ImportedAudio{}, ErrAudioTooLong},
		{"wav streamed", wavHeader(0xFFFFFFFF), big, ImportedAudio{FormatWAV, 0}, nil},
		{"wav data past the head", wavHeader(32000, ImportHeadBytes), big, ImportedAudio{FormatWAV, 0}, nil},
		{"wav header only", []byte("RIFF\x00\x00\x00\x00WAVE"), big, ImportedAudio{FormatWAV, 0}, nil},
		{"flac", []byte("fLaC\x00\x00\x00\x22"), big, ImportedAudio{FormatFLAC, 0}, nil},
		{"mp3 frame", mpeg, big, ImportedAudio{FormatMP3, 0}, nil},
		{"mp3 after id3", id3(20, mpeg...), big, ImportedAudio{FormatMP3, 0}, nil},
		{"flac after id3", id3(20, []byte("fLaC")...), big, ImportedAudio{FormatFLAC, 0}, nil},
		{"id3 longer than the head", id3(5)[:10], big, ImportedAudio{FormatMP3, 0}, nil},
		{"id3 before something else", id3(4, 'a', 'b', 'c', 'd'), big, ImportedAudio{}, ErrUnsupportedAudio},
		{"broken id3 size", []byte{'I', 'D', '3', 4, 0, 0, 0x80, 0, 0, 0, 0}, big, ImportedAudio{}, ErrUnsupportedAudio},
		{"ogg vorbis", ogg("\x01vorbis\x00\x00"), big, ImportedAudio{FormatOGG, 0}, nil},
		{"ogg opus", ogg("OpusHead\x01"), big, ImportedAudio{}, ErrUnsupportedAudio},
		{"aac adts", []byte{0xFF, 0xF1, 0x50, 0x80}, big, ImportedAudio{}, ErrUnsupportedAudio},
		{"m4a", append([]byte{0, 0, 0, 0x20}, "ftypM4A "...), big, ImportedAudio{}, ErrUnsupportedAudio},
		{"wma asf", []byte{0x30, 0x26, 0xB2, 0x75, 0x8E, 0x66, 0xCF, 0x11}, big, ImportedAudio{}, ErrUnsupportedAudio},
		{"webm", []byte{0x1A, 0x45, 0xDF, 0xA3}, big, ImportedAudio{}, ErrUnsupportedAudio},
		{"riff avi", []byte("RIFF\x00\x00\x00\x00AVI "), big, ImportedAudio{}, ErrUnsupportedAudio},
		{"text named .mp3", []byte("hello"), big, ImportedAudio{}, ErrUnsupportedAudio},
		{"empty", nil, 0, ImportedAudio{}, ErrUnsupportedAudio},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DetectAudio(tc.head, tc.size)
			if tc.err != nil {
				if !errors.Is(err, tc.err) {
					t.Fatalf("DetectAudio = %+v, %v; want %v", got, err, tc.err)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("DetectAudio = %+v, %v; want %+v", got, err, tc.want)
			}
		})
	}
}

func TestAudioFormatValid(t *testing.T) {
	for f, want := range map[AudioFormat]bool{
		FormatWAV: true, FormatFLAC: true, FormatMP3: true, FormatOGG: true, "m4a": false, "": false, "WAV": false,
	} {
		if f.Valid() != want {
			t.Errorf("%q.Valid() = %v", f, !want)
		}
	}
}

// Fuzz the header readers: no panic, and a result is always a valid format within the limits.
func FuzzDetectAudio(f *testing.F) {
	f.Add(wavHeader(32000), int64(1000))
	f.Add([]byte("fLaC"), int64(4))
	f.Add([]byte{'I', 'D', '3', 4, 0, 0, 0, 0, 0, 2, 0xFF, 0xFB, 0x90, 0x64}, int64(14))
	f.Fuzz(func(t *testing.T, head []byte, size int64) {
		got, err := DetectAudio(head, size)
		if err != nil {
			return
		}
		if !got.Format.Valid() || got.Duration < 0 || got.Duration > MaxImportedDuration {
			t.Fatalf("DetectAudio = %+v", got)
		}
	})
}
