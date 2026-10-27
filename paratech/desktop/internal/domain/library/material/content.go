package material

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"paratech/desktop/internal/domain/shared"
)

// MaxBytes bounds the size of one file of the library.
const MaxBytes = 100 << 20

// Limits of a photo page (ADR-0027 «Limits», proposed; Open 10 may change them).
const (
	// MaxImageBytes bounds a photo, as uploaded and as stored after it is encoded again.
	MaxImageBytes = 25 << 20

	// MaxImagePixels bounds width × height of a photo (40 Mpx), read from its header before it is decoded.
	MaxImagePixels = 40_000_000
)

// Errors of photos. They are errors of the content, so they live with DetectType; the imagepage adapter returns them.
var (
	// ErrImageTooLarge means a photo over MaxImageBytes or MaxImagePixels.
	ErrImageTooLarge = errors.New("image too large")

	// ErrUnsupportedImage means an image that is not JPEG or PNG (HEIC, GIF, WebP, BMP, TIFF, SVG); it wraps
	// ErrUnsupportedType.
	ErrUnsupportedImage = fmt.Errorf("%w: only JPEG and PNG images", ErrUnsupportedType)

	// ErrImageUnreadable means a JPEG or PNG that does not decode: truncated or damaged.
	ErrImageUnreadable = errors.New("image cannot be decoded")
)

// Type is the detected type of a file; the allowlist below is all the library takes.
type Type string

// Types of the allowlist.
const (
	TypeDOCX Type = "docx"
	TypePPTX Type = "pptx"
	TypeTXT  Type = "txt"
	TypePDF  Type = "pdf"
	TypeJPEG Type = "jpeg" // a photo page of a note, without text (ADR-0027 «Handwriting and scans»)
	TypePNG  Type = "png"
)

// Types returns the allowlist.
func Types() []Type { return []Type{TypeDOCX, TypePPTX, TypeTXT, TypePDF, TypeJPEG, TypePNG} }

func (t Type) valid() bool { return slices.Contains(Types(), t) }

// Image reports whether the type is a photo: its bytes are stored only as the imagepage adapter encoded them again,
// and it has no text.
func (t Type) Image() bool { return t == TypeJPEG || t == TypePNG }

// Readable reports whether the text of the type is read: not of a PDF (ADR-0024) nor of a photo. A material of
// another type is never searched, inspected or cited.
func (t Type) Readable() bool { return t.valid() && t != TypePDF && !t.Image() }

// Signatures and the part names that tell an Office Open XML package.
const (
	pdfMagic      = "%PDF-"
	zipMagic      = "PK\x03\x04"
	jpegMagic     = "\xff\xd8\xff"
	pngMagic      = "\x89PNG\r\n\x1a\n"
	utf8BOM       = "\xef\xbb\xbf"
	utf16LEBOM    = "\xff\xfe"
	utf16BEBOM    = "\xfe\xff"
	docxMainPart  = "word/document.xml"
	pptxMainPart  = "ppt/presentation.xml"
	maxZipEntries = 100_000

	// svgScan bounds the start of a text looked at for an SVG root element.
	svgScan = 1024
)

// otherImageMagics are signatures of images that are not taken (HEIC and AVIF are told by their ftyp box).
var otherImageMagics = []string{"GIF87a", "GIF89a", "BM", "II*\x00", "MM\x00*"}

// DetectType tells the type of a file from its content, never from its name: PDF, JPEG and PNG by their signature,
// DOCX and PPTX by the main part in the ZIP directory (nothing is decompressed), text by being valid UTF-8, or UTF-16
// with a byte order mark, without NUL (DecodeText). Errors: ErrEmpty, ErrTooLarge, ErrImageTooLarge (a photo over
// MaxImageBytes), ErrUnsupportedImage (an image of another format, an SVG text among them), ErrUnsupportedType.
func DetectType(data []byte) (Type, error) {
	switch {
	case len(data) == 0:
		return "", ErrEmpty
	case len(data) > MaxBytes:
		return "", ErrTooLarge
	case bytes.HasPrefix(data, []byte(pdfMagic)):
		return TypePDF, nil
	case bytes.HasPrefix(data, []byte(zipMagic)):
		return officeType(data)
	case bytes.HasPrefix(data, []byte(jpegMagic)):
		return imageType(TypeJPEG, data)
	case bytes.HasPrefix(data, []byte(pngMagic)):
		return imageType(TypePNG, data)
	}
	if text, err := DecodeText(data); err == nil {
		if svgText(text) {
			return "", ErrUnsupportedImage
		}
		return TypeTXT, nil
	}
	if otherImage(data) {
		return "", ErrUnsupportedImage
	}
	return "", ErrUnsupportedType
}

