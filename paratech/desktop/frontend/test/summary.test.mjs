// Summary viewer (TASK-087): bindings glue, polling, the row offer, texts, the route, rendering as text only and source
// guards. Runs on the modules compiled by `npm run test`.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import server from "react-dom/server";
import { errorMessages, methods, summaryApi } from "../.test-build/bindings/summary.js";
import { closeTranscript, routeToAssignment, routeToPage, routeToSummary, routeToTranscript } from "../.test-build/nav.js";
import { HomeworkCard, SummaryCard } from "../.test-build/pages/SummaryParts.js";
import {
  assignmentMeta,
  earlierNote,
  errorText,
  headerMeta,
  homeworkLine,
  idlePollMs,
  keyPointList,
  maxErrorPollMs,
  nextPollMs,
  pageTitle,
  pollMs,
  retryLabel,
  rowSummaryOffer,
  showsEarlier,
  stepLine,
  summaryLine,
  summaryParagraphs,
  updatedText,
} from "../.test-build/summaryView.js";
import { rowOffersTranscript } from "../.test-build/transcriptView.js";

const { renderToStaticMarkup } = server;

// Mirrors SummaryView of internal/input/wails/summary.go.
const step = (state, over = {}) => ({ state, waitReason: "", message: `msg:${state}`, jobError: "", ...over });
const assignment = (over = {}) => ({
  id: "a1",
  title: "Решить задачи 1–5",
  description: "Из учебника, параграф 3",
  due: "2026-10-25T20:59:00Z",
  userEdited: false,
  ...over,
});
const view = (over = {}) => ({
  recordingId: "r1",
  lessonId: "l1",
  title: "Матанализ",
  startedAt: "2026-10-19T06:05:00Z",
  durationSeconds: 2700,
  hasDuration: true,
  transcribed: true,
  summary: {
    ...step("done"),
    ready: true,
    text: "Первый абзац.\nВторая строка.\n\nВторой абзац.",
    keyPoints: ["Пределы", " ", "Производные"],
    updatedAt: "2026-10-19T07:00:00Z",
    ...over.summary,
  },
  homework: { ...step("done"), assignments: [assignment()], ...over.homework },
  ...Object.fromEntries(Object.entries(over).filter(([k]) => k !== "summary" && k !== "homework")),
});

test("the api calls the bound methods with their arguments and checks the replies", async () => {
  const calls = [];
  const replies = {
    [methods.view]: view(),
    [methods.copyAll]: null,
    [methods.saveAs]: { status: "saved", message: "Конспект сохранён.", name: "Конспект Матанализ 2026-10-19.md" },
  };
  const api = summaryApi(async (method, ...args) => {
    calls.push([method, ...args]);
    return replies[method];
  });
  assert.deepEqual(await api.view("r1"), view());
  await api.copyAll("r1");
  assert.equal((await api.saveAs("r1", "md")).name, "Конспект Матанализ 2026-10-19.md");
  assert.deepEqual(calls, [
    [methods.view, "r1"],
    [methods.copyAll, "r1"],
    [methods.saveAs, "r1", "md"],
  ]);
  assert.equal(Object.keys(methods).length, 3);
  for (const name of Object.values(methods)) {
    assert.match(name, /^paratech\/desktop\/internal\/input\/wails\.SummaryService\.[A-Z]\w+$/);
  }
});

test("a malformed reply is an error, not a default", async () => {
  const bad = [
    null,
    {},
    view({ title: null }),
    view({ durationSeconds: -1 }),
    view({ durationSeconds: 1.5 }),
    view({ hasDuration: "yes" }),
    view({ summary: { state: "ready" } }),
    view({ summary: { waitReason: "later" } }),
    view({ summary: { keyPoints: "a" } }),
    view({ summary: { keyPoints: [1] } }),
    view({ summary: { ready: 1 } }),
    view({ homework: { assignments: null } }),
    view({ homework: { assignments: [{ ...assignment(), due: null }] } }),
    view({ homework: { assignments: [{ ...assignment(), userEdited: "no" }] } }),
    { ...view(), summary: null },
  ];
  for (const b of bad) {
    await assert.rejects(summaryApi(async () => b).view("r1"), undefined, JSON.stringify(b));
  }
  for (const b of [null, { status: "ok", message: "", name: "" }, { status: "saved", message: "", name: 1 }]) {
    await assert.rejects(summaryApi(async () => b).saveAs("r1", "txt"), undefined, JSON.stringify(b));
  }
  const empty = view({ lessonId: "", summary: { ready: false, text: "", keyPoints: [], updatedAt: "" } });
  assert.deepEqual(await summaryApi(async () => empty).view("r1"), empty);
});

