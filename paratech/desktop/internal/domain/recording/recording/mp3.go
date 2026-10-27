package recording

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"time"
)

// mp3Header is an MPEG audio frame header (ISO 11172-3, 13818-3 and MPEG-2.5).
type mp3Header struct {
	version   byte   // as in the header: 3 MPEG-1, 2 MPEG-2, 0 MPEG-2.5
	layer     byte   // as in the header: 3 layer I, 2 layer II, 1 layer III
	rateIndex byte   // index of the sample rate
	bitrate   uint32 // bits a second; 0 in free format
	rate      uint32 // samples a second
	samples   uint32 // samples a frame
	length    int    // bytes of the frame with its header; 0 in free format
	crc       bool   // a 16-bit CRC follows the header
	mono      bool
}

// mp3Bitrates in kbit/s by table (MPEG-1 layer I, II, III; MPEG-2/2.5 layer I; MPEG-2/2.5 layer II and III) and index.
var mp3Bitrates = [5][15]uint16{
	{0, 32, 64, 96, 128, 160, 192, 224, 256, 288, 320, 352, 384, 416, 448},
	{0, 32, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 384},
	{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320},
	{0, 32, 48, 56, 64, 80, 96, 112, 128, 144, 160, 176, 192, 224, 256},
	{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160},
}

// mp3Rates by version and index; version 1 is reserved.
var mp3Rates = [4][3]uint32{0: {11025, 12000, 8000}, 2: {22050, 24000, 16000}, 3: {44100, 48000, 32000}}

// parseMP3Header reads the frame header at the start of b.
func parseMP3Header(b []byte) (mp3Header, bool) {
	if !mpegFrame(b) {
		return mp3Header{}, false
	}
	h := mp3Header{version: b[1] >> 3 & 3, layer: b[1] >> 1 & 3, rateIndex: b[2] >> 2 & 3, crc: b[1]&1 == 0, mono: b[3]>>6 == 3}
	table := 4
	switch {
	case h.version == 3:
		table = int(3 - h.layer)
	case h.layer == 3:
		table = 3
	}
	h.bitrate = uint32(mp3Bitrates[table][b[2]>>4]) * 1000
	h.rate = mp3Rates[h.version][h.rateIndex]
	pad := int(b[2] >> 1 & 1)

	switch {
	case h.layer == 3:
		h.samples = 384
		if h.bitrate > 0 {
			h.length = (int(12*h.bitrate/h.rate) + pad) * 4
		}
		return h, true
	case h.layer == 1 && h.version != 3:
		h.samples = 576
	default:
		h.samples = 1152
	}
	if h.bitrate > 0 {
		h.length = int(h.samples/8*h.bitrate/h.rate) + pad
	}
	return h, true
}

// sameStream reports frames of one stream: the same version, layer and sample rate.
func (h mp3Header) sameStream(o mp3Header) bool {
	return h.version == o.version && h.layer == o.layer && h.rateIndex == o.rateIndex
}

// mp3Audio is the MP3 whose first frame is at start of head: its frames are counted during the copy, and it is
// refused now when its Xing/Info or VBRI header says it is too long. No guess from the size and a bitrate: a VBR file
// without such a header may start and end at the lowest bitrate, and the count during the copy is the cap anyway.
func mp3Audio(head []byte, start int) (ImportedAudio, error) {
	if d, ok := mp3Estimate(head[start:]); ok && d > MaxImportedDuration {
		return ImportedAudio{}, fmt.Errorf("%w: about %v, at most %v", ErrAudioTooLong, d.Round(time.Second), MaxImportedDuration)
	}
	return ImportedAudio{Format: FormatMP3, counting: true, countFrom: int64(start)}, nil
}

// mp3Estimate is the duration of an MP3 whose first frame starts b from the frame count of its Xing/Info or VBRI
// header; not ok without one.
func mp3Estimate(b []byte) (time.Duration, bool) {
	h, ok := parseMP3Header(b)
	if !ok {
		return 0, false
	}
	frames, ok := vbrFrames(b, h)
	if !ok {
		return 0, false
	}
	return ratio(uint64(frames)*uint64(h.samples), uint64(h.rate)), true
}