func imageType(t Type, data []byte) (Type, error) {
	if len(data) > MaxImageBytes {
		return "", ErrImageTooLarge
	}
	return t, nil
}

// otherImage reports whether binary data starts like an image of a format that is not taken.
func otherImage(data []byte) bool {
	for _, m := range otherImageMagics {
		if bytes.HasPrefix(data, []byte(m)) {
			return true
		}
	}
	if len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WEBP" {
		return true
	}
	if len(data) >= 12 && string(data[4:8]) == "ftyp" {
		switch string(data[8:12]) {
		case "heic", "heix", "hevc", "hevx", "heim", "heis", "mif1", "msf1", "avif", "avis":
			return true
		}
	}
	return false
}

// svgText reports whether a text is an SVG image: its first element, after an XML prolog, is <svg.
func svgText(text string) bool {
	if len(text) > svgScan {
		text = text[:svgScan]
	}
	head := strings.ToLower(strings.TrimSpace(text))
	if strings.HasPrefix(head, "<svg") {
		return true
	}
	return (strings.HasPrefix(head, "<?xml") || strings.HasPrefix(head, "<!doctype svg")) && strings.Contains(head, "<svg")
}

// DecodeText reads the bytes of a text file: UTF-8 (a byte order mark is dropped) or UTF-16 LE/BE with its byte order
// mark. A text that is empty, not valid in its encoding or holds NUL (a binary file) is ErrUnsupportedType.
func DecodeText(data []byte) (string, error) {
	var s string
	switch {
	case bytes.HasPrefix(data, []byte(utf16LEBOM)), bytes.HasPrefix(data, []byte(utf16BEBOM)):
		body := data[2:]
		if len(body)%2 != 0 {
			return "", ErrUnsupportedType
		}
		units := make([]uint16, len(body)/2)
		for i := range units {
			lo, hi := body[2*i], body[2*i+1]
			if data[0] == 0xfe {
				lo, hi = hi, lo
			}
			units[i] = uint16(lo) | uint16(hi)<<8
		}
		runes := utf16.Decode(units)
		for _, r := range runes {
			if r == utf8.RuneError { // an unpaired surrogate
				return "", ErrUnsupportedType
			}
		}
		s = string(runes)
	default:
		text := bytes.TrimPrefix(data, []byte(utf8BOM))
		if !utf8.Valid(text) {
			return "", ErrUnsupportedType
		}
		s = string(text)
	}
	if s == "" || bytes.IndexByte([]byte(s), 0) >= 0 {
		return "", ErrUnsupportedType
	}
	return s, nil
}

// officeType reads the ZIP directory of data; exactly one of the two main parts makes it DOCX or PPTX.
func officeType(data []byte) (Type, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil || len(zr.File) > maxZipEntries {
		return "", ErrUnsupportedType
	}
	var docx, pptx bool
	for _, f := range zr.File {
		switch f.Name {
		case docxMainPart:
			docx = true
		case pptxMainPart:
			pptx = true
		}
	}
	switch {
	case docx && !pptx:
		return TypeDOCX, nil
	case pptx && !docx:
		return TypePPTX, nil
	}
	return "", ErrUnsupportedType
}

// Digest is the sha256 of a file as 64 lower-case hex characters.
type Digest string

// DigestOf returns the sha256 of data.
func DigestOf(data []byte) Digest {
	sum := sha256.Sum256(data)
	return Digest(hex.EncodeToString(sum[:]))
}

// Validate reports whether d is 64 lower-case hex characters.
func (d Digest) Validate() error {
	if len(d) != sha256.Size*2 {
		return fmt.Errorf("%w: digest length %d", ErrInvalidMaterial, len(d))
	}
	for _, c := range []byte(d) {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return fmt.Errorf("%w: digest is not lower-case hex", ErrInvalidMaterial)
		}
	}
	return nil
}

// String returns the hex form.
func (d Digest) String() string { return string(d) }

// BlobRef is where the bytes with digest d live in the artifact store: one file per sha256, named by it, without an
// extension (the file is never opened from there).
func BlobRef(d Digest) (shared.ArtifactRef, error) {
	if err := d.Validate(); err != nil {
		return shared.ArtifactRef{}, err
	}
	return shared.NewArtifactRef(shared.ArtifactMaterials, string(d))
}
