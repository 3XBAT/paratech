package wails

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"paratech/desktop/internal/cases/turnkey"
	"paratech/desktop/internal/domain/coursework/work"
	"paratech/desktop/internal/domain/shared"
)

func donePaperView() turnkey.View {
	v := writingWork()
	v.Status = work.StatusDone
	v.Writing.Step, v.Writing.Material, v.Writing.PDF = work.StepNone, "m-docx", "m-pdf"
	return v
}

// «Скачать PDF» reaches the use case; the work comes back with the PDF to open.
func TestTurnkeyExportPDF(t *testing.T) {
	f := &fakeTurnkey{view: donePaperView()}
	got, err := NewTurnkeyService(f, discard()).ExportPDF(t.Context(), "w1")
	if err != nil || got.ID != "w1" || got.Writing.PDF != "m-pdf" || got.Writing.MaterialID != "m-docx" {
		t.Fatalf("ExportPDF = %+v, %v", got.Writing, err)
	}
	if !slices.Equal(f.calls, []string{"pdf"}) {
		t.Fatalf("calls = %v", f.calls)
	}
	b, err := json.Marshal(got.Writing)
	if err != nil || !strings.Contains(string(b), `"materialId":"m-docx","result":"","pdfMaterialId":"m-pdf",`) {
		t.Fatalf("JSON = %s, %v", b, err)
	}

	// A new generation has no PDF yet: the field is empty.
	v := writingWork()
	got, err = NewTurnkeyService(&fakeTurnkey{view: v}, discard()).Work(t.Context(), "w1")
	if err != nil || got.Writing.PDF != "" {
		t.Fatalf("Work = %+v, %v", got.Writing, err)
	}
}

// The PDF failures have texts of their own; the rest are those of the other calls. The log gets the id and a code.
func TestTurnkeyExportPDFErrors(t *testing.T) {
	const cause = "Секретная тема реферата"
	tests := []struct {
		err  error
		want error
		code string
	}{
		{fmt.Errorf("%w: %w: %s", turnkey.ErrPDFUnsupported, errors.ErrUnsupported, cause), ErrTurnkeyPDFUnsupported, "pdf_unsupported"},
		{fmt.Errorf("%w: %s", turnkey.ErrPDFFailed, cause), ErrTurnkeyPDFFailed, "pdf_failed"},
		{fmt.Errorf("%w: %s", turnkey.ErrChanged, cause), ErrTurnkeyStale, "stale"},
		{turnkey.ErrWritingRunning, ErrTurnkeyWriting, "writing"},
		{fmt.Errorf("%w: a presentation", turnkey.ErrNotWritable), ErrTurnkeyInvalid, "invalid"},
		{fmt.Errorf("%w: %w", turnkey.ErrNotFound, shared.ErrNotFound), ErrTurnkeyNotFound, "not_found"},
		{turnkey.ErrNotConfigured, ErrTurnkeyUnavailable, "unavailable"},
		{fmt.Errorf("%w: %w", turnkey.ErrUnavailable, context.Canceled), ErrTurnkeyUnavailable, "unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			var logs bytes.Buffer
			s := NewTurnkeyService(&fakeTurnkey{err: tt.err}, slog.New(slog.NewTextHandler(&logs, nil)))
			got, err := s.ExportPDF(t.Context(), "w1")
			if !errors.Is(err, tt.want) || got.ID != "" {
				t.Fatalf("ExportPDF = %+v, %v; want %v", got, err, tt.want)
			}
			line := logs.String()
			if !strings.Contains(line, "work=w1") || !strings.Contains(line, "code="+tt.code) || strings.Contains(line, cause) ||
				strings.Contains(line, "err=") {
				t.Fatalf("log = %s", line)
			}
		})
	}
	if ErrTurnkeyPDFUnsupported.Error() != "Не удалось сохранить PDF: на компьютере нет нужного компонента Windows (WebView2)." ||
		ErrTurnkeyPDFFailed.Error() != "Не удалось сохранить PDF. Попробуйте ещё раз." {
		t.Fatal("the pdf texts changed")
	}

	// An id from the page that is no work id is not logged.
	var logs bytes.Buffer
	s := NewTurnkeyService(&fakeTurnkey{err: turnkey.ErrNotFound}, slog.New(slog.NewTextHandler(&logs, nil)))
	if _, err := s.ExportPDF(t.Context(), "bad id\nwith a line"); !errors.Is(err, ErrTurnkeyNotFound) || strings.Contains(logs.String(), "with a line") {
		t.Fatalf("ExportPDF = %v, log %s", err, logs.String())
	}
}
