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
| `subject` (or `предмет`) | no | the course, e.g. `Матанализ` (TASK-089) |
| `kind` (or `тип`) | no | type of class, see below |
| `teacher` (or `преподаватель`) | no | text |

```csv
title,date,start,end,timezone,uid,subject,kind,teacher
Math,2026-10-14,09:00,10:30,Europe/Moscow,math-1,Math,lecture,Ivanov I. I.
Physics (lab),2026-10-14,10:45,12:15,Europe/Moscow,,,,
```

## Subject, kind and teacher
- `kind` values, case-insensitive, a trailing dot allowed: lecture (`лекция`, `лек`, `лк`, `lecture`, `lec`),
  seminar (`семинар`, `сем`, `seminar`, `sem`), practice (`практика`, `практ`, `пр`, `пз`, `практическое занятие`,
  `practice`, `practical`), lab (`лабораторная`, `лабораторная работа`, `лаб`, `лр`, `lab`, `laboratory`).
  Another value is a row error of the column `kind`.
- An empty `subject` or `kind` cell, or no such column, takes the value derived from the title (rule below).
- `subject` and `teacher` are cut to one line of 200 characters.

## Title rule
Used for ICS and for rows without `subject`/`kind` (`lesson.ClassifyTitle`). The kind is recognized only as a whole
word or abbreviation in one of these places; otherwise the kind is unknown and the subject is the title.

| Title | Subject | Kind |
|---|---|---|
| `Матанализ, лекция` / `Матанализ (лек.)` / `Матанализ: семинар` | Матанализ | lecture / lecture / seminar |
| `Физика - практика` (dash with spaces) | Физика | practice |
| `Лекция: Матанализ` / `[лаб] Физика` / `Lecture: Calculus` | Матанализ / Физика / Calculus | lecture / lab / lecture |
| `Компьютерные сети (лабораторная работа, подгруппа 2)` | Компьютерные сети | lab |
| `Лекция по физике` / `Lecture on Calculus` | Физике / Calculus | lecture |
| `Лабиринты истории`, `Практическая философия`, `Физика-практика` | the title | unknown |

"X по Y" keeps the grammatical case of Y and only capitalizes its first letter (no declension).

## Identity and re-import
- With `uid`: the lesson is `csv:id:<uid>`; changing its time or title shows as "changed".
- Without `uid`: `csv:auto:<date> <start> <title>`; a change of title, date or start shows as removed + added
  (only an `end` change shows as "changed").
- A CSV file is the full CSV schedule: CSV lessons missing from the new file are removed (soft delete).
  Manual lessons and ICS/XLSX lessons are never touched.
- Re-importing the same file changes nothing.

## Errors
- Line numbers are file lines; the header is line 1.
- Any row error (bad date, end not after start, unknown zone, a time skipped by DST, duplicate lesson) blocks the commit.
- Overlaps (inside the file or with kept lessons) are warnings in the preview; the user may still commit.
