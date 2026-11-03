// TASK-351: a lesson is one screen reached by the route (`lesson`, `tab`): the route functions, the history, the pure
// decisions of knowledgeView.ts (the key, the opening tab, the crumbs, the one-shot opening), the summary tools and the
// wiring of the pages and the window, which have no DOM harness, checked in the source text.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import {
  closeLesson,
  closeTranscript,
  initialRoute,
  routeToAssignment,
  routeToLesson,
  routeToLessonTab,
  routeToOpenedAssignment,
  routeToPage,
  routeToSubject,
  routeToSummary,
  routeToTranscript,
} from "../.test-build/nav.js";
import {
  canGoBack,
  canGoForward,
  currentRoute,
  goBack,
  goForward,
  handleKey,
  needsRemount,
  newHistory,
  pushRoute,
  sameRoute,
} from "../.test-build/navHistory.js";
import {
  isLessonTab,
  keepLessonOpening,
  lessonCrumbs,
  lessonScreenTexts,
  lessonTabs,
  noFocus,
  openingRouteTab,
  openingTab,
  recordingIdOfKey,
} from "../.test-build/knowledgeView.js";
import { SummaryTools, summarySaveItems } from "../.test-build/pages/SummaryParts.js";

const read = (path) => readFileSync(new URL(`../src/${path}`, import.meta.url), "utf8");
const noop = () => undefined;
const html = (el) => renderToStaticMarkup(el);

// ---- nav ----

test("the route has the lesson and its tab, empty everywhere else", () => {
  assert.deepEqual(initialRoute, { page: "today", transcript: "", summary: "", assignment: "", subject: "", lesson: "", tab: "" });
  assert.deepEqual(Object.keys(routeToPage("journal")).sort(), ["assignment", "lesson", "page", "subject", "summary", "tab", "transcript"]);
});

test("a lesson opens over the current page, like the summary: the page under it stays as it was left", () => {
  const under = routeToSubject(routeToPage("subjects"), "s1");
  const r = routeToLesson(under, "recording:r1");
  assert.deepEqual(r, { ...under, lesson: "recording:r1", tab: "" });
  assert.equal(r.page, "subjects");
  assert.equal(r.subject, "s1", "the subject under the lesson is kept");
  assert.equal(routeToLesson(under, "recording:r1", "summary").tab, "summary");
  // In place of an open summary or transcript, and the other way round.
  assert.equal(routeToLesson(routeToSummary(under, "r9"), "recording:r1").summary, "");
  assert.equal(routeToLesson(routeToTranscript(under, "r9"), "recording:r1").transcript, "");
  assert.equal(routeToSummary(r, "r2").lesson, "");
  assert.equal(routeToSummary(r, "r2").tab, "");
  assert.equal(routeToTranscript(r, "r2").lesson, "");
});

test("a tab is set on the open lesson only; closing the lesson returns to the page and forgets the tab", () => {
  const r = routeToLesson(routeToPage("knowledge"), "recording:r1");
  const t = routeToLessonTab(r, "lecture");
  assert.deepEqual(t, { ...r, tab: "lecture" });
  assert.equal(routeToLessonTab(routeToPage("knowledge"), "lecture").tab, "", "no lesson, no tab");
  const closed = closeLesson(t);
  assert.deepEqual(closed, routeToPage("knowledge"));
  assert.deepEqual(closeTranscript(t), routeToPage("knowledge"), "the overlay's «Назад» closes a lesson too");
});

test("every route that opens a new place clears the lesson and the tab", () => {
  const r = routeToLessonTab(routeToLesson(routeToPage("knowledge"), "recording:r1"), "notes");
  for (const next of [routeToPage("homework"), routeToSubject(r, "s1"), routeToOpenedAssignment(r, "a1"), routeToAssignment("a1")]) {
    assert.equal(next.lesson, "");
    assert.equal(next.tab, "");
  }
});

// ---- history ----

