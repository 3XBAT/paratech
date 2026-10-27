package imagepage

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"

	"paratech/desktop/internal/domain/library/material"
)

// Obviously fake values that must not survive the cleaning.
const (
	fakeGPS     = "FAKE-GPS-00.0000N-00.0000E"
	fakeComment = "FAKE-COMMENT-not-kept"
)

// halves is a w×h image: the left half red, the right half blue.
func halves(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			c := color.RGBA{R: 255, A: 255}
			if x >= w/2 {
				c = color.RGBA{B: 255, A: 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func jpegOf(t testing.TB, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func pngOf(t testing.TB, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// exifSegment is an APP1 EXIF segment (little endian) with IFD0 holding the orientation, an ImageDescription with
// the fake GPS text and a GPSInfo pointer to a GPS IFD with the latitude reference.
func exifSegment(orientation uint16) []byte {
	le := binary.LittleEndian
	var tiff []byte
	tiff = append(tiff, 'I', 'I')
	tiff = le.AppendUint16(tiff, 42)
	tiff = le.AppendUint32(tiff, 8)

	const entries = 3
	ifd0End := 8 + 2 + entries*12 + 4
	descOff := ifd0End
	gpsOff := descOff + len(fakeGPS) + 1
	entry := func(tag, typ uint16, count, value uint32) {
		tiff = le.AppendUint16(tiff, tag)
		tiff = le.AppendUint16(tiff, typ)
		tiff = le.AppendUint32(tiff, count)
		tiff = le.AppendUint32(tiff, value)
	}
	tiff = le.AppendUint16(tiff, entries)
	entry(0x010e, 2, uint32(len(fakeGPS)+1), uint32(descOff)) // ImageDescription
	entry(tagOrientation, typeShort, 1, uint32(orientation))
	entry(0x8825, 4, 1, uint32(gpsOff)) // GPSInfo
	tiff = le.AppendUint32(tiff, 0)
	tiff = append(tiff, fakeGPS...)
	tiff = append(tiff, 0)
	tiff = le.AppendUint16(tiff, 1)
	entry(0x0001, 2, 2, uint32('N')) // GPSLatitudeRef
	tiff = le.AppendUint32(tiff, 0)

	payload := append([]byte(exifHeader), tiff...)
	seg := []byte{0xff, markerAPP1}
	seg = binary.BigEndian.AppendUint16(seg, uint16(len(payload)+2))
	return append(seg, payload...)
}

// withSegments puts segments right after the SOI of a JPEG.
func withSegments(data []byte, segs ...[]byte) []byte {
	out := append([]byte{}, data[:2]...)
	for _, s := range segs {
		out = append(out, s...)
	}
	return append(out, data[2:]...)
}

// commentSegment is a COM segment.
func commentSegment(text string) []byte {
	seg := []byte{0xff, 0xfe}
	seg = binary.BigEndian.AppendUint16(seg, uint16(len(text)+2))
	return append(seg, text...)
}

// chunk is a PNG chunk with its CRC.
func chunk(typ string, data []byte) []byte {
	out := binary.BigEndian.AppendUint32(nil, uint32(len(data)))
	out = append(out, typ...)
	out = append(out, data...)
	return binary.BigEndian.AppendUint32(out, crc32.ChecksumIEEE(append([]byte(typ), data...)))
}

// withChunks puts chunks right before the IEND of a PNG.
func withChunks(data []byte, chunks ...[]byte) []byte {
	iend := len(data) - 12
	out := append([]byte{}, data[:iend]...)
	for _, c := range chunks {
		out = append(out, c...)
	}
	return append(out, data[iend:]...)
}

// pngHeader is a PNG of w×h with the depth and colour type and an IDAT of rows of zeros, compressed, of the given
// count; with rows < h it is a decompression bomb or a truncated file.
func pngHeader(t testing.TB, w, h uint32, depth, colorType, interlace byte, idat []byte) []byte {
	t.Helper()
	ihdr := binary.BigEndian.AppendUint32(nil, w)
	ihdr = binary.BigEndian.AppendUint32(ihdr, h)
	ihdr = append(ihdr, depth, colorType, 0, 0, interlace)
	out := []byte(pngMagic)
	out = append(out, chunk("IHDR", ihdr)...)
	out = append(out, chunk("IDAT", idat)...)
	return append(out, chunk("IEND", nil)...)
}

const pngMagic = "\x89PNG\r\n\x1a\n"

func zlibZeros(t testing.TB, n int) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	if _, err := zw.Write(make([]byte, n)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// withJPEGSize writes w×h into the frame header of a JPEG.
func withJPEGSize(t *testing.T, data []byte, w, h uint16) []byte {
	t.Helper()
	out := append([]byte{}, data...)
	i := bytes.Index(out, []byte{0xff, markerSOF0})
	if i < 0 {
		t.Fatal("no SOF0")
	}
	binary.BigEndian.PutUint16(out[i+5:], h)
	binary.BigEndian.PutUint16(out[i+7:], w)
	return out
}

func clean(t *testing.T, data []byte) (material.Type, []byte, error) {
	t.Helper()
	return New().Clean(context.Background(), data)
}

func TestCleanKeepsTypeAndPixels(t *testing.T) {
	gray := image.NewGray(image.Rect(0, 0, 20, 10))
	paletted := image.NewPaletted(image.Rect(0, 0, 8, 8), color.Palette{color.Black, color.White})
	nrgba64 := image.NewNRGBA64(image.Rect(0, 0, 9, 7))
	tests := []struct {
		name string
		data []byte
		want material.Type
		w, h int
	}{
		{"jpeg", jpegOf(t, halves(64, 32)), material.TypeJPEG, 64, 32},
		{"gray jpeg", jpegOf(t, gray), material.TypeJPEG, 20, 10},
		{"png", pngOf(t, halves(30, 20)), material.TypePNG, 30, 20},
		{"paletted png", pngOf(t, paletted), material.TypePNG, 8, 8},
		{"16-bit png", pngOf(t, nrgba64), material.TypePNG, 9, 7},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			typ, out, err := clean(t, tt.data)
			if err != nil || typ != tt.want {
				t.Fatalf("Clean = %q, %v", typ, err)
			}
			got, err := decode(typ, out)
			if err != nil {
				t.Fatalf("the clean bytes do not decode: %v", err)
			}
			if b := got.Bounds(); b.Dx() != tt.w || b.Dy() != tt.h {
				t.Fatalf("clean image %v, want %dx%d", b, tt.w, tt.h)
			}
			if again, ok := material.DetectType(out); ok != nil || again != tt.want {
				t.Fatalf("DetectType(clean) = %q, %v", again, ok)
			}
		})
	}
}

// The same photo gives the same clean bytes, so the library keeps it once (dedupe on the clean digest).
func TestCleanIsDeterministic(t *testing.T) {
	data := withSegments(jpegOf(t, halves(40, 40)), exifSegment(1))
	_, a, err := clean(t, data)
	if err != nil {
		t.Fatal(err)
	}
	_, b, err := clean(t, data)
	if err != nil || !bytes.Equal(a, b) {
		t.Fatalf("second Clean differs: %v", err)
	}
}

// EXIF (GPS among it), comments, ICC and text chunks are not written again.
func TestCleanDropsMetadata(t *testing.T) {
	jpg := withSegments(jpegOf(t, halves(32, 32)), exifSegment(1), commentSegment(fakeComment))
	if !bytes.Contains(jpg, []byte(fakeGPS)) || !bytes.Contains(jpg, []byte("Exif")) {
		t.Fatal("test bug: the fixture holds no EXIF")
	}
	icc := zlibZeros(t, 64)
	pg := withChunks(pngOf(t, halves(16, 16)),
		chunk("tEXt", append([]byte("Comment\x00"), fakeComment...)),
		chunk("iTXt", append([]byte("GPS\x00\x00\x00\x00\x00"), fakeGPS...)),
		chunk("iCCP", append([]byte("fake icc\x00\x00"), icc...)),
		chunk("eXIf", exifSegment(1)[4+len(exifHeader):]))
	for name, data := range map[string][]byte{"jpeg": jpg, "png": pg} {
		_, out, err := clean(t, data)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, leak := range []string{fakeGPS, fakeComment, "Exif", "eXIf", "tEXt", "iTXt", "iCCP", "fake icc"} {
			if bytes.Contains(out, []byte(leak)) {
				t.Errorf("%s: clean bytes still hold %q", name, leak)
			}
		}
	}
}

// The EXIF orientation is applied to the pixels before it is dropped: the left (red) half of a photo stored turned
// is where the orientation says.
func TestCleanAppliesOrientation(t *testing.T) {
	red := func(c color.Color) bool { r, _, b, _ := c.RGBA(); return r > 0xc000 && b < 0x4000 }
	tests := []struct {
		orientation uint16
		w, h        int
		redAt       image.Point // a pixel well inside the red half after turning
		blueAt      image.Point
	}{
		{1, 64, 32, image.Pt(8, 16), image.Pt(56, 16)},
		{2, 64, 32, image.Pt(56, 16), image.Pt(8, 16)},
		{3, 64, 32, image.Pt(56, 16), image.Pt(8, 16)},
		{4, 64, 32, image.Pt(8, 16), image.Pt(56, 16)},
		{5, 32, 64, image.Pt(16, 8), image.Pt(16, 56)},
		{6, 32, 64, image.Pt(16, 8), image.Pt(16, 56)},
		{7, 32, 64, image.Pt(16, 56), image.Pt(16, 8)},
		{8, 32, 64, image.Pt(16, 56), image.Pt(16, 8)},
		{9, 64, 32, image.Pt(8, 16), image.Pt(56, 16)}, // not an orientation: kept as stored
	}
	src := jpegOf(t, halves(64, 32))
	for _, tt := range tests {
		_, out, err := clean(t, withSegments(src, exifSegment(tt.orientation)))
		if err != nil {
			t.Fatalf("orientation %d: %v", tt.orientation, err)
		}
		img, err := jpeg.Decode(bytes.NewReader(out))
		if err != nil {
			t.Fatal(err)
		}
		if b := img.Bounds(); b.Dx() != tt.w || b.Dy() != tt.h {
			t.Fatalf("orientation %d: %v, want %dx%d", tt.orientation, b, tt.w, tt.h)
		}
		if !red(img.At(tt.redAt.X, tt.redAt.Y)) || red(img.At(tt.blueAt.X, tt.blueAt.Y)) {
			t.Errorf("orientation %d: red not at %v", tt.orientation, tt.redAt)
		}
	}
}

func TestSource(t *testing.T) {
	// A 3×2 image: every orientation maps the corners of the turned image onto corners of the stored one.
	for o := 1; o <= 8; o++ {
		w, h := 3, 2
		dw, dh := w, h
		if o >= 5 {
			dw, dh = h, w
		}
		seen := map[image.Point]bool{}
		for y := range dh {
			for x := range dw {
				sx, sy := source(o, x, y, w, h)
				p := image.Pt(sx, sy)
				if sx < 0 || sx >= w || sy < 0 || sy >= h || seen[p] {
					t.Fatalf("orientation %d: (%d,%d) -> %v", o, x, y, p)
				}
				seen[p] = true
			}
		}
	}
}

func TestCleanRefuses(t *testing.T) {
	smallJPEG := jpegOf(t, halves(16, 16))
	smallPNG := pngOf(t, halves(16, 16))
	var gifBuf bytes.Buffer
	if err := gif.Encode(&gifBuf, image.NewPaletted(image.Rect(0, 0, 4, 4), color.Palette{color.Black}), nil); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		data []byte
		want error
	}{
		{"empty", nil, material.ErrUnsupportedImage},
		{"text", []byte("Лекция 1"), material.ErrUnsupportedImage},
		{"pdf", []byte("%PDF-1.7"), material.ErrUnsupportedImage},
		{"gif", gifBuf.Bytes(), material.ErrUnsupportedImage},
		{"webp", []byte("RIFF\x10\x00\x00\x00WEBPVP8 \x00\x00"), material.ErrUnsupportedImage},
		{"heic", []byte("\x00\x00\x00\x18ftypheic\x00\x00\x00\x00mif1heic"), material.ErrUnsupportedImage},
		{"bmp", []byte("BM\x3a\x00\x00\x00\x00\x00\x00\x00\x36\x00\x00\x00"), material.ErrUnsupportedImage},
		{"svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`), material.ErrUnsupportedImage},
		{"over 25 MiB", append(append([]byte{}, smallJPEG...), make([]byte, material.MaxImageBytes)...), material.ErrImageTooLarge},
		{"jpeg header 60000x60000", withJPEGSize(t, smallJPEG, 60000, 60000), material.ErrImageTooLarge},
		{"jpeg header 8000x5001 (just over 40 Mpx)", withJPEGSize(t, smallJPEG, 8000, 5001), material.ErrImageTooLarge},
		{"jpeg header 6000x6000 in a small file", withJPEGSize(t, smallJPEG, 6000, 6000), material.ErrImageUnreadable},
		{"png bomb 60000x60000", pngHeader(t, 60000, 60000, 8, 6, 0, zlibZeros(t, 1<<16)), material.ErrImageTooLarge},
		{"png bomb 6000x6000 of tiny data", pngHeader(t, 6000, 6000, 8, 6, 0, zlibZeros(t, 1<<16)), material.ErrImageUnreadable},
		{"interlaced 16-bit png over the memory bound", pngHeader(t, 6000, 6000, 16, 6, 1, zlibZeros(t, 1<<16)), material.ErrImageTooLarge},
		{"png of zero width", pngHeader(t, 0, 10, 8, 6, 0, zlibZeros(t, 10)), material.ErrImageUnreadable},
		{"jpeg cut after the header", smallJPEG[:len(smallJPEG)/2], material.ErrImageUnreadable},
		{"jpeg cut in the header", smallJPEG[:40], material.ErrImageUnreadable},
		{"png cut", smallPNG[:len(smallPNG)-20], material.ErrImageUnreadable},
		{"png cut in the header", smallPNG[:20], material.ErrImageUnreadable},
		{"png with a damaged crc", damagedCRC(smallPNG), material.ErrImageUnreadable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			typ, out, err := clean(t, tt.data)
			if !errors.Is(err, tt.want) || typ != "" || out != nil {
				t.Fatalf("Clean = %q, %d bytes, %v; want %v", typ, len(out), err, tt.want)
			}
		})
	}
}

func damagedCRC(data []byte) []byte {
	out := append([]byte{}, data...)
	out[len(pngMagic)+8+13] ^= 0xff // the CRC of IHDR
	return out
}

// A photo waits for the one being decoded; its context ends the wait.
func TestCleanWaitsForTheSlot(t *testing.T) {
	c := New()
	c.slot <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := c.Clean(ctx, jpegOf(t, halves(8, 8))); !errors.Is(err, context.Canceled) {
		t.Fatalf("Clean while busy = %v", err)
	}
	<-c.slot
	if _, _, err := c.Clean(context.Background(), jpegOf(t, halves(8, 8))); err != nil {
		t.Fatalf("Clean when free = %v", err)
	}
}

func TestExifOrientation(t *testing.T) {
	good := exifSegment(6)[4+len(exifHeader):]
	be := []byte("MM\x00\x2a\x00\x00\x00\x08\x00\x01\x01\x12\x00\x03\x00\x00\x00\x01\x00\x08\x00\x00\x00\x00\x00\x00")
	tests := []struct {
		name string
		tiff []byte
		want int
	}{
		{"little endian", good, 6},
		{"big endian", be, 8},
		{"empty", nil, 1},
		{"bad byte order", append([]byte("XX"), good[2:]...), 1},
		{"bad magic", append([]byte("II\x2b\x00"), good[4:]...), 1},
		{"offset outside", []byte("II\x2a\x00\xff\xff\x00\x00"), 1},
		{"entries outside", append(append([]byte{}, good[:10]...), 0x01), 1},
		{"long type", []byte("MM\x00\x2a\x00\x00\x00\x08\x00\x01\x01\x12\x00\x04\x00\x00\x00\x01\x00\x00\x00\x06\x00\x00\x00\x00"), 1},
		{"value 0", []byte("MM\x00\x2a\x00\x00\x00\x08\x00\x01\x01\x12\x00\x03\x00\x00\x00\x01\x00\x00\x00\x00\x00\x00\x00\x00"), 1},
	}
	for _, tt := range tests {
		if got := exifOrientation(tt.tiff); got != tt.want {
			t.Errorf("%s: %d, want %d", tt.name, got, tt.want)
		}
	}
}

// FuzzClean: any input ends in a typed error or in clean bytes that decode as the type, within the limits, without
// EXIF.
func FuzzClean(f *testing.F) {
	f.Add(jpegOf(f, halves(16, 8)))
	f.Add(withSegments(jpegOf(f, halves(8, 16)), exifSegment(6), commentSegment(fakeComment)))
	f.Add(pngOf(f, halves(5, 3)))
	f.Add(pngOf(f, image.NewPaletted(image.Rect(0, 0, 3, 3), color.Palette{color.White})))
	f.Add(pngHeader(f, 60000, 60000, 8, 6, 0, zlibZeros(f, 64)))
	f.Add([]byte("\xff\xd8\xff"))
	f.Add([]byte(pngMagic))
	c := New()
	f.Fuzz(func(t *testing.T, data []byte) {
		typ, out, err := c.Clean(context.Background(), data)
		if err != nil {
			if !errors.Is(err, material.ErrImageTooLarge) && !errors.Is(err, material.ErrUnsupportedImage) &&
				!errors.Is(err, material.ErrImageUnreadable) {
				t.Fatalf("untyped error %v", err)
			}
			return
		}
		if !typ.Image() || len(out) == 0 || len(out) > material.MaxImageBytes {
			t.Fatalf("Clean = %q, %d bytes", typ, len(out))
		}
		var cfg image.Config
		if typ == material.TypeJPEG {
			cfg, err = jpeg.DecodeConfig(bytes.NewReader(out))
		} else {
			cfg, err = png.DecodeConfig(bytes.NewReader(out))
		}
		if err != nil || int64(cfg.Width)*int64(cfg.Height) > material.MaxImagePixels {
			t.Fatalf("clean bytes: %+v, %v", cfg, err)
		}
		if bytes.Contains(out, []byte(exifHeader)) {
			t.Fatal("clean bytes hold EXIF")
		}
	})
}
