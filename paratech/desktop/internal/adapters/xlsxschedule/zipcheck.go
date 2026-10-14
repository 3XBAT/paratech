package xlsxschedule

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
)

// zipMagic starts every .xlsx. Encrypted workbooks and .xls files are OLE files; excelize would try to decrypt them
// (an expensive key derivation chosen by the file), so they are refused before it sees them.
var zipMagic = []byte("PK\x03\x04")

// checkPackage unpacks every zip entry once, with the limits, and checks that each XML part is well-formed. excelize
// ignores read errors of entries and of the sheet XML, which would silently cut a damaged sheet.
func checkPackage(ctx context.Context, data []byte) error {
	if !bytes.HasPrefix(data, zipMagic) {
		return &Error{Kind: ErrNotWorkbook, Detail: "not a zip file (encrypted workbooks and .xls files are not supported)"}
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return &Error{Kind: ErrNotWorkbook, Detail: "not a valid zip file"}
	}
	if len(zr.File) > MaxParts {
		return &Error{Kind: ErrTooLarge, Detail: fmt.Sprintf("more than %d parts", MaxParts)}
	}
	budget := int64(MaxUnzippedBytes)
	for _, zf := range zr.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := checkPart(zf, budget)
		if err != nil {
			return err
		}
		budget -= n
	}
	return nil
}

var errOverBudget = errors.New("over the unpacked size budget")

// checkPart reads one entry to its end (archive/zip verifies the checksum and the declared size there) and returns
// its unpacked size.
func checkPart(zf *zip.File, budget int64) (int64, error) {
	tooLarge := &Error{Kind: ErrTooLarge, Detail: fmt.Sprintf("more than %d bytes unpacked", MaxUnzippedBytes)}
	if zf.UncompressedSize64 > uint64(budget) {
		return 0, tooLarge
	}
	rc, err := zf.Open()
	if err != nil {
		return 0, &Error{Kind: ErrMalformed, Detail: "a zip entry cannot be read"}
	}
	defer func() { _ = rc.Close() }()

	r := &limitedReader{r: rc, left: budget}
	if isXMLPart(zf.Name) {
		if err := wellFormed(r); err != nil {
			if errors.Is(err, errOverBudget) {
				return 0, tooLarge
			}
			return 0, &Error{Kind: ErrMalformed, Detail: "a part is not well-formed XML"}
		}
	}
	if _, err := io.Copy(io.Discard, r); err != nil {
		if errors.Is(err, errOverBudget) {
			return 0, tooLarge
		}
		return 0, &Error{Kind: ErrMalformed, Detail: "a zip entry is damaged"}
	}
	return budget - r.left, nil
}

func isXMLPart(name string) bool {
	n := strings.ToLower(name)
	return strings.HasSuffix(n, ".xml") || strings.HasSuffix(n, ".rels")
}

// wellFormed reads the XML tokens to the end (Token checks that elements nest and close). Without a CharsetReader, a
// part declaring another encoding than UTF-8 is refused.
func wellFormed(r io.Reader) error {
	d := xml.NewDecoder(r)
	for {
		_, err := d.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// limitedReader fails instead of ending quietly when the budget is spent.
type limitedReader struct {
	r    io.Reader
	left int64
}

func (l *limitedReader) Read(p []byte) (int, error) {
	if l.left <= 0 {
		var probe [1]byte
		n, err := l.r.Read(probe[:])
		if n > 0 {
			return 0, errOverBudget
		}
		return 0, err
	}
	if int64(len(p)) > l.left {
		p = p[:l.left]
	}
	n, err := l.r.Read(p)
	l.left -= int64(n)
	return n, err
}
