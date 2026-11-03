package app

import (
	"context"
	"log/slog"
	"testing"

	"paratech/desktop/internal/input/wails"
)

// The schedule_subjects card of the agent is wired to the library of the screens (TASK-323): the shell gives the card
// the page's binding (wails.BindScheduleCards), the card of the current token waits, an old token is stale, and a «Да»
// runs the page's CreateFromSchedule (nothing to create in an empty store).
func TestAgentScheduleCardWired(t *testing.T) {
	ctx := context.Background()
	a := newTestApp(t, t.TempDir())
	ui, err := a.openCore(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ui.Library.(wails.LibraryScheduleUseCase); !ok {
		t.Fatalf("the library of the screens is %T, without subjects from the schedule", ui.Library)
	}
	page := wails.NewLibraryService(ui.Library, nil, nil, slog.New(slog.DiscardHandler))
	token := page.ScheduleSubjects(ctx).Token

	cards := wails.BindScheduleCards(wails.NewAgentConfirmService(ui.AgentConfirm, slog.New(slog.DiscardHandler)), page)
	got, err := cards.Card(ctx, "schedule_subjects", token)
	if err != nil || got.State != "waiting" || got.Schedule == nil {
		t.Fatalf("card = %+v, %v", got, err)
	}
	other := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if got, err := cards.Card(ctx, "schedule_subjects", other); err != nil || got.State != "stale" {
		t.Fatalf("old token = %+v, %v", got, err)
	}
	if got, err := cards.ConfirmSchedule(ctx, token, nil); err != nil || got.State != "done" || len(got.Schedule.Result.Created) != 0 {
		t.Fatalf("confirm = %+v, %v", got, err)
	}
}
