// TASK-348: the rail with labels, «Записи» as a section, «Записать пару» on «Сегодня».
// Pure view logic on the compiled modules; the pages have no DOM harness, so their wiring is checked in the source text.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { contextFor } from "../.test-build/agentView.js";
import { reservedWidth } from "../.test-build/agent/panelWidth.js";
import {
  closeTranscript,
  initialRoute,
  pageById,
  railItemOf,
  routeToPage,
  routeToSummary,
  routeToTranscript,
} from "../.test-build/nav.js";
import { canGoBack, currentRoute, goBack, goForward, newHistory, pushRoute } from "../.test-build/navHistory.js";
import { keepStartPanel } from "../.test-build/recordStartView.js";
import { queueLine } from "../.test-build/recordingRowView.js";
import { clockText, elapsedText, recordButton, recordTexts, stopProblemText, texts } from "../.test-build/todayView.js";
import { RecordAction, StatusBar, StopProblem } from "../.test-build/pages/TodayParts.js";

const noop = () => undefined;
const html = (el) => renderToStaticMarkup(el);
const src = (path) => readFileSync(new URL(`../src/${path}`, import.meta.url), "utf8");

const at = (s) => new Date(`2026-11-03T${s}Z`).getTime();
const running = (over = {}) => ({
  id: "r1",
  stage: "recording",
  status: "RECORDING",
  startedAt: "2026-11-03T09:00:00Z",
  stoppable: true,
  ...over,
});

// ---- the rail ----

test("the rail is 72 px wide everywhere the width is assumed", () => {
  assert.match(src("styles.css"), /\.shell \{[^}]*grid-template-columns: 72px minmax\(0, 1fr\)/);
  assert.match(src("agent/agent.css"), /\.shell\.with-agent \{[^}]*grid-template-columns: 72px minmax\(0, 1fr\) var\(--ag-w, 340px\)/);
  assert.match(src("styles.css"), /\.toast-host \{[^}]*left: 72px/);
  assert.equal(reservedWidth, 72 + 420);
  assert.doesNotMatch(src("styles.css"), /grid-template-columns: 64px/);
});

test("the rail label is small, one line, and cannot overflow the button; the tooltip rules are gone", () => {
  const css = src("styles.css");
  assert.match(css, /\.rail-label \{[^}]*font-size: var\(--fs-nav\)[^}]*white-space: nowrap/);
  assert.match(css, /\.rail-label \{[^}]*max-width: 100%[^}]*overflow: hidden/);
  assert.doesNotMatch(css, /\.rail \.tip|\.rail button:hover \.tip/);
  // Keyboard focus, the current item and the high-contrast outline stay as before.
  assert.match(css, /\.rail button\[aria-current="page"\] \{/);
  assert.match(css, /@media \(forced-colors: active\) \{\s*\.rail button\[aria-current="page"\] \{ outline: 2px solid Highlight; \}/);
  assert.doesNotMatch(css, /\.rail button \{[^}]*outline: none/);
  // The nav token is the small one of the design tokens.
  assert.match(src("ui/tokens.css"), /--fs-nav: 11\.5px/);
});

// ---- «Записи» as a section: navigation ----

test("«Записи» is a page with its own route: back and forward walk to it and away", () => {
  let hist = newHistory(initialRoute);
  hist = pushRoute(hist, routeToPage("recordings"));
  hist = pushRoute(hist, routeToPage("journal"));
  assert.equal(currentRoute(hist).page, "journal");
  hist = goBack(hist);
  assert.equal(currentRoute(hist).page, "recordings");
  assert.equal(railItemOf(currentRoute(hist).page).label, "Записи");
  hist = goBack(hist);
  assert.equal(currentRoute(hist).page, "today");
  assert.equal(canGoBack(hist), false);
  assert.equal(currentRoute(goForward(hist)).page, "recordings");
});

