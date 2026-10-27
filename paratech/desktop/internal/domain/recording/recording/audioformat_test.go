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

// id3Tag is an ID3v2.4 tag of size bytes (zeros) followed by after.
func id3Tag(size int, after ...byte) []byte {
	b := []byte{'I', 'D', '3', 4, 0, 0, 0, 0, byte(size >> 7), byte(size & 0x7F)}
	b = append(b, make([]byte, size)...)
	return append(b, after...)
}

func TestDetectAudio(t *testing.T) {
	const big = int64(1) << 40 // the file size does not cap the header duration
	mpeg := mp3Frames(cbr128, 3)
	ogg := oggFile(44100, 44100*90)
	tests := []struct {
		name string
		head []byte
		size int64
		want ImportedAudio
		err  error
	}{
		{"wav one minute", wavHeader(32000 * 60), big, ImportedAudio{Format: FormatWAV, Duration: time.Minute}, nil},
		{"wav after a LIST chunk", wavHeader(32000*90, 101), big, ImportedAudio{Format: FormatWAV, Duration: 90 * time.Second}, nil},
		{"wav cut short", wavHeader(32000 * 60), 44 + 32000*10, ImportedAudio{Format: FormatWAV, Duration: 10 * time.Second}, nil},
		{"wav exactly four hours", wavHeader(32000 * 4 * 3600), big, ImportedAudio{Format: FormatWAV, Duration: 4 * time.Hour}, nil},
		{"wav over four hours", wavHeader(32000*4*3600 + 32000), big, ImportedAudio{}, ErrAudioTooLong},
		{"wav streamed", wavHeader(0xFFFFFFFF), 44 + 32000*60, ImportedAudio{Format: FormatWAV, Duration: time.Minute}, nil},
		{"wav with a zero data size", wavHeader(0), 44 + 32000*30, ImportedAudio{Format: FormatWAV, Duration: 30 * time.Second}, nil},
		{"wav streamed into a huge file", wavHeader(0xFFFFFFFF), big, ImportedAudio{}, ErrAudioTooLong},
		{"wav without data", wavHeader(0), 44, ImportedAudio{}, ErrAudioDurationUnknown},
		{"wav data past the head", wavHeader(32000, ImportHeadBytes), big, ImportedAudio{}, ErrAudioDurationUnknown},
		{"wav header only", []byte("RIFF\x00\x00\x00\x00WAVE"), big, ImportedAudio{}, ErrAudioDurationUnknown},
		{"flac", flacHead(48000, 48000*3600), big, ImportedAudio{Format: FormatFLAC, Duration: time.Hour}, nil},
		{"flac over four hours", flacHead(8000, 8000*(4*3600+1)), big, ImportedAudio{}, ErrAudioTooLong},
		{"flac without total samples", flacHead(48000, 0), big, ImportedAudio{}, ErrAudioDurationUnknown},
		{"flac without the streaminfo", []byte("fLaC\x00\x00\x00\x22"), big, ImportedAudio{}, ErrAudioDurationUnknown},
		{"mp3 frames", mpeg, int64(len(mpeg)), ImportedAudio{Format: FormatMP3, counting: true}, nil},
		{"mp3 after id3", id3Tag(20, mpeg...), int64(30 + len(mpeg)), ImportedAudio{Format: FormatMP3, counting: true, countFrom: 30}, nil},
		{"flac after id3", id3Tag(20, flacHead(16000, 16000*60)...), big, ImportedAudio{Format: FormatFLAC, Duration: time.Minute}, nil},
		{"id3 longer than the head", id3Tag(5)[:10], big, ImportedAudio{Format: FormatMP3, counting: true, countFrom: 15}, nil},
		{"id3 before something else", id3Tag(4, 'a', 'b', 'c', 'd'), big, ImportedAudio{}, ErrUnsupportedAudio},
		{"broken id3 size", []byte{'I', 'D', '3', 4, 0, 0, 0x80, 0, 0, 0, 0}, big, ImportedAudio{}, ErrUnsupportedAudio},
		{"ogg vorbis", ogg, int64(len(ogg)), ImportedAudio{Format: FormatOGG, Duration: 90 * time.Second}, nil},
		{"ogg opus", oggPage(7, 0, []byte("OpusHead\x01")), big, ImportedAudio{}, ErrUnsupportedAudio},
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
			got, err := DetectAudio(tc.head, nil, tc.size)
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

// The duration of an Ogg file is read from its tail; the head holds only the first page then.
func TestDetectAudioOggTail(t *testing.T) {
	head := oggPage(7, 0, vorbisID(48000))
	size := int64(1) << 30
	tests := []struct {
		name string
		tail []byte
		want time.Duration
		err  error
	}{
		{"last page", append([]byte("audio"), oggPage(7, 48000*3600, []byte("x"))...), time.Hour, nil},
		{"over four hours", oggPage(7, 48000*(4*3600+1), []byte("x")), 0, ErrAudioTooLong},
		{"a huge granule", oggPage(7, 1<<62, []byte("x")), 0, ErrAudioTooLong},
		{"last page without a granule", append(oggPage(7, 48000*60, []byte("a")), oggPage(7, -1, []byte("b"))...), time.Minute, nil},
		{"a cut last page", append(oggPage(7, 48000*60, []byte("a")), oggPage(7, 48000*120, []byte("bbbb"))[:30]...), time.Minute, nil},
		{"only another stream", oggPage(8, 48000*60, []byte("x")), 0, ErrAudioDurationUnknown},
		{"a negative granule", oggPage(7, -5, []byte("x")), 0, ErrAudioDurationUnknown},
		{"no page", []byte("no pages at the end of this file"), 0, ErrAudioDurationUnknown},
		{"no tail", nil, 0, ErrAudioDurationUnknown},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DetectAudio(head, tc.tail, size)
			if tc.err != nil {
				if !errors.Is(err, tc.err) {
					t.Fatalf("DetectAudio = %+v, %v; want %v", got, err, tc.err)
				}
				return
			}
			if err != nil || got != (ImportedAudio{Format: FormatOGG, Duration: tc.want}) {
				t.Fatalf("DetectAudio = %+v, %v; want %v", got, err, tc.want)
			}
		})
	}

	if _, err := DetectAudio(oggPage(7, 0, vorbisID(0)), oggPage(7, 1000, []byte("x")), size); !errors.Is(err, ErrAudioDurationUnknown) {
		t.Fatalf("sample rate 0: %v", err)
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
	f.Add(wavHeader(32000), []byte(nil), int64(1000))
	f.Add(wavHeader(0xFFFFFFFF), []byte(nil), int64(1)<<31)
	f.Add(flacHead(44100, 44100*60), []byte(nil), int64(1000))
	f.Add(oggPage(1, 0, vorbisID(44100)), oggPage(1, 44100*60, []byte("x")), int64(1)<<30)
	f.Add(mp3Frames(cbr128, 4), mp3Frames(cbr128, 4), int64(1)<<30)
	f.Add(xingFrame(1000), []byte(nil), int64(1)<<20)
	f.Add([]byte{'I', 'D', '3', 4, 0, 0, 0, 0, 0, 2, 0xFF, 0xFB, 0x90, 0x64}, []byte(nil), int64(14))
	f.Fuzz(func(t *testing.T, head, tail []byte, size int64) {
		got, err := DetectAudio(head, tail, size)
		if err != nil {
			return
		}
		if !got.Format.Valid() || got.Duration < 0 || got.Duration > MaxImportedDuration ||
			(got.Duration == 0) != got.NeedsFrameCount() || got.NeedsFrameCount() != (got.Format == FormatMP3) {
			t.Fatalf("DetectAudio = %+v", got)
		}
	})
}
