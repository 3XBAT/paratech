// Runs on the modules compiled by `npm run test` (tsconfig.test.json) with the Node test runner.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { errorMessages, methods, parsePreview, parseResult, scheduleApi } from "../.test-build/bindings/schedule.js";
import {
  applyLabel,
  canApply,
  changeParts,
  changeSides,
  commitErrorText,
  conflictParts,
  csvTemplate,
  dayHeaders,
  dayLabel,
  detailParts,
  errorText,
  fillsDetails,
  hasChanges,
  kindLabel,
  lessonMarks,
  lessonParts,
  lessonTime,
  lineParts,
  notes,
  plain,
  resultText,
  rowErrorParts,
  showEmptyState,
  summaryText,
  summaryChips,
  timeRange,
  weekHeaders,
  windowText,
} from "../.test-build/scheduleView.js";

// The pieces of parts that come from the file, which the page isolates in <bdi>.
const fromFile = (parts) => parts.filter((p) => p.file).map((p) => p.text);

// No details to show: the subject is the title, the kind unknown, no teacher.
const math = {
  title: "Математика <b>", start: "14.10.2026 09:00", end: "14.10.2026 10:30", zone: "Europe/Moscow",
  subject: "Математика <b>", kind: "unknown", teacher: "",
};
const lab = {
  title: "Лаба", start: "14.10.2026 10:00", end: "14.10.2026 11:30", zone: "Europe/Moscow",
  subject: "Лаба", kind: "unknown", teacher: "",
};
const lecture = {
  ...math, title: "Матанализ (лек.)", subject: "Матанализ", kind: "lecture", teacher: "Иванов И. И.",
};

// Mirrors the JSON of internal/input/wails/schedule.go (TestScheduleChooseFileShowsThePreview).
const preview = {
  cancelled: false, selectionId: "1", fileName: "lessons.csv", source: "csv", errors: [],
  added: [{ line: 2, ...math }],
  changed: [{ line: 3, before: math, after: lab }],
  removed: [{ line: 0, ...lab }],
  unchangedCount: 2,
  conflicts: [{ line: 2, lesson: math, withLine: 6, with: lab }, { line: 3, lesson: lab, withLine: 0, with: math }],
  conflictsTruncated: false, window: null, canCommit: true,
};

function fakeCall(reply) {
  const calls = [];
  const call = async (method, ...args) => {
    calls.push([method, ...args]);
    return reply;
  };
  return { call, calls };
}

test("the api calls the bound Go methods; the page sends no path or bytes", async () => {
  const { call, calls } = fakeCall(preview);
  const api = scheduleApi(call);
  assert.deepEqual(await api.chooseFile(), preview);
  await api.discard("1");
  assert.deepEqual(calls, [
    ["paratech/desktop/internal/input/wails.ScheduleImportService.ChooseFile"],
    ["paratech/desktop/internal/input/wails.ScheduleImportService.Discard", "1"],
  ]);

  const commit = fakeCall({ added: 1, changed: 2, removed: 3 });
  assert.deepEqual(await scheduleApi(commit.call).commit("1"), { added: 1, changed: 2, removed: 3 });
  assert.deepEqual(commit.calls, [["paratech/desktop/internal/input/wails.ScheduleImportService.Commit", "1"]]);
  assert.equal(Object.keys(methods).length, 3);
});

test("a preview is parsed strictly", () => {
  const ics = { ...preview, source: "ics", window: { from: "14.10.2026 00:00 UTC", to: "15.10.2027 00:00 UTC" } };
  assert.deepEqual(parsePreview(ics).window, ics.window);
  assert.deepEqual(parsePreview({ ...preview, errors: null, conflicts: null }).errors, []);
  assert.deepEqual(parsePreview({ cancelled: true, selectionId: "", fileName: "", source: "", errors: null, added: null,
    changed: null, removed: null, unchangedCount: 0, conflicts: null, conflictsTruncated: false, window: null,
    canCommit: false }).cancelled, true);

  for (const bad of [
    null, "x", { ...preview, canCommit: "yes" }, { ...preview, selectionId: 1 }, { ...preview, added: [{ line: 2 }] },
    { ...preview, unchangedCount: -1 }, { ...preview, changed: [{ line: 1, before: math }] }, { ...preview, window: 3 },
    { ...preview, errors: [{ line: "1", field: "", reason: "" }] },
    { ...preview, added: [{ line: 2, ...math, subject: undefined }] }, { ...preview, removed: [{ line: 0, ...lab, kind: 1 }] },
    { ...preview, conflicts: [{ line: 2, lesson: math, withLine: 0, with: { ...lab, teacher: null } }] },
  ]) {
    assert.throws(() => parsePreview(bad), undefined, JSON.stringify(bad));
  }
  assert.throws(() => parseResult({ added: 1, changed: 0 }));
  assert.deepEqual(parseResult({ added: 1, changed: 0, removed: 0 }), { added: 1, changed: 0, removed: 0 });
});

