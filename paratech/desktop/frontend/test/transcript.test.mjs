// Transcript viewer (TASK-079): bindings glue, fetch planning, paragraphs, search and highlight, texts, the route and
// source guards. Runs on the modules compiled by `npm run test`.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { errorMessages, maxPage, methods, transcriptApi } from "../.test-build/bindings/transcript.js";
import { closeTranscript, initialRoute, routeToPage, routeToTranscript } from "../.test-build/nav.js";
import {
  applyPage,
  canOpenTranscript,
  emptyLoaded,
  errorText,
  findRanges,
  foldText,
  isLive,
  lessonRecordings,
  localMatch,
  matchCountText,
  maxParagraphChars,
  morePages,
  idlePollMs,
  maxErrorPollMs,
  nextFetch,
  nextPollMs,
  pageSize,
  pollMs,
  paragraphPieces,
  paragraphs,
  partialNote,
  playSeconds,
  rowOffersTranscript,
  saveResultText,
  searchParagraphs,
  searchQuery,
  statusView,
  stepMatch,
} from "../.test-build/transcriptView.js";

// Mirrors TranscriptStatus and TranscriptSegment of internal/input/wails/transcript.go.
const status = (over = {}) => ({
  recordingId: "r1",
  title: "Матанализ",
  startedAt: "2026-10-19T06:05:00Z",
  state: "done",
  percent: 100,
  message: "Расшифровка готова.",
  segments: 3,
  final: true,
  audioUrl: "/media/recording/r1",
  ...over,
});

const mmss = (ms) => {
  const s = Math.floor(ms / 1000);
  return `${String(Math.floor(s / 60)).padStart(2, "0")}:${String(s % 60).padStart(2, "0")}`;
};
const seg = (index, startMs, text, endMs = startMs + 1500) => ({ index, startMs, endMs, start: mmss(startMs), text });
const page = (from, total, segments, final = true) => ({ from, total, final, segments });

test("the api calls the bound methods with their arguments and checks the replies", async () => {
  const calls = [];
  const replies = {
    [methods.status]: status(),
    [methods.segments]: page(0, 1, [seg(0, 0, " Привет")]),
    [methods.text]: "[00:00] Привет",
    [methods.copyAll]: null,
    [methods.copyText]: null,
    [methods.saveAs]: { status: "saved", message: "Расшифровка сохранена.", name: "Матанализ 2026-10-19.txt" },
  };
  const api = transcriptApi(async (method, ...args) => {
    calls.push([method, ...args]);
    return replies[method];
  });
  assert.deepEqual(await api.status("r1"), status());
  assert.equal((await api.segments("r1", 0, pageSize)).segments[0].text, " Привет");
  assert.equal(await api.text("r1", true), "[00:00] Привет");
  await api.copyAll("r1", false);
  await api.copyText("выделено");
  assert.equal((await api.saveAs("r1", "md", true)).status, "saved");
  assert.deepEqual(calls, [
    [methods.status, "r1"],
    [methods.segments, "r1", 0, 1000],
    [methods.text, "r1", true],
    [methods.copyAll, "r1", false],
    [methods.copyText, "выделено"],
    [methods.saveAs, "r1", "md", true],
  ]);
  assert.equal(pageSize, maxPage);
  for (const name of Object.values(methods)) {
    assert.match(name, /^paratech\/desktop\/internal\/input\/wails\.TranscriptService\.[A-Z]\w+$/);
  }
});

test("a malformed reply is an error, not a default", async () => {
  const bad = [
    null, {}, status({ state: "running" }), status({ percent: -1 }), status({ segments: 1.5 }), status({ final: "no" }),
    status({ audioUrl: "https://evil.example/a.wav" }), status({ audioUrl: "javascript:x" }), status({ title: null }),
  ];
  for (const b of bad) {
    await assert.rejects(transcriptApi(async () => b).status("r1"), undefined, JSON.stringify(b));
  }
  for (const b of [null, page(0, 1, null), page(0, 1, [{ ...seg(0, 0, "x"), text: 5 }]), { ...page(0, 1, []), from: -1 }]) {
    await assert.rejects(transcriptApi(async () => b).segments("r1", 0, 10), undefined, JSON.stringify(b));
  }
  await assert.rejects(transcriptApi(async () => 5).text("r1", true));
  await assert.rejects(transcriptApi(async () => ({ status: "ok", message: "", name: "" })).saveAs("r1", "txt", true));
  assert.equal((await transcriptApi(async () => status({ audioUrl: "" })).status("r1")).audioUrl, "");
});