test("opening a lesson is an entry; switching tabs is not, and Alt+← leaves the lesson whatever tabs were visited", () => {
  let hist = newHistory(routeToPage("knowledge"));
  hist = pushRoute(hist, routeToLesson(currentRoute(hist), "recording:r1"));
  assert.equal(hist.entries.length, 2);
  const open = hist;
  hist = pushRoute(hist, routeToLessonTab(currentRoute(hist), "summary"));
  hist = pushRoute(hist, routeToLessonTab(currentRoute(hist), "transcript"));
  hist = pushRoute(hist, routeToLessonTab(currentRoute(hist), "homework"));
  assert.equal(hist.entries.length, 2, "tabs add no entry");
  assert.equal(hist.index, open.index);
  assert.equal(currentRoute(hist).tab, "homework", "the entry holds the tab last used");
  const back = goBack(hist);
  assert.equal(currentRoute(back).lesson, "", "one step back leaves the lesson");
  assert.equal(currentRoute(back).page, "knowledge");
  assert.equal(canGoBack(back), false);
  // Alt+→ returns to the lesson on the tab last used.
  const fwd = goForward(back);
  assert.equal(currentRoute(fwd).lesson, "recording:r1");
  assert.equal(currentRoute(fwd).tab, "homework");
});

test("the keys walk it: Alt+ArrowLeft from a lesson with visited tabs, Alt+ArrowRight back", () => {
  const key = (k) => ({ key: k, altKey: true, ctrlKey: false, metaKey: false, shiftKey: false, target: { tagName: "DIV" } });
  let hist = newHistory(routeToPage("knowledge"));
  hist = pushRoute(hist, routeToLesson(currentRoute(hist), "recording:r1", "notes"));
  hist = pushRoute(hist, routeToLessonTab(currentRoute(hist), "lecture"));
  const left = handleKey(key("ArrowLeft"), hist);
  assert.equal(left.prevent, true);
  assert.equal(currentRoute(left.next).lesson, "");
  const right = handleKey(key("ArrowRight"), left.next);
  assert.equal(currentRoute(right.next).lesson, "recording:r1");
  assert.equal(currentRoute(right.next).tab, "lecture");
  assert.equal(handleKey(key("ArrowRight"), right.next).next, null, "nothing after the lesson");
});

test("a tab change keeps the entries after the current one; the same tab changes nothing", () => {
  let hist = newHistory(routeToPage("knowledge"));
  hist = pushRoute(hist, routeToLesson(currentRoute(hist), "recording:r1", "notes"));
  hist = pushRoute(hist, routeToAssignment("a1"));
  hist = goBack(hist);
  assert.equal(currentRoute(hist).lesson, "recording:r1");
  assert.equal(canGoForward(hist), true);
  const same = pushRoute(hist, routeToLessonTab(currentRoute(hist), "notes"));
  assert.equal(same, hist, "the same tab is the same place");
  const other = pushRoute(hist, routeToLessonTab(currentRoute(hist), "summary"));
  assert.equal(other.entries.length, 3);
  assert.equal(canGoForward(other), true, "the assignment after the lesson is still forward");
  assert.equal(currentRoute(other).tab, "summary");
});

test("another lesson is a new entry; the tab is not part of the place", () => {
  assert.equal(sameRoute(routeToLesson(initialRoute, "a", "notes"), routeToLesson(initialRoute, "a", "summary")), true);
  assert.equal(sameRoute(routeToLesson(initialRoute, "a"), routeToLesson(initialRoute, "b")), false);
  assert.equal(sameRoute(routeToLesson(initialRoute, "a"), initialRoute), false);
  let hist = newHistory(initialRoute);
  hist = pushRoute(hist, routeToLesson(currentRoute(hist), "a"));
  hist = pushRoute(hist, routeToLesson(currentRoute(hist), "b"));
  assert.deepEqual(hist.entries.map((r) => r.lesson), ["", "a", "b"]);
});

