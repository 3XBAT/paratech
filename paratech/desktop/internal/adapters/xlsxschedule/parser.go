package xlsxschedule

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/xuri/excelize/v2"

	"paratech/desktop/internal/domain/schedule/services/importplan"
)

// Limits against hostile or broken files. A semester of lessons is far below each of them.
const (
	// MaxFileBytes bounds the file, as for CSV and ICS.
	MaxFileBytes = 2 << 20

	// MaxUnzippedBytes bounds the unpacked size of all zip entries together (zip bombs). excelize keeps the
	// unpacked parts in memory; with its XML limit equal to this one it never writes temporary files.
	MaxUnzippedBytes = 16 << 20

	// MaxParts bounds the zip entries; a workbook has a few dozen.
	MaxParts = 1000
)

const cancelCheck = 256 // rows between context checks

// Parser reads the XLSX schedule format of the package comment. The zero value is ready to use; it is safe for
// concurrent use.
type Parser struct{}

// New returns an XLSX schedule parser.
func New() *Parser { return &Parser{} }

// Parse implements the schedulesync.ScheduleParser port.
func (p *Parser) Parse(ctx context.Context, data []byte) (importplan.Parsed, error) {
	if err := ctx.Err(); err != nil {
		return importplan.Parsed{}, err
	}
	if len(data) > MaxFileBytes {
		return importplan.Parsed{}, &Error{Kind: ErrTooLarge, Detail: fmt.Sprintf("more than %d bytes", MaxFileBytes)}
	}
	if err := checkPackage(ctx, data); err != nil {
		return importplan.Parsed{}, err
	}
	var out importplan.Parsed
	err := safely(func() error {
		var err error
		out, err = readWorkbook(ctx, data)
		return err
	})
	if err != nil {
		return importplan.Parsed{}, err
	}
	return out, nil
}

// safely turns a panic of excelize on hostile input into a malformed-file error instead of ending the app.
func safely(fn func() error) (err error) {
	defer func() {
		if recover() != nil {
			err = &Error{Kind: ErrMalformed, Detail: "the workbook cannot be read"}
		}
	}()
	return fn()
}

func readWorkbook(ctx context.Context, data []byte) (importplan.Parsed, error) {
	f, err := excelize.OpenReader(bytes.NewReader(data), excelize.Options{
		UnzipSizeLimit:    MaxUnzippedBytes,
		UnzipXMLSizeLimit: MaxUnzippedBytes,
		RawCellValue:      true, // stored values only: no number formats applied
	})
	if f != nil {
		defer func() { _ = f.Close() }()
	}
	if err != nil {
		return importplan.Parsed{}, &Error{Kind: ErrMalformed, Detail: "the workbook cannot be opened"}
	}

	sheet, ok := firstVisibleSheet(f)
	if !ok {
		return importplan.Parsed{}, &Error{Kind: ErrMalformed, Detail: "the workbook has no visible sheet"}
	}
	props, err := f.GetWorkbookProps()
	if err != nil {
		return importplan.Parsed{}, &Error{Kind: ErrMalformed, Detail: "the workbook properties cannot be read"}
	}
	rows, err := f.Rows(sheet)
	if err != nil {
		return importplan.Parsed{}, &Error{Kind: ErrMalformed, Detail: "the sheet cannot be read"}
	}
	defer func() { _ = rows.Close() }()

	return readRows(ctx, rows, props.Date1904 != nil && *props.Date1904)
}

func firstVisibleSheet(f *excelize.File) (string, bool) {
	for _, name := range f.GetSheetList() {
		if visible, err := f.GetSheetVisible(name); err == nil && visible {
			return name, true
		}
	}
	return "", false
}

// rowSource is the part of excelize.Rows the reader uses.
type rowSource interface {
	Next() bool
	Columns(opts ...excelize.Options) ([]string, error)
	Error() error
}

// readRows walks the sheet: Next is called once per sheet row, empty rows included, so the counter is the row number.
func readRows(ctx context.Context, rows rowSource, date1904 bool) (importplan.Parsed, error) {
	var (
		out     importplan.Parsed
		reader  *rowReader
		lessons int
	)
	for line := 1; rows.Next(); line++ {
		if line%cancelCheck == 0 {
			if err := ctx.Err(); err != nil {
				return importplan.Parsed{}, err
			}
		}
		cells, err := rows.Columns()
		if err != nil {
			return importplan.Parsed{}, &Error{Kind: ErrMalformed, Line: line, Detail: "the row cannot be read"}
		}
		if blank(cells) {
			continue
		}
		if reader == nil {
			if reader, err = newRowReader(line, cells, date1904); err != nil {
				return importplan.Parsed{}, err
			}
			continue
		}
		if lessons == importplan.MaxRows {
			return importplan.Parsed{}, &Error{Kind: ErrTooMany, Detail: fmt.Sprintf("more than %d lessons", importplan.MaxRows)}
		}
		lessons++
		row, rowErr := reader.parse(line, cells)
		if rowErr != nil {
			out.Errors = append(out.Errors, *rowErr)
			continue
		}
		out.Rows = append(out.Rows, row)
	}
	if err := rows.Error(); err != nil {
		if errors.Is(err, excelize.ErrMaxRows) {
			return importplan.Parsed{}, &Error{Kind: ErrTooMany, Detail: "a row number is over the sheet limit"}
		}
		return importplan.Parsed{}, &Error{Kind: ErrMalformed, Detail: "the sheet cannot be read"}
	}
	if reader == nil {
		return importplan.Parsed{}, &Error{Kind: ErrHeader, Detail: "the sheet is empty"}
	}
	return out, nil
}

func blank(cells []string) bool {
	for _, c := range cells {
		if strings.TrimSpace(c) != "" {
			return false
		}
	}
	return true
}
