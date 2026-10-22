package material

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"unicode/utf8"

	"paratech/desktop/internal/domain/shared"
)

// MaxBytes bounds the size of one file of the library.
const MaxBytes = 100 << 20

// Type is the detected type of a file; the allowlist below is all the library takes.
type Type string

// Types of the allowlist.
const (
	TypeDOCX Type = "docx"
	TypePPTX Type = "pptx"
	TypeTXT  Type = "txt"
	TypePDF  Type = "pdf"
)

// Types returns the allowlist.
func Types() []Type { return []Type{TypeDOCX, TypePPTX, TypeTXT, TypePDF} }

func (t Type) valid() bool {
	switch t {
	case TypeDOCX, TypePPTX, TypeTXT, TypePDF:
		return true
	}
	return false
}

// Signatures and the part names that tell an Office Open XML package.
const (
	pdfMagic      = "%PDF-"
	zipMagic      = "PK\x03\x04"
	utf8BOM       = "\xef\xbb\xbf"
	docxMainPart  = "word/document.xml"
	pptxMainPart  = "ppt/presentation.xml"
	maxZipEntries = 100_000
)

// DetectType tells the type of a file from its content, never from its name: PDF by its signature, DOCX and PPTX by
// the main part in the ZIP directory (nothing is decompressed), text by being valid UTF-8 without NUL. Errors:
// ErrEmpty, ErrTooLarge, ErrUnsupportedType.
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
	}
	text := bytes.TrimPrefix(data, []byte(utf8BOM))
	if len(text) > 0 && utf8.Valid(text) && bytes.IndexByte(text, 0) < 0 {
		return TypeTXT, nil
	}
	return "", ErrUnsupportedType
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
