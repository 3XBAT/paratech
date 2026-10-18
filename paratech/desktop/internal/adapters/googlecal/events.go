package googlecal

import (
	"context"
	"crypto/sha256"
	"encoding/base32"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
)

// Event content written by the adapter.
const (
	appMarker = "paratech"

	kindLesson   = "lesson"
	kindDeadline = "deadline"

	lessonDescription   = "Занятие из расписания paratech. Изменения делайте в приложении."
	deadlineDescription = "Дедлайн из paratech. Изменения делайте в приложении."
	lessonFallback      = "Занятие"
	deadlineFallback    = "Дедлайн"

	// maxSummaryRunes caps an event title; longer titles are cut.
	maxSummaryRunes = 200
	// deadlineLength is the length of a deadline event: it starts at the due time. The docs do not state that a
	// zero-length event is accepted (docs/refs/google-calendar.md).
	deadlineLength = 15 * time.Minute

	statusConfirmed = "confirmed"
	statusCancelled = "cancelled"

	// Limits of extendedProperties.private: Google drops a longer key and cuts a longer value without an error.
	maxPropertyKey   = 44
	maxPropertyValue = 1024
)

// eventIDEncoding is base32hex in lower case without padding: exactly the characters Google allows (a-v, 0-9).
var eventIDEncoding = base32.NewEncoding("0123456789abcdefghijklmnopqrstuv").WithPadding(base32.NoPadding)

// eventID is the Google event id of a lesson or a deadline: base32hex of SHA-256 over "lesson:<id>" or
// "deadline:<id>", 52 characters. The namespaces keep a lesson and an assignment with equal ids apart.
func eventID(kind, id string) string {
	sum := sha256.Sum256([]byte(kind + ":" + id))
	return eventIDEncoding.EncodeToString(sum[:])
}

// LessonEventID is the event id of a lesson.
func LessonEventID(id shared.LessonID) string { return eventID(kindLesson, id.String()) }

// DeadlineEventID is the event id of an assignment deadline.
func DeadlineEventID(id shared.AssignmentID) string { return eventID(kindDeadline, id.String()) }

// eventTime is the start or end of an event.
type eventTime struct {
	DateTime string `json:"dateTime"`
	TimeZone string `json:"timeZone"`
}

type extendedProperties struct {
	Private map[string]string `json:"private"`
}

// fits reports whether every private property stays within Google's limits (bytes, so at most as many characters).
func (p extendedProperties) fits() bool {
	for k, v := range p.Private {
		if len(k) > maxPropertyKey || len(v) > maxPropertyValue {
			return false
		}
	}
	return true
}

// event is the part of an Events resource the adapter writes.
type event struct {
	ID                 string             `json:"id,omitempty"`
	Summary            string             `json:"summary"`
	Description        string             `json:"description"`
	Start              eventTime          `json:"start"`
	End                eventTime          `json:"end"`
	Status             string             `json:"status"`
	ExtendedProperties extendedProperties `json:"extendedProperties"`
}

// writtenEvent is the part of the answer to an update the adapter checks.
type writtenEvent struct {
	Status string `json:"status"`
}

// UpsertLesson writes the event of a live lesson into the app calendar (schedulesync.CalendarAdapter).
func (c *Client) UpsertLesson(ctx context.Context, calendarID string, l *lesson.Lesson) error {
	if l == nil || l.Deleted() || l.ID().Validate() != nil {
		return permanent(opUpsertLesson, ErrInvalidRequest)
	}
	content := l.Content()
	if content.Validate() != nil {
		return permanent(opUpsertLesson, ErrInvalidRequest)
	}
	if err := c.checkCalendar(ctx, opUpsertLesson, calendarID); err != nil {
		return err
	}
	e := event{
		Summary: summary(content.Title, lessonFallback), Description: lessonDescription, Status: statusConfirmed,
		Start: eventTime{DateTime: rfc3339(content.Start), TimeZone: content.TimeZone},
		End:   eventTime{DateTime: rfc3339(content.End), TimeZone: content.TimeZone},
		ExtendedProperties: extendedProperties{Private: map[string]string{
			"app": appMarker, "kind": kindLesson, "lessonId": l.ID().String(),
			"revision": strconv.FormatInt(l.ContentRevision(), 10),
		}},
	}
	return c.upsert(ctx, opUpsertLesson, calendarID, LessonEventID(l.ID()), e)
}

