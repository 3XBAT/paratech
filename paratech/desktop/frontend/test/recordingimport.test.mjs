// «Импортировать запись» (TASK-162): the form's checks, the texts, the refusals of the binding and the panel and the row.
// Runs on the modules compiled by `npm run test` (tsconfig.test.json) with the Node test runner.
import { test } from "node:test";
import assert from "node:assert/strict";
import { createElement as h } from "react";
import server from "react-dom/server";
import { activityApi, methods as activityMethods } from "../.test-build/bindings/activity.js";
import { errorMessages, methods, parseImportResult, recordingImportApi } from "../.test-build/bindings/recordingImport.js";
import {
  checkLecture,
  importErrorText,
  importOutcome,
  importTexts,
  initialForm,
  localRfc3339,
  pastLessonOptions,
} from "../.test-build/recordingImportView.js";
import { RecordingImportPanel } from "../.test-build/pages/RecordingImportPanel.js";
import { RecordingRow } from "../.test-build/pages/RecordingRow.js";
import { recordingRowView } from "../.test-build/recordingRowView.js";

const { renderToStaticMarkup } = server;

const now = new Date(2026, 9, 27, 12, 0).getTime();
const form = (over = {}) => ({ ...initialForm("", new Date(now), false), ...over });
const result = (over = {}) => ({
  cancelled: false, recordingId: "r1", existing: false, message: "", name: "лекция.mp3", format: "mp3", durationSeconds: 3725, ...over,
});

test("the note and the hints are in Russian, the local note is shown", () => {
  assert.equal(importTexts.local, "Звук не покидает компьютер, распознаётся локально.");
  assert.equal(importTexts.imported, "импортирована");
  assert.match(importTexts.formats, /WAV, FLAC, MP3 и OGG/);
});

test("the binding passes the lecture and checks the reply", async () => {
  const calls = [];
  const api = recordingImportApi(async (m, ...args) => {
    calls.push([m, args]);
    return result();
  });
  const lecture = { lessonId: "l1", startedAt: "", subject: "", kind: "", title: "" };
  assert.equal((await api.importRecording(lecture)).recordingId, "r1");
  assert.deepEqual(calls, [[methods.import, [lecture]]]);
  assert.throws(() => parseImportResult({ ...result(), format: "m4a" }));
  assert.throws(() => parseImportResult({ ...result(), durationSeconds: -1 }));
  assert.throws(() => parseImportResult(null));
});

test("localRfc3339 gives the local offset and refuses unreal moments", () => {
  const s = localRfc3339("2026-10-20", "09:05");
  assert.match(s, /^2026-10-20T09:05:00[+-]\d\d:\d\d$/);
  assert.equal(Date.parse(s), new Date(2026, 9, 20, 9, 5).getTime());
  assert.equal(localRfc3339("2026-02-31", "09:05"), null);
  assert.equal(localRfc3339("2026-10-20", "25:00"), null);
  assert.equal(localRfc3339("", "09:05"), null);
  assert.equal(localRfc3339("2026-10-20", ""), null);
});

test("a manual lecture needs a past date, a time and a subject", () => {
  const ok = checkLecture(form({ date: "2026-10-20", time: "09:05", subject: " Матанализ ", title: " Предел " }), now);
  assert.equal(ok.ok, true);
  assert.equal(ok.lecture.lessonId, "");
  assert.equal(ok.lecture.subject, "Матанализ");
  assert.equal(ok.lecture.title, "Предел");
  assert.equal(ok.lecture.kind, "lecture");
  assert.match(ok.lecture.startedAt, /^2026-10-20T09:05:00/);

  const problem = (over) => {
    const c = checkLecture(form({ subject: "Матанализ", date: "2026-10-20", time: "09:05", ...over }), now);
    assert.equal(c.ok, false);
    return c.problem;
  };
  assert.equal(problem({ date: "" }), importTexts.needDate);
  assert.equal(problem({ time: "" }), importTexts.needTime);
  assert.equal(problem({ date: "2026-02-31" }), errorMessages.invalidDate);
  assert.equal(problem({ date: "2026-10-28" }), errorMessages.future);
  assert.equal(problem({ subject: "  " }), importTexts.needSubject);
  // An unknown kind falls back to a lecture.
  assert.equal(checkLecture(form({ date: "2026-10-20", time: "09:05", subject: "М", kind: "<x>" }), now).lecture.kind, "lecture");
});

test("a lesson of the schedule is sent by its id only", () => {
  assert.equal(checkLecture(form({ mode: "lesson" }), now).problem, importTexts.needLesson);
  const c = checkLecture(form({ mode: "lesson", lessonId: "l1", subject: "ignored" }), now);
  assert.deepEqual(c.lecture, { lessonId: "l1", startedAt: "", subject: "", kind: "", title: "" });
});

