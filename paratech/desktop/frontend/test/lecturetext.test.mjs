// Lecture text (TASK-090): the bindings, the recordings row (stage, parts, failure, links) and the lecture card of the
// summary page. Runs on the modules compiled by `npm run test`.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import server from "react-dom/server";
import { formatTime } from "../.test-build/activityView.js";
import { parseRecording } from "../.test-build/bindings/activity.js";
import { lectureErrorMessages, lectureMethods, lectureTextApi, summaryApi } from "../.test-build/bindings/summary.js";
import {
  lectureBlocks,
  lectureCopiedText,
  lectureEarlierNote,
  lectureLine,
  lectureStartLabel,
  lectureUpdatedText,
  showsEarlierLecture,
} from "../.test-build/lectureTextView.js";
import { LectureTextCard } from "../.test-build/pages/LectureTextParts.js";
import { RecordingRow } from "../.test-build/pages/RecordingRow.js";
import {
  chainStatusView,
  errorTexts,
  recordingRowView,
  rowAction,
  rowActions,
  retryHints,
  rowLinks,
  stageLabels,
} from "../.test-build/recordingRowView.js";
import { errorText, idlePollMs, nextPollMs, pollMs, rowSummaryOffer } from "../.test-build/summaryView.js";
import { rowOffersTranscript } from "../.test-build/transcriptView.js";

const { renderToStaticMarkup } = server;

// Mirrors RecordingItem of internal/input/wails/activity.go.
const row = (over = {}) => ({
  id: "r1", lessonId: "l1", title: "Матанализ", subject: "", kind: "", teacher: "",
  status: "COMPLETED", startedAt: "2026-10-18T17:19:35Z", stoppedAt: "",
  durationSeconds: 0, hasDuration: false, problem: "", stoppable: false, stage: "done", step: "", progressPercent: 0,
  partsDone: 0, parts: 0, queueReason: "", errorCode: "", jobError: "", action: "", actionJobId: "", retryWork: "",
  canRedoSummary: false, lectureTextReady: false, remainingSeconds: 0, hasRemaining: false, advice: null, ...over,
});

// Mirrors SummaryView and SummaryLectureText of internal/input/wails/summary.go and lecturetext.go.
const step = (state, over = {}) => ({ state, waitReason: "", message: `msg:${state}`, jobError: "", ...over });
const block = (over = {}) => ({ heading: "Пределы", startSeconds: 725, time: "12:05", paragraphs: ["Первый абзац.", "Второй."], ...over });
const lecture = (over = {}) => ({
  ...step("done"),
  ready: true,
  blocks: [block()],
  updatedAt: "2026-10-19T07:10:00Z",
  partsDone: 0,
  parts: 0,
  canStart: false,
  retryWork: "",
  ...over,
});
const view = (lt = {}, over = {}) => ({
  recordingId: "r1",
  lessonId: "l1",
  title: "Матанализ",
  startedAt: "2026-10-19T06:05:00Z",
  durationSeconds: 2700,
  hasDuration: true,
  transcribed: true,
  summary: { ...step("done"), ready: true, text: "Конспект.", keyPoints: [], updatedAt: "2026-10-19T07:00:00Z" },
  homework: { ...step("done"), assignments: [] },
  lectureText: lecture(lt),
  ...over,
});

test("the recording parses the lecture text fields strictly", () => {
  const r = row({ stage: "lectureText", step: "lectureText", partsDone: 3, parts: 12, lectureTextReady: true });
  assert.deepEqual(parseRecording(r), r);
  assert.equal(parseRecording(row({ stage: "failed", errorCode: "lectureTextFailed", retryWork: "lectureText" })).retryWork, "lectureText");
  for (const bad of [{ partsDone: "3" }, { parts: null }, { lectureTextReady: 1 }, { lectureTextReady: undefined }]) {
    assert.throws(() => parseRecording(row(bad)), undefined, JSON.stringify(bad));
  }
});

