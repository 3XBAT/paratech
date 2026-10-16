package googlecal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"paratech/desktop/internal/domain/shared"
)

// The app calendar. It is found again at Google by the summary and the marker in the description.
const (
	CalendarSummary     = "Learning Assistant"
	calendarMarker      = "[paratech-calendar-v1]"
	calendarDescription = "Расписание занятий и дедлайны из приложения Learning Assistant. " + calendarMarker

	// maxCalendarIDBytes bounds a calendar id read from Google or the store.
	maxCalendarIDBytes = 1024
	// listPageSize and maxListPages bound the lookup in calendarList.
	listPageSize = 250
	maxListPages = 20
)

// binding is the stored calendar of this installation. Fresh stays true until the core confirms that it marked every
// lesson for a rewrite into the calendar.
type binding struct {
	ID    string `json:"id"`
	Fresh bool   `json:"fresh"`
}

// calendarResource is the part of a Calendar or CalendarListEntry resource the adapter reads or sends.
type calendarResource struct {
	ID          string `json:"id,omitempty"`
	Summary     string `json:"summary"`
	Description string `json:"description"`
	AccessRole  string `json:"accessRole,omitempty"`
}

type calendarList struct {
	Items         []calendarResource `json:"items"`
	NextPageToken string             `json:"nextPageToken"`
}

// EnsureCalendar returns the app calendar, creating it when needed (schedulesync.CalendarAdapter). The stored calendar
// is checked with calendars.get; when it is gone (404, 410, or 403 for a calendar this sign-in cannot see, e.g. another
// Google account) the adapter looks for an app calendar in calendarList and otherwise creates one. fresh is true for a
// calendar new to this installation and stays true until ConfirmCalendar.
//
// Crash window: Google creates the calendar before its id is stored. If the app stops in between (or the answer is
// lost), the next call finds that calendar in calendarList by its summary and marker and reuses it. If Google refuses
// the list for the scope (not verified, docs/refs/google-calendar.md), a second, empty calendar is created; the first
// one stays unused and is never written.
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

// ForgetCalendar removes the stored calendar id (at sign-out). The calendar at Google stays: a later sign-in finds it
// again, or creates a new one for another account.
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
		live, err := c.exists(ctx, op, b.ID)
		if err != nil {
			return binding{}, err
		}
		if live {
			return b, nil
		}
	}

	id, err := c.lookup(ctx, op)
	if err != nil {
		return binding{}, err
	}
	if id == "" {
		if id, err = c.create(ctx, op); err != nil {
			return binding{}, err
		}
	}
	b = binding{ID: id, Fresh: true}
	if err := c.save(ctx, op, b); err != nil {
		return binding{}, err
	}
	return b, nil
}

// load reads the stored binding; a missing or unreadable entry is no binding (the lookup finds the calendar again).
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

// exists reports whether the stored calendar is still there for this sign-in.
func (c *Client) exists(ctx context.Context, op, id string) (bool, error) {
	status, err := c.do(ctx, op, request{method: http.MethodGet, path: calendarPath(id), accept: []int{404, 410, 403}})
	if err != nil {
		return false, err
	}
	return status == http.StatusOK, nil
}

// lookup finds an app calendar in calendarList: owned, summary CalendarSummary, the marker in the description. Of
// several, the smallest id wins, so every run picks the same one. "" when there is none or the list is refused.
func (c *Client) lookup(ctx context.Context, op string) (string, error) {
	var found []string
	token := ""
	for range maxListPages {
		q := url.Values{"maxResults": {fmt.Sprint(listPageSize)}, "showHidden": {"true"}}
		if token != "" {
			q.Set("pageToken", token)
		}
		var page calendarList
		status, err := c.do(ctx, op, request{
			method: http.MethodGet, path: "/users/me/calendarList", query: q, out: &page, accept: []int{403},
		})
		if err != nil {
			return "", err
		}
		if status == http.StatusForbidden {
			return "", nil // the scope does not allow the list: create
		}
		for _, e := range page.Items {
			if e.AccessRole == "owner" && e.Summary == CalendarSummary && strings.Contains(e.Description, calendarMarker) &&
				validCalendarID(e.ID) {
				found = append(found, e.ID)
			}
		}
		if token = page.NextPageToken; token == "" {
			break
		}
	}
	if len(found) == 0 {
		return "", nil
	}
	return slices.Min(found), nil
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
