// Summary viewer (TASK-087): bindings glue, polling, the row offer, texts, the route, rendering as text only and source
// guards. Runs on the modules compiled by `npm run test`.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import server from "react-dom/server";
import { errorMessages, methods, summaryApi } from "../.test-build/bindings/summary.js";
import { closeTranscript, routeToAssignment, routeToPage, routeToSummary, routeToTranscript } from "../.test-build/nav.js";
import { HomeworkCard, SummaryCard, SummarySections } from "../.test-build/pages/SummaryParts.js";
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
  redoTexts,
  retryAction,
  retryClick,
  retryLabel,
  rowSummaryOffer,
  showsEarlier,
  stepLine,
  summaryLine,
  summaryParagraphs,
  summarySections,
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
  subject: "Матанализ",
  subjectSource: "schedule",
  kind: "lecture",
  teacher: "",
  teacherSource: "",
  lessonDate: "2026-10-19T06:00:00Z",
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
    // A summary made before TASK-101: one section without a heading.
    sections: [{ heading: "", text: "Первый абзац.\nВторая строка.\n\nВторой абзац.", points: [] }],
    keyPoints: ["Пределы", " ", "Производные"],
    updatedAt: "2026-10-19T07:00:00Z",
    ...over.summary,
  },
  homework: { ...step("done"), assignments: [assignment()], ...over.homework },
  lectureText: {
    ...step("done"),
    ready: true,
    blocks: [{ heading: "Пределы", startSeconds: 0, time: "0:00", paragraphs: ["Абзац."] }],
    updatedAt: "2026-10-19T07:10:00Z",
    partsDone: 0,
    parts: 0,
    canStart: false,
    retryWork: "",
    ...over.lectureText,
  },
  ...Object.fromEntries(Object.entries(over).filter(([k]) => !["summary", "homework", "lectureText"].includes(k))),
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
    view({ summary: { sections: null } }),
    view({ summary: { sections: [null] } }),
    view({ summary: { sections: [{ heading: 1, text: "", points: [] }] } }),
    view({ summary: { sections: [{ heading: "", text: "", points: null }] } }),
    view({ summary: { sections: [{ heading: "", text: "", points: [1] }] } }),
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
  const empty = view({ lessonId: "", summary: { ready: false, text: "", sections: [], keyPoints: [], updatedAt: "" } });
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
  for (const stage of ["recording", "saving", "queued", "transcribing", "summarizing", "lectureText", "done", "failed"]) {
    for (const s of ["", "transcription", "summary", "lectureText"]) {
      for (const errorCode of ["", "recordingFailed", "transcriptionFailed", "summaryFailed", "homeworkFailed",
        "lectureTextFailed", "silentRecording"]) {
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
    summary: {
      text: `${hostile}\nстрока\n\nабзац`,
      sections: [{ heading: "", text: `${hostile}\nстрока\n\nабзац`, points: [] }],
      keyPoints: [hostile],
    },
    homework: { assignments: [assignment({ title: hostile, description: "<b>описание</b>" })] },
  });
  const summary = renderToStaticMarkup(h(SummaryCard, { view: v }));
  assert.ok(!summary.includes("<img"), summary);
  assert.ok(summary.includes("&lt;img src=x onerror=alert(1)&gt; [ссылка](https://evil.example) **жирный**\nстрока</p>"));
  assert.ok(summary.includes("<p>абзац</p>"));
  assert.ok(summary.includes("<li>&lt;img"), "key points are list items");
  assert.ok(!summary.includes("href"));
  assert.ok(!summary.includes("Повторить") && !summary.includes("Сделать конспект заново"), "no retry button without a handler");

  const opened = [];
  const homework = renderToStaticMarkup(h(HomeworkCard, { view: v, onOpenAssignment: (id) => opened.push(id) }));
  assert.ok(!homework.includes("<img") && !homework.includes("<b>"), homework);
  assert.ok(homework.includes("&lt;b&gt;описание&lt;/b&gt;"));
  assert.ok(homework.includes("Открыть задание"));
  assert.ok(!homework.includes("href"));
});

// TASK-101: the summary in logical sections.
const sectioned = () =>
  view({
    summary: {
      text: "Реки:\nРеки текут.\n\nВ море.\n- Исток\n\nОзёра:\nОзёра стоят.",
      sections: [
        { heading: "Реки", text: "Реки текут.\n\nВ море.", points: ["Исток", " "] },
        { heading: " ", text: " \n ", points: [] },
        { heading: "Озёра", text: "Озёра стоят.", points: [] },
      ],
      keyPoints: ["Вода"],
    },
  });

