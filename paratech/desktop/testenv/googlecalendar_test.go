package testenv

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// calClient calls a FakeGoogleCalendar like the adapter does.
type calClient struct {
	t     *testing.T
	f     *FakeGoogleCalendar
	token string
	query url.Values // of the next request only
}

func newCalClient(t *testing.T) *calClient {
	return &calClient{t: t, f: StartFakeGoogleCalendar(t), token: FakeCalendarAccessToken}
}

// do sends a request to the path below the base URL; segments are escaped. It returns the status, the error reason and
// the decoded body; status 0 means the connection broke.
func (c *calClient) do(method string, body any, segments ...string) (int, string, map[string]any) {
	c.t.Helper()
	escaped := make([]string, len(segments))
	for i, s := range segments {
		escaped[i] = url.PathEscape(s)
	}
	var reader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			c.t.Fatal(err)
		}
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}
	target := c.f.BaseURL() + "/" + strings.Join(escaped, "/")
	if c.query != nil {
		target += "?" + c.query.Encode()
		c.query = nil
	}
	req, err := http.NewRequest(method, target, reader)
	if err != nil {
		c.t.Fatal(err)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return 0, "", nil
	}
	defer func() { _ = resp.Body.Close() }()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	reason := ""
	if e, ok := out["error"].(map[string]any); ok {
		if list, ok := e["errors"].([]any); ok && len(list) > 0 {
			reason, _ = list[0].(map[string]any)["reason"].(string)
		}
	}
	return resp.StatusCode, reason, out
}

func (c *calClient) newCalendar() string {
	c.t.Helper()
	status, _, body := c.do(http.MethodPost, map[string]any{"summary": "Learning Assistant"}, "calendars")
	id, _ := body["id"].(string)
	if status != http.StatusOK || id == "" {
		c.t.Fatalf("calendars.insert = %d", status)
	}
	return id
}

func calEvent(id, summary string) map[string]any {
	e := map[string]any{
		"summary": summary, "status": "confirmed",
		"start":              map[string]string{"dateTime": "2026-10-20T10:00:00+03:00", "timeZone": "Europe/Moscow"},
		"end":                map[string]string{"dateTime": "2026-10-20T11:30:00+03:00", "timeZone": "Europe/Moscow"},
		"extendedProperties": map[string]any{"private": map[string]string{"app": "paratech"}},
	}
	if id != "" {
		e["id"] = id
	}
	return e
}

const calEventID = "0123456789abcdefghijklmnopqrstuv"

func TestFakeCalendarEventLifecycle(t *testing.T) {
	c := newCalClient(t)
	cal := c.newCalendar()

	steps := []struct {
		name   string
		method string
		body   any
		path   []string
		status int
		reason string
		live   int
	}{
		{"update of a missing event", http.MethodPut, calEvent("", "A"), []string{"calendars", cal, "events", calEventID}, 404, "notFound", 0},
		{"insert with an id", http.MethodPost, calEvent(calEventID, "A"), []string{"calendars", cal, "events"}, 200, "", 1},
		{"insert of the same id", http.MethodPost, calEvent(calEventID, "B"), []string{"calendars", cal, "events"}, 409, "duplicate", 1},
		{"update", http.MethodPut, calEvent("", "B"), []string{"calendars", cal, "events", calEventID}, 200, "", 1},
		{"get", http.MethodGet, nil, []string{"calendars", cal, "events", calEventID}, 200, "", 1},
		{"delete", http.MethodDelete, nil, []string{"calendars", cal, "events", calEventID}, 204, "", 0},
		{"delete again", http.MethodDelete, nil, []string{"calendars", cal, "events", calEventID}, 410, "deleted", 0},
		{"insert of a deleted id", http.MethodPost, calEvent(calEventID, "C"), []string{"calendars", cal, "events"}, 409, "duplicate", 0},
		{"update restores", http.MethodPut, calEvent("", "C"), []string{"calendars", cal, "events", calEventID}, 200, "", 1},
		{"delete of an unknown event", http.MethodDelete, nil, []string{"calendars", cal, "events", "unknownid"}, 404, "notFound", 1},
		{"id with a w", http.MethodPost, calEvent("abcdw", "D"), []string{"calendars", cal, "events"}, 400, "invalid", 1},
		{"id with upper case", http.MethodPost, calEvent("ABCDE", "D"), []string{"calendars", cal, "events"}, 400, "invalid", 1},
		{"id too short", http.MethodPost, calEvent("abcd", "D"), []string{"calendars", cal, "events"}, 400, "invalid", 1},
		{"id too long", http.MethodPost, calEvent(strings.Repeat("a", 1025), "D"), []string{"calendars", cal, "events"}, 400, "invalid", 1},
		{"id of length 1024", http.MethodPost, calEvent(strings.Repeat("a", 1024), "D"), []string{"calendars", cal, "events"}, 200, "", 2},
		{"end before start", http.MethodPost, map[string]any{
			"id": "abcde", "start": map[string]string{"dateTime": "2026-10-20T11:00:00Z"},
			"end": map[string]string{"dateTime": "2026-10-20T10:00:00Z"},
		}, []string{"calendars", cal, "events"}, 400, "invalid", 2},
	}
	for _, s := range steps {
		status, reason, _ := c.do(s.method, s.body, s.path...)
		if status != s.status || reason != s.reason {
			t.Fatalf("%s: %d %q, want %d %q", s.name, status, reason, s.status, s.reason)
		}
		if n := len(c.f.Events(cal)); n != s.live {
			t.Fatalf("%s: %d live events, want %d", s.name, n, s.live)
		}
	}
	e, ok := c.f.Event(cal, calEventID)
	if !ok || e.Summary != "C" || e.Status != FakeEventConfirmed || e.Private["app"] != "paratech" ||
		e.Start.TimeZone != "Europe/Moscow" {
		t.Fatalf("restored event %+v", e)
	}
}

