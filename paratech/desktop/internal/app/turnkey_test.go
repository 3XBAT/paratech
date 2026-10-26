package app

import (
	"context"
	"errors"
	"testing"

	"paratech/desktop/internal/input/wails"
)

// TASK-179: the app wires the step advisor, so «Спросить по шагу» no longer answers «пока недоступны»: an unknown
// work is reported as such (the advisor is checked first, before the work is read).
func TestAskStepHasAnAdvisor(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	core, err := a.openCore(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	binding := wails.NewTurnkeyService(core.Turnkey, a.log)
	_, err = binding.AskStep(context.Background(), "w-missing", 0, "Какой масштаб?")
	if errors.Is(err, wails.ErrTurnkeyNoStepHelp) || !errors.Is(err, wails.ErrTurnkeyNotFound) {
		t.Fatalf("AskStep = %v, want the work not found", err)
	}
}

// TASK-198: «Скачать PDF» is wired: an unknown work is reported as such, not as a missing printer (the port is checked
// first). Nothing is printed.
func TestExportPDFIsWired(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	core, err := a.openCore(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	binding := wails.NewTurnkeyService(core.Turnkey, a.log)
	if _, err := binding.ExportPDF(context.Background(), "w-missing"); !errors.Is(err, wails.ErrTurnkeyNotFound) {
		t.Fatalf("ExportPDF = %v, want the work not found", err)
	}
}