test("sections are trimmed, split into paragraphs, and empty ones skipped with their place kept", () => {
  assert.deepEqual(summarySections(sectioned().summary.sections), [
    { index: 0, heading: "Реки", paragraphs: ["Реки текут.", "В море."], points: ["Исток"] },
    { index: 2, heading: "Озёра", paragraphs: ["Озёра стоят."], points: [] },
  ]);
  assert.deepEqual(summarySections([]), []);
});

test("sections render as headings, paragraphs and points; an old summary has no heading; «Главное» stays", () => {
  const html = renderToStaticMarkup(h(SummaryCard, { view: sectioned() }));
  assert.ok(
    html.includes(
      '<section class="sm-sec"><h3 class="sm-h">Реки</h3><p>Реки текут.</p><p>В море.</p><ul class="sm-points"><li>Исток</li></ul></section>' +
        '<section class="sm-sec"><h3 class="sm-h">Озёра</h3><p>Озёра стоят.</p></section>',
    ),
    html,
  );
  assert.equal(html.split('class="sm-sec"').length - 1, 2, "the empty section is not shown");
  assert.ok(html.indexOf("Озёра стоят.") < html.indexOf("Главное") && html.includes("<li>Вода</li>"), html);

  const old = renderToStaticMarkup(h(SummaryCard, { view: view() }));
  assert.ok(old.includes('<section class="sm-sec"><p>Первый абзац.\nВторая строка.</p><p>Второй абзац.</p></section>'), old);
  assert.equal(old.split("<h3").length - 1, 1, "only «Главное» is a heading");

  const notReady = renderToStaticMarkup(h(SummaryCard, { view: view({ summary: { ...step("running"), ready: false } }) }));
  assert.ok(!notReady.includes("sm-sec"), "nothing but the state before the summary is ready");

  const focused = renderToStaticMarkup(h(SummarySections, { sections: sectioned().summary.sections, focusSection: 2 }));
  assert.ok(focused.includes('<section class="sm-sec kb-focus"><h3 class="sm-h">Озёра</h3>'), focused);
});

test("section headings, paragraphs and points are text only", () => {
  const html = renderToStaticMarkup(
    h(SummarySections, { sections: [{ heading: hostile, text: `${hostile}\n\n<b>x</b>`, points: [hostile] }] }),
  );
  assert.ok(!html.includes("<img") && !html.includes("<b>") && !html.includes("href"), html);
  assert.ok(html.includes(`<h3 class="sm-h">&lt;img src=x onerror=alert(1)&gt; [ссылка](https://evil.example) **жирный**</h3>`), html);
  assert.ok(html.includes("<p>&lt;b&gt;x&lt;/b&gt;</p>") && html.includes("<li>&lt;img"), html);
});

test("the states show, and the retry buttons appear only with a handler", () => {
  const failed = view({
    summary: { ...step("failed", { message: "Не удалось составить конспект.", jobError: "Сервис ответил ошибкой." }), ready: false },
    homework: { ...step("failed", { message: "Не удалось найти домашние задания." }), assignments: [] },
  });
  const s = renderToStaticMarkup(h(SummaryCard, { view: failed }));
  assert.ok(s.includes("Не удалось составить конспект."), s);
  // The job error is technical: only the tooltip of the state line (TASK-097).
  assert.ok(s.includes('title="Сервис ответил ошибкой."'), s);
  assert.equal(s.split("Сервис ответил ошибкой.").length - 1, 1, "and nowhere else");
  assert.ok(!s.includes("<button"));
  const withRetry = renderToStaticMarkup(h(SummaryCard, { view: failed, onRetry: () => undefined }));
  assert.ok(withRetry.includes("Повторить"));
  const hw = renderToStaticMarkup(h(HomeworkCard, { view: failed, onOpenAssignment: () => undefined, onRetry: () => undefined }));
  assert.ok(hw.includes("Не удалось найти домашние задания.") && hw.includes("Повторить"), hw);

  const rerun = renderToStaticMarkup(h(SummaryCard, { view: view({ summary: step("waiting", { message: "Конспект ждёт очереди." }) }) }));
  assert.ok(rerun.includes("Конспект ждёт очереди.") && rerun.includes(earlierNote) && rerun.includes("Первый абзац."), rerun);
});