test("the core's error texts are shown as they are, anything else as the generic text", () => {
  for (const m of Object.values(errorMessages)) {
    assert.equal(errorText(new Error(m)), m);
    assert.equal(errorText(m), m);
  }
  assert.equal(errorText(new Error("open C:\\Users\\student: denied")), errorMessages.unavailable);
  assert.equal(errorText(undefined), errorMessages.unavailable);
});

test("the action is offered for a queued, running or finished recognition, or one that left text", () => {
  for (const state of ["waiting", "recognising", "done"]) {
    assert.ok(canOpenTranscript(status({ state, segments: 0 })), state);
  }
  assert.ok(!canOpenTranscript(status({ state: "none", segments: 0 })));
  assert.ok(!canOpenTranscript(status({ state: "failed", segments: 0 })));
  assert.ok(canOpenTranscript(status({ state: "failed", segments: 4 })));
  const recs = [{ id: "a", lessonId: "l1" }, { id: "b", lessonId: "l2" }, { id: "c", lessonId: "l1" }];
  assert.deepEqual(lessonRecordings(recs, "l1").map((r) => r.id), ["a", "c"]);
  assert.deepEqual(lessonRecordings([{ id: "m", lessonId: "" }], ""), []);
});

test("polling stops only when the recognition is done or failed", () => {
  assert.ok(isLive("waiting") && isLive("recognising"));
  assert.ok(!isLive("done") && !isLive("failed") && !isLive("none"));
  assert.equal(nextPollMs(status({ state: "recognising", final: false }), 0), pollMs);
  assert.equal(nextPollMs(status({ state: "waiting", segments: 0, final: false }), 0), pollMs);
  // A viewer opened before the recognition job is enqueued keeps asking, slower.
  assert.equal(nextPollMs(status({ state: "none", segments: 0, final: false }), 0), idlePollMs);
  assert.ok(idlePollMs > pollMs);
  for (const state of ["done", "failed"]) {
    assert.equal(nextPollMs(status({ state }), 0), null, state);
  }
});

test("a failed call is retried with a growing wait, never given up", () => {
  // The first status failed: nothing is known yet, the viewer still retries.
  assert.equal(nextPollMs(null, 1), pollMs);
  assert.equal(nextPollMs(null, 2), pollMs * 2);
  assert.equal(nextPollMs(null, 3), pollMs * 4);
  assert.equal(nextPollMs(null, 100), maxErrorPollMs);
  // An error while fetching the pages of a finished transcript is retried too, so the text is completed.
  assert.equal(nextPollMs(status({ state: "done" }), 1), pollMs);
  assert.equal(nextPollMs(status({ state: "none", segments: 0 }), 1), pollMs);
  // After a good tick the wait is the state's again.
  assert.equal(nextPollMs(status({ state: "recognising", final: false }), 0), pollMs);
});

test("new segments are fetched from the current count, never all again", () => {
  const live = (n) => ({ segments: Array.from({ length: n }, (_, i) => seg(i, i * 2000, `ф${i}`)), final: false });
  const st = (segments, over = {}) => status({ state: "recognising", final: false, segments, ...over });
  assert.deepEqual(nextFetch(st(0), emptyLoaded), { kind: "none" });
  assert.deepEqual(nextFetch(st(5), emptyLoaded), { kind: "fetch", from: 0, reset: false });
  assert.deepEqual(nextFetch(st(8), live(5)), { kind: "fetch", from: 5, reset: false });
  assert.deepEqual(nextFetch(st(5), live(5)), { kind: "none" });
  // The stored transcript replaces the live parts, even with the same count.
  assert.deepEqual(nextFetch(status({ segments: 5 }), live(5)), { kind: "fetch", from: 0, reset: true });
  // Fewer segments: the recognition started again after a restart.
  assert.deepEqual(nextFetch(st(2), live(5)), { kind: "fetch", from: 0, reset: true });
  assert.deepEqual(nextFetch(st(0), live(5)), { kind: "clear" });
  const done = { segments: live(3).segments, final: true };
  assert.deepEqual(nextFetch(status({ segments: 3 }), done), { kind: "none" });
});

