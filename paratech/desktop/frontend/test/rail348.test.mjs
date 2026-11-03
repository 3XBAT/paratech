// TASK-348: the rail with labels, «Записи» as a section, «Записать пару» on «Сегодня».
// Pure view logic on the compiled modules; the pages have no DOM harness, so their wiring is checked in the source text.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { contextFor } from "../.test-build/agentView.js";
import { reservedWidth } from "../.test-build/agent/panelWidth.js";
import { closeLesson, initialRoute, pageById, railItemOf, routeToLesson, routeToPage } from "../.test-build/nav.js";
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
  // 11px (not the 11.5px token of the popup) with a tighter track; what still does not fit ends in «…».
  assert.match(css, /\.rail-label \{[^}]*font-size: 11px[^}]*letter-spacing: -0\.01em[^}]*white-space: nowrap[^}]*text-overflow: ellipsis/);
  assert.match(css, /\.rail-label \{[^}]*max-width: 100%[^}]*overflow: hidden/);
  assert.doesNotMatch(css, /\.rail \.tip|\.rail button:hover \.tip/);
  // Keyboard focus, the current item and the high-contrast outline stay as before.
  assert.match(css, /\.rail button\[aria-current="page"\] \{/);
  assert.match(css, /@media \(forced-colors: active\) \{\s*\.rail button\[aria-current="page"\] \{ outline: 2px solid Highlight; outline-offset: -2px; \}/);
  assert.doesNotMatch(css, /\.rail button \{[^}]*outline: none/);
});

test("the focus ring of a rail button is drawn inside it: the button fills the rail and an outer ring is cut at the window edge", () => {
  assert.match(src("styles.css"), /\.rail button:focus-visible \{ outline-offset: -2px; \}/);
});