test("a failed step is retried at once, a ready summary is made again only after the confirmation, a running one offers nothing", () => {
  const failed = view({
    summary: { ...step("failed"), ready: false },
    homework: { ...step("failed"), assignments: [] },
  });
  assert.deepEqual(retryAction(failed, "summary"), { kind: "retry", work: "summary" });
  assert.deepEqual(retryAction(failed, "homework"), { kind: "retry", work: "homework" });
  assert.deepEqual(retryAction(view(), "summary"), { kind: "redo" });
  for (const state of ["waiting", "running"]) {
    const going = view({ summary: step(state), homework: { ...step(state), assignments: [] } });
    assert.equal(retryAction(going, "summary"), null, state);
    assert.equal(retryAction(going, "homework"), null, state);
    assert.equal(renderToStaticMarkup(h(SummaryCard, { view: going, onRetry: () => undefined })).includes("<button"), false, state);
  }

  const clicks = (v, s) => {
    const calls = [];
    retryClick(v, s, { retry: (work) => calls.push(`retry:${work}`), askRedo: () => calls.push("ask") });
    return calls;
  };
  assert.deepEqual(clicks(failed, "summary"), ["retry:summary"]);
  assert.deepEqual(clicks(failed, "homework"), ["retry:homework"]);
  assert.deepEqual(clicks(view(), "summary"), ["ask"], "a redo spends quota: the click only asks");
  assert.deepEqual(clicks(view({ summary: step("running") }), "summary"), []);
  assert.deepEqual(clicks(view(), "homework"), []);

  assert.match(redoTexts.body, /квоту/);
  assert.match(redoTexts.body, /Текущий конспект останется/);
  const redo = renderToStaticMarkup(h(SummaryCard, { view: view(), onRetry: () => undefined }));
  assert.ok(redo.includes("Сделать конспект заново"), redo);
  const idle = renderToStaticMarkup(h(SummaryCard, { view: failed, onRetry: () => undefined }));
  assert.ok(!idle.includes("disabled"), idle);
  for (const card of [SummaryCard, HomeworkCard]) {
    const busy = renderToStaticMarkup(
      h(card, { view: failed, onRetry: () => undefined, retryDisabled: true, onOpenAssignment: () => undefined }),
    );
    assert.ok(busy.includes("Повторить") && busy.includes("disabled"), "no start while copy or save runs: " + busy);
  }
});

