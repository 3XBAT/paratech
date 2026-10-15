// Runs on the modules compiled by `npm run test` (tsconfig.test.json) with the Node test runner.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { errorMessages, methods, parsePreview, parseResult, scheduleApi } from "../.test-build/bindings/schedule.js";
import {
  canApply,
  commitErrorText,
  conflictText,
  csvTemplate,
  errorText,
  hasChanges,
  lessonText,
  lineText,
  notes,
  resultText,
  rowErrorText,
  summaryText,
  timeRange,
  windowText,
} from "../.test-build/scheduleView.js";

const math = { title: "Математика <b>", start: "14.10.2026 09:00", end: "14.10.2026 10:30", zone: "Europe/Moscow" };
const lab = { title: "Лаба", start: "14.10.2026 10:00", end: "14.10.2026 11:30", zone: "Europe/Moscow" };

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
  ]) {
    assert.throws(() => parsePreview(bad), undefined, JSON.stringify(bad));
  }
  assert.throws(() => parseResult({ added: 1, changed: 0 }));
  assert.deepEqual(parseResult({ added: 1, changed: 0, removed: 0 }), { added: 1, changed: 0, removed: 0 });
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
  assert.equal(rowErrorText({ line: 3, field: "start", reason: "not a time" }), "строка 3, колонка start: not a time");
  assert.equal(rowErrorText({ line: 4, field: "", reason: "end is not after start" }), "строка 4: end is not after start");
  assert.equal(rowErrorText({ line: 0, field: "", reason: "the file contains no lessons" }), "файл: the file contains no lessons");
});

test("lessons, changes and conflicts are plain text", () => {
  assert.equal(timeRange(math.start, math.end), "14.10.2026 09:00–10:30");
  assert.equal(timeRange("14.10.2026 23:00", "15.10.2026 00:30"), "14.10.2026 23:00 – 15.10.2026 00:30");
  assert.equal(lessonText(math), "Математика <b>, 14.10.2026 09:00–10:30 (Europe/Moscow)");
  assert.equal(lineText(2, "x"), "строка 2: x");
  assert.equal(lineText(0, "x"), "x");
  assert.equal(
    conflictText(preview.conflicts[0]),
    "строка 2: Математика <b>, 14.10.2026 09:00–10:30 (Europe/Moscow) — пересекается со строкой 6: " +
      "Лаба, 14.10.2026 10:00–11:30 (Europe/Moscow)",
  );
  assert.match(conflictText(preview.conflicts[1]), /пересекается с занятием в расписании: Математика <b>/);
  assert.equal(summaryText(preview), "Добавится: 1, изменится: 1, удалится: 1, без изменений: 2.");
  assert.equal(hasChanges(preview), true);
  assert.equal(hasChanges({ ...preview, added: [], changed: [], removed: [] }), false);
  assert.match(windowText({ from: "a", to: "b" }), /с a до b; удаляются только занятия внутри этого периода/);
  assert.equal(resultText({ added: 1, changed: 2, removed: 3 }), "Расписание обновлено: добавлено 1, изменено 2, удалено 3.");
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
  assert.equal(csvTemplate.split("\n")[0], "title,date,start,end,timezone,uid");
  assert.equal(csvTemplate.split("\n")[1].split(",").length, 6);
  assert.match(notes.allDay, /весь день/);
  assert.match(notes.uid, /uid/);
  assert.match(notes.uid, /удалено \+ добавлено/);
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