test("a rail click on «Записи» while a transcript or summary is open over it returns to the list of recordings", () => {
  const over = routeToSummary(routeToPage("recordings"), "r1");
  assert.equal(over.page, "recordings");
  assert.equal(over.summary, "r1");
  const again = routeToPage("recordings");
  assert.equal(again.summary, "");
  assert.equal(again.transcript, "");
  // Back from the transcript is the same page, so the list stays as the user left it.
  assert.deepEqual(closeTranscript(routeToTranscript(routeToPage("recordings"), "r2")), routeToPage("recordings"));
  // The rail click goes through openPage + a new visit in App.
  assert.match(src("App.tsx"), /<Rail\s+current=\{current\}\s+onOpen=\{\(id\) => \{\s*openPage\(id\);\s*setVisit\(\(v\) => v \+ 1\);/);
});

test("the start panel request is not part of the route", () => {
  assert.deepEqual(Object.keys(routeToPage("recordings")).sort(), ["assignment", "page", "subject", "summary", "transcript"]);
  assert.doesNotMatch(src("nav.ts"), /startPanel|openStart/);
  assert.doesNotMatch(src("navHistory.ts"), /startPanel|openStart/);
});

test("the agent panel context for «Записи» is «Записи»; the core is asked as on «Сегодня»", () => {
  assert.equal(pageById("recordings").label, "Записи");
  const ctx = contextFor("recordings", "", false);
  assert.equal(ctx.label, "Записи");
  assert.equal(ctx.ask.screen, "today");
  assert.match(src("agentView.ts"), /recordings: "today",/);
});

// ---- the one-shot start panel ----

test("the start panel request lives only while the route is on «Записи»", () => {
  assert.equal(keepStartPanel(true, { page: "recordings" }), true);
  assert.equal(keepStartPanel(true, { page: "today" }), false, "Alt+← to «Сегодня» drops it");
  assert.equal(keepStartPanel(true, { page: "journal" }), false);
  assert.equal(keepStartPanel(false, { page: "recordings" }), false, "a visit without a request opens nothing");
});

test("App wires «Записать пару» as taken-once state, like the schedule review", () => {
  const app = src("App.tsx");
  assert.match(app, /const \[startPanel, setStartPanel\] = useState\(false\);/);
  assert.match(app, /setStartPanel\(true\);\s*setRoute\(routeToPage\("recordings"\)\);/);
  assert.match(app, /const startPanelTaken = useCallback\(\(\) => setStartPanel\(false\), \[\]\);/);
  assert.match(app, /setStartPanel\(\(s\) => keepStartPanel\(s, \{ page: route\.page \}\)\);\s*\}, \[route\.page\]\);/);
  assert.match(app, /openStart=\{props\.startPanel\}\s+onStartTaken=\{props\.onStartPanelTaken\}/);
  assert.match(app, /onRecord=\{props\.onOpenStartPanel\}/);
});

test("«Записи» opens the panel on arrival, takes the request once and moves the focus into the panel", () => {
  const page = src("pages/RecordingsPage.tsx");
  assert.match(page, /const \[startOpen, setStartOpen\] = useState\(openStart\);/);
  assert.match(page, /if \(openStart\) \{\s*onStartTaken\?\.\(\);\s*\}\s*\}, \[\]\);/);
  assert.match(page, /const focusStart = useRef\(openStart\);/);
});

// ---- the main button of «Сегодня» ----

test("clockText: 00:41 under an hour, 1:02:05 from an hour", () => {
  assert.equal(clockText(0), "00:00");
  assert.equal(clockText(41), "00:41");
  assert.equal(clockText(41.9), "00:41");
  assert.equal(clockText(65), "01:05");
  assert.equal(clockText(3599), "59:59");
  assert.equal(clockText(3725), "1:02:05");
  assert.equal(clockText(-1), "—");
  assert.equal(clockText(NaN), "—");
});

test("elapsedText counts from startedAt by the given clock, never below zero", () => {
  assert.equal(elapsedText(running(), at("09:00:41")), "00:41");
  assert.equal(elapsedText(running(), at("08:59:00")), "00:00", "clock skew is clamped");
  assert.equal(elapsedText(running({ startedAt: "" }), at("09:00:41")), "");
  assert.equal(elapsedText(running({ startedAt: "не дата" }), at("09:00:41")), "");
  assert.equal(elapsedText(running({ status: "STARTING" }), at("09:00:41")), "", "not capturing yet");
});