test("a lesson never remounts the page under it: its screen has its own key (the lesson), not the tab", () => {
  const under = routeToPage("knowledge");
  const a = routeToLesson(under, "recording:a");
  const b = routeToLesson(under, "recording:b");
  assert.equal(needsRemount(a, under), false);
  assert.equal(needsRemount(b, a), false);
  assert.equal(needsRemount(routeToLessonTab(a, "summary"), a), false);
  assert.equal(needsRemount(under, a), false);
  const app = read("App.tsx");
  assert.match(app, /<div key=\{`lesson:\$\{route\.lesson\}`\} className="screen">/);
  assert.doesNotMatch(app, /key=\{`lesson:[^`]*route\.tab/);
});

// ---- knowledgeView ----

test("a recorded lesson is found by the id in its key; any other key gives none", () => {
  assert.equal(recordingIdOfKey("recording:r-1"), "r-1");
  assert.equal(recordingIdOfKey("recording:"), "");
  assert.equal(recordingIdOfKey("day:s1:2026-09-12"), "");
  assert.equal(recordingIdOfKey("note:n1"), "");
  assert.equal(recordingIdOfKey("lesson:l1"), "");
  assert.equal(recordingIdOfKey(""), "");
  // The same prefix as the core's kb.RecordingKey.
  const go = readFileSync(new URL("../../internal/domain/coursework/services/knowledgebase/notes.go", import.meta.url), "utf8");
  assert.match(go, /recordingKeyPrefix\s*=\s*"recording:"/);
});

test("the tabs are known by value; the route's tab is one of them or empty", () => {
  assert.deepEqual(lessonTabs.map((t) => t.label), ["Мой конспект", "Конспект", "Текст лекции", "Расшифровка", "Задания"]);
  assert.equal(isLessonTab("summary"), true);
  assert.equal(isLessonTab(""), false);
  assert.equal(isLessonTab("settings"), false);
});

test("a search asks for a tab only when it names a place or a note; a plain opening leaves the choice to the screen", () => {
  assert.equal(openingRouteTab(noFocus, ""), "");
  assert.equal(openingRouteTab({ tab: "lecture", index: 3 }, ""), "lecture");
  assert.equal(openingRouteTab({ tab: "transcript", index: 0 }, ""), "transcript");
  assert.equal(openingRouteTab({ tab: "summary", index: -1 }, ""), "summary");
  assert.equal(openingRouteTab(noFocus, "m1"), "notes", "a hit in the student's notes opens them");
});

test("the opening tab: the route's, else own notes, else the summary, else lecture text, else transcript, else notes", () => {
  const ready = (over) => ({ summaryReady: false, lectureTextReady: false, transcriptReady: false, ...over });
  const pick = (routeTab, hasNotes, lesson) => openingTab({ routeTab, hasNotes, lesson: ready(lesson) });
  // The tab of the route is never replaced.
  assert.equal(pick("transcript", true, { summaryReady: true }), "transcript");
  assert.equal(pick("homework", false, {}), "homework");
  // Notes first, whatever else is ready.
  assert.equal(pick("", true, { summaryReady: true, lectureTextReady: true, transcriptReady: true }), "notes");
  assert.equal(pick("", false, { summaryReady: true, lectureTextReady: true }), "summary");
  assert.equal(pick("", false, { lectureTextReady: true, transcriptReady: true }), "lecture");
  assert.equal(pick("", false, { transcriptReady: true }), "transcript");
  // Nothing yet: «Мой конспект» with its hint how to add.
  assert.equal(pick("", false, {}), "notes");
  // An unknown tab in the route is no choice.
  assert.equal(pick("nope", false, { summaryReady: true }), "summary");
});