test("the summary view carries the lecture text; a malformed one is an error", async () => {
  assert.deepEqual(await summaryApi(async () => view()).view("r1"), view());
  const none = view({ ...step("none"), ready: false, blocks: [], updatedAt: "", canStart: true, retryWork: "lectureText" });
  assert.deepEqual(await summaryApi(async () => none).view("r1"), none);
  for (const bad of [
    { blocks: null },
    { blocks: [block({ paragraphs: "a" })] },
    { blocks: [block({ paragraphs: [1] })] },
    { blocks: [block({ startSeconds: -1 })] },
    { blocks: [block({ heading: null })] },
    { parts: -1 },
    { partsDone: 1.5 },
    { canStart: "no" },
    { retryWork: "summary" },
    { state: "ready" },
  ]) {
    await assert.rejects(summaryApi(async () => view(bad)).view("r1"), undefined, JSON.stringify(bad));
  }
  await assert.rejects(summaryApi(async () => ({ ...view(), lectureText: undefined })).view("r1"));
});

test("copy and save of the lecture text call LectureTextService with the block times", async () => {
  const calls = [];
  const api = lectureTextApi(async (method, ...args) => {
    calls.push([method, ...args]);
    return method === lectureMethods.saveAs ? { status: "saved", message: "Текст лекции сохранён.", name: "x.md" } : null;
  });
  await api.copy("r1");
  assert.equal((await api.saveAs("r1", "md")).name, "x.md");
  assert.deepEqual(calls, [
    [lectureMethods.copy, "r1", true],
    [lectureMethods.saveAs, "r1", "md", true],
  ]);
  assert.deepEqual(Object.values(lectureMethods), [
    "paratech/desktop/internal/input/wails.LectureTextService.Copy",
    "paratech/desktop/internal/input/wails.LectureTextService.SaveAs",
  ]);
  await assert.rejects(lectureTextApi(async () => ({ status: "ok", message: "", name: "" })).saveAs("r1", "txt"));
  for (const msg of Object.values(lectureErrorMessages)) {
    assert.equal(errorText(new Error(msg)), msg);
  }
});

test("the page keeps asking while the lecture text is made, and stops when nothing goes on", () => {
  assert.equal(nextPollMs(view(), 0), null);
  assert.equal(nextPollMs(view(step("running")), 0), pollMs);
  assert.equal(nextPollMs(view(step("waiting", { waitReason: "network" })), 0), pollMs);
  assert.equal(nextPollMs(view(step("failed")), 0), null);
  // Never made (an older transcript): nothing comes until the click, which polls again.
  assert.equal(nextPollMs(view({ ...step("none"), ready: false, canStart: true }), 0), null);
  assert.equal(nextPollMs(view(step("none"), { transcribed: false, summary: { ...step("none"), ready: false, text: "", keyPoints: [], updatedAt: "" }, homework: { ...step("none"), assignments: [] } }), 0), idlePollMs);
});

test("the lecture card's state line, start button, blocks and dates", () => {
  assert.equal(lectureLine(view()), null);
  assert.deepEqual(lectureLine(view(step("running", { message: "Текст лекции составляется: готово частей 3 из 12." }))), {
    kind: null,
    message: "Текст лекции составляется: готово частей 3 из 12.",
    detail: "",
  });
  assert.deepEqual(lectureLine(view({ ...step("failed", { jobError: "Сервис ответил ошибкой." }), ready: false })), {
    kind: "fail",
    message: "msg:failed",
    detail: "Сервис ответил ошибкой.",
  });

  assert.equal(lectureStartLabel(view({ ...step("none"), ready: false, canStart: true, retryWork: "lectureText" })), "Сделать текст лекции");
  assert.equal(lectureStartLabel(view({ ...step("failed"), retryWork: "lectureText" })), "Повторить");
  assert.equal(lectureStartLabel(view(step("running"))), null);
  assert.equal(lectureStartLabel(view()), null);
  // The core decides: without retryWork there is no button.
  assert.equal(lectureStartLabel(view({ ...step("failed"), retryWork: "" })), null);

  assert.ok(showsEarlierLecture(view(step("running"))));
  assert.ok(!showsEarlierLecture(view()));
  assert.ok(!showsEarlierLecture(view({ ...step("failed"), ready: false })));
  assert.equal(lectureUpdatedText(view()), `Составлен ${formatTime("2026-10-19T07:10:00Z")}`);
  assert.equal(lectureUpdatedText(view({ ready: false })), "");

  const blocks = [block({ heading: "  Введение ", paragraphs: [" a ", " ", ""] }), block({ heading: " ", paragraphs: [" "] }), block({ heading: "", time: "1:02:05" })];
  assert.deepEqual(lectureBlocks(view({ blocks })), [
    { heading: "Введение", time: "12:05", paragraphs: ["a"] },
    { heading: "", time: "1:02:05", paragraphs: ["Первый абзац.", "Второй."] },
  ]);
  assert.deepEqual(lectureBlocks(view({ ready: false })), []);
});