// DeleteEvent removes the event of a lesson (schedulesync.CalendarAdapter); any 2xx, a missing (404) or an already
// deleted (410) event is success.
func (c *Client) DeleteEvent(ctx context.Context, calendarID string, id shared.LessonID) error {
	if id.Validate() != nil {
		return permanent(opDeleteEvent, ErrInvalidRequest)
	}
	if err := c.checkCalendar(ctx, opDeleteEvent, calendarID); err != nil {
		return err
	}
	_, err := c.do(ctx, opDeleteEvent, request{
		method: http.MethodDelete, path: calendarPath(calendarID, "events", LessonEventID(id)), accept: []int{404, 410},
	})
	return err
}

// UpsertDeadline writes the deadline of a confirmed proposal into the app calendar, creating the calendar if needed
// (homeworkflow.DeadlineCalendar). The event is keyed by the assignment; the proposal key is kept as a private
// property. A calendar created here stays fresh, so the next schedule synchronization rewrites the lessons.
func (c *Client) UpsertDeadline(ctx context.Context, cmd proposal.CalendarCommand) error {
	if cmd.IsZero() || cmd.AssignmentID().Validate() != nil || cmd.Due().IsZero() {
		return permanent(opUpsertDeadline, ErrInvalidRequest)
	}
	c.mu.Lock()
	b, err := c.ensure(ctx, opUpsertDeadline)
	c.mu.Unlock()
	if err != nil {
		return err
	}
	due := cmd.Due().UTC()
	e := event{
		Summary: summary(cmd.Title(), deadlineFallback), Description: deadlineDescription, Status: statusConfirmed,
		Start: eventTime{DateTime: rfc3339(due), TimeZone: "UTC"},
		End:   eventTime{DateTime: rfc3339(due.Add(deadlineLength)), TimeZone: "UTC"},
		ExtendedProperties: extendedProperties{Private: map[string]string{
			"app": appMarker, "kind": kindDeadline, "assignmentId": cmd.AssignmentID().String(),
			"proposalKey": cmd.IdempotencyKey(),
		}},
	}
	return c.upsert(ctx, opUpsertDeadline, b.ID, DeadlineEventID(cmd.AssignmentID()), e)
}

// upsert writes e under eventID: update; 404 or 410 → insert with the id; 409 (taken, maybe by a deleted event) →
// update once more. A lost answer is safe to retry: the next try updates.
func (c *Client) upsert(ctx context.Context, op, calendarID, eventID string, e event) error {
	if !e.ExtendedProperties.fits() {
		return permanent(op, ErrInvalidRequest)
	}
	path := calendarPath(calendarID, "events", eventID)
	status, err := c.update(ctx, op, path, e)
	if err != nil || status < 300 {
		return err
	}

	insert := e
	insert.ID = eventID
	status, err = c.do(ctx, op, request{
		method: http.MethodPost, path: calendarPath(calendarID, "events"), body: insert, accept: []int{409},
	})
	if err != nil || status < 300 {
		return err
	}

	status, err = c.update(ctx, op, path, e)
	if err != nil {
		return err
	}
	if status >= 300 {
		return transient(op, ErrConflict) // taken but not updatable: a race, or Google keeps a deleted id
	}
	return nil
}

// update replaces the event at path; 404 and 410 are returned as statuses. Restoring a deleted event by an update is
// not documented, so an answer that is still cancelled fails: the item stays pending instead of synced and invisible.
func (c *Client) update(ctx context.Context, op, path string, e event) (int, error) {
	var got writtenEvent
	status, err := c.do(ctx, op, request{method: http.MethodPut, path: path, body: e, out: &got, accept: []int{404, 410}})
	if err == nil && status < 300 && got.Status == statusCancelled {
		return 0, transient(op, ErrNotRestored)
	}
	return status, err
}

func rfc3339(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// summary is the event title of text: control characters, line and paragraph separators and bidi controls removed
// or turned into spaces, spaces collapsed, at most maxSummaryRunes; fallback when nothing is left. A title is data
// from an import or the model and must not reorder or break the calendar's display.
func summary(text, fallback string) string {
	text = strings.ToValidUTF8(text, "")
	var b strings.Builder
	for _, r := range text {
		switch {
		case isBidiControl(r):
		case unicode.IsControl(r), r == '\u2028', r == '\u2029':
			b.WriteRune(' ')
		default:
			b.WriteRune(r)
		}
	}
	out := strings.Join(strings.Fields(b.String()), " ")
	if utf8.RuneCountInString(out) > maxSummaryRunes {
		out = strings.TrimSpace(string([]rune(out)[:maxSummaryRunes]))
	}
	if out == "" {
		return fallback
	}
	return out
}

// isBidiControl reports the bidirectional formatting characters: LRM, RLM, ALM, the embeddings and overrides and
// the isolates.
func isBidiControl(r rune) bool {
	return r == '\u200E' || r == '\u200F' || r == '\u061C' || (r >= '\u202A' && r <= '\u202E') ||
		(r >= '\u2066' && r <= '\u2069')
}