// Only calendars the app created are visible; the bearer token is checked first.
func TestFakeCalendarAccess(t *testing.T) {
	c := newCalClient(t)
	cal := c.newCalendar()
	gone := c.newCalendar()
	c.f.DeleteCalendar(gone)

	tests := []struct {
		name   string
		token  string
		method string
		body   any
		path   []string
		status int
	}{
		{"own calendar", FakeCalendarAccessToken, http.MethodGet, nil, []string{"calendars", cal}, 200},
		{"primary", FakeCalendarAccessToken, http.MethodGet, nil, []string{"calendars", "primary"}, 404},
		{"event in primary", FakeCalendarAccessToken, http.MethodPost, calEvent(calEventID, "A"), []string{"calendars", "primary", "events"}, 404},
		{"deleted calendar", FakeCalendarAccessToken, http.MethodGet, nil, []string{"calendars", gone}, 404},
		{"no token", "", http.MethodGet, nil, []string{"calendars", cal}, 401},
		{"wrong token", "other", http.MethodGet, nil, []string{"calendars", cal}, 401},
		{"unknown route", FakeCalendarAccessToken, http.MethodGet, nil, []string{"colors"}, 404},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c.token = tt.token
			if status, _, _ := c.do(tt.method, tt.body, tt.path...); status != tt.status {
				t.Fatalf("status %d, want %d", status, tt.status)
			}
		})
	}
	if n := c.f.ForeignRequests(); n != 2 {
		t.Fatalf("foreign requests %d, want 2", n)
	}
	if got := c.f.Calendars(); len(got) != 1 || got[0].ID != cal {
		t.Fatalf("calendars %+v", got)
	}

	c.f.AcceptTokens(func(token string) bool { return token == "other" })
	c.token = "other"
	if status, _, _ := c.do(http.MethodGet, nil, "calendars", cal); status != 200 {
		t.Fatalf("AcceptTokens: status %d", status)
	}
}

// calendarList.get knows only the live calendars the app created.
func TestFakeCalendarListEntry(t *testing.T) {
	c := newCalClient(t)
	cal := c.newCalendar()
	gone := c.newCalendar()
	c.f.DeleteCalendar(gone)

	tests := []struct {
		name   string
		id     string
		status int
	}{
		{"own calendar", cal, 200},
		{"deleted calendar", gone, 404},
		{"primary", "primary", 404},
	}
	for _, tt := range tests {
		status, _, body := c.do(http.MethodGet, nil, "users", "me", "calendarList", tt.id)
		if status != tt.status {
			t.Fatalf("%s: status %d, want %d", tt.name, status, tt.status)
		}
		if status == 200 && (body["id"] != cal || body["accessRole"] != "owner") {
			t.Fatalf("%s: entry %v", tt.name, body)
		}
	}
	if n := c.f.ForeignRequests(); n != 1 {
		t.Fatalf("foreign requests %d, want 1", n)
	}
}

// KeepCancelledOnUpdate: an update of a cancelled event applies the body and leaves it cancelled.
func TestFakeCalendarKeepCancelledOnUpdate(t *testing.T) {
	c := newCalClient(t)
	cal := c.newCalendar()
	if status, _, _ := c.do(http.MethodPost, calEvent(calEventID, "A"), "calendars", cal, "events"); status != 200 {
		t.Fatal(status)
	}
	c.f.CancelEvent(cal, calEventID)
	c.f.KeepCancelledOnUpdate(true)
	status, _, body := c.do(http.MethodPut, calEvent("", "B"), "calendars", cal, "events", calEventID)
	if status != 200 || body["status"] != FakeEventCancelled || body["summary"] != "B" {
		t.Fatalf("update = %d %v", status, body)
	}
	c.f.KeepCancelledOnUpdate(false)
	if status, _, body := c.do(http.MethodPut, calEvent("", "C"), "calendars", cal, "events", calEventID); status != 200 ||
		body["status"] != FakeEventConfirmed {
		t.Fatalf("update = %d %v", status, body)
	}
}