test("crumbs «Предметы / <предмет> / <пара>»: the subject level only for a matched library subject", () => {
  const matched = { subject: "Матанализ", librarySubjectId: "s1", librarySubjectState: "matched" };
  assert.deepEqual(lessonCrumbs(matched, "Пара 21.10"), [
    { label: "Предметы", go: { kind: "subjects" } },
    { label: "Матанализ", go: { kind: "subject", id: "s1" } },
    { label: "Пара 21.10", go: null },
  ]);
  for (const librarySubjectState of ["none", "ambiguous", "no-subject"]) {
    const c = lessonCrumbs({ subject: "Матанализ", librarySubjectId: "", librarySubjectState }, "Пара 21.10");
    assert.deepEqual(c.map((x) => x.label), ["Предметы", "Пара 21.10"], librarySubjectState);
  }
  assert.equal(lessonCrumbs(matched, "x").at(-1).go, null, "the lesson itself is not a link");
  // A subject name stays text.
  const evil = lessonCrumbs({ ...matched, subject: "<img src=x onerror=1>" }, "x");
  assert.equal(evil[1].label, "<img src=x onerror=1>");
});

test("the one-shot opening is kept only while the route shows its lesson", () => {
  const o = { key: "recording:r1", focus: { tab: "lecture", index: 2 }, materialId: "", editing: false };
  assert.equal(keepLessonOpening(o, { lesson: "recording:r1" }), o);
  assert.equal(keepLessonOpening(o, { lesson: "" }), null, "Alt+← drops it");
  assert.equal(keepLessonOpening(o, { lesson: "recording:r2" }), null);
  assert.equal(keepLessonOpening(null, { lesson: "recording:r1" }), null);
});

test("a lesson that cannot be opened says why in plain words and offers the way to the subjects", () => {
  assert.equal(lessonScreenTexts.toSubjects, "К предметам");
  assert.equal(lessonScreenTexts.loading, "Загрузка…");
  assert.match(lessonScreenTexts.gone, /не найдена/);
  assert.doesNotMatch(lessonScreenTexts.gone, /ключ|key|ошибк/i);
});

// ---- the summary tools ----

const tools = (over = {}) =>
  html(h(SummaryTools, { ready: true, idle: true, notReady: "Конспекта пока нет", busy: "", message: null, onCopy: noop, onSave: noop, ...over }));

test("the tools: one «Скопировать» as the primary action and one «⋯» menu; no format switch, no «Сохранить как…»", () => {
  const out = tools();
  assert.equal((out.match(/<span>Скопировать<\/span>/g) ?? []).length, 1);
  assert.equal((out.match(/class="btn primary"/g) ?? []).length, 1, "the one accent");
  assert.match(out, /<button type="button" class="btn primary"><span class="btn-labels"><span>Скопировать<\/span>/);
  assert.equal((out.match(/aria-haspopup="menu"/g) ?? []).length, 1);
  assert.match(out, /aria-label="Ещё действия с конспектом"/);
  assert.doesNotMatch(out, /Сохранить как…|aria-pressed|Формат файла|хранится на этом компьютере/);
  // The menu items are the two formats, as menu items of the shared «⋯» menu.
  assert.deepEqual(summarySaveItems, [
    { key: "txt", label: "Сохранить как .txt" },
    { key: "md", label: "Сохранить как .md" },
  ]);
  assert.match(read("ui/RowMenu.tsx"), /role="menuitem"/);
});

test("the tools: not ready, copy is off with the reason and there is nothing to save; busy and the result line", () => {
  const off = tools({ ready: false });
  assert.match(off, /<button[^>]*disabled=""[^>]*><span class="btn-labels"><span>Скопировать<\/span>/);
  assert.match(off, /aria-describedby="[^"]+"[^>]*disabled/);
  assert.match(off, /Конспекта пока нет/);
  assert.doesNotMatch(off, /aria-haspopup/, "no menu without a summary");
  const busy = tools({ idle: false, busy: "save" });
  assert.match(busy, /<button[^>]*disabled=""[^>]*><span class="btn-labels"><span>Скопировать<\/span>/);
  assert.match(busy, /role="status">Сохранение…</);
  assert.doesNotMatch(busy, /Конспекта пока нет/, "a running action is the reason, not the missing summary");
  // The menu items are off while an action runs.
  assert.match(busy, /aria-haspopup="menu"/);
  const copying = tools({ idle: false, busy: "copy" });
  assert.match(copying, /<span class="btn-off">Скопировать<\/span><span>Копирование…<\/span>/);
  const ok = tools({ message: { kind: "ok", text: "Конспект скопирован в буфер обмена." } });
  assert.match(ok, /role="status"[^>]*>.*<span>Конспект скопирован в буфер обмена\.<\/span>/);
  const fail = tools({ message: { kind: "fail", text: "Не удалось" } });
  assert.match(fail, /role="alert"/);
  // Extra actions of a page sit beside them.
  assert.match(tools({ extra: h("span", null, "доп") }), /<span>доп<\/span>/);
});