test("pages are appended to one document; a page of another document restarts from 0", () => {
  const p1 = page(0, 3, [seg(0, 0, "а"), seg(1, 2000, "б")], false);
  const d1 = applyPage(emptyLoaded, p1);
  assert.equal(d1.segments.length, 2);
  assert.equal(morePages(d1, p1), 2);
  const p2 = page(2, 3, [seg(2, 4000, "в")], false);
  const d2 = applyPage(d1, p2);
  assert.deepEqual(d2.segments.map((s) => s.text), ["а", "б", "в"]);
  assert.equal(morePages(d2, p2), null);
  assert.equal(applyPage(d1, page(2, 3, [seg(2, 4000, "в")], true)), null, "the final document mid-way");
  assert.equal(applyPage(d1, page(5, 9, [seg(5, 0, "x")], false)), null, "a gap");
  assert.equal(applyPage(d2, page(0, 1, [seg(0, 0, "новое")], true)).segments.length, 1);
  assert.equal(morePages(d1, page(2, 9, [], false)), null, "an empty page ends the loop");
});

test("a three-hour transcript loads in pages of 1000", () => {
  const total = 4000;
  const all = Array.from({ length: total }, (_, i) => seg(i, i * 2700, `Фраза номер ${i}.`, i * 2700 + 2500));
  let doc = emptyLoaded;
  let from = 0;
  const froms = [];
  while (from !== null) {
    froms.push(from);
    const p = page(from, total, all.slice(from, from + pageSize));
    doc = applyPage(doc, p);
    from = morePages(doc, p);
  }
  assert.deepEqual(froms, [0, 1000, 2000, 3000]);
  assert.equal(doc.segments.length, total);
  const started = Date.now();
  const paras = paragraphs(doc.segments);
  const r = searchParagraphs(paras, searchQuery("номер 39"));
  assert.ok(Date.now() - started < 1000, "grouping and search stay quick");
  assert.equal(r.total, 111); // 39, 390..399, 3900..3999
  assert.equal(paragraphs(doc.segments).length, paras.length);
});

test("while recognising the page shows the core's message, the progress and the partial note", () => {
  const v = statusView(status({ state: "recognising", percent: 42, message: "Распознаётся… 42 %", final: false }), 7);
  assert.equal(v.message, "Распознаётся… 42 %");
  assert.equal(v.progress, 42);
  assert.equal(v.partial, partialNote);
  assert.equal(v.empty, "");
  const w = statusView(status({ state: "waiting", percent: 0, message: "Распознавание ждёт очереди.", segments: 0, final: false }), 0);
  assert.equal(w.progress, null);
  assert.equal(w.partial, "");
  assert.match(w.empty, /появится/);
  assert.match(statusView(status({ state: "recognising", segments: 0, final: false }), 0).empty, /Первые фразы/);
  const done = statusView(status(), 3);
  assert.deepEqual([done.message, done.progress, done.partial, done.failed], ["Расшифровка готова.", null, "", false]);
  const failed = statusView(status({ state: "failed", final: false, message: "Распознавание не удалось. Показано то, что успело распознаться." }), 3);
  assert.ok(failed.failed);
  assert.equal(failed.partial, "", "the core's message already says it");
  assert.equal(statusView(status({ state: "recognising", percent: 140 }), 1).progress, 100);
});