test("the button is «Записать пару» without a running recording, also while the list is not read", () => {
  assert.deepEqual(recordButton(null, 0), { kind: "start", label: "Записать пару", id: "" });
  assert.deepEqual(recordButton([], 0), { kind: "start", label: "Записать пару", id: "" });
  assert.equal(recordButton([running({ stage: "done", stoppable: false })], 0).kind, "start");
  assert.equal(recordTexts.start, "Записать пару");
});

test("a running recording turns the button into «● Идёт запись 00:41 · Остановить» for that recording", () => {
  const b = recordButton([running({ id: "old", stage: "done", stoppable: false }), running()], at("09:00:41"));
  assert.deepEqual(b, { kind: "stop", label: "● Идёт запись 00:41 · Остановить", id: "r1" });
  assert.equal(recordButton([running()], at("10:02:05")).label, "● Идёт запись 1:02:05 · Остановить");
  // A start time that cannot be read leaves out the time, not the stop.
  assert.equal(recordButton([running({ startedAt: "" })], 0).label, "● Идёт запись · Остановить");
});

test("a recording that runs but is not stoppable yet is only named; the button is off", () => {
  const b = recordButton([running({ stoppable: false, status: "STARTING" })], 0);
  assert.deepEqual(b, { kind: "wait", label: "● Идёт запись", id: "r1" });
  const off = html(h(RecordAction, { recordings: [running({ stoppable: false })], now: 0, stopping: false, onStart: noop, onStop: noop }));
  assert.match(off, /<button[^>]*disabled=""/);
});

test("RecordAction: primary, with the record icon to start; the stop shows the time and busy text while stopping", () => {
  const start = html(h(RecordAction, { recordings: [], now: 0, stopping: false, onStart: noop, onStop: noop }));
  assert.match(start, /class="btn primary"/);
  assert.match(start, /<svg/);
  assert.match(start, /Записать пару/);
  assert.doesNotMatch(start, /disabled/);

  const stop = html(h(RecordAction, { recordings: [running()], now: at("09:00:41"), stopping: false, onStart: noop, onStop: noop }));
  assert.match(stop, /class="btn primary"/);
  assert.match(stop, /● Идёт запись 00:41 · Остановить/);
  assert.doesNotMatch(stop, /disabled/);

  const busy = html(h(RecordAction, { recordings: [running()], now: at("09:00:41"), stopping: true, onStart: noop, onStop: noop }));
  assert.match(busy, /disabled=""/);
  assert.match(busy, /Остановка…/);
});

test("RecordAction presses: start opens «Записи», stop stops that recording; the seconds tick only while capturing", () => {
  const parts = src("pages/TodayParts.tsx");
  assert.match(parts, /onClick=\{\(\) => \(b\.kind === "start" \? props\.onStart\(\) : props\.onStop\(b\.id\)\)\}/);
  // The clock ticks in the page (the hook is not loadable by the node tests), only while a recording captures.
  const page = src("pages/TodayPage.tsx");
  assert.match(page, /const now = useNow\(recordings !== null && anyRecording\(recordings\)\);/);
  assert.match(page, /<RecordAction recordings=\{recordings\} now=\{now\}/);
});

test("a failed stop says what happened, gives the way out and a button to «Записи»", () => {
  const text = stopProblemText(new Error("boom"));
  assert.match(text, /^Не удалось выполнить действие\. Остановить запись можно и в разделе «Записи»\.$/);
  assert.match(stopProblemText(new Error("recording is not running in this session")), /Остановить запись можно и в разделе «Записи»/);
  let opened = 0;
  const out = html(h(StopProblem, { text, onOpenRecordings: () => opened++ }));
  assert.match(out, /role="alert"/);
  assert.match(out, /Не удалось выполнить действие/);
  assert.match(out, /<button[^>]*>Открыть «Записи»<\/button>/);
  assert.equal(opened, 0);
});

