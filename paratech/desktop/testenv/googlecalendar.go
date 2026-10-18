package testenv

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

// FakeCalendarAccessToken is the bearer token a FakeGoogleCalendar accepts by default.
const FakeCalendarAccessToken = "fake-calendar-access-token" //nolint:gosec // G101: an obviously fake test value

// fakeCalendarAPIPath is the path of the base URL, as in https://www.googleapis.com/calendar/v3.
const fakeCalendarAPIPath = "/calendar/v3"

// Limits of the fake, like the documented limits of the Calendar API (docs/refs/google-calendar.md).
const (
	fakeCalendarMaxBody  = 1 << 20
	fakeEventIDMin       = 5
	fakeEventIDMax       = 1024
	fakePropertyKeyMax   = 44
	fakePropertyValueMax = 1024
)

// Event statuses of the Calendar API.
const (
	FakeEventConfirmed = "confirmed"
	FakeEventCancelled = "cancelled"
)

// CalendarFault is an injected failure of a FakeGoogleCalendar request.
type CalendarFault int

const (
	// FaultUnauthorized answers 401 authError, as for a revoked access token.
	FaultUnauthorized CalendarFault = iota + 1
	// FaultRateLimited answers 429 rateLimitExceeded with "Retry-After: 7".
	FaultRateLimited
	// FaultUserRateLimit answers 403 userRateLimitExceeded.
	FaultUserRateLimit
	// FaultForbidden answers 403 forbidden.
	FaultForbidden
	// FaultQuotaExceeded answers 403 quotaExceeded.
	FaultQuotaExceeded
	// FaultNotFound answers 404 notFound, as a spurious answer for a resource that exists.
	FaultNotFound
	// FaultServerError answers 500 backendError.
	FaultServerError
	// FaultBadRequest answers 400 invalid.
	FaultBadRequest
	// FaultDropAfterWrite applies the request, then closes the connection without an answer: the client cannot
	// know whether the write happened.
	FaultDropAfterWrite
)

// FakeEventTime is the start or end of an event.
type FakeEventTime struct {
	DateTime string `json:"dateTime,omitempty"`
	TimeZone string `json:"timeZone,omitempty"`
}

// FakeCalendarEvent is an event as the fake stores it.
type FakeCalendarEvent struct {
	ID          string
	Summary     string
	Description string
	Status      string
	Start, End  FakeEventTime
	Private     map[string]string
}

// FakeCalendarInfo is a calendar the app created.
type FakeCalendarInfo struct {
	ID, Summary, Description, TimeZone string
}

// FakeCalendarRequest is one request the fake got.
type FakeCalendarRequest struct {
	Method string
	Path   string // unescaped, below the base URL, e.g. /calendars/<id>/events/<event id>
}

// FakeGoogleCalendar is a local Calendar API v3 server for the googlecal tests (TASK-051). It implements the subset the
// adapter uses with the rules of docs/refs/google-calendar.md: calendars insert/get/patch, calendarList get, events
// insert/get/update/delete; client event ids of base32hex characters a-v0-9 and length 5..1024, unique per calendar;
// private properties with a key over 44 characters dropped and a value over 1024 cut, silently. Behavior the docs do
// not state is the fake's choice: insert of an existing id answers 409 also when the event is deleted; update restores
// a cancelled event unless KeepCancelledOnUpdate; delete answers 204, and 410 for a deleted event. A token with
// calendar.app.created sees only the calendars the app created: every other calendar id ("primary" included) answers
// 404. It checks the bearer token and records every request.
type FakeGoogleCalendar struct {
	srv *httptest.Server

	mu            sync.Mutex
	accept        func(token string) bool
	keepCancelled bool
	faults        []fakeCalendarFault

	seq       int
	calendars map[string]*fakeCalendar
	order     []string // calendar ids in creation order
	requests  []FakeCalendarRequest
	foreign   int // requests that named a calendar the app did not create
}

type fakeCalendar struct {
	info    FakeCalendarInfo
	deleted bool
	events  map[string]*FakeCalendarEvent
}

