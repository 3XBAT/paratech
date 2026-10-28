package tests

// Scenario test of find_lessons on the planned schedule (beta 2026-10-28: «расписание пар на субботу» answered
// «пар нет» while the schedule had them): weekly lessons imported from ICS through the real use case and SQLite, read
// by the real Driver.

import (
	"context"
	"strings"
	"testing"

	"paratech/desktop/internal/cases/schedulesync"
	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/schedule/services/importplan"
	"paratech/desktop/testenv"
)

// Weekly classes on Fridays 10:00 and Saturdays 12:00 Moscow for the whole autumn semester.
const icsFridaySaturday = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nX-WR-TIMEZONE:Europe/Moscow\r\n" +
	"BEGIN:VEVENT\r\nUID:fri@uni.example\r\nSUMMARY:Физика (лекция)\r\n" +
	"DTSTART;TZID=Europe/Moscow:20260904T100000\r\nDTEND;TZID=Europe/Moscow:20260904T113000\r\n" +
	"RRULE:FREQ=WEEKLY;BYDAY=FR;UNTIL=20261231T205959Z\r\nEND:VEVENT\r\n" +
	"BEGIN:VEVENT\r\nUID:sat@uni.example\r\nSUMMARY:Физика (семинар)\r\n" +
	"DTSTART;TZID=Europe/Moscow:20260905T120000\r\nDTEND;TZID=Europe/Moscow:20260905T133000\r\n" +
	"RRULE:FREQ=WEEKLY;BYDAY=SA;UNTIL=20261231T205959Z\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"

// importSchedule imports file as ICS into the database of e, as the schedule page does.
func (e *driverEnv) importSchedule(t *testing.T, file string) {
	t.Helper()
	svc := e.schedule
	ctx := context.Background()
	p, err := svc.PreviewImport(ctx, schedulesync.ImportRequest{Source: importplan.SourceICS, Data: []byte(file)})
	if err != nil || !p.Plan.Valid() {
		t.Fatalf("PreviewImport: %v, %+v", err, p.Plan.Errors)
	}
	if _, err := svc.CommitImport(ctx, schedulesync.CommitRequest{Source: importplan.SourceICS, Data: []byte(file), ConfirmToken: p.ConfirmToken}); err != nil {
		t.Fatalf("CommitImport: %v", err)
	}
}

// find_lessons for a future day lists the lessons of the schedule on that day, not recorded yet: their schedule
// entry, subject, date, start and kind, never their title.
func TestDriverFindLessonsReadsPlannedSchedule(t *testing.T) {
	e := newDriverEnv(t)
	e.importSchedule(t, icsFridaySaturday)
	e.hub.Then(testenv.AgentCall{ID: "c1", Tool: "find_lessons", Args: `{"from":"2026-10-31","to":"2026-10-31"}`},
		testenv.AgentCall{ID: "c2", Tool: "find_lessons", Args: `{"from":"2026-10-30","to":"2026-10-30"}`}).
		Then(testenv.AgentCall{ID: "c3", Tool: "reply", Args: `{"text":"Пары есть"}`})

	if up := e.ask(t, physicsScope(t), "скажи мне расписание пар на субботу и пятницу"); up.Status != thread.StatusDone {
		t.Fatalf("final update = %+v", up)
	}
	res := e.resultsOf(t, 1)
	if len(res) != 2 {
		t.Fatalf("results = %v", res)
	}
	for i, want := range []string{
		`"subject":"s1","date":"2026-10-31","time":"09:00","kind":"seminar"`,
		`"subject":"s1","date":"2026-10-30","time":"07:00","kind":"lecture"`,
	} {
		if !strings.Contains(res[i], `"count":1`) || !strings.Contains(res[i], `"lesson":"lesson-`) || !strings.Contains(res[i], want) ||
			strings.Contains(res[i], `"recording"`) || strings.Contains(res[i], "Физика") {
			t.Errorf("find_lessons %d = %s, want %s", i, res[i], want)
		}
	}
}
