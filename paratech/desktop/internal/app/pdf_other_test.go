//go:build !windows

package app

import (
	"context"
	"errors"
	"testing"

	"paratech/desktop/internal/domain/coursework/work"
	"paratech/desktop/internal/input/wails"
)

// TASK-198: off Windows the PDF printer is the stub: a done paper answers that PDF is unavailable and keeps its .docx.
func TestExportPDFUnsupportedOffWindows(t *testing.T) {
	ctx := context.Background()
	a, _ := proposalCore(t, teacher)
	m := doneWork(t, a, "w1", work.Source{Assignment: "a1"}, docxBytes(t, "Реферат"))
	binding := wails.NewTurnkeyService(a.core.ui.Turnkey, a.log)

	if _, err := binding.ExportPDF(ctx, "w1"); !errors.Is(err, wails.ErrTurnkeyPDFUnsupported) {
		t.Fatalf("ExportPDF = %v, want unsupported", err)
	}
	v, err := binding.Work(ctx, "w1")
	if err != nil || v.Writing == nil || v.Writing.PDF != "" || v.Writing.MaterialID != string(m.ID()) {
		t.Fatalf("work = %+v, %v", v.Writing, err)
	}
}