type fakeCalendarFault struct {
	fault  CalendarFault
	method string // "" matches any method
}

// StartFakeGoogleCalendar serves a fake Calendar API until the test ends. It accepts FakeCalendarAccessToken.
func StartFakeGoogleCalendar(t testing.TB) *FakeGoogleCalendar {
	t.Helper()
	f := &FakeGoogleCalendar{
		accept:    func(token string) bool { return token == FakeCalendarAccessToken },
		calendars: map[string]*fakeCalendar{},
	}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

// BaseURL is the API base, the counterpart of https://www.googleapis.com/calendar/v3.
func (f *FakeGoogleCalendar) BaseURL() string { return f.srv.URL + fakeCalendarAPIPath }

// AcceptTokens replaces the bearer token check (for example FakeGoogleOAuth.IsLiveAccessToken).
func (f *FakeGoogleCalendar) AcceptTokens(accept func(token string) bool) {
	f.set(func() { f.accept = accept })
}

// KeepCancelledOnUpdate makes an update of a cancelled event apply the body but leave the event cancelled, as Google
// might: the docs do not state that an update restores a deleted event.
func (f *FakeGoogleCalendar) KeepCancelledOnUpdate(keep bool) {
	f.set(func() { f.keepCancelled = keep })
}

// FailNext queues a fault for the next request with the method ("" for any). Faults apply in order.
func (f *FakeGoogleCalendar) FailNext(fault CalendarFault, method string) {
	f.set(func() { f.faults = append(f.faults, fakeCalendarFault{fault: fault, method: method}) })
}

// AddCalendar creates an app calendar outside the API, as one an earlier installation created.
func (f *FakeGoogleCalendar) AddCalendar(summary, description string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.newCalendar(summary, description, "")
}

// DeleteCalendar deletes an app calendar, as the user does in Google Calendar.
func (f *FakeGoogleCalendar) DeleteCalendar(id string) {
	f.set(func() {
		if c := f.calendars[id]; c != nil {
			c.deleted = true
		}
	})
}

// CancelEvent deletes an event, as the user does in Google Calendar: it stays with status cancelled.
func (f *FakeGoogleCalendar) CancelEvent(calendarID, eventID string) {
	f.set(func() {
		if c := f.calendars[calendarID]; c != nil {
			if e := c.events[eventID]; e != nil {
				e.Status = FakeEventCancelled
			}
		}
	})
}

// Calendars returns the live calendars the app created, in creation order.
func (f *FakeGoogleCalendar) Calendars() []FakeCalendarInfo {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []FakeCalendarInfo
	for _, id := range f.order {
		if c := f.calendars[id]; !c.deleted {
			out = append(out, c.info)
		}
	}
	return out
}

// Events returns the events of a calendar that are not cancelled, sorted by id.
func (f *FakeGoogleCalendar) Events(calendarID string) []FakeCalendarEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := f.calendars[calendarID]
	if c == nil {
		return nil
	}
	var out []FakeCalendarEvent
	for _, id := range slices.Sorted(maps.Keys(c.events)) {
		if e := c.events[id]; e.Status != FakeEventCancelled {
			out = append(out, calCloneEvent(e))
		}
	}
	return out
}

// Event returns an event of a calendar, also a cancelled one.
func (f *FakeGoogleCalendar) Event(calendarID, eventID string) (FakeCalendarEvent, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c := f.calendars[calendarID]; c != nil {
		if e := c.events[eventID]; e != nil {
			return calCloneEvent(e), true
		}
	}
	return FakeCalendarEvent{}, false
}

// Requests returns every request, in order.
func (f *FakeGoogleCalendar) Requests() []FakeCalendarRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]FakeCalendarRequest(nil), f.requests...)
}

// ForeignRequests counts the requests that named a calendar the app did not create.
func (f *FakeGoogleCalendar) ForeignRequests() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.foreign
}

func (f *FakeGoogleCalendar) set(fn func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn()
}