const card = (v, over = {}) =>
  renderToStaticMarkup(
    h(LectureTextCard, {
      view: v, busy: "", disabled: false, format: "txt", onFormat: () => undefined, onCopy: () => undefined,
      onSave: () => undefined, onStart: () => undefined, onOpenTranscript: () => undefined, message: null, ...over,
    }),
  );

test("the lecture card renders model text as text: headings with times and paragraphs", () => {
  const hostile = "<img src=x onerror=alert(1)> [ссылка](https://example.com) **жирный**";
  const html = card(view({ blocks: [block({ heading: hostile, paragraphs: [hostile] })] }));
  assert.ok(html.includes("Текст лекции"));
  assert.ok(!html.includes("<img"), "no HTML from the model");
  assert.ok(!html.includes("<a"), "no links from the model");
  assert.ok(!html.includes("<strong"), "no markdown rendering");
  assert.ok(html.includes("&lt;img src=x onerror=alert(1)&gt; [ссылка](https://example.com) **жирный**"));
  assert.match(html, /<h3 class="sm-h"><span class="muted num sm-time">12:05<\/span>/);
  for (const text of ["Скопировать", "Сохранить как…", ".txt", ".md", "Исходная расшифровка", "Составлен"]) {
    assert.ok(html.includes(text), text);
  }
  assert.ok(!html.includes("Повторить") && !html.includes("Сделать текст лекции"));
});

test("the lecture card without a text: the core's state, the start button, no tools", () => {
  const html = card(view({ ...step("none", { message: "Текст лекции ещё не составлялся." }), ready: false, blocks: [], canStart: true, retryWork: "lectureText" }));
  assert.ok(html.includes("Текст лекции ещё не составлялся."));
  assert.ok(html.includes("Сделать текст лекции"));
  assert.ok(!html.includes("Скопировать") && !html.includes("Сохранить как…"));

  const failed = card(view({ ...step("failed", { message: "Не удалось составить текст лекции." }), retryWork: "lectureText" }), {
    message: { kind: "fail", text: "Работа уже выполняется." },
  });
  assert.ok(failed.includes("Не удалось составить текст лекции.") && failed.includes("Повторить"));
  assert.ok(failed.includes(lectureEarlierNote), "the earlier text stays");
  assert.ok(failed.includes("Работа уже выполняется."));

  assert.ok(card(view(), { busy: "copy" }).includes("Копирование…"));
  assert.ok(!card(view({}, { transcribed: false })).includes("Исходная расшифровка"));
  assert.ok(lectureCopiedText.includes("скопирован"));
});

test("the row shows the lecture text with its parts and the progress bar", () => {
  assert.equal(stageLabels.lectureText, "Текст лекции…");
  const v = chainStatusView(row({ stage: "lectureText", step: "lectureText", partsDone: 3, parts: 12 }), 0);
  assert.deepEqual([v.label, v.value, v.kind], ["Текст лекции…", "3 из 12", "run"]);
  assert.equal(chainStatusView(row({ stage: "lectureText", step: "lectureText" }), 0).value, "", "parts unknown yet");
  assert.equal(recordingRowView(row({ stage: "lectureText", progressPercent: 25 }), 0).progress, 25);
  assert.equal(recordingRowView(row({ stage: "lectureText" }), 0).progress, null);

  // Waiting halfway (the Hub stopped): how far it got.
  const waiting = chainStatusView(row({ stage: "queued", step: "lectureText", queueReason: "network", partsDone: 3, parts: 12 }), 0);
  assert.match(waiting.hint, /^Текст лекции: готово 3 из 12\. /);
  assert.match(chainStatusView(row({ stage: "queued", step: "lectureText", queueReason: "inLine" }), 0).hint, /^Дальше текст лекции\./);
});

