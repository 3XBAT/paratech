package imagepage

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"

	"paratech/desktop/internal/domain/library/material"
)

const (
	// jpegQuality of the copy: close to a phone photo by eye, far smaller than a lossless copy.
	jpegQuality = 90

	// maxDecodedBytes bounds the memory the decoder of one photo may take, as estimated from its header: 8 bytes for
	// each of material.MaxImagePixels.
	maxDecodedBytes = 8 * int64(material.MaxImagePixels)

	// deflateMaxRatio is the most deflate can shrink data: a PNG smaller than its pixels over it cannot hold them.
	deflateMaxRatio = 1032

	// jpegPixelsPerByte: every 8×8 block of luma needs one bit at least (its DC code), so a JPEG holds at most 512
	// pixels per byte; twice that for safety. A smaller JPEG cannot hold its pixels.
	jpegPixelsPerByte = 1024
)

// Cleaner decodes and encodes photos again. Create it with New; it is safe for concurrent use.
type Cleaner struct {
	slot chan struct{} // one photo at a time, so two never hold their pixels at once
}

// New returns a Cleaner.
func New() *Cleaner { return &Cleaner{slot: make(chan struct{}, 1)} }

// header is what is read of a photo before it is decoded.
type header struct {
	typ           material.Type
	width, height int
	model         color.Model
	orientation   int  // EXIF orientation of a JPEG, 1 (as stored) when none or unknown
	progressive   bool // a progressive JPEG keeps the coefficients of every block while it decodes
	components    int  // of a JPEG
	interlaced    bool // an interlaced PNG decodes pass by pass into images of their own
	rawBytes      int64
}

// Clean returns the type of a JPEG or PNG photo and its bytes encoded again by the standard library, without
// metadata. Errors: material.ErrImageTooLarge (over MaxImageBytes, MaxImagePixels or the decoder's memory bound, also
// for the encoded copy), material.ErrUnsupportedImage (not a JPEG or PNG), material.ErrImageUnreadable (truncated,
// damaged, or too small for its pixels), or the context's error while another photo is decoded.
func (c *Cleaner) Clean(ctx context.Context, data []byte) (material.Type, []byte, error) {
	typ, err := material.DetectType(data)
	switch {
	case errors.Is(err, material.ErrTooLarge), errors.Is(err, material.ErrImageTooLarge):
		return "", nil, material.ErrImageTooLarge
	case err != nil || !typ.Image():
		return "", nil, material.ErrUnsupportedImage
	}
	h, err := readHeader(typ, data)
	if err != nil {
		return "", nil, err
	}
	if err := h.check(len(data)); err != nil {
		return "", nil, err
	}

	select {
	case c.slot <- struct{}{}:
	case <-ctx.Done():
		return "", nil, ctx.Err()
	}
	defer func() { <-c.slot }()

	img, err := decode(typ, data)
	if err != nil {
		return "", nil, fmt.Errorf("%w: %v", material.ErrImageUnreadable, err)
	}
	if b := img.Bounds(); b.Dx() != h.width || b.Dy() != h.height {
		return "", nil, fmt.Errorf("%w: decoded %dx%d, header %dx%d", material.ErrImageUnreadable, b.Dx(), b.Dy(), h.width, h.height)
	}
	if h.orientation > 1 {
		img = orient(img, h.orientation)
	}

	var buf bytes.Buffer
	if typ == material.TypeJPEG {
		err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: jpegQuality})
	} else {
		err = (&png.Encoder{CompressionLevel: png.DefaultCompression}).Encode(&buf, img)
	}
	switch {
	case err != nil:
		return "", nil, fmt.Errorf("%w: encode: %v", material.ErrImageUnreadable, err)
	case buf.Len() > material.MaxImageBytes:
		return "", nil, material.ErrImageTooLarge
	}
	return typ, buf.Bytes(), nil
}

func decode(typ material.Type, data []byte) (image.Image, error) {
	if typ == material.TypeJPEG {
		return jpeg.Decode(bytes.NewReader(data))
	}
	return png.Decode(bytes.NewReader(data))
}