test("the page retries through the activity binding and redoes the summary only from the dialog's confirm", () => {
  const page = src("pages/SummaryPage.tsx");
  assert.match(page, /activityApi\(callByName\)/);
  assert.match(page, /retryClick\(view, step,/);
  assert.match(page, /props\.activity\.retryPostProcessing\(id, work\)/);
  assert.equal(page.split("redoSummary(").length - 1, 1, "one call site of redoSummary");
  const redo = page.slice(page.indexOf("const redo = () =>"), page.indexOf("};", page.indexOf("const redo = () =>")));
  assert.match(redo, /props\.activity\.redoSummary\(id\)/, "the call site is the confirm handler");
  assert.match(page, /onConfirm=\{redo\}/);
  assert.match(page, /onCancel=\{\(\) => setAskingRedo\(false\)\}/, "cancel only closes the dialog");
  assert.match(page, /open=\{askingRedo && redoOffered\}/, "no dialog once a new run started");
  assert.match(page, /actionErrorText\(err\)/);
  const start = page.slice(page.indexOf("const start = "), page.indexOf("const retry = "));
  assert.equal(start.split("props.onStarted()").length - 1, 1, "one reload");
  assert.ok(start.indexOf("props.onStarted()") > start.indexOf("catch (err)"), "a refused start reloads too");
  assert.match(start, /return refused;/, "the refusal stays visible after the reload");
  assert.match(page, /retryDisabled=\{!idle\}/);
  assert.ok(!page.includes("window.confirm"));
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
  // The row's actions (rowActions, TASK-096) come from rowLinks and rowSummaryOffer over the list's chain.
  assert.match(src("recordingRowView.ts"), /const summary = rowSummaryOffer\(r\)/);
  assert.match(src("recordingRowView.ts"), /: rowSummaryOffer\(r\)/);
  const rows = src("pages/RecordingRow.tsx");
  assert.match(rows, /rowActions\(r, /);
  assert.match(rows, /props\.onOpenSummary\?\.\(r\.id\)/);
  assert.match(src("pages/RecordingsPage.tsx"), /onOpenSummary=\{openSummary\}/);
  for (const rel of ["pages/RecordingRow.tsx", "pages/RecordingsPage.tsx"]) {
    assert.ok(!src(rel).includes("summaryApi"), "no summary call per row");
  }
  const links = src("pages/TranscriptLinks.tsx");
  assert.match(links, /rowSummaryOffer\(r\)/);
  assert.ok(!links.includes("summaryApi"));
  assert.match(src("pages/TranscriptPage.tsx"), /props\.onOpenSummary\(id\)/);
  const app = src("App.tsx");
  assert.match(app, /<SummaryPage/);
  assert.match(app, /onOpenAssignment=\{openAssignment\}/);
  assert.match(src("pages/HomeworkPage.tsx"), /useState\(props\.openAssignment \?\? ""\)/);
});

// TASK-100: the summary screen shows one part at a time in a segmented control.
test("the summary screen has the three segments, the summary first, the lecture text first from its row", async () => {
  const { firstSegment, summarySegments } = await import("../.test-build/summaryView.js");
  assert.deepEqual(summarySegments.map((s) => s.label), ["Конспект", "Текст лекции", "Задания"]);
  assert.deepEqual(summarySegments.map((s) => s.value), ["summary", "lecture", "homework"]);
  assert.equal(firstSegment(), "summary");
  assert.equal(firstSegment("lecture"), "lecture");
});

test("the segments are a tablist with one tab stop, moved by the arrows, Home and End", async () => {
  const { SegmentTabs, nextSegment, segmentPanelId, segmentTabId } = await import("../.test-build/ui/controls.js");
  const { summarySegments } = await import("../.test-build/summaryView.js");
  const html = renderToStaticMarkup(
    h(SegmentTabs, { label: "Раздел", base: "s", options: summarySegments, value: "summary", onChange: () => {} }),
  );
  assert.match(html, /^<div class="seg lg" role="tablist" aria-label="Раздел">/);
  assert.equal(html.split('role="tab"').length - 1, 3);
  assert.equal(html.split('aria-selected="true"').length - 1, 1, "one chosen segment");
  assert.match(html, /id="s-tab-summary" aria-selected="true" aria-controls="s-panel-summary" tabindex="0">Конспект</);
  assert.match(html, /id="s-tab-lecture" aria-selected="false" tabindex="-1">Текст лекции</);
  assert.match(html, /id="s-tab-homework" aria-selected="false" tabindex="-1">Задания</);
  assert.equal(segmentTabId("s", "lecture"), "s-tab-lecture");
  assert.equal(segmentPanelId("s", "lecture"), "s-panel-lecture");
  assert.equal(nextSegment(summarySegments, "summary", "ArrowRight"), "lecture");
  assert.equal(nextSegment(summarySegments, "homework", "ArrowRight"), "summary", "wraps");
  assert.equal(nextSegment(summarySegments, "summary", "ArrowLeft"), "homework");
  assert.equal(nextSegment(summarySegments, "lecture", "Home"), "summary");
  assert.equal(nextSegment(summarySegments, "lecture", "End"), "homework");
  assert.equal(nextSegment(summarySegments, "lecture", "Enter"), null);
  assert.equal(nextSegment([], "x", "ArrowRight"), null);
});

test("the page shows only the chosen segment, in its tabpanel", () => {
  const page = src("pages/SummaryPage.tsx");
  assert.match(page, /<SegmentTabs label="Раздел" base=\{tabsId\} options=\{summarySegments\} value=\{segment\}/);
  assert.match(page, /useState<SummarySegment>\(\(\) => firstSegment\(props\.section\)\)/);
  assert.match(page, /role="tabpanel"\s+id=\{segmentPanelId\(tabsId, segment\)\}\s+aria-labelledby=\{segmentTabId\(tabsId, segment\)\}/);
  for (const [seg, part] of [["summary", "<SummaryCard"], ["lecture", "<LectureTextCard"], ["homework", "<HomeworkCard"]]) {
    const at = page.indexOf(`{segment === "${seg}" && (`);
    assert.ok(at > 0, `a branch for ${seg}`);
    assert.equal(page.split(part).length - 1, 1, `${part} is rendered once`);
    const next = page.indexOf("{segment === ", at + 1);
    const branch = page.slice(at, next < 0 ? page.indexOf("<Dialog") : next);
    assert.ok(branch.includes(part), `${part} is inside the ${seg} branch`);
  }
  // The summary tools stay with the summary; the screen is one reading column at any width.
  const summary = page.slice(page.indexOf('{segment === "summary" && ('), page.indexOf('{segment === "lecture" && ('));
  assert.match(summary, /Скопировать конспект/);
  assert.match(summary, /Конспект хранится на этом компьютере/);
  const css = readFileSync(new URL("../src/styles.css", import.meta.url), "utf8");
  assert.match(css, /\.page\.sm \{ width: 100%; max-width: 800px; margin-inline: auto; \}/);
});
