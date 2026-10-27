# ADR-0032: Call link of a lesson

- **Status:** Proposed (owner request 2026-10-27, «заводи»); the items under «Open» wait for the owner
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
- `values.IsCallHost(host)`: the host equals a listed domain or ends with `"." + domain` (label boundary, so
  `evilzoom.us` does not match `zoom.us`). The list is Open 4; it lives in this one function.
- Errors name the rule, never quote the text.

### Where links live
- **Calendar link**: `lesson.Content.CallLink` (canonical string, "" none), filled only by an import. A re-import
  that changes or drops it is an ordinary content change (`ChangeContent`, revision bump, importplan diff). The
  Google projection does not write it (not requested); the extra rewrite on a link change is accepted.
  Storage `lessons.call_link TEXT NOT NULL DEFAULT ''`.
- **Student link**: where it is entered is Open 1. Whatever is chosen, it is not part of `Content`, so an import
  never overwrites it: a lesson field set by `lesson.SetStudentCallLink` (no revision bump) and/or a
  `library/subject` field with its own copy of the shape check (ARCH-11, as the teacher email in ADR-0030). The read
  path re-parses every stored value with `ParseCallLink`; a value that fails is not shown.
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
- Domain service `schedule/services/joinlink` (pure): from the calendar link, the student link(s) and the lesson
  title it returns `Shown{Link, Source, EventTitle}` or nothing. `Source` is `student` or `calendar`; precedence is
  Open 2. `Visible(start, end, now)` is the «Сегодня» window, Open 3. The popup shows the link whenever it shows the
  lesson.
- Use case `cases/calllinks`: resolves `Shown` for lesson IDs (ports: lesson reader, student-link reader, `Clock`),
  sets and clears the student link. `todayfeed` and `recordingprompt` stay unchanged; `input/wails` adds the field
  to the popup and «Сегодня» DTOs from `calllinks`.
- UI line (Russian, plain text): «Ссылка на пару: <host> · вписано вами» or «… · из календаря, событие „<title>“»,
  buttons «Открыть» and «Копировать». No link → no line. Nothing opens by itself.

### Opening and copying
- The page sends only the lesson ID (`OpenCallLink(lessonID)`, `CopyCallLink(lessonID)`); Go resolves the link
  again at the click and never takes a URL from the page.
- Opening: new `weblink.OpenCallLink(ctx, values.CallLink)`: re-checks the canonical form, writes it to the
  temporary `.url` file and opens that file with `%WINDIR%\explorer.exe` (the `OpenAuthPage` path), so the URL never
  reaches a command line and the browser starts outside our process tree (ADR-0002). `input/wails` reaches it through
  its own `CallLinkOpener` interface (ARCH-42). Copy uses the existing `Clipboard` of `input/wails`.
- Trust boundary: security review (`docs/process/security-review.md`) for `ParseCallLink`, ICS extraction and
  `OpenCallLink`.

## Consequences
- One migration (next free number): `lessons.call_link`; plus the student-link column(s) chosen in Open 1.
- `docs/refs/schedule-ics.md` and the `icsschedule` doc say which properties are read.
- http links are allowed (owner); the line shows the host, not a «защищено» mark.
- A call service missing from the list is not taken from the calendar; the student can still type its link (if
  Open 4-b says the list does not apply to typed links).
- Context map (`docs/architecture.md` §12): no new context or aggregate; the `schedule` row names the link and
  `joinlink`. New use case `cases/calllinks`.

## Open (owner)
1. Где студент вписывает ссылку: (а) у предмета — одна постоянная комната на все пары предмета, дешевле всего;
   (б) у конкретной пары в расписании; (в) и там, и там — тогда что важнее: ссылка пары или ссылка предмета.
2. Есть и ссылка из календаря, и вписанная студентом — какую показать: (а) вписанную студентом; (б) из календаря;
   (в) обе строки, каждая со своим источником. Дешевле всего (а) или (б), одна строка.
3. «Сегодня»: за сколько минут до начала показывать ссылку и до какого момента: (а) за 10 минут до начала и до
   конца пары, дешевле всего; (б) за 15/30 минут; (в) весь день. Число N — на ваш выбор.
4. Какие сервисы считать звонками. Предложение: Zoom (`zoom.us` и поддомены), Teams (`teams.microsoft.com`,
   `teams.live.com`), Google Meet (`meet.google.com`), Яндекс Телемост (`telemost.yandex.ru`,
   `telemost.360.yandex.ru`), МТС Линк (`mts-link.ru`, `webinar.ru`), Контур.Толк (`ktalk.ru`), Jitsi (`meet.jit.si`).
   Добавить или убрать? И вопрос 4-б: ограничивать ли этим списком ссылку, которую вписывает сам студент
   (университетский BigBlueButton в список не попадёт): (а) нет, студент вписывает любую http/https — дешевле всего;
   (б) да, только из списка.
