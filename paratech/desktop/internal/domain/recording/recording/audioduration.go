package recording

import (
	"bytes"
	"encoding/binary"
	"math"
	"time"
)

// flacDuration reads the STREAMINFO block that follows "fLaC" at the start of b: its total samples over its sample
// rate. Not ok when the block is missing or cut, or says 0 for either (a streamed FLAC does not know its length).
func flacDuration(b []byte) (time.Duration, bool) {
	const info = 8 // "fLaC" and the 4-byte block header
	if len(b) < info+18 || b[4]&0x7F != 0 || int(b[5])<<16|int(b[6])<<8|int(b[7]) != 34 {
		return 0, false
	}
	s := b[info:]
	rate := uint64(s[10])<<12 | uint64(s[11])<<4 | uint64(s[12])>>4
	total := uint64(s[13]&0x0F)<<32 | uint64(binary.BigEndian.Uint32(s[14:18]))
	if rate == 0 || total == 0 {
		return 0, false
	}
	return ratio(total, rate), true
}

var oggMagic = []byte("OggS")

// oggDuration is the granule position (samples) of the last page of the Vorbis stream in tail over the sample rate of
// its identification header in head. Not ok without a rate, without a whole page of the stream in the tail that ends
// a packet, or with a granule that is not positive.
func oggDuration(head, tail []byte) (time.Duration, bool) {
	if len(head) < 27 {
		return 0, false
	}
	body := 27 + int(head[26])
	if body+16 > len(head) {
		return 0, false
	}
	rate := uint64(binary.LittleEndian.Uint32(head[body+12 : body+16]))
	g, ok := lastGranule(tail, binary.LittleEndian.Uint32(head[14:18]))
	if !ok || rate == 0 || g == 0 || g > math.MaxInt64 { // the granule is signed: above MaxInt64 is negative
		return 0, false
	}
	return ratio(g, rate), true
}

// lastGranule finds the last whole page of the stream serial in b whose granule position is set (-1: no packet ends
// on the page). Pages of other streams (a chained file) and an "OggS" inside the audio of a cut page are skipped.
func lastGranule(b []byte, serial uint32) (uint64, bool) {
	for off := bytes.LastIndex(b, oggMagic); off >= 0; off = bytes.LastIndex(b[:off], oggMagic) {
		p := b[off:]
		if len(p) < 27 || p[4] != 0 || binary.LittleEndian.Uint32(p[14:18]) != serial {
			continue
		}
		n := 27 + int(p[26])
		if n > len(p) {
			continue
		}
		end := n
		for _, l := range p[27:n] {
			end += int(l)
		}
		g := binary.LittleEndian.Uint64(p[6:14])
		if end > len(p) || g == math.MaxUint64 { // -1

			continue
		}
		return g, true
	}
	return 0, false
}

// ratio is num/den as a duration (num units, den units a second), saturated at the largest duration; den > 0 and
// below 2^34, so the remainder in nanoseconds does not overflow.
func ratio(num, den uint64) time.Duration {
	const maxSeconds = uint64(math.MaxInt64 / int64(time.Second))
	sec := num / den
	if sec >= maxSeconds {
		return math.MaxInt64
	}
	frac := num % den * uint64(time.Second) / den
	return time.Duration(sec)*time.Second + time.Duration(frac) //nolint:gosec // frac < 1e9: the remainder is below den
}