// readHeader reads the dimensions and the colour model (image/jpeg and image/png DecodeConfig) and what else bounds
// the decoder's memory; nothing is decoded.
func readHeader(typ material.Type, data []byte) (header, error) {
	h := header{typ: typ, orientation: 1}
	var (
		cfg image.Config
		err error
	)
	if typ == material.TypeJPEG {
		cfg, err = jpeg.DecodeConfig(bytes.NewReader(data))
	} else {
		cfg, err = png.DecodeConfig(bytes.NewReader(data))
	}
	if err != nil {
		return header{}, fmt.Errorf("%w: header: %v", material.ErrImageUnreadable, err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return header{}, fmt.Errorf("%w: %dx%d", material.ErrImageUnreadable, cfg.Width, cfg.Height)
	}
	h.width, h.height, h.model = cfg.Width, cfg.Height, cfg.ColorModel
	if typ == material.TypeJPEG {
		scanJPEG(data, &h)
	} else {
		readIHDR(data, &h)
	}
	return h, nil
}

// check refuses a photo whose pixels or decoder memory are over the bounds, or whose file is too small to hold them.
func (h header) check(size int) error {
	pixels := int64(h.width) * int64(h.height)
	if pixels > material.MaxImagePixels {
		return fmt.Errorf("%w: %dx%d", material.ErrImageTooLarge, h.width, h.height)
	}

	need := pixels * pixelBytes(h.model)
	if h.progressive {
		need += pixels * int64(h.components) * 4 // an int32 per sample until the last scan
	}
	if h.interlaced {
		need *= 2
	}
	if h.orientation > 1 {
		need += pixels * 4 // the turned copy
	}
	if need > maxDecodedBytes {
		return fmt.Errorf("%w: %d bytes to decode", material.ErrImageTooLarge, need)
	}

	var least int64
	if h.typ == material.TypePNG {
		least = h.rawBytes / deflateMaxRatio
	} else {
		least = pixels / jpegPixelsPerByte
	}
	if int64(size) < least {
		return fmt.Errorf("%w: %d bytes cannot hold %dx%d", material.ErrImageUnreadable, size, h.width, h.height)
	}
	return nil
}

// pixelBytes is the most bytes per pixel the decoder keeps for a colour model.
func pixelBytes(m color.Model) int64 {
	switch m {
	case color.GrayModel:
		return 1
	case color.Gray16Model:
		return 2
	case color.YCbCrModel:
		return 3
	case color.RGBAModel, color.NRGBAModel, color.CMYKModel:
		return 4
	}
	if _, ok := m.(color.Palette); ok {
		return 1
	}
	return 8
}

// PNG layout: the signature, then the IHDR chunk (length, type, 13 bytes of data).
const (
	ihdrData   = 16
	ihdrLength = 13
)

// readIHDR reads the bit depth, colour type and interlace method of a PNG whose header DecodeConfig accepted, and the
// size of its unpacked rows (with their filter bytes).
func readIHDR(data []byte, h *header) {
	if len(data) < ihdrData+ihdrLength {
		return
	}
	ihdr := data[ihdrData : ihdrData+ihdrLength]
	depth := int64(ihdr[8])
	var channels int64
	switch ihdr[9] { // colour type
	case 0, 3: // grey, palette
		channels = 1
	case 4: // grey with alpha
		channels = 2
	case 2: // RGB
		channels = 3
	case 6: // RGBA
		channels = 4
	}
	h.interlaced = ihdr[12] == 1
	rowBytes := (int64(h.width)*channels*depth+7)/8 + 1
	h.rawBytes = rowBytes * int64(h.height)
}

// JPEG markers read by scanJPEG.
const (
	markerSOF0 = 0xc0
	markerSOF2 = 0xc2 // progressive, the only other frame image/jpeg decodes besides SOF0 and SOF1
	markerSOI  = 0xd8
	markerEOI  = 0xd9
	markerSOS  = 0xda
	markerAPP1 = 0xe1

	exifHeader     = "Exif\x00\x00"
	tagOrientation = 0x0112
	typeShort      = 3
)

// scanJPEG walks the segments before the first scan: the frame tells progressive and the components, the first
// EXIF segment the orientation. A damaged layout stops the walk; the decoder judges the file.
func scanJPEG(data []byte, h *header) {
	h.components = 3
	exif := false
	for i := 2; i+4 <= len(data); {
		if data[i] != 0xff {
			return
		}
		marker := data[i+1]
		switch {
		case marker == 0xff: // fill byte
			i++
			continue
		case marker == markerSOI || marker >= 0xd0 && marker <= 0xd7 || marker == 0x01:
			i += 2
			continue
		case marker == markerEOI || marker == markerSOS:
			return
		}
		n := int(binary.BigEndian.Uint16(data[i+2:]))
		if n < 2 || i+2+n > len(data) {
			return
		}
		seg := data[i+4 : i+2+n]
		switch {
		case marker == markerAPP1 && !exif && bytes.HasPrefix(seg, []byte(exifHeader)):
			exif = true
			h.orientation = exifOrientation(seg[len(exifHeader):])
		case marker >= markerSOF0 && marker <= markerSOF2:
			h.progressive = marker == markerSOF2
			if len(seg) >= 6 {
				h.components = int(seg[5])
			}
		}
		i += 2 + n
	}
}

// exifOrientation reads the Orientation tag of IFD0 of an EXIF TIFF block: 1 to 8, or 1 for none or anything odd.
func exifOrientation(tiff []byte) int {
	if len(tiff) < 8 {
		return 1
	}
	var bo binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 1
	}
	if bo.Uint16(tiff[2:]) != 42 {
		return 1
	}
	off := int64(bo.Uint32(tiff[4:]))
	size := int64(len(tiff))
	if off < 8 || off+2 > size {
		return 1
	}
	n := int64(bo.Uint16(tiff[off:]))
	for k := range n {
		e := off + 2 + k*12
		if e+12 > size {
			return 1
		}
		if bo.Uint16(tiff[e:]) != tagOrientation {
			continue
		}
		if bo.Uint16(tiff[e+2:]) != typeShort || bo.Uint32(tiff[e+4:]) != 1 {
			return 1
		}
		if v := int(bo.Uint16(tiff[e+8:])); v >= 1 && v <= 8 {
			return v
		}
		return 1
	}
	return 1
}

// orient turns and mirrors img as EXIF orientation o (2 to 8) says, into a new RGBA image the right side up.
func orient(img image.Image, o int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	dw, dh := w, h
	if o >= 5 {
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := range dh {
		for x := range dw {
			sx, sy := source(o, x, y, w, h)
			dst.Set(x, y, img.At(b.Min.X+sx, b.Min.Y+sy))
		}
	}
	return dst
}

// source is the pixel of a w×h image stored with orientation o that is at (x, y) of the turned image.
func source(o, x, y, w, h int) (int, int) {
	switch o {
	case 2: // mirrored
		return w - 1 - x, y
	case 3: // upside down
		return w - 1 - x, h - 1 - y
	case 4: // mirrored upside down
		return x, h - 1 - y
	case 5: // transposed
		return y, x
	case 6: // turned 90° clockwise to show
		return y, h - 1 - x
	case 7: // transversed
		return w - 1 - y, h - 1 - x
	case 8: // turned 90° counter-clockwise to show
		return w - 1 - y, x
	}
	return x, y
}