test("a failed lecture text says so and offers «Повторить» with the lecture text work", () => {
  const r = row({ stage: "failed", step: "lectureText", errorCode: "lectureTextFailed", action: "retry", retryWork: "lectureText" });
  assert.equal(errorTexts.lectureTextFailed, "Не удалось составить текст лекции.");
  assert.equal(chainStatusView(r, 0).hint, `Не удалось составить текст лекции. ${retryHints.retry}`);
  assert.deepEqual(rowAction(r), { kind: "retry", work: "lectureText", label: "Повторить" });
});

test("the row's links: the lecture text first once ready, the transcript then as its source", () => {
  assert.deepEqual(rowLinks(row()), { lectureText: false, summary: "main", transcript: "full" });
  assert.deepEqual(rowLinks(row({ lectureTextReady: true })), { lectureText: true, summary: "extra", transcript: "source" });
  // While a new lecture text runs or after it failed the stored one stays the main link.
  assert.deepEqual(rowLinks(row({ stage: "lectureText", step: "lectureText", lectureTextReady: true })).lectureText, true);
  assert.deepEqual(rowLinks(row({ stage: "lectureText", step: "lectureText" })), { lectureText: false, summary: "main", transcript: "full" });
  assert.deepEqual(rowLinks(row({ stage: "failed", errorCode: "lectureTextFailed" })), { lectureText: false, summary: "main", transcript: "full" });
  assert.deepEqual(rowLinks(row({ stage: "recording", lectureTextReady: true })), { lectureText: false, summary: null, transcript: null });

  assert.equal(rowSummaryOffer({ stage: "lectureText", step: "lectureText", errorCode: "" }), "main");
  assert.equal(rowSummaryOffer({ stage: "queued", step: "lectureText", errorCode: "" }), "main");
  assert.equal(rowSummaryOffer({ stage: "failed", step: "lectureText", errorCode: "lectureTextFailed" }), "main");
  assert.ok(rowOffersTranscript({ stage: "lectureText", errorCode: "" }));

  // TASK-096: one main button; a ready summary leads, the lecture text leads only without it.
  const offers = { texts: true, redo: false };
  const ready = rowActions(row({ lectureTextReady: true }), offers);
  assert.deepEqual([ready.main.label, ready.menu.map((i) => i.label)], ["Открыть конспект", ["Открыть текст лекции", "Открыть расшифровку"]]);
  const running = rowActions(row({ stage: "summarizing", lectureTextReady: true }), offers);
  assert.equal(running.main.label, "Открыть текст лекции");
  const before = rowActions(row(), offers);
  assert.deepEqual([before.main.label, before.menu.map((i) => i.label)], ["Открыть конспект", ["Открыть расшифровку"]]);

  const noop = () => undefined;
  const html = renderToStaticMarkup(
    h(RecordingRow, {
      item: row({ stage: "summarizing", lectureTextReady: true }), now: 0, stopping: false, starting: false,
      onStop: noop, onRunNow: noop, onOpen: noop, onOpenSummary: noop, onOpenTranscript: noop,
    }),
  );
  assert.ok(html.includes(">Открыть текст лекции<") && !html.includes("Открыть расшифровку"));
});

// Source guards: read the TypeScript sources, not the build.
const src = (rel) =>
  readFileSync(new URL(`../src/${rel}`, import.meta.url), "utf8")
    .replace(/\/\*[\s\S]*?\*\//g, "")
    .replace(/(^|[^:])\/\/.*$/gm, "$1");

test("the lecture text renders text only, talks only to the core and starts through the activity binding", () => {
  for (const rel of ["pages/LectureTextParts.tsx", "lectureTextView.ts", "pages/RecordingRow.tsx", "pages/TranscriptLinks.tsx"]) {
    const text = src(rel);
    for (const banned of ["dangerouslySetInnerHTML", "innerHTML", "href=", "window.open", "localStorage", "fetch(", "marked",
      "markdown", "navigator.clipboard"]) {
      assert.ok(!text.includes(banned), `${rel} uses ${banned}`);
    }
  }
  const page = src("pages/SummaryPage.tsx");
  assert.match(page, /props\.lecture\.copy\(id\)/);
  assert.match(page, /props\.lecture\.saveAs\(id, lectureFormat\)/);
  assert.match(page, /props\.activity\.retryPostProcessing\(id, "lectureText"\)/);
  assert.match(src("pages/RecordingRow.tsx"), /props\.onOpenSummary\?\.\(r\.id, "lecture"\)/);
  assert.match(src("App.tsx"), /section=\{summarySection\}/);
});