test("only started lessons are offered, newest first, and narrowed by the subject", () => {
  const at = (h, m) => new Date(2026, 9, 27, h, m).toISOString();
  const lessons = [
    { id: "a", title: "Лекция 1", subject: "Матанализ", start: at(9, 0) },
    { id: "b", title: "", subject: "Физика", start: at(10, 30) },
    { id: "c", title: "Позже", subject: "Матанализ", start: at(15, 0) },
    { id: "d", title: "Битая", subject: "Матанализ", start: "not a date" },
  ];
  assert.deepEqual(pastLessonOptions(lessons, now, "").map((o) => o.id), ["b", "a"]);
  assert.deepEqual(pastLessonOptions(lessons, now, " матанализ ").map((o) => o.id), ["a"]);
  assert.match(pastLessonOptions(lessons, now, "")[0].label, /Физика$/);
});

test("refusals of the binding are shown in Russian, anything else gets the generic text", () => {
  for (const text of Object.values(errorMessages)) {
    assert.equal(importErrorText(new Error(text)), text);
    assert.equal(importErrorText(text), text);
  }
  for (const key of ["format", "tooLarge", "tooLong", "noDuration", "future"]) {
    assert.match(errorMessages[key], /[а-я]/);
  }
  // An unknown duration names the format of the file.
  for (const [key, name] of [["noDurationWav", "WAV"], ["noDurationMp3", "MP3"], ["noDurationFlac", "FLAC"], ["noDurationOgg", "OGG"]]) {
    assert.ok(errorMessages[key].includes(`в этом ${name}-файле`), key);
    assert.equal(importErrorText(new Error(errorMessages[key])), errorMessages[key]);
  }
  assert.equal(importErrorText(new Error("open C:\\secret\\a.wav: access denied")), errorMessages.unavailable);
  assert.equal(importErrorText(undefined), errorMessages.unavailable);
});

test("the outcome says what happened, a duplicate gets the core's message", () => {
  assert.equal(importOutcome(result({ cancelled: true, recordingId: "", name: "", format: "", durationSeconds: 0 })).kind, "cancelled");
  const ok = importOutcome(result());
  assert.equal(ok.kind, "ok");
  assert.match(ok.text, /«лекция\.mp3»/);
  assert.match(ok.text, /1:02:05/);
  assert.match(ok.text, /Расшифровка поставлена в очередь/);
  const dup = importOutcome(result({ existing: true, message: "Эта запись уже добавлена.", durationSeconds: 0 }));
  assert.deepEqual([dup.kind, dup.text, dup.recordingId], ["existing", "Эта запись уже добавлена.", "r1"]);
});

test("the panel shows the steps, the local note and the formats, with names as text", () => {
  const html = renderToStaticMarkup(
    h(RecordingImportPanel, {
      api: { importRecording: async () => result() },
      loadLessons: async () => [],
      subject: "<b>Матанализ</b>",
      now: () => now,
      onImported: () => undefined,
      onClose: () => undefined,
    }),
  );
  assert.match(html, /Звук не покидает компьютер, распознаётся локально/);
  assert.match(html, /Выбрать файл и импортировать/);
  assert.match(html, /Пара из расписания/);
  assert.match(html, /type="date"/);
  assert.match(html, /type="time"/);
  assert.match(html, /aria-pressed/);
  assert.doesNotMatch(html, /<b>Матанализ/);
  assert.match(html, /&lt;b&gt;Матанализ/);
});

const started = "2026-10-18T17:19:35Z";
const item = (over = {}) => ({
  id: "r1", lessonId: "l1", title: "Матанализ", subject: "", kind: "", teacher: "", status: "COMPLETED", startedAt: started,
  stoppedAt: "", durationSeconds: 3600, hasDuration: false, problem: "", stoppable: false, stage: "queued", step: "transcription",
  progressPercent: 0, partsDone: 0, parts: 0, queueReason: "power", errorCode: "", jobError: "", action: "", actionJobId: "",
  retryWork: "", canRedoSummary: false, lectureTextReady: false, remainingSeconds: 1500, hasRemaining: true, advice: null, ...over,
});
const renderRow = (r, props = {}) =>
  renderToStaticMarkup(
    h(RecordingRow, {
      item: r, now: Date.parse(started), stopping: false, starting: false,
      onStop: () => undefined, onRunNow: () => undefined, onOpen: () => undefined, ...props,
    }),
  );

test("an imported row says so and still shows the estimate and the power wait", () => {
  const plain = renderRow(item());
  const marked = renderRow(item(), { imported: true });
  assert.doesNotMatch(plain, /импортирована/);
  assert.match(marked, /импортирована/);
  const v = recordingRowView(item(), Date.parse(started));
  assert.notEqual(v.remaining, "");
  assert.ok(marked.includes(v.remaining), "the recognition estimate is shown");
  assert.ok(marked.includes(v.status.label), "the status with the power wait is shown");
});

test("the mark comes from the origin of the reloaded list, not from the session (TASK-300)", async () => {
  const raw = (id, origin) => ({ ...item({ id }), origin });
  const api = activityApi(async (m) => {
    assert.equal(m, activityMethods.listRecordings);
    return [raw("r1", "imported"), raw("r2", "captured"), raw("r3", "weird")];
  });
  const [a, b, c] = await api.listRecordings();
  // The page passes imported={r.origin === "imported"} to the row.
  const html = (r) => renderRow(r, { imported: r.origin === "imported" });
  assert.match(html(a), /импортирована/);
  assert.doesNotMatch(html(b), /импортирована/);
  assert.doesNotMatch(html(c), /импортирована/);
});