// ---- wiring in the source ----

test("the lesson page: the summary tools in «Конспект», the storage paragraph and «Открыть конспект» gone, the fix in the head menu", () => {
  const src = read("pages/KnowledgeLessonPage.tsx");
  assert.match(src, /<SummaryTools/);
  assert.doesNotMatch(src, /Открыть конспект|хранится на этом компьютере|Сохранить как…|useOpenSummary/);
  assert.doesNotMatch(src, /<Segmented/);
  // «Исправить предмет или тип пары» is an item of the «⋯» menu of the head, not a button.
  assert.match(src, /<RowMenu\s+label="Ещё действия с парой"[^>]*items=\{\[\{ key: "edit", label: "Исправить предмет или тип пары" \}\]\}\s+onPick=\{\(\) => setEditing\(\(e\) => !e\)\}/);
  assert.doesNotMatch(src, /<Button[^>]*>\s*Исправить предмет или тип пары/);
  // The tabs are a tablist with a tabpanel.
  assert.match(src, /<SegmentTabs label="Раздел пары" base=\{tabsId\} options=\{lessonTabs\} value=\{tab\} onChange=\{props\.onTab\} \/>/);
  assert.match(src, /role="tabpanel" id=\{segmentPanelId\(tabsId, tab\)\} aria-labelledby=\{segmentTabId\(tabsId, tab\)\}/);
  // The retry of a failed summary or homework stays, since the old page with it is not opened from here any more.
  assert.match(src, /retryClick\(view, step,/);
  assert.match(src, /props\.activity\.redoSummary\(id\)/);
  assert.match(src, /onConfirm=\{redo\}/);
  // A recording without speech offers the sound check.
  assert.match(src, /view\.noSpeech \? \(\s*<NoSpeechCard view=\{view\} onCheckSound=\{props\.onCheckSound\} \/>/);
});

test("the summary page shares the tools with the lesson: one copy of them, in SummaryParts", () => {
  const page = read("pages/SummaryPage.tsx");
  assert.match(page, /<SummaryTools/);
  assert.doesNotMatch(page, /Сохранить как…|хранится на этом компьютере|<Segmented/);
  const parts = read("pages/SummaryParts.tsx");
  assert.equal((parts.match(/export function SummaryTools/g) ?? []).length, 1);
});

test("the opening tab is decided once, after the notes and the fresh lesson are known, and never replaces a chosen tab", () => {
  const src = read("pages/KnowledgeLessonPage.tsx");
  assert.match(src, /const needsAuto = useRef\(!isLessonTab\(props\.tab\)\);/);
  assert.match(src, /readLectureNotes\(notes, \{ kind: "recording", recordingId: id \}\)/);
  assert.match(src, /if \(needsAuto\.current && auto === null && hasNotes !== null && fresh\) \{\s*setAuto\(openingTab\(\{ routeTab: "", hasNotes, lesson \}\)\);/);
  assert.match(src, /const tab: LessonTab \| null = isLessonTab\(props\.tab\) \? props\.tab : auto;/);
  // Until it is known: a small inline line, not a banner.
  assert.match(src, /\{tab === null \? \(\s*<p className="muted" role="status">\s*\{lessonScreenTexts\.loading\}/);
});

test("the lesson is found in the window's memory, else by the id of its key; a lost note-only lecture has a way out", () => {
  const src = read("pages/KnowledgeLessonPage.tsx");
  assert.match(src, /const cached = props\.cache\.get\(key\);/);
  assert.match(src, /api\.lesson\(recordingId\)/);
  assert.match(src, /const text = recordingId === "" \? lessonScreenTexts\.gone : error;/);
  assert.match(src, /<Button onClick=\{props\.nav\.onOpenSubjects\}>\{lessonScreenTexts\.toSubjects\}<\/Button>/);
  // The re-read of a cached lesson says so with the same way out.
  assert.match(src, /\{lessonError !== "" && \(\s*<div className="kb-gone">/);
  assert.doesNotMatch(src, /dangerouslySetInnerHTML|innerHTML/);
});

test("the window: the lesson is an overlay over the page, the cache is filled on every open, the opening is one-shot", () => {
  const app = read("App.tsx");
  assert.match(app, /const overlay = route\.transcript !== "" \|\| route\.summary !== "" \|\| route\.lesson !== "";/);
  assert.match(app, /\{route\.lesson !== "" && \(/);
  assert.match(app, /<LessonScreen\s+lessonKey=\{route\.lesson\}\s+tab=\{route\.tab\}\s+cache=\{lessons\.current\}/);
  assert.match(app, /lessons\.current\.set\(lesson\.key, lesson\);/);
  assert.match(app, /setRoute\(\(r\) => routeToLesson\(r, lesson\.key, openingRouteTab\(o\.focus, o\.materialId\)\)\);/);
  assert.match(app, /setRoute\(\(r\) => routeToLessonTab\(r, tab\)\)/);
  assert.match(app, /onBack=\{\(\) => setRoute\(closeLesson\)\}/);
  // The one-shot opening is dropped as soon as the route leaves the lesson.
  assert.match(app, /setLessonOpening\(\(o\) => keepLessonOpening\(o, \{ lesson: route\.lesson \}\)\);/);
  // The crumbs are routes: «Предметы» is the rail click, the subject opens in «Предметы».
  assert.match(app, /onOpenSubjects: \(\) => \{\s*openPage\("subjects"\);\s*setVisit\(\(v\) => v \+ 1\);\s*\},\s*onOpenSubject: selectSubject,/);
  // The «Задания» tab goes to the assignment as before; the sound check opens «Диагностика».
  assert.match(app, /onOpenAssignment=\{openAssignment\}\s+onCheckSound=\{\(\) => openPage\("diagnostics"\)\}\s+nav=\{lessonNav\}/);
  // «База знаний» under the lesson reads and searches again after the lesson.
  assert.match(app, /lessonsSeen=\{lessonsSeen\}/);
  assert.match(app, /lessonsEdited=\{lessonsEdited\}/);
});

test("«База знаний» opens a lesson through the window; it draws none itself and searches again after a correction", () => {
  const page = read("pages/KnowledgeBasePage.tsx");
  assert.doesNotMatch(page, /<KnowledgeLessonPage|<NoteLecturePage|\bOpenLesson\b/);
  assert.match(page, /onOpenLesson: \(lesson: KnowledgeLesson, opening: LessonOpeningRequest\) => void;/);
  // The list, the search hits, «Добавить конспект прошедшей пары» and the citation jump all go the same way.
  assert.match(page, /props\.onOpenLesson\(lesson, \{ focus, materialId, editing \}\);/);
  assert.match(page, /onOpenLesson\.current\(r\.lesson, \{ focus: r\.focus, materialId: r\.materialId, editing: false \}\);/);
  assert.match(page, /openLesson\(o\.lesson, o\.focus, false, o\.materialId\)/);
  assert.match(page, /onChoose=\{\(c: PastChoice\) => props\.onOpen\(pastLesson\(subject, c\), false\)\}/);
  assert.match(page, /void runSearch\(queryNow\.current, true\);/);
  assert.match(page, /\}, \[api, subject\.key, imports, props\.reread\]\);/);
});