test("a kind code the page does not know is shown as no kind", () => {
  const p = parsePreview({ ...preview, added: [{ line: 2, ...lecture, kind: "exam" }], changed: [], removed: [] });
  assert.equal(p.added[0].kind, "unknown");
  assert.deepEqual(parsePreview({ ...preview, added: [{ line: 2, ...lecture }] }).added[0], { line: 2, ...lecture });
});

test("Apply is offered only after a preview the core accepts", () => {
  assert.equal(canApply(null), false, "before a preview");
  assert.equal(canApply(preview), true);
  assert.equal(canApply({ ...preview, canCommit: false }), false, "no token or no changes");
  assert.equal(canApply({ ...preview, errors: [{ line: 3, field: "start", reason: "bad" }] }), false, "row errors");
  assert.equal(canApply({ ...preview, selectionId: "" }), false, "no selection");
  assert.equal(canApply({ ...preview, cancelled: true }), false, "a cancelled dialog");
});

test("row errors name the line and the column", () => {
  const withField = rowErrorParts({ line: 3, field: "start", reason: "not a time" });
  assert.equal(plain(withField), "строка 3, колонка start: not a time");
  assert.deepEqual(fromFile(withField), ["start", "not a time"]);
  assert.equal(plain(rowErrorParts({ line: 4, field: "", reason: "end is not after start" })), "строка 4: end is not after start");
  assert.equal(
    plain(rowErrorParts({ line: 0, field: "", reason: "the file contains no lessons" })),
    "файл: the file contains no lessons",
  );
});

test("lessons, changes and conflicts are plain text", () => {
  assert.equal(timeRange(math.start, math.end), "14.10.2026 09:00–10:30");
  assert.equal(timeRange("14.10.2026 23:00", "15.10.2026 00:30"), "14.10.2026 23:00 – 15.10.2026 00:30");
  assert.equal(plain(lessonParts(math)), "Математика <b>, 14.10.2026 09:00–10:30 (Europe/Moscow)");
  assert.equal(plain(lineParts(2, lessonParts(lab))), "строка 2: Лаба, 14.10.2026 10:00–11:30 (Europe/Moscow)");
  assert.equal(plain(lineParts(0, lessonParts(lab))), "Лаба, 14.10.2026 10:00–11:30 (Europe/Moscow)");
  assert.equal(
    plain(changeParts(preview.changed[0])),
    "строка 3: Математика <b>, 14.10.2026 09:00–10:30 (Europe/Moscow) → Лаба, 14.10.2026 10:00–11:30 (Europe/Moscow)",
  );
  assert.equal(
    plain(conflictParts(preview.conflicts[0])),
    "строка 2: Математика <b>, 14.10.2026 09:00–10:30 (Europe/Moscow) — пересекается со строкой 6: " +
      "Лаба, 14.10.2026 10:00–11:30 (Europe/Moscow)",
  );
  assert.match(plain(conflictParts(preview.conflicts[1])), /пересекается с занятием в расписании: Математика <b>/);
  assert.equal(summaryText(preview), "Добавится: 1, изменится: 1, удалится: 1, без изменений: 2.");
  assert.equal(hasChanges(preview), true);
  assert.equal(hasChanges({ ...preview, added: [], changed: [], removed: [] }), false);
  assert.match(windowText({ from: "a", to: "b" }), /с a до b; удаляются только занятия внутри этого периода/);
  assert.equal(resultText({ added: 1, changed: 2, removed: 3 }), "Расписание обновлено: добавлено 1, изменено 2, удалено 3.");
});

