# Schedule CSV format

Source: TASK-046, parser `paratech/desktop/internal/adapters/csvschedule` (Go `encoding/csv`, Go 1.26). PRD section 11.2, AC-16.

## File
- UTF-8, optional BOM, at most 2 MiB and 5000 lessons.
- Separator: comma. Semicolon if the header line has `;` and no `,` (Excel in Russian locale).
- Quoting as in RFC 4180 (`"Physics, lab"`; a quoted field may span lines).

## Columns
First line is the header. Names are case-insensitive, any order. Unknown columns are ignored.

| Column | Required | Format |
|---|---|---|
| `title` | yes | text, kept as data |
| `date` | yes | `YYYY-MM-DD` |
| `start` | yes | `HH:MM`, 24 h |
| `end` | yes | `HH:MM`, after `start` on the same date |
| `timezone` | yes | IANA name, e.g. `Europe/Moscow`, `UTC` |
| `uid` | no | stable id of the lesson in your source |

```csv
title,date,start,end,timezone,uid
Math,2026-10-14,09:00,10:30,Europe/Moscow,math-1
Physics,2026-10-14,10:45,12:15,Europe/Moscow,
```

## Identity and re-import
- With `uid`: the lesson is `csv:id:<uid>`; changing its time or title shows as "changed".
- Without `uid`: `csv:auto:<date> <start> <title>`; moving it shows as removed + added.
- A CSV file is the full CSV schedule: CSV lessons missing from the new file are removed (soft delete).
  Manual lessons and ICS/XLSX lessons are never touched.
- Re-importing the same file changes nothing.

## Errors
- Line numbers are file lines; the header is line 1.
- Any row error (bad date, end not after start, unknown zone, a time skipped by DST, duplicate lesson) blocks the commit.
- Overlaps (inside the file or with kept lessons) are warnings in the preview; the user may still commit.