test("the stale «Записи и задачи» wording is gone from the stylesheet and nav.ts", () => {
  assert.doesNotMatch(src("styles.css"), /Записи и задачи/);
  assert.doesNotMatch(src("nav.ts"), /Записи и задачи/);
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

test("a rail click on «Записи» while a lesson is open over it returns to the list of recordings", () => {
  const over = routeToLesson(routeToPage("recordings"), "recording:r1", "transcript");
  assert.equal(over.page, "recordings");
  assert.equal(over.lesson, "recording:r1");
  const again = routeToPage("recordings");
  assert.equal(again.lesson, "");
  assert.equal(again.tab, "");
  // Back from the lesson is the same page, so the list stays as the user left it.
  assert.deepEqual(closeLesson(routeToLesson(routeToPage("recordings"), "recording:r2")), routeToPage("recordings"));
  // The rail click goes through openPage + a new visit in App.
  assert.match(src("App.tsx"), /<Rail\s+current=\{current\}\s+onOpen=\{\(id\) => \{\s*openPage\(id\);\s*setVisit\(\(v\) => v \+ 1\);/);
});

test("the start panel request is not part of the route", () => {
  assert.deepEqual(Object.keys(routeToPage("recordings")).sort(), ["assignment", "lesson", "page", "subject", "tab"]);
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
  assert.deepEqual(b, { kind: "stop", label: "● Идёт запись 00:41 · Остановить", id: "r1", time: "00:41" });
  assert.equal(recordButton([running()], at("10:02:05")).label, "● Идёт запись 1:02:05 · Остановить");
  // A start time that cannot be read leaves out the time, not the stop.
  assert.equal(recordButton([running({ startedAt: "" })], 0).label, "● Идёт запись · Остановить");
});

test("a running recording that is not stoppable can only be starting, whatever its age; the button is off", () => {
  const fresh = recordButton([running({ stoppable: false, status: "STARTING" })], at("09:00:03"));
  assert.deepEqual(fresh, { kind: "wait", label: "Запись начинается…", id: "r1", title: recordTexts.startingHint });
  // The core puts only STARTING and RECORDING into stage «recording»; stopping ones are «saving»: no age guess.
  for (const clock of ["09:00:04.999", "09:00:05", "10:30:00", "08:00:00"]) {
    assert.equal(recordButton([running({ stoppable: false })], at(clock)).label, "Запись начинается…", clock);
  }
  assert.equal(recordButton([running({ stoppable: false, startedAt: "" })], at("12:00:00")).label, "Запись начинается…");
  assert.ok(!("stopWait" in recordTexts) && !("stopWaitHint" in recordTexts));
  assert.doesNotMatch(src("todayView.ts"), /останавливается|startingMs/);
  const off = html(h(RecordAction, { recordings: [running({ stoppable: false })], now: at("09:00:01"), stopping: false, onStart: noop, onStop: noop }));
  assert.match(off, /<button[^>]*disabled=""/);
  assert.match(off, /aria-busy="true"/);
  assert.match(off, /title="Остановить можно через несколько секунд\."/);
  assert.match(off, />Запись начинается…</);
  const later = html(h(RecordAction, { recordings: [running({ stoppable: false })], now: at("09:01:00"), stopping: false, onStart: noop, onStop: noop }));
  assert.match(later, />Запись начинается…</);
  assert.match(later, /title="Остановить можно через несколько секунд\."/);
  assert.match(later, /aria-busy="true"/);
});

test("RecordAction: primary, with the record icon to start; the stop shows the time and busy text while stopping", () => {
  const start = html(h(RecordAction, { recordings: [], now: 0, stopping: false, onStart: noop, onStop: noop }));
  assert.match(start, /class="btn primary"/);
  assert.match(start, /<svg/);
  assert.match(start, /Записать пару/);
  assert.doesNotMatch(start, /disabled/);

  const stop = html(h(RecordAction, { recordings: [running()], now: at("09:00:41"), stopping: false, onStart: noop, onStop: noop }));
  assert.match(stop, /class="btn primary"/);
  // The name is the action; the time is a tabular-figure span so the width does not jitter.
  assert.match(stop, /aria-label="Остановить запись"/);
  assert.match(stop, /● Идёт запись <span class="num">00:41<\/span> · Остановить/);
  assert.doesNotMatch(stop, /disabled|aria-busy/);
  const unknown = html(h(RecordAction, { recordings: [running({ startedAt: "" })], now: 0, stopping: false, onStart: noop, onStop: noop }));
  assert.match(unknown, /● Идёт запись · Остановить/);
  assert.doesNotMatch(unknown, /class="num"/);

  const busy = html(h(RecordAction, { recordings: [running()], now: at("09:00:41"), stopping: true, onStart: noop, onStop: noop }));
  assert.match(busy, /disabled=""/);
  assert.match(busy, /Остановка…/);
  assert.match(busy, /aria-busy="true"/);
  assert.doesNotMatch(busy, /aria-label/, "the busy button is named by its text");
});

test("RecordAction presses: start opens «Записи», stop stops that recording; the seconds tick only while capturing", () => {
  const parts = src("pages/TodayParts.tsx");
  assert.match(parts, /onClick=\{\(\) => \(b\.kind === "start" \? props\.onStart\(\) : props\.onStop\(b\.id\)\)\}/);
  // The clock ticks in the page (the hook is not loadable by the node tests), only while a recording captures.
  const page = src("pages/TodayPage.tsx");
  assert.match(page, /const now = useNow\(recordings !== null && anyRecording\(recordings\)\);/);
  assert.match(page, /<RecordAction recordings=\{recordings\} now=\{now\}/);
});

test("a failed stop says the recording goes on; «Повторить» and a quiet button to «Записи»", () => {
  const first = "Не удалось остановить запись — она продолжается.";
  assert.equal(recordTexts.stopFailed, first);
  // Any other cause is not repeated: the core's generic sentences would only say «не удалось» twice.
  assert.equal(stopProblemText(new Error("boom")), first);
  assert.equal(stopProblemText(new Error("recordings and jobs are unavailable")), `${first} Записи сейчас недоступны.`);
  assert.equal(stopProblemText("recordings and jobs are unavailable"), `${first} Записи сейчас недоступны.`, "a plain string works too");
  // The recording is not running any more: it does not «go on», and no repeat makes sense.
  assert.equal(recordTexts.alreadyStopped, "Запись уже остановлена.");
  assert.equal(stopProblemText(new Error("the recording is not running in this session")), "Запись уже остановлена.");
  assert.equal(stopProblemText(new Error("not found")), "Запись уже остановлена.");
  assert.equal(stopProblemText("not found"), "Запись уже остановлена.");
  assert.doesNotMatch(stopProblemText(new Error("not found")), /продолжается/);
  assert.doesNotMatch(src("todayView.ts"), /stopWay|можно и в разделе/);
  const text = stopProblemText(new Error("boom"));
  let opened = 0;
  let retried = 0;
  const out = html(h(StopProblem, { text, retrying: false, canRetry: true, onRetry: () => retried++, onOpenRecordings: () => opened++ }));
  assert.match(out, /role="alert"/);
  assert.match(out, /Не удалось остановить запись — она продолжается\./);
  assert.match(out, /<button[^>]*class="btn sm"[^>]*><span class="btn-labels"><span>Повторить<\/span>/);
  assert.match(out, /<button[^>]*class="btn ghost sm"[^>]*>Перейти к записям<\/button>/);
  assert.doesNotMatch(out, /Открыть «Записи»|btn primary/);
  assert.equal(opened + retried, 0);
  // While the repeat runs the button is busy and off, the line stays.
  const busy = html(h(StopProblem, { text, retrying: true, canRetry: true, onRetry: noop, onOpenRecordings: noop }));
  assert.match(busy, /<button[^>]*disabled=""[^>]*>[^]*Остановка…/);
  assert.match(busy, /role="alert"/);
  // «Повторить» is never an enabled button that does nothing: off when the main button is not «Остановить».
  assert.doesNotMatch(out, /disabled/);
  const idle = html(h(StopProblem, { text, retrying: false, canRetry: false, onRetry: noop, onOpenRecordings: noop }));
  assert.match(idle, /<button[^>]*class="btn sm"[^>]*disabled=""[^>]*>[^]*Повторить/);
  assert.match(idle, /<span>Повторить<\/span><span class="btn-off">Остановка…<\/span>/, "not busy: nothing is being stopped");
  assert.doesNotMatch(idle, /class="btn ghost sm"[^>]*disabled/, "the way to «Записи» stays on");
  assert.match(src("pages/TodayPage.tsx"), /canRetry=\{stopButton\.kind === "stop"\}/);
});

test("a stop keeps the busy state until the list is read again; a shown failure stays during the repeat", () => {
  const page = src("pages/TodayPage.tsx");
  assert.match(page, /const readRecordings = useCallback\(\s*\(\) =>\s*activity\.listRecordings\(\)\.then\(/);
  assert.match(page, /await readRecordings\(\);\s*\} finally \{\s*stoppingRef\.current = false;/);
  assert.doesNotMatch(page, /setStopProblem\(""\);\s*try \{/, "the line is not cleared when a repeat starts");
  assert.match(page, /retrying=\{stopping\}/);
  assert.match(page, /onRetry=\{\(\) => stopButton\.kind === "stop" && void stop\(stopButton\.id\)\}/);
});

test("the head toggle of «Записи» gives the accent to the open panel's own primary button (start or import)", () => {
  const page = src("pages/RecordingsPage.tsx");
  assert.match(page, /const panelOpen = startOpen \|\| importOpen;/);
  assert.match(page, /variant=\{panelOpen \? undefined : "primary"\}/);
  assert.match(page, /aria-expanded=\{startOpen\}/);
  // The panel's button keeps the accent.
  assert.match(page, /<Button\s+variant="primary"\s+icon="rec"\s+busy=\{starting\}/);
});

test("TodayPage: the head holds the one primary; the stop uses the binding of the recordings page; no «all» mode", () => {
  const page = src("pages/TodayPage.tsx");
  assert.match(page, /<RecordAction recordings=\{recordings\} now=\{now\} stopping=\{stopping\} onStart=\{props\.onRecord\} onStop=\{\(id\) => void stop\(id\)\} \/>/);
  assert.match(page, /await activity\.stopRecording\(recordingId\);/);
  assert.match(page, /setStopProblem\(stopProblemText\(err\)\);/);
  // One stop at a time; the list is read again whatever the result.
  assert.match(page, /if \(stoppingRef\.current\) \{\s*return;\s*\}/);
  assert.match(page, /<StopProblem\s+text=\{stopProblem\}/);
  assert.match(page, /onOpenRecordings=\{openRecordings\}/);
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
  // The visible text is only the count; the name says where the button leads.
  assert.match(bar, /aria-label="В очереди: 2\. Открыть раздел «Записи»"/);
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
