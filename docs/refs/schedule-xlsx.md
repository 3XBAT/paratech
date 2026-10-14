# Schedule XLSX format

Source: TASK-048, parser `paratech/desktop/internal/adapters/xlsxschedule` (`github.com/xuri/excelize/v2` v2.11.0).
PRD sections 4.3 and 11.2, AC-16. The columns are those of [schedule-csv.md](schedule-csv.md).

## File
- `.xlsx` only, at most 2 MiB, at most 1000 zip entries and 16 MiB unpacked, 5000 lessons.
- Encrypted workbooks and old `.xls` files are refused. A damaged entry or broken XML part refuses the whole file
  (no partial import).
- Only the first visible sheet (the leftmost tab) is read; other sheets are ignored.

## Columns
The first non-empty row is the header; names as in CSV (`title`, `date`, `start`, `end`, `timezone`, optional
`uid`), case-insensitive, any order. Unknown columns and empty rows are ignored.

| Column | Text cell | Number cell (typed in Excel) |
|---|---|---|
| `date` | `YYYY-MM-DD` | a date (whole day; 1900 or 1904 date system of the workbook) |
| `start`, `end` | `HH:MM`, 24 h | a time of day, whole minutes |
| `title`, `timezone`, `uid` | text | the stored number as text |

- Stored values are read; number formats are not applied and formulas are never calculated. A formula cell gives
  the value saved with it (what Excel shows); without a saved value it is empty.
- Merged cells hold their value only in the top-left cell.

## Identity and re-import
- As in CSV: `xlsx:id:<uid>`, else `xlsx:auto:<date> <start> <title>`. An XLSX file and the same schedule as CSV give
  the same rows, keys and preview (only the `csv:` / `xlsx:` prefix differs).
- An XLSX file is the full XLSX schedule: XLSX lessons missing from the new file are removed (soft delete). Manual,
  CSV and ICS lessons are never touched. Re-importing the same file changes nothing.

## Errors
- Rows are sheet row numbers. A row error names the column and its letter, e.g. `start (column C)`, and blocks the
  commit, as in CSV.
- Whole-file errors (`importplan.ErrUnreadableFile`): size, not an `.xlsx`, damaged file, header, too many rows.
- Messages never quote cell values.