test("the core's error texts are shown as they are, anything else as the generic text", () => {
  for (const msg of Object.values(errorMessages)) {
    assert.equal(errorText(new Error(msg)), msg);
    assert.equal(errorText(msg), msg);
  }
  assert.equal(errorText(new Error("sql: no rows")), errorMessages.unavailable);
  assert.equal(errorText(undefined), errorMessages.unavailable);
});

test("a recording row offers the summary from its chain: main when it is made, extra while it is on its way or failed", () => {
  // Mirrors the chain stages, steps and error codes of internal/cases/activity/types.go.
  assert.equal(rowSummaryOffer({ stage: "done", step: "", errorCode: "" }), "main");
  assert.equal(rowSummaryOffer({ stage: "failed", step: "summary", errorCode: "homeworkFailed" }), "main");
  assert.equal(rowSummaryOffer({ stage: "summarizing", step: "summary", errorCode: "" }), "extra");
  assert.equal(rowSummaryOffer({ stage: "queued", step: "summary", errorCode: "" }), "extra");
  assert.equal(rowSummaryOffer({ stage: "failed", step: "summary", errorCode: "summaryFailed" }), "extra");
  for (const r of [
    { stage: "queued", step: "transcription", errorCode: "" },
    { stage: "transcribing", step: "transcription", errorCode: "" },
    { stage: "recording", step: "", errorCode: "" },
    { stage: "saving", step: "", errorCode: "" },
    { stage: "failed", step: "transcription", errorCode: "transcriptionFailed" },
    { stage: "failed", step: "", errorCode: "recordingFailed" },
    { stage: "failed", step: "", errorCode: "silentRecording" },
    { stage: "unknown", step: "", errorCode: "" },
  ]) {
    assert.equal(rowSummaryOffer(r), null, JSON.stringify(r));
  }
  // The row puts the summary inside the transcript's slot: every chain that offers the summary offers the transcript.
  for (const stage of ["recording", "saving", "queued", "transcribing", "summarizing", "done", "failed"]) {
    for (const s of ["", "transcription", "summary"]) {
      for (const errorCode of ["", "recordingFailed", "transcriptionFailed", "summaryFailed", "homeworkFailed", "silentRecording"]) {
        const r = { stage, step: s, errorCode };
        if (rowSummaryOffer(r) !== null) {
          assert.ok(rowOffersTranscript(r), JSON.stringify(r));
        }
      }
    }
  }
});

test("the page asks again only while a step is not finished, slower before it starts, backing off after errors", () => {
  assert.equal(nextPollMs(null, 0), pollMs);
  assert.equal(nextPollMs(view(), 0), null);
  assert.equal(nextPollMs(view({ summary: step("failed"), homework: step("done") }), 0), null);
  assert.equal(nextPollMs(view({ summary: step("running") }), 0), pollMs);
  assert.equal(nextPollMs(view({ homework: step("waiting", { waitReason: "network" }) }), 0), pollMs);
  assert.equal(nextPollMs(view({ transcribed: false, summary: step("none"), homework: step("none") }), 0), idlePollMs);
  assert.equal(nextPollMs(view(), 1), pollMs);
  assert.equal(nextPollMs(view(), 2), pollMs * 2);
  assert.equal(nextPollMs(view(), 50), maxErrorPollMs);
  assert.ok(pollMs >= 2000 && pollMs <= 5000);
});

