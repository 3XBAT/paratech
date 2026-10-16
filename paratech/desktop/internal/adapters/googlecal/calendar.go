package googlecal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"paratech/desktop/internal/domain/shared"
)

// The app calendar.
const (
	CalendarSummary     = "Learning Assistant"
	calendarMarker      = "[paratech-calendar-v1]"
	calendarDescription = "Расписание занятий и дедлайны из приложения Learning Assistant. " + calendarMarker

	// maxCalendarIDBytes bounds a calendar id read from Google or the store.
	maxCalendarIDBytes = 1024
)

// binding is the stored calendar of this installation. Fresh stays true until the core confirms that it marked every
// lesson for a rewrite into the calendar.
type binding struct {
	ID    string `json:"id"`
	Fresh bool   `json:"fresh"`
}

// calendarResource is the part of a Calendar resource the adapter reads or sends.
type calendarResource struct {
	ID          string `json:"id,omitempty"`
	Summary     string `json:"summary"`
	Description string `json:"description"`
}

// EnsureCalendar returns the app calendar, creating it when needed (schedulesync.CalendarAdapter). The calendar id in
// the credential store is the only binding: calendarList.list is not allowed with calendar.app.created, so the adapter
// cannot look a calendar up. The stored calendar is replaced only when it is gone (see gone). fresh is true for a
// calendar new to this installation and stays true until ConfirmCalendar.
//
// Crash window: Google creates the calendar before its id is stored. If the app stops in between (or the answer is
// lost), the next call creates another calendar; the first one stays empty and is never written (the user can delete
// it). Accepted: finding it again would need a wider scope.
func (c *Client) EnsureCalendar(ctx context.Context) (calendarID string, fresh bool, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, err := c.ensure(ctx, opEnsure)
	if err != nil {
		return "", false, err
	}
	return b.ID, b.Fresh, nil
}

// ConfirmCalendar clears fresh when calendarID is still the stored calendar (schedulesync.CalendarAdapter).
func (c *Client) ConfirmCalendar(ctx context.Context, calendarID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, ok, err := c.load(ctx, opConfirm)
	if err != nil || !ok || b.ID != calendarID || !b.Fresh {
		return err
	}
	return c.save(ctx, opConfirm, binding{ID: b.ID})
}

// ForgetCalendar removes the stored calendar id. The calendar at Google stays, and the next EnsureCalendar creates a new
// one. A sign-out need not call it: with the id kept, the same account gets its calendar back and another account's
// sign-in finds it gone.
func (c *Client) ForgetCalendar(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.store.Delete(ctx, CalendarIDName); err != nil {
		return transient(opForget, ErrCredentials)
	}
	return nil
}

// ensure is EnsureCalendar with c.mu held.
func (c *Client) ensure(ctx context.Context, op string) (binding, error) {
	b, ok, err := c.load(ctx, op)
	if err != nil {
		return binding{}, err
	}
	if ok {
		gone, err := c.gone(ctx, op, b.ID)
		if err != nil {
			return binding{}, err
		}
		if !gone {
			return b, nil
		}
	}

	// Write the old entry back first: a store that cannot keep the new id fails here, before Google creates a
	// calendar nobody could find again (one per attempt otherwise).
	if err := c.save(ctx, op, b); err != nil {
		return binding{}, err
	}
	id, err := c.create(ctx, op)
	if err != nil {
		return binding{}, err
	}
	b = binding{ID: id, Fresh: true}
	if err := c.save(ctx, op, b); err != nil {
		return binding{}, err
	}
	return b, nil
}

// load reads the stored binding; a missing or unreadable entry is no binding.
func (c *Client) load(ctx context.Context, op string) (binding, bool, error) {
	raw, err := c.store.Get(ctx, CalendarIDName)
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return binding{}, false, nil
	case err != nil:
		if ctx.Err() != nil {
			return binding{}, false, fmt.Errorf("%s: %w", op, ctx.Err())
		}
		return binding{}, false, transient(op, ErrCredentials)
	}
	var b binding
	if json.Unmarshal([]byte(raw), &b) != nil || !validCalendarID(b.ID) {
		return binding{}, false, nil
	}
	return b, true, nil
}

func (c *Client) save(ctx context.Context, op string, b binding) error {
	raw, err := json.Marshal(b)
	if err != nil {
		return permanent(op, ErrInvalidRequest)
	}
	// The calendar exists at Google now; storing it must not depend on the caller giving up.
	if err := c.store.Set(context.WithoutCancel(ctx), CalendarIDName, string(raw)); err != nil {
		return transient(op, ErrCredentials)
	}
	return nil
}

// gone reports whether the stored calendar is gone for this sign-in: calendars.get AND calendarList.get both answer
// 404, 410 or 403, so one spurious 404 (the errors guide advises a backoff for it) never makes a second calendar. A
// 403 that is not a rate limit (those are errors) counts too: the stored calendar of another Google account may answer
// it. Alone it could mean anything, so it needs the other check to agree; a sign-in that may not use calendars at all
// is refused by calendars.insert as well. When the checks disagree the call fails transiently and nothing is created.
func (c *Client) gone(ctx context.Context, op, id string) (bool, error) {
	absent := []int{http.StatusNotFound, http.StatusGone, http.StatusForbidden}
	status, err := c.do(ctx, op, request{method: http.MethodGet, path: calendarPath(id), accept: absent})
	if err != nil || status < 300 {
		return false, err
	}
	status, err = c.do(ctx, op, request{
		method: http.MethodGet, path: "/users/me/calendarList/" + url.PathEscape(id), accept: absent,
	})
	switch {
	case err != nil:
		return false, err
	case status < 300:
		return false, transient(op, ErrUnavailable)
	}
	return true, nil
}

func (c *Client) create(ctx context.Context, op string) (string, error) {
	var out calendarResource
	_, err := c.do(ctx, op, request{
		method: http.MethodPost, path: "/calendars",
		body: calendarResource{Summary: CalendarSummary, Description: calendarDescription}, out: &out,
	})
	if err != nil {
		return "", err
	}
	if !validCalendarID(out.ID) {
		return "", transient(op, ErrBadResponse)
	}
	return out.ID, nil
}

// validCalendarID bounds an id from Google or the store: printable text without spaces.
func validCalendarID(id string) bool {
	return id != "" && len(id) <= maxCalendarIDBytes && utf8.ValidString(id) &&
		!strings.ContainsFunc(id, func(r rune) bool { return unicode.IsSpace(r) || !unicode.IsPrint(r) })
}

// checkCalendar refuses a calendar id that is not the stored app calendar: nothing is written anywhere else (AC-17).
func (c *Client) checkCalendar(ctx context.Context, op, calendarID string) error {
	if !validCalendarID(calendarID) {
		return permanent(op, ErrInvalidRequest)
	}
	c.mu.Lock()
	b, ok, err := c.load(ctx, op)
	c.mu.Unlock()
	switch {
	case err != nil:
		return err
	case !ok || b.ID != calendarID:
		// Transient: a parallel EnsureCalendar may have replaced a gone calendar; the next attempt ensures again.
		return transient(op, ErrForeignCalendar)
	}
	return nil
}
