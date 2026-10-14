// Package xlsxschedule reads a class schedule from an Excel workbook (.xlsx) (PRD sections 4.3 and 11.2, AC-16). It
// implements the schedulesync.ScheduleParser port for importplan.SourceXLSX with excelize. The columns are those of
// the CSV format (package csvschedule); the format is also described in docs/refs/schedule-xlsx.md.
//
// File:
//
//   - An .xlsx (zip) file of at most MaxFileBytes, at most MaxParts zip entries and MaxUnzippedBytes unpacked.
//     Encrypted workbooks and old .xls files are refused. Before excelize opens the file, every entry is unpacked
//     once here (checksums, real sizes) and every XML part must be well-formed UTF-8 XML: excelize ignores some
//     read errors, so a damaged file could otherwise import only part of a sheet.
//   - Only the first visible sheet (the leftmost tab Excel shows) is read; other sheets are ignored.
//   - The first non-empty row is the header, with the column names of CSV (title, date, start, end, timezone,
//     optional uid; case-insensitive, any order, unknown columns ignored). Empty rows are skipped. At most
//     importplan.MaxRows lessons.
//
// Cells: stored values are read, never formatted and never calculated. A formula cell gives the value the
// spreadsheet saved with it (what Excel shows); a formula without a saved value is an empty cell. Text cells use the
// CSV forms (date YYYY-MM-DD, start and end HH:MM). A number in date is an Excel date (a whole day, 1900 or 1904
// date system of the workbook); a number in start or end is an Excel time of day (a fraction of a day, a whole
// minute). Merged cells hold their value only in the top-left cell. A title such as "=SUM(A1)" typed as text is
// just text.
//
// Identity is that of CSV: "id:<uid>" with a uid, else "auto:<date> <start> <title>", so an XLSX and the equivalent
// CSV give the same rows. The stored source uid is "xlsx:<key>" (importplan).
//
// Line numbers are sheet row numbers; a row problem names the column and its letter, e.g. "start (column C)". A
// file that cannot be read at all gives an *Error wrapping importplan.ErrUnreadableFile. Messages never quote cell
// values.
package xlsxschedule