test("the head, the paragraphs, the key points and the assignments are arranged from the core's view", () => {
  assert.equal(pageTitle(view()), "Матанализ");
  assert.equal(pageTitle(view({ title: " " })), "Запись");
  const meta = headerMeta(view());
  assert.match(meta, / · Длительность 0:45:00$/);
  assert.equal(headerMeta(view({ hasDuration: false })).includes("Длительность"), false);
  assert.equal(headerMeta(view({ startedAt: "", hasDuration: false })), "");

  assert.deepEqual(summaryParagraphs("Первый абзац.\r\nВторая строка.\r\n\r\n  \n\nВторой абзац.\n"), [
    "Первый абзац.\nВторая строка.",
    "Второй абзац.",
  ]);
  assert.deepEqual(summaryParagraphs("  \n "), []);
  assert.deepEqual(keyPointList([" Пределы ", "", "  ", "Производные"]), ["Пределы", "Производные"]);

  assert.match(assignmentMeta(assignment()), /^до /);
  assert.equal(assignmentMeta(assignment({ due: "" })), "Без срока");
  assert.equal(assignmentMeta(assignment({ due: "", userEdited: true })), "Без срока · изменено вами");
  assert.match(updatedText(view()), /^Составлен /);
  assert.equal(updatedText(view({ summary: { ready: false, updatedAt: "" } })), "");
});

test("the state of each step is the core's text, with its error, and stays visible next to an earlier summary", () => {
  assert.deepEqual(stepLine(step("failed", { jobError: "Сервис ответил ошибкой." })), {
    kind: "fail",
    message: "msg:failed",
    detail: "Сервис ответил ошибкой.",
  });
  assert.deepEqual(stepLine(step("waiting", { waitReason: "retry", jobError: "e" })), { kind: "wait", message: "msg:waiting", detail: "e" });
  assert.deepEqual(stepLine(step("running", { jobError: "e" })), { kind: null, message: "msg:running", detail: "" });
  assert.equal(stepLine(step("none")).kind, null);

  assert.equal(summaryLine(view()), null, "a ready summary needs no line");
  assert.equal(summaryLine(view({ summary: { ready: false } })).message, "msg:done");
  const rerun = view({ summary: step("failed") });
  assert.equal(summaryLine(rerun).kind, "fail");
  assert.ok(showsEarlier(rerun));
  assert.ok(!showsEarlier(view()));
  assert.ok(!showsEarlier(view({ summary: { ...step("failed"), ready: false } })));

  assert.equal(homeworkLine(view()), null, "found assignments need no line");
  assert.equal(homeworkLine(view({ homework: { assignments: [] } })).message, "msg:done");
  assert.equal(homeworkLine(view({ homework: { ...step("running"), assignments: [] } })).message, "msg:running");

  assert.equal(retryLabel(view({ summary: { ...step("failed"), ready: false } }), "summary"), "Повторить");
  assert.equal(retryLabel(view(), "summary"), "Сделать конспект заново");
  assert.equal(retryLabel(view({ summary: step("running") }), "summary"), null);
  assert.equal(retryLabel(view({ homework: { ...step("failed"), assignments: [] } }), "homework"), "Повторить");
  assert.equal(retryLabel(view(), "homework"), null);
});

test("the summary opens over the current section in place of the transcript, and jumps to an assignment", () => {
  const s = routeToSummary(routeToPage("homework"), "r1");
  assert.deepEqual(s, { page: "homework", transcript: "", summary: "r1", assignment: "" });
  assert.deepEqual(routeToTranscript(s, "r1"), { page: "homework", transcript: "r1", summary: "", assignment: "" });
  assert.deepEqual(routeToSummary(routeToTranscript(routeToPage("recordings"), "r2"), "r2"), {
    page: "recordings",
    transcript: "",
    summary: "r2",
    assignment: "",
  });
  assert.deepEqual(closeTranscript(s), { page: "homework", transcript: "", summary: "", assignment: "" });
  assert.deepEqual(routeToAssignment("a1"), { page: "homework", transcript: "", summary: "", assignment: "a1" });
  assert.equal(routeToPage("homework").assignment, "", "a rail click shows the list");
});

const hostile = '<img src=x onerror=alert(1)> [ссылка](https://evil.example) **жирный**';

