package pdf

import (
	"context"
	"errors"
	"fmt"

	"paratech/desktop/internal/domain/coursework/values"
)

// HTMLPrinter prints a complete HTML page to PDF bytes: the Printer of this package, a fake in tests.
type HTMLPrinter interface {
	Print(ctx context.Context, html []byte) ([]byte, error)
}

var _ HTMLPrinter = (*Printer)(nil)

// Renderer makes the PDF of a paper (turnkey PaperPDF, TASK-198): the print page of the document (printHTML), printed
// by the Printer.
type Renderer struct {
	printer HTMLPrinter
}

// NewRenderer returns a Renderer printing with p.
func NewRenderer(p HTMLPrinter) *Renderer {
	return &Renderer{printer: p}
}

// RenderPDF returns the PDF of doc. Errors: ErrRender for a document that cannot be printed; ErrUnsupported, which also
// wraps errors.ErrUnsupported, when this computer cannot print to PDF; ErrFailed for a print that failed or ran out of
// ctx's deadline; ctx's context.Canceled when the caller cancelled.
func (r *Renderer) RenderPDF(ctx context.Context, doc values.Document) ([]byte, error) {
	page, err := printHTML(doc)
	if err != nil {
		return nil, err
	}
	out, err := r.printer.Print(ctx, page)
	switch {
	case err == nil:
		return out, nil
	case errors.Is(err, context.Canceled):
		return nil, err
	case errors.Is(err, ErrUnsupported):
		return nil, fmt.Errorf("%w: %w", err, errors.ErrUnsupported)
	case errors.Is(err, context.DeadlineExceeded):
		// Only the deadline notices a browser that crashed or hangs: a failed print, not the caller's cancel.
		return nil, fmt.Errorf("%w: the print ran out of time", ErrFailed)
	case errors.Is(err, ErrFailed):
		return nil, err
	}
	return nil, fmt.Errorf("%w: %w", ErrFailed, err)
}
