package googlecal_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"paratech/desktop/internal/adapters/googlecal"
	"paratech/desktop/testenv"
)

// A stored calendar named «Learning Assistant» (before ADR-0015) is renamed in place with calendars.patch: no second
// calendar, the same id and binding. A name the user chose stays, and a failed patch never fails EnsureCalendar. A
// transient failure is retried at the next call, a refusal is not asked again in this process.
func TestEnsureCalendarRenamesTheLegacyCalendar(t *testing.T) {
	const legacyDescription = "Расписание занятий и дедлайны из приложения Learning Assistant. [paratech-calendar-v1]"
	tests := []struct {
		name        string
		summary     string
		faults      []testenv.CalendarFault // for the next PATCH requests
		wantPatches int                     // after the first EnsureCalendar
		wantSummary string                  // after the first EnsureCalendar
		refused     bool                    // the failed patch is not retried
	}{
		{"legacy name", googlecal.LegacyCalendarSummary, nil, 1, googlecal.CalendarSummary, false},
		{"current name", googlecal.CalendarSummary, nil, 0, googlecal.CalendarSummary, false},
		{"renamed by the user", "Мои пары", nil, 0, "Мои пары", false},
		{"legacy name in another case", "learning assistant", nil, 0, "learning assistant", false},
		{"patch fails", googlecal.LegacyCalendarSummary, []testenv.CalendarFault{testenv.FaultServerError}, 1,
			googlecal.LegacyCalendarSummary, false},
		{"patch rate limited", googlecal.LegacyCalendarSummary, []testenv.CalendarFault{testenv.FaultUserRateLimit}, 1,
			googlecal.LegacyCalendarSummary, false},
		{"patch refused", googlecal.LegacyCalendarSummary, []testenv.CalendarFault{testenv.FaultForbidden}, 1,
			googlecal.LegacyCalendarSummary, true},
		{"patch rejected", googlecal.LegacyCalendarSummary, []testenv.CalendarFault{testenv.FaultBadRequest}, 1,
			googlecal.LegacyCalendarSummary, true},
		{"patch answer lost", googlecal.LegacyCalendarSummary, []testenv.CalendarFault{testenv.FaultDropAfterWrite}, 1,
			googlecal.CalendarSummary, false},
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

			// The next call retries a transiently failed rename and does nothing after a successful or refused one.
			wantAfter := tt.wantPatches
			if tt.wantSummary == googlecal.LegacyCalendarSummary && !tt.refused {
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

// A 2xx calendars.get whose body is not JSON still means the calendar exists: EnsureCalendar keeps it and does not
// rename it (the name is unknown).
func TestEnsureCalendarKeepsACalendarWithAnUndecodableAnswer(t *testing.T) {
	const id = "cal-1"
	var patches, inserts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case req.Method == http.MethodGet && req.URL.Path == "/calendars/"+id:
			_, _ = w.Write([]byte("not json"))
		case req.Method == http.MethodPatch:
			patches.Add(1)
			_, _ = w.Write([]byte(`{}`))
		case req.Method == http.MethodPost:
			inserts.Add(1)
			_, _ = w.Write([]byte(`{"id":"cal-2"}`))
		default:
			http.NotFound(w, req)
		}
	}))
	t.Cleanup(srv.Close)
	tr := &http.Transport{}
	t.Cleanup(tr.CloseIdleConnections)
	store := testenv.NewMemCredentials()
	ctx := context.Background()
	if err := store.Set(ctx, googlecal.CalendarIDName, `{"id":"`+id+`"}`); err != nil {
		t.Fatal(err)
	}
	c, err := googlecal.New(&fakeAuth{transport: tr, token: "t"}, store, classify, googlecal.WithBaseURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}

	got, fresh, err := c.EnsureCalendar(ctx)
	if err != nil || got != id || fresh {
		t.Fatalf("EnsureCalendar = %q, %v, %v, want %q, not fresh", got, fresh, err, id)
	}
	if p, i := patches.Load(), inserts.Load(); p != 0 || i != 0 {
		t.Fatalf("%d patches and %d inserts, want none", p, i)
	}
}