test("segments are grouped into paragraphs by pauses and length; appending keeps the earlier ones", () => {
  const segs = [
    seg(0, 0, " Сегодня тема — пределы.", 2000),
    seg(1, 2200, "Начнём с определения.", 4000),
    seg(2, 7000, "Новая мысль после паузы.", 9000),
  ];
  const ps = paragraphs(segs);
  assert.deepEqual(ps.map((p) => [p.first, p.start, p.startMs, p.segments.length]), [
    [0, "00:00", 0, 2],
    [2, "00:07", 7000, 1],
  ]);
  const long = Array.from({ length: 60 }, (_, i) => seg(i, i * 1000, "слово ".repeat(5).trim(), i * 1000 + 900));
  const lp = paragraphs(long);
  assert.ok(lp.length > 1, "a long run without pauses is split");
  for (const p of lp) {
    assert.ok(p.segments.map((s) => s.text).join(" ").length <= maxParagraphChars + 40);
  }
  const more = paragraphs([...segs, seg(3, 9100, "И ещё.", 9500)]);
  assert.deepEqual(more[0], ps[0]);
  assert.equal(more[1].segments.length, 2);
  assert.deepEqual(paragraphs([]), []);
});

test("search folds case and ё, keeps positions, and finds every match", () => {
  assert.equal(foldText("ЁЛКА Ёж ёж"), "елка еж еж");
  assert.equal(foldText("Привет, МИР"), "привет, мир");
  assert.equal(foldText("İx").length, 2, "a character that changes length stays as it is");
  assert.equal(searchQuery("  Ёж "), "еж");
  assert.deepEqual(findRanges("Ёжик и ежиха, ЕЖ!", searchQuery("ёж")), [
    { start: 0, end: 2 },
    { start: 7, end: 9 },
    { start: 14, end: 16 },
  ]);
  assert.deepEqual(findRanges("аааа", "аа"), [{ start: 0, end: 2 }, { start: 2, end: 4 }], "no overlaps");
  assert.deepEqual(findRanges("текст", ""), []);
});

test("matches are numbered through the paragraphs; next and previous wrap round", () => {
  const ps = paragraphs([seg(0, 0, "Предел и предел"), seg(1, 5000, "нет"), seg(2, 10000, "ПРЕДЕЛ")]);
  const r = searchParagraphs(ps, searchQuery("предел"));
  assert.deepEqual([r.total, r.counts, r.offsets], [3, [2, 0, 1], [0, 2, 2]]);
  assert.deepEqual([0, 1, 2].map((c) => [localMatch(r, 0, c), localMatch(r, 2, c)]), [[0, -1], [1, -1], [-1, 0]]);
  assert.equal(stepMatch(0, 3, 1), 1);
  assert.equal(stepMatch(2, 3, 1), 0);
  assert.equal(stepMatch(0, 3, -1), 2);
  assert.equal(stepMatch(-1, 3, -1), 2);
  assert.equal(stepMatch(-1, 3, 1), 0);
  assert.equal(stepMatch(0, 0, 1), -1);
  assert.equal(matchCountText(2, 12, "x"), "3 из 12");
  assert.equal(matchCountText(-1, 0, "x"), "Не найдено");
  assert.equal(matchCountText(0, 0, ""), "");
  assert.equal(matchCountText(9, 3, "x"), "3 из 3");
});

test("the highlight is text pieces, never markup", () => {
  const [p] = paragraphs([seg(0, 0, " <b>Предел</b> и пРедел "), seg(1, 500, "без совпадений")]);
  const pieces = paragraphPieces(p, searchQuery("предел"));
  assert.deepEqual(pieces, [
    [
      { text: "<b>", match: -1 },
      { text: "Предел", match: 0 },
      { text: "</b> и ", match: -1 },
      { text: "пРедел", match: 1 },
    ],
    [{ text: "без совпадений", match: -1 }],
  ]);
  assert.deepEqual(paragraphPieces(p, ""), [[{ text: "<b>Предел</b> и пРедел", match: -1 }], [{ text: "без совпадений", match: -1 }]]);
  const [empty] = paragraphs([seg(0, 0, "  ")]);
  assert.deepEqual(paragraphPieces(empty, "x"), [[{ text: "", match: -1 }]]);
});

test("timestamps play from their second; save results say what happened", () => {
  assert.equal(playSeconds(65500), 65.5);
  assert.equal(playSeconds(-5), 0);
  assert.deepEqual(saveResultText({ status: "saved", message: "Расшифровка сохранена.", name: "Матанализ 2026-10-19.md" }), {
    ok: true,
    text: "Сохранено: Матанализ 2026-10-19.md",
  });
  assert.equal(saveResultText({ status: "cancelled", message: "Сохранение отменено.", name: "" }), null);
  const failed = "Не удалось сохранить файл. Выберите другую папку и попробуйте ещё раз.";
  assert.deepEqual(saveResultText({ status: "failed", message: failed, name: "" }), { ok: false, text: failed });
});