test("a lesson shows its kind, a subject other than the title and the teacher", () => {
  assert.deepEqual(["lecture", "seminar", "practice", "lab"].map(kindLabel), ["лекция", "семинар", "практика", "лабораторная"]);
  for (const k of ["unknown", "", "exam", "toString", "__proto__"]) {
    assert.equal(kindLabel(k), "", k);
  }

  assert.equal(
    plain(lessonParts(lecture)),
    "Матанализ (лек.), лекция · предмет: Матанализ · преподаватель: Иванов И. И., 14.10.2026 09:00–10:30 (Europe/Moscow)",
  );
  assert.deepEqual(fromFile(lessonParts(lecture)), [lecture.title, "Матанализ", "Иванов И. И.", "Europe/Moscow"]);
  assert.equal(plain(detailParts({ ...lecture, title: " Матанализ " })), "лекция · преподаватель: Иванов И. И.", "subject = title");
  assert.equal(plain(detailParts({ ...lecture, kind: "unknown", teacher: "" })), "предмет: Матанализ");
  assert.equal(plain(detailParts({ ...lecture, subject: "", kind: "lab", teacher: "" })), "лабораторная", "old lesson");
  assert.deepEqual(detailParts(math), []);
  assert.equal(plain(lessonParts(math)), "Математика <b>, 14.10.2026 09:00–10:30 (Europe/Moscow)");
  // Fixtures written before the details existed still render.
  assert.deepEqual(detailParts({ title: "Урок", start: "", end: "", zone: "" }), []);
});

test("a change shows a subject the titles do not", () => {
  const old = { ...lecture, title: "Матанализ", subject: "", kind: "unknown", teacher: "" };
  const now = { ...lecture, title: "Матанализ" };
  const filled = changeSides({ line: 2, before: old, after: now });
  assert.equal(plain(filled.before), "Матанализ, предмет не указан, 14.10.2026 09:00–10:30 (Europe/Moscow)");
  assert.equal(
    plain(filled.after),
    "Матанализ, лекция · предмет: Матанализ · преподаватель: Иванов И. И., 14.10.2026 09:00–10:30 (Europe/Moscow)",
  );
  assert.equal(fillsDetails([{ line: 2, before: old, after: now }]), true);
  assert.equal(fillsDetails(preview.changed), false);

  const kindOnly = changeParts({ line: 3, before: { ...math, kind: "seminar" }, after: { ...math, kind: "practice" } });
  assert.equal(
    plain(kindOnly),
    "строка 3: Математика <b>, семинар, 14.10.2026 09:00–10:30 (Europe/Moscow) → Математика <b>, практика, " +
      "14.10.2026 09:00–10:30 (Europe/Moscow)",
  );
  // Title and subject change together: the titles show it, the subject is not repeated.
  assert.equal(plain(changeSides(preview.changed[0]).after), "Лаба, 14.10.2026 10:00–11:30 (Europe/Moscow)");
});

test("file text is isolated piece by piece: a bidi override cannot reorder the line", () => {
  const rlo = "\u202E"; // RIGHT-TO-LEFT OVERRIDE
  const evil = { ...math, title: `a${rlo}b`, subject: `a${rlo}b`, zone: `Z${rlo}` };
  assert.deepEqual(fromFile(lessonParts(evil)), [evil.title, evil.zone]);
  const details = { ...lecture, subject: `S${rlo}`, teacher: `T${rlo}` };
  assert.deepEqual(fromFile(lessonParts(details)), [details.title, details.subject, details.teacher, details.zone]);
  for (const p of lessonParts(details).filter((p) => !p.file)) {
    assert.ok(!p.text.includes(rlo), `fixed text ${JSON.stringify(p.text)} holds file text`);
  }
  assert.deepEqual(fromFile(changeParts({ line: 1, before: evil, after: lab })), [evil.title, evil.zone, lab.title, lab.zone]);
  assert.deepEqual(fromFile(conflictParts({ line: 1, lesson: evil, withLine: 2, with: lab })), [
    evil.title, evil.zone, lab.title, lab.zone,
  ]);
  for (const parts of [lessonParts(evil), conflictParts({ line: 1, lesson: evil, withLine: 2, with: lab })]) {
    for (const p of parts.filter((p) => !p.file)) {
      assert.ok(!p.text.includes(rlo), `fixed text ${JSON.stringify(p.text)} holds file text`);
    }
  }
  const page = readFileSync(new URL("../src/pages/SchedulePage.tsx", import.meta.url), "utf8");
  assert.match(page, /p\.file \? <bdi key=\{i\}>\{p\.text\}<\/bdi>/);
  assert.match(page, /<bdi>\{p\.fileName\}<\/bdi>/);
});

