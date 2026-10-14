# Schedule ICS format

Source: TASK-047, parser `paratech/desktop/internal/adapters/icsschedule` (`github.com/arran4/golang-ical` v0.3.7 for
syntax and RRULE values; recurrence expansion in our code). PRD sections 4.3 and 11.2, AC-16.

## File
- UTF-8, optional BOM, at most 2 MiB, 5000 VEVENTs, 5000 lessons in the import dates.
- One complete `VCALENDAR` (a cut download is refused). A logical line is at most 64 KiB with at most 32 parameters.
- Tested on exports of Google Calendar, Outlook/Exchange and a university portal (`testdata/`).

## Import dates
- Only occurrences starting in [start of yesterday UTC, +366 days) become lessons, single events too.
- The dates follow the app clock and move once a day; a preview and its commit on another UTC day may be stale
  (re-preview).
- ICS lessons outside the import dates are never removed: past lessons stay after later imports.

## Supported properties
| Property | Use |
|---|---|
| `SUMMARY` | title (line breaks become spaces) |
| `DTSTART` | with `TZID` (IANA or Windows name such as `Russian Standard Time`), UTC `Z`, or floating (needs `X-WR-TIMEZONE`) |
| `DTEND` / `DURATION` | exactly one of them |
| `RRULE` | one per event: `FREQ=DAILY/WEEKLY/MONTHLY/YEARLY`, `INTERVAL` 1..1000, `COUNT` or `UNTIL`, `BYDAY` without numbers (DAILY, WEEKLY), `WKST` |
| `EXDATE` / `RDATE` | remove / add occurrences (`RDATE` date-times only) |
| `RECURRENCE-ID` | a changed or cancelled occurrence of the series with the same `UID` |
| `STATUS:CANCELLED` | drops the event (on the master: the whole series) |
| `X-WR-TIMEZONE` | zone for floating times and the shown zone of UTC events |

- Repeating lessons keep their wall-clock time across DST in the event zone; UTC series recur in UTC.
- All-day events (`VALUE=DATE`) are not lessons and are skipped.
- `VTIMEZONE` blocks are not read: the zone database is compiled in; unknown `TZID` is a row error.
- Ignored: `LOCATION`, `DESCRIPTION`, alarms, attendees.

## Identity and re-import
- Each occurrence is `ics:<UID>/<original start in UTC, RFC 3339>`; a long or non-printable UID is hashed
  (`sha256-...`); without UID `auto-<hash of title>`.
- A moved occurrence (`RECURRENCE-ID`) keeps the key of the start it replaces, so it shows as "changed".
- Re-importing the same file changes nothing. Manual and CSV/XLSX lessons are never touched.

## Errors
- Row errors point at the line of `BEGIN:VEVENT` and block the commit: unknown zone, floating time without
  `X-WR-TIMEZONE`, a time skipped by DST, unsupported RRULE parts (`BYMONTHDAY`, `BYSETPOS`, `2MO`, `EXRULE`,
  several `RRULE`s, `RANGE`, ...), duplicate `UID` or `RECURRENCE-ID`, bad times or durations.
  Unsupported rules are never approximated.
- Whole-file errors (`importplan.ErrUnreadableFile`): size, encoding, not a calendar, broken syntax, too many events
  or lessons, recurrences too long to expand.
- Messages never quote values from the file.