func (f *FakeGoogleCalendar) newCalendar(summary, description, zone string) string {
	f.seq++
	id := fmt.Sprintf("c%d.fake@group.calendar.google.com", f.seq)
	f.calendars[id] = &fakeCalendar{
		info:   FakeCalendarInfo{ID: id, Summary: summary, Description: description, TimeZone: zone},
		events: map[string]*FakeCalendarEvent{},
	}
	f.order = append(f.order, id)
	return id
}

// fakeAnswer is what a route produces; the handler writes it after the lock is released.
type fakeAnswer struct {
	status     int
	body       any
	reason     string // error reason; "" for a success
	retryAfter string
}

func (f *FakeGoogleCalendar) serve(w http.ResponseWriter, r *http.Request) {
	segments, ok := fakeSegments(r)
	f.mu.Lock()
	f.requests = append(f.requests, FakeCalendarRequest{Method: r.Method, Path: "/" + strings.Join(segments, "/")})
	token, bearer := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	accept := f.accept
	fault := f.takeFault(r.Method)
	f.mu.Unlock()

	switch {
	case !ok:
		writeFakeAnswer(w, fakeAnswer{status: http.StatusNotFound, reason: "notFound"})
		return
	case !bearer || !accept(token):
		writeFakeAnswer(w, fakeAnswer{status: http.StatusUnauthorized, reason: "authError"})
		return
	}
	if a, injected := calFaultAnswer(fault); injected {
		writeFakeAnswer(w, a)
		return
	}

	var payload map[string]any
	if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch {
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, fakeCalendarMaxBody))
		if err := dec.Decode(&payload); err != nil {
			writeFakeAnswer(w, fakeAnswer{status: http.StatusBadRequest, reason: "parseError"})
			return
		}
	}

	f.mu.Lock()
	a := f.route(r.Method, segments, payload)
	f.mu.Unlock()

	if fault == FaultDropAfterWrite {
		calDrop(w)
		return
	}
	writeFakeAnswer(w, a)
}

// takeFault removes and returns the first queued fault for method, or 0.
func (f *FakeGoogleCalendar) takeFault(method string) CalendarFault {
	for i, q := range f.faults {
		if q.method == "" || q.method == method {
			f.faults = slices.Delete(f.faults, i, i+1)
			return q.fault
		}
	}
	return 0
}

func calFaultAnswer(fault CalendarFault) (fakeAnswer, bool) {
	switch fault {
	case FaultUnauthorized:
		return fakeAnswer{status: http.StatusUnauthorized, reason: "authError"}, true
	case FaultRateLimited:
		return fakeAnswer{status: http.StatusTooManyRequests, reason: "rateLimitExceeded", retryAfter: "7"}, true
	case FaultUserRateLimit:
		return fakeAnswer{status: http.StatusForbidden, reason: "userRateLimitExceeded"}, true
	case FaultForbidden:
		return fakeAnswer{status: http.StatusForbidden, reason: "forbidden"}, true
	case FaultQuotaExceeded:
		return fakeAnswer{status: http.StatusForbidden, reason: "quotaExceeded"}, true
	case FaultNotFound:
		return fakeAnswer{status: http.StatusNotFound, reason: "notFound"}, true
	case FaultServerError:
		return fakeAnswer{status: http.StatusInternalServerError, reason: "backendError"}, true
	case FaultBadRequest:
		return fakeAnswer{status: http.StatusBadRequest, reason: "invalid"}, true
	}
	return fakeAnswer{}, false
}

// fakeSegments returns the unescaped path segments below the base path.
func fakeSegments(r *http.Request) ([]string, bool) {
	rest, ok := strings.CutPrefix(r.URL.EscapedPath(), fakeCalendarAPIPath+"/")
	if !ok {
		return nil, false
	}
	parts := strings.Split(rest, "/")
	for i, p := range parts {
		s, err := url.PathUnescape(p)
		if err != nil || s == "" {
			return nil, false
		}
		parts[i] = s
	}
	return parts, true
}