test("the transcript opens over the current section and goes back to it", () => {
  const r = routeToTranscript(routeToPage("homework"), "r1");
  // TASK-087 added the summary overlay and the assignment to open to the route.
  assert.deepEqual(r, { page: "homework", transcript: "r1", summary: "", assignment: "" });
  assert.deepEqual(closeTranscript(r), { page: "homework", transcript: "", summary: "", assignment: "" });
  assert.deepEqual(routeToPage("settings"), { page: "settings", transcript: "", summary: "", assignment: "" });
  assert.equal(initialRoute.transcript, "");
});

test("a recording row offers the transcript from its chain once recognition is queued, running or past", () => {
  // Mirrors the chain stages and error codes of internal/cases/activity/types.go.
  for (const stage of ["queued", "transcribing", "summarizing", "done"]) {
    assert.ok(rowOffersTranscript({ stage, errorCode: "" }), stage);
  }
  // The summary or the homework failed or waits for the Hub: the local transcript stays reachable.
  for (const errorCode of ["transcriptionFailed", "summaryFailed", "homeworkFailed"]) {
    assert.ok(rowOffersTranscript({ stage: "failed", errorCode }), errorCode);
  }
  assert.ok(!rowOffersTranscript({ stage: "recording", errorCode: "" }));
  assert.ok(!rowOffersTranscript({ stage: "saving", errorCode: "" }));
  assert.ok(!rowOffersTranscript({ stage: "failed", errorCode: "recordingFailed" }));
  assert.ok(!rowOffersTranscript({ stage: "failed", errorCode: "" }));
  assert.ok(!rowOffersTranscript({ stage: "unknown", errorCode: "" }));
});

// Source guards: read the TypeScript sources, not the build.
const src = (rel) =>
  readFileSync(new URL(`../src/${rel}`, import.meta.url), "utf8")
    .replace(/\/\*[\s\S]*?\*\//g, "")
    .replace(/(^|[^:])\/\/.*$/gm, "$1");

test("the viewer renders text only, plays only on a click and polls without overlap", () => {
  const files = ["pages/TranscriptPage.tsx", "pages/TranscriptLinks.tsx", "transcriptView.ts", "bindings/transcript.ts", "openTranscript.ts"];
  for (const rel of files) {
    const text = src(rel);
    for (const banned of ["dangerouslySetInnerHTML", "innerHTML", "outerHTML", "insertAdjacentHTML", "href=", "window.open",
      "localStorage", "sessionStorage", "navigator.clipboard", "fetch(", "autoPlay", "console."]) {
      assert.ok(!text.includes(banned), `${rel} uses ${banned}`);
    }
  }
  const page = src("pages/TranscriptPage.tsx");
  assert.equal(page.split(".play()").length - 1, 1, "one play call");
  assert.ok(page.indexOf(".play()") > page.indexOf("const play = useCallback("), "play only from the timestamp handler");
  assert.match(page, /onClick=\{\(\) => props\.onPlay\(para\.startMs\)\}/);
  assert.match(page, /preload="none"/);
  assert.ok(!page.includes("setInterval"), "the next poll waits for the previous answer");
  assert.match(page, /clearTimeout\(timer\)/);
  assert.match(page, /<mark /);
  // The selection copy reads the page's selection, the full copy goes through the core.
  assert.match(page, /api\.copyText\(text\)/);
  assert.match(page, /api\.copyAll\(id, withTimestamps\)/);
});

test("the recordings page decides the row's transcript action from the list, without a call per row", () => {
  const page = src("pages/RecordingsPage.tsx");
  assert.match(page, /extraActions=\{\s*rowOffersTranscript\(r\) &&/);
  assert.match(page, /onClick=\{\(\) => openTranscript\(r\.id\)\}/);
  assert.ok(!page.includes("transcriptApi"), "no transcript status call per row");
});