test("errors of the core map to Russian text, unknown ones to a generic text", () => {
  for (const msg of Object.values(errorMessages)) {
    assert.match(errorText(new Error(msg)), /[а-яё]/i);
  }
  assert.equal(new Set(Object.values(errorMessages).map((m) => errorText(m))).size, Object.keys(errorMessages).length);
  assert.equal(errorText({ message: errorMessages.stale }), "Расписание или файл изменились. Выберите файл снова.");
  assert.equal(errorText(new Error("open C:\\Users\\student\\a.csv")), errorText(errorMessages.failed));
  assert.equal(errorText(undefined), errorText(errorMessages.failed));
  assert.equal(commitErrorText(new Error(errorMessages.stale)), "Расписание или файл изменились. Выберите файл снова.");
  assert.equal(commitErrorText(new Error(errorMessages.failed)), "Не удалось импортировать расписание. Выберите файл снова.");
});

test("the page states the owner's decisions and the template", () => {
  const rows = csvTemplate.split("\n");
  assert.equal(rows[0], "title,date,start,end,timezone,uid,subject,kind,teacher");
  assert.deepEqual(rows.map((r) => r.split(",").length), [9, 9, 9], "no quoted cells in the template");
  assert.match(rows[1], /,лекция,/);
  assert.match(rows[2], /\(лаб\.\),.*,,,$/, "empty optional cells: the title tells");
  for (const word of ["subject (предмет)", "kind (тип", "teacher (преподаватель)", "лабораторная", "из названия"]) {
    assert.ok(notes.details.includes(word), word);
  }
  assert.match(notes.icsDetails, /«Преподаватель: …» в описании/);
  assert.match(notes.filledDetails, /предметом, типом и преподавателем/);
  assert.match(notes.allDay, /весь день/);
  assert.match(notes.uid, /uid/);
  assert.match(notes.uid, /Без uid изменение названия, даты или начала занятия выглядит как «удалено \+ добавлено»/);
  assert.match(notes.formats, /до 2 МиБ\./);
  assert.equal(errorText(errorMessages.fileTooLarge), "Файл слишком большой: не более 2 МиБ.");
  assert.match(notes.noNotifications, /Уведомления/);
});

test("file text is never rendered as HTML", () => {
  for (const file of ["../src/pages/SchedulePage.tsx", "../src/scheduleView.ts", "../src/bindings/schedule.ts"]) {
    const src = readFileSync(new URL(file, import.meta.url), "utf8");
    for (const banned of ["dangerouslySetInnerHTML", "innerHTML", "outerHTML", "insertAdjacentHTML", "href="]) {
      assert.ok(!src.includes(banned), `${file} uses ${banned}`);
    }
  }
});

test("Apply names the number of changes; tiles split the counters", () => {
  assert.equal(applyLabel(preview), "Применить 3 изменения");
  assert.equal(applyLabel({ ...preview, changed: [], removed: [] }), "Применить 1 изменение");
  assert.equal(applyLabel({ ...preview, added: Array(150).fill(preview.added[0]) }), "Применить 152 изменения");
  assert.equal(applyLabel({ ...preview, added: Array(9).fill(preview.added[0]), removed: [] }), "Применить 10 изменений");
  assert.equal(applyLabel({ ...preview, added: [], changed: [], removed: [] }), "Применить");
  assert.deepEqual(summaryChips(preview).map((c) => [c.label, c.count]), [
    ["Добавится", 1], ["Изменится", 1], ["Удалится", 1], ["Без изменений", 2], ["Ошибки", 0],
  ]);
});

test("week dividers start every run of lessons in one Monday–Sunday week", () => {
  const at = (start) => ({ start });
  const h = weekHeaders([
    at("31.08.2026 10:40"), at("06.09.2026 09:00"), at("07.09.2026 09:00"), at("bad"), at("08.09.2026 09:00"),
    at("31.12.2026 09:00"), at("01.01.2027 09:00"), at("31.02.2026 09:00"),
  ]);
  assert.deepEqual(h.map((x) => x && [x.label, x.countText]), [
    ["31.08–06.09", "2 занятия"], null, ["07.09–13.09", "1 занятие"], null, ["07.09–13.09", "1 занятие"],
    ["28.12–03.01", "2 занятия"], null, null,
  ]);
  assert.deepEqual(weekHeaders([]), []);
  assert.equal(weekHeaders(Array(5).fill(at("14.10.2026 09:00")))[0].countText, "5 занятий");
});

