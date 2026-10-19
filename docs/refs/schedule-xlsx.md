# Schedule XLSX format

Source: TASK-048, parser `paratech/desktop/internal/adapters/xlsxschedule` (`github.com/xuri/excelize/v2` v2.11.0).
Indirect dependencies it brings: richardlehane/mscfb, richardlehane/msoleps, tiendc/go-deepcopy, xuri/efp, xuri/nfp,
golang.org/x/crypto, golang.org/x/net, golang.org/x/text.
PRD sections 4.3 and 11.2, AC-16. The columns are those of [schedule-csv.md](schedule-csv.md).

## File
- `.xlsx` only, at most 2 MiB, at most 1000 zip entries and 16 MiB unpacked, 5000 lessons.
- Encrypted workbooks and old `.xls` files are refused. A damaged entry or broken XML part refuses the whole file
  (no partial import).
- Only the first visible sheet (the leftmost tab) is read; other sheets are ignored. excelize gives no public sheet
  type, so a chart sheet as the first visible tab is not skipped: it reads as an empty sheet (header error).

## Columns
The first non-empty row is the header; names as in CSV (`title`, `date`, `start`, `end`, `timezone`, optional
`uid`, `subject`/`предмет`, `kind`/`тип`, `teacher`/`преподаватель`), case-insensitive, any order. Unknown columns
and empty rows are ignored. Subject, kind and teacher follow CSV: kind values, the title rule for empty cells and the
`kind` row error are in [schedule-csv.md](schedule-csv.md).

| Column | Text cell | Number cell (typed in Excel) |
|---|---|---|
| `date` | `YYYY-MM-DD` | a date (whole day; 1900 or 1904 date system of the workbook) |
| `start`, `end` | `HH:MM`, 24 h | a time of day, whole minutes |
| `title`, `timezone`, `uid`, `subject`, `kind`, `teacher` | text | the stored number as text |

- Stored values are read; number formats are not applied and formulas are never calculated. A formula cell gives
  the value saved with it (what Excel shows); without a saved value it is empty.
- Merged cells hold their value only in the top-left cell.
- Boolean cells read as `1` / `0`. ISO date cells (`t="d"`, only written by strict-mode tools) are read as their
  stored text: a value with a time part is refused, a bare `YYYY-MM-DD` is accepted as date text.

## Identity and re-import
- As in CSV: `xlsx:id:<uid>`, else `xlsx:auto:<date> <start> <title>`. An XLSX file and the same schedule as CSV give
  the same rows, keys and preview (only the `csv:` / `xlsx:` prefix differs).
- An XLSX file is the full XLSX schedule: XLSX lessons missing from the new file are removed (soft delete). Manual,
  CSV and ICS lessons are never touched. Re-importing the same file changes nothing.

## Errors
- Rows are sheet row numbers. A row error has the bare column name as its field (`start`, as in CSV) and starts its
  reason with the letter, e.g. `column C: not a time cell ...`; it blocks the commit, as in CSV. Title, length and
  identity limits of the domain are checked per cell, so they name their column too.
- Row numbers come from the `r` attribute of each `<row>`. Excel and LibreOffice write them ascending without
  repeats; a hand-made sheet with repeated or descending numbers is read in file order and its line numbers can drift.
- Whole-file errors (`importplan.ErrUnreadableFile`): size, not an `.xlsx`, damaged file, header, too many rows.
- Messages never quote cell values.
