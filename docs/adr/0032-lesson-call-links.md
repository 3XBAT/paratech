# ADR-0032: Call link of a lesson

- **Status:** Accepted (owner request 2026-10-27, «заводи»; Open 1–4 answered 2026-10-27)
- **Date:** 2026-10-27
- **Source:** owner 2026-10-27; PRD §6.1 (recording prompt), §11.2 (imports), §13.3 (untrusted input); ADR-0007
  (popup), ADR-0009 (`adapters/weblink`), ADR-0022 (untrusted input), ADR-0024/0030 (subjects, teacher). Tasks
  TASK-256…264.

## Context
Classes are online. The student wants the link to the call (Zoom, Teams, Meet, Телемост, …) in the recording popup
and in «Сегодня» shortly before the lesson. The calendar often has no link, so mostly the student types it; an
imported event may carry one in `URL`, `LOCATION` or `DESCRIPTION`, which `icsschedule` ignores today. `lesson`
has no link. Links open today only through `adapters/weblink` (`explorer.exe`, strict https check without query or
`%`), which real call links (Zoom `?pwd=…`, Teams `%3a…`) do not pass.

## Decision

### Value: `schedule/values.CallLink`
- Parsed from untrusted text by `values.ParseCallLink(s)`: trimmed, at most 2048 bytes, printable ASCII only (no
  spaces, quotes, `<>\^{|}` or backtick), scheme `http` or `https`, a host of lowercase `a-z0-9.-` with a dot (IDN and
  IP literals refused, so the host shown is the host opened), no user info, no port, no fragment. Path and query may
  hold RFC 3986 characters and `%XX`. Stored and opened in the canonical form `String()`; `Host()` is shown in full.
  TASK-256 added DNS label rules, `xn--` and a numeric last label refused.
- `values.IsCallHost(host)`: the host equals a listed domain or ends with `"." + domain` (label boundary, so
  `evilzoom.us` does not match `zoom.us`). The list lives in this one function (Open 4, accepted): `zoom.us`,
  `teams.microsoft.com`, `teams.live.com`, `meet.google.com`, `telemost.yandex.ru`, `telemost.360.yandex.ru`,
  `mts-link.ru`, `webinar.ru`, `ktalk.ru`, `meet.jit.si`. It applies only to links taken from the calendar.
- Errors name the rule, never quote the text.

### Where links live
- **Calendar link**: `lesson.Content.CallLink` (canonical string, "" none), filled only by an import. A re-import
  that changes or drops it is an ordinary content change (`ChangeContent`, revision bump, importplan diff; the plan
  token hashes it). The Google projection does not write it. Storage `schedule_entries.call_link TEXT NOT NULL
  DEFAULT ''` (migration 0043, TASK-256).
- **Student links (Open 1 (в)): at the subject and at a lesson.** A `library/subject` field `CallLink` with its own
  copy of the shape check (ARCH-11, as the teacher email in ADR-0030), storage `subjects.call_link`; and a lesson field
  set by `lesson.SetStudentCallLink` / `ClearStudentCallLink` outside `Content` (no revision bump, an import never
  overwrites it), storage `schedule_entries.student_call_link`. Both `TEXT NOT NULL DEFAULT ''`.
- **Open 4-б (а):** a link the student types is any http/https link that passes `ParseCallLink`; `IsCallHost` is not
  applied (a university BigBlueButton works).
- A stored value that no longer parses is read as no link (it is not shown and does not fail the read); this replaces
  the TASK-256 choice of failing the whole schedule read, so tightening `ParseCallLink` needs no clearing migration.
- Set and cleared only by the student in `input/wails`. The agent has no tool to read or set a link and the planner
  never sees one (ADR-0022).

### ICS extraction (`adapters/icsschedule`)
- Candidates, in this order: `X-GOOGLE-CONFERENCE`, `X-MICROSOFT-SKYPETEAMSMEETINGURL` (only if present in the
  reference samples of `docs/refs/schedule-ics.md`; nothing is assumed), `URL`, `LOCATION`, then `DESCRIPTION`.
  In free text a candidate starts at `http://` or `https://` and ends at whitespace, `<`, `>` or a quote; trailing
  `.,;:!?)]»` is cut. The first candidate that parses and whose host passes `IsCallHost` becomes `CallLink`; none →
  "". Material links (other hosts) are never taken. No guessing (no host completion, no "meet.google.com/xyz" without
  a scheme).
- A bad link is not a row error: the lesson imports without a link.

### Which link is shown, and when
- Domain service `schedule/services/joinlink` (pure). Precedence (Open 1 (в) + Open 2 (а)): the lesson's student link,
  else the subject's student link, else the calendar link. It returns `Shown{Link, Source (student | calendar),
  EventTitle, CalendarHost}` or nothing; `CalendarHost` is set only when a student link is shown and the calendar link
  exists and differs from it (canonical strings).
- `Visible(start, end, now)` (Open 3 (а)): «Сегодня» shows the link from 10 minutes before the start until the end of
  the lesson. The popup shows the link whenever it shows the lesson.
- Use case `cases/calllinks`: resolves `Shown` for lesson IDs (ports: lesson reader, subject link reader, `Clock`),
  sets and clears the student links (subject and lesson). `todayfeed` and `recordingprompt` stay unchanged;
  `input/wails` adds the field to the popup and «Сегодня» DTOs from `calllinks`.
- UI line (Russian, plain text): «Ссылка на пару: <host> · вписано вами» or «… · из календаря, событие „<title>“»,
  buttons «Открыть» and «Копировать». With `CalendarHost` a calm second line (not red, not a button): «Ссылка в
  календаре другая: <host>». No link → no line. Nothing opens by itself.

### Opening and copying
- The page sends only the lesson ID (`OpenCallLink(lessonID)`, `CopyCallLink(lessonID)`); Go resolves the link
  again at the click and never takes a URL from the page.
- Opening: `weblink.OpenCallLink(ctx, values.CallLink)` (TASK-258): re-checks the canonical form, writes it to the
  temporary `.url` file and opens that file with `%WINDIR%\explorer.exe` (the `OpenAuthPage` path), so the URL never
  reaches a command line and the browser starts outside our process tree (ADR-0002). The `.url` writer writes only
  `CallLink.String()` of a value that passed `ParseCallLink` again. `input/wails` reaches it through its own
  `CallLinkOpener` interface (ARCH-42). Copy uses the existing `Clipboard` of `input/wails`.
- `%NAME%` in a link: TASK-258 checks names against our process environment; before the open is exposed (TASK-261)
  the check also covers names under `HKCU\Environment`, and a Windows test records whether the `.url` handler expands
  `%VAR%` at all (if not, the check stays as defence).
- Trust boundary: security review (`docs/process/security-review.md`) for `ParseCallLink`, ICS extraction and
  `OpenCallLink`.

## Consequences
- Migrations: 0043 `schedule_entries.call_link` (done); one more (next free number) with `subjects.call_link` and
  `schedule_entries.student_call_link` (TASK-259).
- `docs/refs/schedule-ics.md` and the `icsschedule` doc say which properties are read.
- http links are allowed (owner); the line shows the host, not a «защищено» mark.
- A call service missing from the list is not taken from the calendar; the student can still type its link.
- Context map (`docs/architecture.md` §12): no new context or aggregate; the `schedule` row names the link and
  `joinlink`, the `library` row the subject link. New use case `cases/calllinks`.