// 12:00 on Friday 16.10.2026 in Moscow (UTC+3), 16:00 in Novosibirsk (UTC+7).
const now = new Date("2026-10-16T09:00:00Z");

test("day labels: today and tomorrow in the lesson's zone, otherwise weekday and date", () => {
  const msk = "Europe/Moscow";
  assert.equal(dayLabel("16.10.2026 09:00", msk, now), "Сегодня");
  assert.equal(dayLabel("17.10.2026 23:59", msk, now), "Завтра");
  assert.equal(dayLabel("15.10.2026 09:00", msk, now), "чт, 15 октября");
  assert.equal(dayLabel("19.10.2026 09:00", msk, now), "пн, 19 октября");
  assert.equal(dayLabel("01.02.2027 09:00", msk, now), "пн, 1 февраля 2027", "another year");
  // 02:00 on 17.10 in Novosibirsk is 22:00 on 16.10 in Moscow: today is taken in each lesson's own zone.
  assert.equal(dayLabel("17.10.2026 02:00", "Asia/Novosibirsk", new Date("2026-10-16T19:30:00Z")), "Сегодня");
  assert.equal(dayLabel("31.12.2026 09:00", msk, new Date("2026-12-30T12:00:00Z")), "Завтра", "across a month");
  assert.equal(dayLabel("01.01.2027 09:00", msk, new Date("2026-12-31T12:00:00Z")), "Завтра", "across a year");
  // An invalid zone is file text; it gets the plain date, no «Сегодня».
  assert.equal(dayLabel("16.10.2026 09:00", "Not/AZone", now), "пт, 16 октября");
  // Like the core, an empty or "Local" zone means UTC (not the machine zone): 23:30 UTC is still 16.10 there.
  const late = new Date("2026-10-16T23:30:00Z");
  assert.equal(dayLabel("16.10.2026 09:00", "", late), "Сегодня");
  assert.equal(dayLabel("16.10.2026 09:00", "Local", late), "Сегодня");
  assert.equal(dayLabel("16.10.2026 09:00", msk, late), "пт, 16 октября", "02:30 on 17.10 in Moscow");
  for (const bad of ["bad", "31.02.2026 09:00", "16.10.2026 25:00", ""]) {
    assert.equal(dayLabel(bad, msk, now), null, bad);
  }
});

test("day headers start every run of lessons on one day", () => {
  const at = (start, zone = "Europe/Moscow") => ({ start, zone });
  assert.deepEqual(
    dayHeaders(
      [
        at("16.10.2026 09:00"), at("16.10.2026 12:00"), at("17.10.2026 09:00"), at("bad"), at("17.10.2026 12:00"),
        at("20.10.2026 09:00"), at("20.10.2026 10:00", "Not/AZone"), at("20.10.2026 11:00", "Not/AZone"),
      ],
      now,
    ),
    ["Сегодня", null, "Завтра", null, "Завтра", "вт, 20 октября", null, null],
  );
  assert.deepEqual(dayHeaders([at("16.10.2026 09:00"), at("16.10.2026 10:00", "Not/AZone")], now), [
    "Сегодня",
    "пт, 16 октября",
  ]);
  assert.deepEqual(dayHeaders([], now), []);
});

