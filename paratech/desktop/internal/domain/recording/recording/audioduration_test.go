package recording

import (
	"encoding/binary"
	"math"
	"testing"
	"time"
)

// flacHead is "fLaC" and a synthetic STREAMINFO block: mono 16-bit at rate with total samples.
func flacHead(rate uint32, total uint64) []byte {
	s := make([]byte, 34)
	s[10], s[11] = byte(rate>>12), byte(rate>>4)
	s[12] = byte(rate << 4)             // channels-1 = 0, top bit of bits-1 (15) = 0
	s[13] = 0xF0 | byte(total>>32)&0x0F // the low bits of bits-1, the top of the total
	binary.BigEndian.PutUint32(s[14:18], uint32(total))
	return append([]byte("fLaC\x80\x00\x00\x22"), s...) // the last block, STREAMINFO, 34 bytes
}

// oggPage is a synthetic Ogg page of one packet (shorter than 255 bytes); the CRC is not set, as it is not checked.
func oggPage(serial uint32, granule int64, packet []byte) []byte {
	b := append([]byte("OggS"), 0, 0)
	b = binary.LittleEndian.AppendUint64(b, uint64(granule))
	b = binary.LittleEndian.AppendUint32(b, serial)
	b = binary.LittleEndian.AppendUint32(b, 0) // sequence
	b = binary.LittleEndian.AppendUint32(b, 0) // CRC
	b = append(b, 1, byte(len(packet)))
	return append(b, packet...)
}

// vorbisID is a Vorbis identification header of a mono stream at rate.
func vorbisID(rate uint32) []byte {
	p := binary.LittleEndian.AppendUint32([]byte("\x01vorbis"), 0)
	p = append(p, 1)
	p = binary.LittleEndian.AppendUint32(p, rate)
	return append(p, make([]byte, 14)...)
}

// oggFile is a synthetic Ogg Vorbis file: the identification page and one audio page that ends at granule.
func oggFile(rate uint32, granule int64) []byte {
	return append(oggPage(7, 0, vorbisID(rate)), oggPage(7, granule, []byte("audio"))...)
}

func TestFLACDuration(t *testing.T) {
	tests := []struct {
		name string
		b    []byte
		want time.Duration
		ok   bool
	}{
		{"one hour", flacHead(44100, 44100*3600), time.Hour, true},
		{"the largest rate and count", flacHead(1<<20-1, 1<<36-1), ratio(1<<36-1, 1<<20-1), true},
		{"rate 0", flacHead(0, 1000), 0, false},
		{"total 0", flacHead(44100, 0), 0, false},
		{"cut", flacHead(44100, 44100)[:20], 0, false},
		{"another first block", append([]byte("fLaC\x84\x00\x00\x22"), make([]byte, 34)...), 0, false},
		{"a wrong block size", append([]byte("fLaC\x00\x00\x00\x10"), flacHead(44100, 44100)[8:]...), 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if d, ok := flacDuration(tc.b); d != tc.want || ok != tc.ok {
				t.Fatalf("flacDuration = %v, %v; want %v, %v", d, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestRatio(t *testing.T) {
	tests := []struct {
		num, den uint64
		want     time.Duration
	}{
		{1, 2, 500 * time.Millisecond},
		{44100 * 90, 44100, 90 * time.Second},
		{math.MaxUint64, 1, math.MaxInt64},
		{math.MaxUint64, math.MaxUint32, (1<<32 + 1) * time.Second}, // (2^32-1)(2^32+1)
		{math.MaxUint32 - 1, math.MaxUint32, time.Second - 1},
	}
	for _, tc := range tests {
		if got := ratio(tc.num, tc.den); got != tc.want {
			t.Errorf("ratio(%d, %d) = %v, want %v", tc.num, tc.den, got, tc.want)
		}
	}
}

func FuzzFLACDuration(f *testing.F) {
	f.Add(flacHead(44100, 44100*60))
	f.Add(flacHead(0, 0))
	f.Add([]byte("fLaC"))
	f.Fuzz(func(t *testing.T, b []byte) {
		if d, ok := flacDuration(b); ok && d <= 0 {
			t.Fatalf("flacDuration = %v, ok", d)
		}
	})
}

func FuzzOggDuration(f *testing.F) {
	f.Add(oggPage(7, 0, vorbisID(44100)), oggPage(7, 44100*60, []byte("x")))
	f.Add(oggPage(7, 0, vorbisID(0)), oggPage(7, -1, []byte("x")))
	f.Add(oggFile(8000, 1<<62), []byte("OggSOggSOggS"))
	f.Fuzz(func(t *testing.T, head, tail []byte) {
		if d, ok := oggDuration(head, tail); ok && d <= 0 {
			t.Fatalf("oggDuration = %v, ok", d)
		}
	})
}
