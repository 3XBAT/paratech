package googlecal_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"paratech/desktop/internal/adapters/googlecal"
	"paratech/desktop/testenv"
)

// A stored calendar named «Learning Assistant» (before ADR-0015) is renamed in place with calendars.patch: no second
// calendar, the same id and binding. A name the user chose stays, and a failed patch never fails EnsureCalendar.
func TestEnsureCalendarRenamesTheLegacyCalendar(t *testing.T) {
	const legacyDescription = "Расписание занятий и дедлайны из приложения Learning Assistant. [paratech-calendar-v1]"
	tests := []struct {
		name        string
		summary     string
		faults      []testenv.CalendarFault // for the next PATCH requests
		wantPatches int                     // after the first EnsureCalendar
		wantSummary string                  // after the first EnsureCalendar
	}{
		{"legacy name", googlecal.LegacyCalendarSummary, nil, 1, googlecal.CalendarSummary},
		{"current name", googlecal.CalendarSummary, nil, 0, googlecal.CalendarSummary},
		{"renamed by the user", "Мои пары", nil, 0, "Мои пары"},
		{"legacy name in another case", "learning assistant", nil, 0, "learning assistant"},
		{"patch fails", googlecal.LegacyCalendarSummary, []testenv.CalendarFault{testenv.FaultServerError}, 1,
			googlecal.LegacyCalendarSummary},
		{"patch refused", googlecal.LegacyCalendarSummary, []testenv.CalendarFault{testenv.FaultForbidden}, 1,
			googlecal.LegacyCalendarSummary},
		{"patch answer lost", googlecal.LegacyCalendarSummary, []testenv.CalendarFault{testenv.FaultDropAfterWrite}, 1,
			googlecal.CalendarSummary},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t)
			ctx := context.Background()
			old := r.api.AddCalendar(tt.summary, legacyDescription)
			if err := r.store.Set(ctx, googlecal.CalendarIDName, `{"id":"`+old+`"}`); err != nil {
				t.Fatal(err)
			}
			for _, f := range tt.faults {
				r.api.FailNext(f, http.MethodPatch)
			}

			id, fresh, err := r.c.EnsureCalendar(ctx)
			if err != nil || id != old || fresh {
				t.Fatalf("EnsureCalendar = %q, %v, %v, want %q, not fresh", id, fresh, err, old)
			}
			if n := r.requests(http.MethodPatch, "/calendars/"+old); n != tt.wantPatches {
				t.Fatalf("%d calendars.patch requests, want %d", n, tt.wantPatches)
			}
			cals := r.api.Calendars()
			if len(cals) != 1 || cals[0].ID != old || cals[0].Summary != tt.wantSummary {
				t.Fatalf("calendars %+v, want only %q named %q", cals, old, tt.wantSummary)
			}
			if tt.wantSummary == googlecal.CalendarSummary && tt.summary != googlecal.CalendarSummary &&
				(strings.Contains(cals[0].Description, "Learning Assistant") ||
					!strings.Contains(cals[0].Description, "[paratech-calendar-v1]")) {
				t.Fatalf("description %q after the rename", cals[0].Description)
			}

			// The next call retries a failed rename and does nothing after a successful one.
			wantAfter := tt.wantPatches
			if tt.wantSummary == googlecal.LegacyCalendarSummary {
				wantAfter++
			}
			if again, _, err := r.c.EnsureCalendar(ctx); err != nil || again != old {
				t.Fatalf("second EnsureCalendar = %q, %v", again, err)
			}
			if n := r.requests(http.MethodPatch, "/calendars/"+old); n != wantAfter {
				t.Fatalf("%d calendars.patch requests after the second call, want %d", n, wantAfter)
			}
			if n := r.requests(http.MethodPost, "/calendars"); n != 0 {
				t.Fatalf("%d calendar inserts, want none", n)
			}

			// Events keep going into the same calendar.
			if err := r.c.UpsertLesson(ctx, old, newLesson(t, "l1", "Матанализ")); err != nil {
				t.Fatal(err)
			}
			if n := len(r.api.Events(old)); n != 1 {
				t.Fatalf("%d events in the stored calendar, want 1", n)
			}
		})
	}
}
