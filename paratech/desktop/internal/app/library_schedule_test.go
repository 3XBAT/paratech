package app

import (
	"context"
	"log/slog"
	"testing"

	"paratech/desktop/internal/input/wails"
)

// The library the shell gets has subjects from the schedule, so NewLibraryService attaches them (TASK-311): an empty
// store proposes nothing and gives a token.
func TestLibraryHasScheduleSubjects(t *testing.T) {
	ctx := context.Background()
	a := newTestApp(t, t.TempDir())
	ui, err := a.openCore(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ui.Library.(wails.LibraryScheduleUseCase); !ok {
		t.Fatalf("the library %T has no subjects from the schedule", ui.Library)
	}

	got := wails.NewLibraryService(ui.Library, nil, nil, slog.New(slog.DiscardHandler)).ScheduleSubjects(ctx)
	if got.Problem != "" || got.Token == "" || len(got.Groups) != 0 {
		t.Fatalf("ScheduleSubjects = %+v", got)
	}
}