test("past lessons are muted, today's and the next one highlighted", () => {
  const l = (start, end, zone = "Europe/Moscow") => ({ start, end, zone });
  const marks = lessonMarks(
    [
      l("15.10.2026 09:00", "15.10.2026 10:30"), // yesterday
      l("16.10.2026 09:00", "16.10.2026 12:00"), // ended at 12:00, now is 12:00
      l("16.10.2026 11:30", "16.10.2026 13:00"), // going on now
      l("16.10.2026 15:00", "16.10.2026 16:30"), // later today
      l("17.10.2026 09:00", "17.10.2026 10:30"), // tomorrow
      l("16.10.2026 23:00", "17.10.2026 00:30"), // out of order, still today
    ],
    now,
  );
  assert.deepEqual(marks, [
    { past: true, today: false, next: false },
    { past: true, today: false, next: false },
    { past: false, today: true, next: true },
    { past: false, today: true, next: false },
    { past: false, today: false, next: false },
    { past: false, today: true, next: false },
  ]);
  // A lesson started yesterday that has not ended is today's.
  assert.deepEqual(lessonMarks([l("15.10.2026 23:00", "16.10.2026 13:00")], now), [{ past: false, today: true, next: true }]);
  // In Novosibirsk it is 16:00 already: the 14:00 lesson there is over, while in Moscow it would be ahead.
  assert.deepEqual(lessonMarks([l("16.10.2026 14:00", "16.10.2026 15:30", "Asia/Novosibirsk")], now), [
    { past: true, today: false, next: false },
  ]);
  const none = { past: false, today: false, next: false };
  assert.deepEqual(
    lessonMarks([l("15.10.2026 09:00", "15.10.2026 10:30", "Not/AZone"), l("bad", "bad"), l("17.10.2026 09:00", "bad")], now),
    [none, none, { past: false, today: false, next: true }],
    "no mark without a zone or a start; an unparsable end counts as the start",
  );
  // "" and "Local" mean UTC, as in the core: at 09:00 UTC a 10:00 lesson is ahead, an 08:00 one is over.
  assert.deepEqual(lessonMarks([l("16.10.2026 08:00", "16.10.2026 08:30", ""), l("16.10.2026 10:00", "16.10.2026 11:00", "Local")], now), [
    { past: true, today: false, next: false },
    { past: false, today: true, next: true },
  ]);
});

test("the next lesson is the earliest start, not the first in the file order", () => {
  const l = (start, end, zone = "Europe/Moscow") => ({ start, end, zone });
  const later = l("19.10.2026 09:00", "19.10.2026 10:30");
  const sooner = l("17.10.2026 09:00", "17.10.2026 10:30");
  assert.deepEqual(lessonMarks([later, sooner], now).map((m) => m.next), [false, true], "unsorted input");
  assert.deepEqual(lessonMarks([sooner, later, sooner], now).map((m) => m.next), [true, false, false], "a tie: the first");
  // `sooner` is 06:00 UTC; 12:00 in Novosibirsk is 05:00 UTC, though later in wall time.
  const nsk = l("17.10.2026 12:00", "17.10.2026 13:00", "Asia/Novosibirsk"); // 05:00 UTC
  assert.deepEqual(lessonMarks([sooner, nsk], now).map((m) => m.next), [false, true], "two zones: by the instant");
  // In wall time the Moscow lesson (08:00) looks earlier, but it starts at 05:00 UTC, after 04:00 UTC in Novosibirsk.
  const msk8 = l("17.10.2026 08:00", "17.10.2026 09:00"); // 05:00 UTC
  const nsk11 = l("17.10.2026 11:00", "17.10.2026 12:00", "Asia/Novosibirsk"); // 04:00 UTC
  assert.deepEqual(lessonMarks([msk8, nsk11], now).map((m) => m.next), [false, true], "wall time would pick Moscow");
});

test("lessons to be removed are only muted, never today's or the next", () => {
  const l = (start, end) => ({ start, end, zone: "Europe/Moscow" });
  assert.deepEqual(
    lessonMarks([l("16.10.2026 09:00", "16.10.2026 10:30"), l("16.10.2026 15:00", "16.10.2026 16:30")], now, false),
    [
      { past: true, today: false, next: false },
      { past: false, today: false, next: false },
    ],
  );
});

test("the empty state shows only before anything is chosen, failed or done", () => {
  assert.equal(showEmptyState(null, "", ""), true);
  assert.equal(showEmptyState(preview, "", ""), false, "a preview");
  assert.equal(showEmptyState(null, "Не удалось прочитать файл.", ""), false, "an error");
  assert.equal(showEmptyState(null, "", "Расписание обновлено: добавлено 1, изменено 0, удалено 0."), false, "a result");
  assert.match(notes.choose, /«Выбрать файл…»/);
});

test("the row time omits the day shown in the header", () => {
  assert.equal(lessonTime("14.10.2026 09:00", "14.10.2026 10:30"), "09:00–10:30");
  assert.equal(lessonTime("14.10.2026 23:00", "15.10.2026 00:30"), "23:00 – 15.10.2026 00:30");
  assert.equal(lessonTime("bad", "worse"), "bad – worse", "no header: the full text");
});
