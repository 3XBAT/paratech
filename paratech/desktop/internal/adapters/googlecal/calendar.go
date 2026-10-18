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
	CalendarSummary     = "paratech"
	calendarMarker      = "[paratech-calendar-v1]"
	calendarDescription = "Расписание занятий и дедлайны из приложения paratech. " + calendarMarker

	// LegacyCalendarSummary is the name the app gave its calendar before the rename (ADR-0015). A stored calendar
	// with exactly this name is renamed; any other name is the user's choice and stays.
	LegacyCalendarSummary = "Learning Assistant"

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
		current, gone, err := c.gone(ctx, op, b.ID)
		if err != nil {
			return binding{}, err
		}
		if !gone {
			if current.Summary == LegacyCalendarSummary && c.renameRefused != b.ID {
				// Best effort: a failure keeps the old name, it never fails the sync. A transient one is retried at the
				// next call; a refusal is not, so a calendar the user may not edit costs one request per process.
				if err := c.rename(ctx, op, b.ID); errors.Is(err, ErrForbidden) || errors.Is(err, ErrRejected) {
					c.renameRefused = b.ID
				}
			}
			return b, nil
		}
	}

	// After a calendar whose id could not be stored, the store must take a write (the old entry back) before Google
	// creates another one nobody could find again: one orphan per failure, not one per attempt.
	if c.unsaved {
		if err := c.save(ctx, op, b); err != nil {
			return binding{}, err
		}
		c.unsaved = false
	}
	id, err := c.create(ctx, op)
	if err != nil {
		return binding{}, err
	}
	b = binding{ID: id, Fresh: true}
	if err := c.save(ctx, op, b); err != nil {
		c.unsaved = true
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
// A calendar that is not gone comes back as calendars.get answered it; a body that does not decode leaves it empty
// (only the rename reads it).
func (c *Client) gone(ctx context.Context, op, id string) (calendarResource, bool, error) {
	absent := []int{http.StatusNotFound, http.StatusGone, http.StatusForbidden}
	var current calendarResource
	status, err := c.do(ctx, op, request{
		method: http.MethodGet, path: calendarPath(id), out: &current, lenient: true, accept: absent,
	})
	if err != nil || status < 300 {
		return current, false, err
	}
	status, err = c.do(ctx, op, request{
		method: http.MethodGet, path: "/users/me/calendarList/" + url.PathEscape(id), accept: absent,
	})
	switch {
	case err != nil:
		return calendarResource{}, false, err
	case status < 300:
		return calendarResource{}, false, transient(op, ErrUnavailable)
	}
	return calendarResource{}, true, nil
}

// rename gives the stored calendar the current name and description (calendars.patch): the same calendar, so its
// events and the binding stay.
func (c *Client) rename(ctx context.Context, op, id string) error {
	_, err := c.do(ctx, op, request{
		method: http.MethodPatch, path: calendarPath(id),
		body: calendarResource{Summary: CalendarSummary, Description: calendarDescription},
	})
	return err
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