test("TodayPage: the head holds the one primary; the stop uses the binding of the recordings page; no «all» mode", () => {
  const page = src("pages/TodayPage.tsx");
  assert.match(page, /<RecordAction recordings=\{recordings\} now=\{now\} stopping=\{stopping\} onStart=\{props\.onRecord\} onStop=\{\(id\) => void stop\(id\)\} \/>/);
  assert.match(page, /await activity\.stopRecording\(recordingId\);/);
  assert.match(page, /setStopProblem\(stopProblemText\(err\)\);/);
  // One stop at a time; the list is read again whatever the result.
  assert.match(page, /if \(stoppingRef\.current\) \{\s*return;\s*\}/);
  assert.match(page, /if \(mounted\.current\) \{\s*readRecordings\(\);\s*\}/);
  assert.match(page, /\{stopProblem !== "" && <StopProblem text=\{stopProblem\} onOpenRecordings=\{openRecordings\} \/>\}/);
  // The old head button, the back button and the embedded page are gone.
  assert.doesNotMatch(page, /RecordingsPage|texts\.allRecords|texts\.back|setAll|today-back/);
  assert.ok(!("allRecords" in texts) && !("back" in texts));
  assert.doesNotMatch(src("pages/today.css"), /today-back/);
  // The problem chips and the status line open «Записи».
  assert.match(page, /const openRecordings = \(\) => props\.onOpen\("recordings"\);/);
  assert.match(page, /onOpenRecordings=\{openRecordings\}/);
  assert.match(page, /<StatusBar queue=\{queue\} nearest=\{nearest\} onOpenQueue=\{openRecordings\} \/>/);
});

// ---- the status line ----

test("the queue line says «В очереди: N» and never «задачи»; «Фоновая обработка» keeps its own name", () => {
  const queued = { stage: "queued", queueReason: "inLine" };
  assert.equal(queueLine([queued, queued, queued], []), "В очереди: 3");
  assert.equal(queueLine([queued], [{ status: "QUEUED" }]), "В очереди: 2");
  assert.equal(queueLine([], []), "");
  for (const n of [1, 2, 5, 11, 21]) {
    assert.doesNotMatch(queueLine(Array.from({ length: n }, () => queued), []), /задач/i);
  }
});

test("the queue in the status line of «Сегодня» is a quiet text button to «Записи»", () => {
  const bar = html(h(StatusBar, { queue: "В очереди: 2", nearest: "Ближайший срок — пт", onOpenQueue: noop }));
  assert.match(bar, /<button[^>]*class="btn ghost sm"[^>]*title="Открыть раздел «Записи»"[^>]*>В очереди: 2<\/button>/);
  assert.equal((bar.match(/<button/g) ?? []).length, 1, "the deadline is plain text");
  assert.doesNotMatch(bar, /class="btn primary/);
  const plain = html(h(StatusBar, { queue: "В очереди: 2", nearest: "" }));
  assert.doesNotMatch(plain, /<button/);
  assert.equal(html(h(StatusBar, { queue: "", nearest: "", onOpenQueue: noop })), "");
});

test("«Записи» keeps its own queue line, and the privacy sentence stays only in the empty state", () => {
  const page = src("pages/RecordingsPage.tsx");
  assert.match(page, /const queue = queueLine\(recordings \?\? \[\], visibleJobs\);/);
  assert.doesNotMatch(page, /Звук обрабатывается/);
  const view = src("recordingRowView.ts");
  assert.equal((view.match(/Звук обрабатывается/g) ?? []).length, 1);
});

// ---- one accent ----

test("the install button of the speech notice is secondary on «Сегодня» and «Записи», primary where it is the main action", () => {
  const notice = src("pages/SpeechComponentNotice.tsx");
  assert.match(notice, /primary = true/);
  assert.match(notice, /variant=\{primary \? "primary" : "secondary"\}/);
  assert.match(src("pages/TodayPage.tsx"), /<SpeechComponentNotice primary=\{false\} \/>/);
  assert.match(src("pages/RecordingsPage.tsx"), /<SpeechComponentNotice onReady=\{\(\) => void refresh\(\)\} primary=\{false\} \/>/);
  assert.match(src("pages/ModelsPage.tsx"), /<SpeechComponentNotice onReady=\{\(\) => void refresh\(\)\} \/>/);
});