// vbrFrames reads the frame count of the Xing/Info header (after the side information) or of the VBRI header (32
// bytes after the frame header) of the first layer III frame b; not ok without one or with a zero count.
func vbrFrames(b []byte, h mp3Header) (uint32, bool) {
	if h.layer != 1 {
		return 0, false
	}
	side := 17
	switch {
	case h.version == 3 && !h.mono:
		side = 32
	case h.version != 3 && h.mono:
		side = 9
	}
	off := 4 + side
	if h.crc {
		off += 2
	}
	if off+12 <= len(b) && (string(b[off:off+4]) == "Xing" || string(b[off:off+4]) == "Info") {
		if binary.BigEndian.Uint32(b[off+4:off+8])&1 == 0 {
			return 0, false
		}
		f := binary.BigEndian.Uint32(b[off+8 : off+12])
		return f, f > 0
	}
	const vbri = 4 + 32
	if vbri+18 <= len(b) && string(b[vbri:vbri+4]) == "VBRI" {
		f := binary.BigEndian.Uint32(b[vbri+14 : vbri+18])
		return f, f > 0
	}
	return 0, false
}

// MP3FrameCounter counts the frames of an MP3 while its bytes are copied (it is the io.Writer of the copy): the sum of
// their samples is the duration, also of a VBR file without a Xing or VBRI header. It jumps from frame to frame and
// never decodes; bytes that are not a frame (tags, junk) are skipped. The stream is the version, layer and sample rate
// of the first frame that is followed right away by another one of the same; until then a frame is only a candidate,
// and a false sync in junk costs at most the frames it covers. Later frames of another stream are taken for junk. It
// holds no more than one header.
type MP3FrameCounter struct {
	skip      int64 // bytes to pass before the next header: the ID3 tag, the rest of a frame
	hdr       [4]byte
	n         int  // bytes in hdr
	expect    bool // the next header must start right here: it confirms the candidate
	lock      mp3Header
	locked    bool   // lock is the stream, or the candidate when not confirmed
	confirmed bool   // two frames of lock in a row were seen
	pending   uint64 // samples of the candidate frame
	samples   uint64
}

// NewMP3FrameCounter counts the frames of a; give it every byte of the file from the first one.
func NewMP3FrameCounter(a ImportedAudio) *MP3FrameCounter {
	return &MP3FrameCounter{skip: max(a.countFrom, 0)}
}

// Write counts the frames in p; it never fails.
func (c *MP3FrameCounter) Write(p []byte) (int, error) {
	total := len(p)
	for len(p) > 0 {
		if c.skip > 0 {
			k := int(min(c.skip, int64(len(p))))
			p, c.skip = p[k:], c.skip-int64(k)
			continue
		}
		if c.n == 0 && !c.expect {
			i := bytes.IndexByte(p, 0xFF)
			if i < 0 {
				break
			}
			p = p[i:]
		}
		k := copy(c.hdr[c.n:], p)
		c.n, p = c.n+k, p[k:]
		if c.n == len(c.hdr) {
			c.header()
		}
	}
	return total, nil
}

// header takes the four bytes in hdr: a frame of the stream is counted and its body skipped; anything else is junk,
// passed up to the next 0xFF.
func (c *MP3FrameCounter) header() {
	h, ok := parseMP3Header(c.hdr[:])
	ok = ok && h.length >= 4
	if c.locked && !c.confirmed && (!ok || !h.sameStream(c.lock)) {
		c.locked, c.pending = false, 0 // the candidate was junk: start over from this header
	}
	switch {
	case ok && !c.locked:
		c.lock, c.locked, c.pending = h, true, uint64(h.samples)
	case ok && h.sameStream(c.lock) && c.confirmed:
		c.samples += uint64(h.samples)
	case ok && h.sameStream(c.lock):
		c.confirmed, c.samples = true, c.pending+uint64(h.samples)
	default:
		c.expect = false
		i := bytes.IndexByte(c.hdr[1:], 0xFF)
		if i < 0 {
			c.n = 0
			return
		}
		c.n = copy(c.hdr[:], c.hdr[1+i:])
		return
	}
	c.skip, c.n, c.expect = int64(h.length-4), 0, !c.confirmed
}

// Counted returns a with the duration of the frames c counted, when a NeedsFrameCount; otherwise a as it is. No frame
// is ErrAudioDurationUnknown, longer than MaxImportedDuration ErrAudioTooLong.
func (a ImportedAudio) Counted(c *MP3FrameCounter) (ImportedAudio, error) {
	if !a.counting {
		return a, nil
	}
	if c == nil || !c.confirmed {
		return knownAudio(a.Format, 0, false)
	}
	return knownAudio(a.Format, ratio(c.samples, uint64(c.lock.rate)), true)
}
