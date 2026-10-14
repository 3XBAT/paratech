// Package csvschedule reads a class schedule from CSV (PRD section 11.2, AC-16). It implements the
// schedulesync.ScheduleParser port with encoding/csv only. The format is also described in docs/refs/schedule-csv.md.
//
// Format:
//
//   - UTF-8, an optional byte order mark, at most MaxFileBytes and importplan.MaxRows lessons.
//   - Separator: comma; a semicolon when the header line has semicolons and no commas (spreadsheet exports in
//     locales that use the decimal comma). Fields may be quoted as in RFC 4180.
//   - The first line is the header. Column names are case-insensitive, in any order: title, date, start, end,
//     timezone are required; uid is optional. Other columns (room, teacher, ...) are ignored.
//   - date is YYYY-MM-DD, start and end are HH:MM (24 h) on that date in the zone timezone, an IANA name such as
//     Europe/Moscow. The end must be after the start on the same date; a lesson cannot pass midnight.
//   - Values are trimmed and kept as plain data (a title starting with "=" is just text).
//
// Identity: a row with a uid has the key "id:<uid>". A row without one gets "auto:<date> <start> <title>", so
// moving such a lesson to another time is a removal plus an addition; add a uid column for stable identity. The
// stored source uid is "csv:<key>" (importplan).
//
// Line numbers in errors are lines of the file; the header is line 1. A row problem is returned as data; a file that
// cannot be read at all gives an *Error wrapping importplan.ErrUnreadableFile.
package csvschedule