// route runs one request with f.mu held.
func (f *FakeGoogleCalendar) route(method string, seg []string, payload map[string]any) fakeAnswer {
	switch {
	case len(seg) == 1 && seg[0] == "calendars" && method == http.MethodPost:
		return f.insertCalendar(payload)
	case len(seg) == 4 && seg[0] == "users" && seg[1] == "me" && seg[2] == "calendarList" && method == http.MethodGet:
		return f.calendarListEntry(seg[3])
	case len(seg) >= 2 && seg[0] == "calendars":
		c := f.calendars[seg[1]]
		if c == nil || c.deleted {
			if c == nil {
				f.foreign++
			}
			return fakeAnswer{status: http.StatusNotFound, reason: "notFound"}
		}
		switch {
		case len(seg) == 2 && method == http.MethodGet:
			return fakeAnswer{status: http.StatusOK, body: calJSON(c.info)}
		case len(seg) == 2 && method == http.MethodPatch:
			return patchCalendar(c, payload)
		case len(seg) == 3 && seg[2] == "events" && method == http.MethodPost:
			return calInsertEvent(c, payload)
		case len(seg) == 4 && seg[2] == "events":
			return calEventRequest(c, method, seg[3], payload, f.keepCancelled)
		}
	}
	return fakeAnswer{status: http.StatusNotFound, reason: "notFound"}
}

func (f *FakeGoogleCalendar) insertCalendar(p map[string]any) fakeAnswer {
	summary := calStr(p, "summary")
	if summary == "" {
		return fakeAnswer{status: http.StatusBadRequest, reason: "required"}
	}
	id := f.newCalendar(summary, calStr(p, "description"), calStr(p, "timeZone"))
	return fakeAnswer{status: http.StatusOK, body: calJSON(f.calendars[id].info)}
}

// patchCalendar is calendars.patch: only the fields sent change; an empty summary is refused, as on insert.
func patchCalendar(c *fakeCalendar, p map[string]any) fakeAnswer {
	if v, ok := p["summary"]; ok {
		s, _ := v.(string)
		if s == "" {
			return fakeAnswer{status: http.StatusBadRequest, reason: "required"}
		}
		c.info.Summary = s
	}
	if v, ok := p["description"]; ok {
		c.info.Description, _ = v.(string)
	}
	return fakeAnswer{status: http.StatusOK, body: calJSON(c.info)}
}

// calendarListEntry is calendarList.get: the entry of a live calendar the app created, else 404.
func (f *FakeGoogleCalendar) calendarListEntry(id string) fakeAnswer {
	c := f.calendars[id]
	if c == nil || c.deleted {
		if c == nil {
			f.foreign++
		}
		return fakeAnswer{status: http.StatusNotFound, reason: "notFound"}
	}
	entry := calJSON(c.info)
	entry["kind"], entry["accessRole"] = "calendar#calendarListEntry", "owner"
	return fakeAnswer{status: http.StatusOK, body: entry}
}

func calInsertEvent(c *fakeCalendar, p map[string]any) fakeAnswer {
	id := calStr(p, "id")
	if id == "" {
		id = fmt.Sprintf("gen%d", len(c.events)+1)
	} else if !validFakeEventID(id) {
		return fakeAnswer{status: http.StatusBadRequest, reason: "invalid"}
	}
	if _, exists := c.events[id]; exists {
		return fakeAnswer{status: http.StatusConflict, reason: "duplicate"} // also for a cancelled event
	}
	e, ok := calEventFrom(id, p)
	if !ok {
		return fakeAnswer{status: http.StatusBadRequest, reason: "invalid"}
	}
	c.events[id] = e
	return fakeAnswer{status: http.StatusOK, body: calEventJSON(e)}
}

func calEventRequest(c *fakeCalendar, method, id string, p map[string]any, keepCancelled bool) fakeAnswer {
	e := c.events[id]
	if e == nil {
		return fakeAnswer{status: http.StatusNotFound, reason: "notFound"}
	}
	switch method {
	case http.MethodGet:
		return fakeAnswer{status: http.StatusOK, body: calEventJSON(e)}
	case http.MethodPut:
		next, ok := calEventFrom(id, p)
		if !ok {
			return fakeAnswer{status: http.StatusBadRequest, reason: "invalid"}
		}
		if keepCancelled && e.Status == FakeEventCancelled {
			next.Status = FakeEventCancelled
		}
		c.events[id] = next
		return fakeAnswer{status: http.StatusOK, body: calEventJSON(next)}
	case http.MethodDelete:
		if e.Status == FakeEventCancelled {
			return fakeAnswer{status: http.StatusGone, reason: "deleted"}
		}
		e.Status = FakeEventCancelled
		return fakeAnswer{status: http.StatusNoContent}
	}
	return fakeAnswer{status: http.StatusNotFound, reason: "notFound"}
}