// Private properties over Google's limits are dropped (key) or cut (value) without an error.
func TestFakeCalendarPrivatePropertyLimits(t *testing.T) {
	c := newCalClient(t)
	cal := c.newCalendar()
	e := calEvent(calEventID, "A")
	e["extendedProperties"] = map[string]any{"private": map[string]string{
		strings.Repeat("k", 44): "fits", strings.Repeat("k", 45): "dropped", "long": strings.Repeat("я", 1025),
	}}
	if status, _, _ := c.do(http.MethodPost, e, "calendars", cal, "events"); status != 200 {
		t.Fatal(status)
	}
	got, _ := c.f.Event(cal, calEventID)
	if len(got.Private) != 2 || got.Private[strings.Repeat("k", 44)] != "fits" ||
		got.Private["long"] != strings.Repeat("я", 1024) {
		t.Fatalf("private %v", got.Private)
	}
}

func TestFakeCalendarFaults(t *testing.T) {
	tests := []struct {
		fault  CalendarFault
		status int
		reason string
	}{
		{FaultUnauthorized, 401, "authError"},
		{FaultRateLimited, 429, "rateLimitExceeded"},
		{FaultUserRateLimit, 403, "userRateLimitExceeded"},
		{FaultForbidden, 403, "forbidden"},
		{FaultQuotaExceeded, 403, "quotaExceeded"},
		{FaultNotFound, 404, "notFound"},
		{FaultServerError, 500, "backendError"},
		{FaultBadRequest, 400, "invalid"},
	}
	for _, tt := range tests {
		t.Run(tt.reason, func(t *testing.T) {
			c := newCalClient(t)
			cal := c.newCalendar()
			c.f.FailNext(tt.fault, http.MethodPut)
			if status, _, _ := c.do(http.MethodGet, nil, "calendars", cal); status != 200 {
				t.Fatalf("a GET took the PUT fault: %d", status)
			}
			if status, reason, _ := c.do(http.MethodPut, calEvent("", "A"), "calendars", cal, "events", calEventID); status != tt.status || reason != tt.reason {
				t.Fatalf("faulted = %d %q", status, reason)
			}
			if n := len(c.f.Events(cal)); n != 0 {
				t.Fatalf("a faulted request wrote %d events", n)
			}
			if status, _, _ := c.do(http.MethodPut, calEvent("", "A"), "calendars", cal, "events", calEventID); status != 404 {
				t.Fatalf("the fault applied twice: %d", status)
			}
		})
	}
}

// The write happens, the answer does not arrive.
func TestFakeCalendarDropAfterWrite(t *testing.T) {
	c := newCalClient(t)
	cal := c.newCalendar()
	c.f.FailNext(FaultDropAfterWrite, "")
	if status, _, _ := c.do(http.MethodPost, calEvent(calEventID, "A"), "calendars", cal, "events"); status != 0 {
		t.Fatalf("status %d, want a broken connection", status)
	}
	if n := len(c.f.Events(cal)); n != 1 {
		t.Fatalf("%d events, want the write applied", n)
	}
	reqs := c.f.Requests()
	if last := reqs[len(reqs)-1]; last.Method != http.MethodPost || last.Path != "/calendars/"+cal+"/events" {
		t.Fatalf("last request %+v", last)
	}
}

func TestFakeCalendarUserActions(t *testing.T) {
	c := newCalClient(t)
	added := c.f.AddCalendar("Learning Assistant", "marker")
	if status, _, body := c.do(http.MethodGet, nil, "calendars", added); status != 200 || body["description"] != "marker" {
		t.Fatalf("added calendar = %d %v", status, body)
	}
	if status, _, _ := c.do(http.MethodPost, calEvent(calEventID, "A"), "calendars", added, "events"); status != 200 {
		t.Fatal(status)
	}
	c.f.CancelEvent(added, calEventID)
	if n := len(c.f.Events(added)); n != 0 {
		t.Fatalf("%d live events after the user deleted it", n)
	}
	if status, _, body := c.do(http.MethodGet, nil, "calendars", added, "events", calEventID); status != 200 || body["status"] != FakeEventCancelled {
		t.Fatalf("get of a cancelled event = %d %v", status, body["status"])
	}
}