test("model text renders as text only: no HTML, no links, line breaks kept as text", () => {
  const v = view({
    summary: { text: `${hostile}\nстрока\n\nабзац`, keyPoints: [hostile] },
    homework: { assignments: [assignment({ title: hostile, description: "<b>описание</b>" })] },
  });
  const summary = renderToStaticMarkup(h(SummaryCard, { view: v }));
  assert.ok(!summary.includes("<img"), summary);
  assert.ok(summary.includes("&lt;img src=x onerror=alert(1)&gt; [ссылка](https://evil.example) **жирный**\nстрока</p>"));
  assert.ok(summary.includes("<p>абзац</p>"));
  assert.ok(summary.includes("<li>&lt;img"), "key points are list items");
  assert.ok(!summary.includes("href"));
  assert.ok(!summary.includes("Повторить") && !summary.includes("Сделать конспект заново"), "no retry button without TASK-086");

  const opened = [];
  const homework = renderToStaticMarkup(h(HomeworkCard, { view: v, onOpenAssignment: (id) => opened.push(id) }));
  assert.ok(!homework.includes("<img") && !homework.includes("<b>"), homework);
  assert.ok(homework.includes("&lt;b&gt;описание&lt;/b&gt;"));
  assert.ok(homework.includes("Открыть задание"));
  assert.ok(!homework.includes("href"));
});

test("the states show, and the retry buttons appear only with a handler", () => {
  const failed = view({
    summary: { ...step("failed", { message: "Не удалось составить конспект.", jobError: "Сервис ответил ошибкой." }), ready: false },
    homework: { ...step("failed", { message: "Не удалось найти домашние задания." }), assignments: [] },
  });
  const s = renderToStaticMarkup(h(SummaryCard, { view: failed }));
  assert.ok(s.includes("Не удалось составить конспект.") && s.includes("Сервис ответил ошибкой."), s);
  assert.ok(!s.includes("<button"));
  const withRetry = renderToStaticMarkup(h(SummaryCard, { view: failed, onRetry: () => undefined }));
  assert.ok(withRetry.includes("Повторить"));
  const hw = renderToStaticMarkup(h(HomeworkCard, { view: failed, onOpenAssignment: () => undefined, onRetry: () => undefined }));
  assert.ok(hw.includes("Не удалось найти домашние задания.") && hw.includes("Повторить"), hw);

  const rerun = renderToStaticMarkup(h(SummaryCard, { view: view({ summary: step("waiting", { message: "Конспект ждёт очереди." }) }) }));
  assert.ok(rerun.includes("Конспект ждёт очереди.") && rerun.includes(earlierNote) && rerun.includes("Первый абзац."), rerun);
});

// Source guards: read the TypeScript sources, not the build.
const src = (rel) =>
  readFileSync(new URL(`../src/${rel}`, import.meta.url), "utf8")
    .replace(/\/\*[\s\S]*?\*\//g, "")
    .replace(/(^|[^:])\/\/.*$/gm, "$1");

test("the viewer renders text only, talks only to the core and polls without overlap", () => {
  const files = ["pages/SummaryPage.tsx", "pages/SummaryParts.tsx", "summaryView.ts", "bindings/summary.ts", "openSummary.ts"];
  for (const rel of files) {
    const text = src(rel);
    for (const banned of ["dangerouslySetInnerHTML", "innerHTML", "outerHTML", "insertAdjacentHTML", "href=", "window.open",
      "localStorage", "sessionStorage", "navigator.clipboard", "fetch(", "console.", "marked", "markdown"]) {
      assert.ok(!text.includes(banned), `${rel} uses ${banned}`);
    }
  }
  const page = src("pages/SummaryPage.tsx");
  assert.ok(!page.includes("setInterval"), "the next poll waits for the previous answer");
  assert.match(page, /clearTimeout\(timer\)/);
  assert.match(page, /api\.copyAll\(id\)/);
  assert.match(page, /api\.saveAs\(id, format\)/);
});

test("the row, the homework page and the transcript open the summary without a call per row", () => {
  const rows = src("pages/RecordingsPage.tsx");
  assert.match(rows, /rowSummaryOffer\(r\)/);
  assert.match(rows, /onClick=\{\(\) => openSummary\(r\.id\)\}/);
  assert.ok(!rows.includes("summaryApi"), "no summary call per row");
  const links = src("pages/TranscriptLinks.tsx");
  assert.match(links, /rowSummaryOffer\(r\)/);
  assert.ok(!links.includes("summaryApi"));
  assert.match(src("pages/TranscriptPage.tsx"), /props\.onOpenSummary\(id\)/);
  const app = src("App.tsx");
  assert.match(app, /<SummaryPage/);
  assert.match(app, /onOpenAssignment=\{openAssignment\}/);
  assert.match(src("pages/HomeworkPage.tsx"), /useState\(props\.openAssignment \?\? ""\)/);
});
