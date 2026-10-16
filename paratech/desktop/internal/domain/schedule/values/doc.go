// Package values holds value objects shared by the schedule context and its calendar projection.
//
// It currently holds the rules of the Google authorization of the calendar projection (PRD sections 11.3 and 12,
// TASK-050): the user's own OAuth client (a client id of a Google Cloud "Desktop" client and its client secret, which
// Google does not treat as confidential for installed apps), and the only scope the app may hold,
// GoogleCalendarScope (calendar.app.created: only calendars the app created, AC-17).
package values