// eventFrom reads an event body: start and end need an RFC 3339 dateTime, end not before start.
func calEventFrom(id string, p map[string]any) (*FakeCalendarEvent, bool) {
	e := &FakeCalendarEvent{
		ID: id, Summary: calStr(p, "summary"), Description: calStr(p, "description"), Status: calStr(p, "status"),
		Start: calEventTime(p["start"]), End: calEventTime(p["end"]), Private: map[string]string{},
	}
	if e.Status == "" {
		e.Status = FakeEventConfirmed
	}
	start, err1 := time.Parse(time.RFC3339, e.Start.DateTime)
	end, err2 := time.Parse(time.RFC3339, e.End.DateTime)
	if err1 != nil || err2 != nil || end.Before(start) {
		return nil, false
	}
	if props, ok := p["extendedProperties"].(map[string]any); ok {
		if private, ok := props["private"].(map[string]any); ok {
			for k, v := range private {
				s, ok := v.(string)
				if !ok {
					return nil, false
				}
				if utf8.RuneCountInString(k) > fakePropertyKeyMax {
					continue
				}
				if r := []rune(s); len(r) > fakePropertyValueMax {
					s = string(r[:fakePropertyValueMax])
				}
				e.Private[k] = s
			}
		}
	}
	return e, true
}

func calEventTime(v any) FakeEventTime {
	m, _ := v.(map[string]any)
	return FakeEventTime{DateTime: calStr(m, "dateTime"), TimeZone: calStr(m, "timeZone")}
}

// validFakeEventID is the documented rule: base32hex characters (a-v, 0-9), length 5..1024.
func validFakeEventID(id string) bool {
	if len(id) < fakeEventIDMin || len(id) > fakeEventIDMax {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if (c < 'a' || c > 'v') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

func calStr(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

func calJSON(info FakeCalendarInfo) map[string]any {
	out := map[string]any{"kind": "calendar#calendar", "id": info.ID, "summary": info.Summary, "description": info.Description}
	if info.TimeZone != "" {
		out["timeZone"] = info.TimeZone
	}
	return out
}

func calEventJSON(e *FakeCalendarEvent) map[string]any {
	return map[string]any{
		"kind": "calendar#event", "id": e.ID, "status": e.Status, "summary": e.Summary, "description": e.Description,
		"start": e.Start, "end": e.End, "extendedProperties": map[string]any{"private": e.Private},
	}
}

func calCloneEvent(e *FakeCalendarEvent) FakeCalendarEvent {
	out := *e
	out.Private = maps.Clone(e.Private)
	return out
}

// writeFakeAnswer writes a success body or the error format of the Calendar API.
func writeFakeAnswer(w http.ResponseWriter, a fakeAnswer) {
	if a.retryAfter != "" {
		w.Header().Set("Retry-After", a.retryAfter)
	}
	if a.status == http.StatusNoContent {
		w.WriteHeader(a.status)
		return
	}
	body := a.body
	if a.reason != "" {
		msg := "fake google calendar: " + a.reason
		body = map[string]any{"error": map[string]any{
			"code": a.status, "message": msg,
			"errors": []any{map[string]any{"domain": "global", "reason": a.reason, "message": msg}},
		}}
	}
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.WriteHeader(a.status)
	_ = json.NewEncoder(w).Encode(body)
}

// dropConnection closes the connection without an answer.
func calDrop(w http.ResponseWriter) {
	if hj, ok := w.(http.Hijacker); ok {
		if conn, _, err := hj.Hijack(); err == nil {
			_ = conn.Close()
			return
		}
	}
	panic(http.ErrAbortHandler)
}
