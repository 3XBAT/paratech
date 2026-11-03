// TASK-351: a lesson is one screen reached by the route (`lesson`, `tab`): the route functions, the history, the pure
// decisions of knowledgeView.ts (the key, the opening tab, the crumbs, the one-shot opening), the summary tools and the
// wiring of the pages and the window, which have no DOM harness, checked in the source text.
import { test } from "node:test";
import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import { createElement as h } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import {
  closeLesson,
  initialRoute,
  routeToAssignment,
  routeToLesson,
  routeToLessonTab,
  routeToOpenedAssignment,
  routeToPage,
  routeToSubject,
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
  lessonRootLabel,
  lessonScreenTexts,
  lessonTabs,
  noFocus,
  openedFromKnowledge,
  openingRouteTab,
  openingTab,
  recordingLessonKey,
} from "../.test-build/knowledgeView.js";
import { HomeworkCard, SummaryCard, SummaryTools, summarySaveItems } from "../.test-build/pages/SummaryParts.js";
import { ariaLabels, buttonTag, isOff, visibleText } from "./iconbtn.mjs";

const read = (path) => readFileSync(new URL(`../src/${path}`, import.meta.url), "utf8");
const noop = () => undefined;
const html = (el) => renderToStaticMarkup(el);

// ---- nav ----

test("the route has the lesson and its tab, empty everywhere else", () => {
  // TASK-352: the summary and transcript overlays are gone, the lesson is the one screen over a page.
  // TASK-353: the tab of the subject page joined the route.
  assert.deepEqual(initialRoute, { page: "today", assignment: "", subject: "", lesson: "", tab: "", subjectTab: "" });
  assert.deepEqual(Object.keys(routeToPage("journal")).sort(), ["assignment", "lesson", "page", "subject", "subjectTab", "tab"]);
});

test("a lesson opens over the current page: the page under it stays as it was left", () => {
  const under = routeToSubject(routeToPage("subjects"), "s1");
  const r = routeToLesson(under, "recording:r1");
  assert.deepEqual(r, { ...under, lesson: "recording:r1", tab: "" });
  assert.equal(r.page, "subjects");
  assert.equal(r.subject, "s1", "the subject under the lesson is kept");
  assert.equal(routeToLesson(under, "recording:r1", "summary").tab, "summary");
  // Another lesson in place of the open one starts without the old tab.
  assert.deepEqual(routeToLesson(routeToLesson(under, "recording:r1", "summary"), "recording:r2"), { ...under, lesson: "recording:r2", tab: "" });
});

test("a tab is set on the open lesson only; closing the lesson returns to the page and forgets the tab", () => {
  const r = routeToLesson(routeToPage("knowledge"), "recording:r1");
  const t = routeToLessonTab(r, "lecture");
  assert.deepEqual(t, { ...r, tab: "lecture" });
  assert.equal(routeToLessonTab(routeToPage("knowledge"), "lecture").tab, "", "no lesson, no tab");
  const closed = closeLesson(t);
  assert.deepEqual(closed, routeToPage("knowledge"));
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
  // The count of openings is in the key (TASK-352): the same lesson opened again at another place opens afresh.
  // The key is the pure lessonScreenKey (lesson, visit): tested in lessonEntry352.test.mjs; the tab is not an input.
  assert.match(app, /<div key=\{lessonScreenKey\(route\.lesson, lessonVisit\)\} className="screen">/);
  assert.doesNotMatch(app, /lessonScreenKey\([^)]*route\.tab/);
});

// ---- knowledgeView ----

test("the key of a recorded lesson is the recording's id after the core's prefix", () => {
  assert.equal(recordingLessonKey("r-1"), "recording:r-1");
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

test("the opening tab: the route's, else own notes, else the summary, else lecture text, else transcript, else the summary card", () => {
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
  // Nothing ready yet: «Конспект», where a running or failed step, «Повторить» and the no-speech card are seen.
  assert.equal(pick("", false, {}), "summary");
  // An unknown tab in the route is no choice.
  assert.equal(pick("nope", false, { summaryReady: true }), "summary");
});

test("crumbs «Предметы / <предмет> / <пара>» from other sections: the subject level only for a matched library subject", () => {
  const matched = { subject: "Матанализ", librarySubjectId: "s1", librarySubjectState: "matched" };
  assert.deepEqual(lessonCrumbs(matched, "Пара 21.10", "subjects"), [
    { label: "Предметы", go: { kind: "subjects" } },
    { label: "Матанализ", go: { kind: "subject", id: "s1" } },
    { label: "Пара 21.10", go: null },
  ]);
  for (const librarySubjectState of ["none", "ambiguous", "no-subject"]) {
    const c = lessonCrumbs({ subject: "Матанализ", librarySubjectId: "", librarySubjectState }, "Пара 21.10", "subjects");
    assert.deepEqual(c.map((x) => x.label), ["Предметы", "Пара 21.10"], librarySubjectState);
  }
  assert.equal(lessonCrumbs(matched, "x", "subjects").at(-1).go, null, "the lesson itself is not a link");
  // A subject name stays text.
  const evil = lessonCrumbs({ ...matched, subject: "<img src=x onerror=1>" }, "x", "subjects");
  assert.equal(evil[1].label, "<img src=x onerror=1>");
});

test("crumbs from «База знаний»: «База знаний / <пара>», only the root closes the lesson, no subject crumb", () => {
  const matched = { subject: "Матанализ", librarySubjectId: "s1", librarySubjectState: "matched" };
  assert.equal(openedFromKnowledge("knowledge"), true);
  assert.equal(openedFromKnowledge("subjects"), false);
  assert.deepEqual(lessonCrumbs(matched, "Пара 21.10", "knowledge"), [
    { label: "База знаний", go: { kind: "close" } },
    { label: "Пара 21.10", go: null },
  ]);
  const plain = lessonCrumbs({ subject: "Матанализ", librarySubjectId: "", librarySubjectState: "none" }, "Пара 21.10", "knowledge");
  assert.deepEqual(plain, [
    { label: "База знаний", go: { kind: "close" } },
    { label: "Пара 21.10", go: null },
  ]);
  assert.equal(lessonRootLabel("knowledge"), "База знаний");
  assert.equal(lessonRootLabel("subjects"), "Предметы");
  assert.equal(lessonRootLabel("today"), "Предметы");
});

test("the one-shot opening is kept only while the route shows its lesson", () => {
  const o = { key: "recording:r1", focus: { tab: "lecture", index: 2 }, materialId: "", editing: false };
  assert.equal(keepLessonOpening(o, { lesson: "recording:r1" }), o);
  assert.equal(keepLessonOpening(o, { lesson: "" }), null, "Alt+← drops it");
  assert.equal(keepLessonOpening(o, { lesson: "recording:r2" }), null);
  assert.equal(keepLessonOpening(null, { lesson: "recording:r1" }), null);
});

test("a lesson that cannot be opened says why in plain words and offers «Назад»", () => {
  assert.equal(lessonScreenTexts.back, "Назад");
  assert.equal("toSubjects" in lessonScreenTexts, false);
  assert.equal(lessonScreenTexts.loading, "Загрузка…");
  // TASK-352: the core opens any lesson by its key, so there is no «only from the list» text; a failure shows the
  // binding's fixed text (see the wiring test below).
  assert.equal("gone" in lessonScreenTexts, false);
});

test("the lesson-info form: focus returns to the head «⋯» when it closes, a saved line is a status", () => {
  const src = read("pages/KnowledgeLessonPage.tsx");
  assert.equal(lessonScreenTexts.infoSaved, "Предмет и тип пары сохранены.");
  // The effect starts from the opening value, so the first render (even with the form open) never moves the focus.
  assert.match(src, /const wasEditing = useRef\(props\.editing\);/);
  assert.match(
    src,
    /if \(wasEditing\.current && !editing\) \{\s*menuBox\.current\?\.querySelector<HTMLElement>\("button"\)\?\.focus\(\);\s*\}\s*wasEditing\.current = editing;/,
  );
  assert.match(src, /<div className="acts" ref=\{menuBox\}>\s*<RowMenu/);
  // Saved: the line shows only while the form is closed, and goes when the form opens again or the lesson changes.
  assert.match(src, /setSaved\(true\);/);
  assert.match(src, /\{saved && !editing && \(\s*<ResultLine kind="ok" role="status">\s*\{lessonScreenTexts\.infoSaved\}/);
  assert.match(src, /if \(editing\) \{\s*setSaved\(false\);\s*\}/);
  assert.match(src, /useEffect\(\(\) => setSaved\(false\), \[id\]\);/);
});

// ---- the summary tools ----

const tools = (over = {}) =>
  html(h(SummaryTools, { ready: true, idle: true, busy: "", message: null, onCopy: noop, onSave: noop, ...over }));

test("the tools: one «Скопировать» icon and one «⋯» menu; no format switch, no «Сохранить как…»", () => {
  const out = tools();
  // TASK-368: «Скопировать» is a copy icon named by its tooltip and aria-label; no word button, no accent button.
  assert.deepEqual(ariaLabels(out).filter((l) => l === "Скопировать"), ["Скопировать"]);
  assert.match(buttonTag(out, "Скопировать"), /class="ia"/);
  assert.match(out, /<use href="#i-copy"><\/use>/);
  assert.equal(visibleText(out).includes("Скопировать"), false);
  assert.equal((out.match(/class="btn primary"/g) ?? []).length, 0);
  assert.equal((out.match(/aria-haspopup="menu"/g) ?? []).length, 1);
  assert.match(out, /aria-label="Ещё действия с конспектом"/);
  assert.doesNotMatch(out, /Сохранить как…|aria-pressed|Формат файла|хранится на этом компьютере/);
  // The menu items are the two formats, as menu items of the shared «⋯» menu.
  assert.deepEqual(summarySaveItems, [
    { key: "txt", label: "Сохранить как текст (.txt)" },
    { key: "md", label: "Сохранить в Markdown (.md)" },
  ]);
  assert.match(read("ui/RowMenu.tsx"), /role="menuitem"/);
});

test("the tools: not ready, copy is off and there is nothing to save; busy and the result line", () => {
  // The lesson screen does not draw the tools before the summary is ready (the card's state line says where it stands),
  // so the component has no text of its own for it (TASK-352 removed notReady).
  const off = tools({ ready: false });
  assert.ok(isOff(off, "Скопировать"));
  assert.doesNotMatch(off, /aria-haspopup/, "no menu without a summary");
  assert.doesNotMatch(off, /Конспекта пока нет/);
  const busy = tools({ idle: false, busy: "save" });
  assert.ok(isOff(busy, "Скопировать"));
  assert.match(busy, /role="status">Сохранение…</);
  // The menu items are off while an action runs.
  assert.match(busy, /aria-haspopup="menu"/);
  // While copying the icon is a spinner named «Копирование…».
  const copying = tools({ idle: false, busy: "copy" });
  assert.ok(isOff(copying, "Копирование…"));
  assert.match(copying, /aria-busy="true"/);
  assert.match(copying, /ia-spin/);
  const ok = tools({ message: { kind: "ok", text: "Конспект скопирован в буфер обмена." } });
  assert.match(ok, /role="status"[^>]*>.*<span>Конспект скопирован в буфер обмена\.<\/span>/);
  const fail = tools({ message: { kind: "fail", text: "Не удалось" } });
  assert.match(fail, /role="alert"/);
});

// ---- wiring in the source ----

test("the lesson page: the summary tools in «Конспект», the storage paragraph and «Открыть конспект» gone, the fix in the head menu", () => {
  const src = read("pages/KnowledgeLessonPage.tsx");
  assert.match(src, /<SummaryTools/);
  assert.doesNotMatch(src, /Открыть конспект|хранится на этом компьютере|Сохранить как…|useOpenSummary/);
  assert.doesNotMatch(src, /<Segmented/);
  // «Исправить предмет или тип пары» is an item of the «⋯» menu of the head, not a button (TASK-352 added «Удалить запись»
  // after it). It only opens the form; the whole «⋯» is gone while the form is open (the form has its own «Закрыть»).
  assert.match(src, /\{!editing && \(\s*<div className="acts" ref=\{menuBox\}>\s*<RowMenu\s+label="Ещё действия с парой"[^>]*items=\{lessonMenu\}\s+onPick=\{\(key\) => \{\s*if \(key === "edit"\) \{\s*setEditing\(true\);/);
  assert.match(src, /\{ key: "edit", label: "Исправить предмет или тип пары" \},/);
  assert.doesNotMatch(src, /setEditing\(\(e\) => !e\)/);
  assert.doesNotMatch(src, /<Button[^>]*>\s*Исправить предмет или тип пары/);
  // The form takes the focus (its first input or select) when it opens.
  assert.match(src, /<div ref=\{formBox\}>\s*<LessonInfoForm/);
  assert.match(src, /if \(editing\) \{\s*formBox\.current\?\.querySelector<HTMLElement>\("input, select"\)\?\.focus\(\);\s*\}\s*\}, \[editing\]\);/);
  // The head holds no readiness marks: the tabs and the cards' state lines say it.
  assert.doesNotMatch(src, /ReadyMarks/);
  // The tools only when the summary is ready; the cards of the tabs drop the heading the tab already shows.
  assert.match(src, /\{view\.summary\.ready \? \(\s*<SummaryTools\s+ready\s+idle=\{idle\}/);
  assert.doesNotMatch(src, /noSummaryHint|notReady/);
  assert.match(src, /<SummaryCard[^>]*\sheading=\{false\}/);
  assert.match(src, /<HomeworkCard\s+view=\{view\}\s+heading=\{false\}/);
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

test("the summary tools have one copy, in SummaryParts, and no dead props; the overlay summary page is gone (TASK-352)", () => {
  const parts = read("pages/SummaryParts.tsx");
  assert.equal((parts.match(/export function SummaryTools/g) ?? []).length, 1);
  assert.doesNotMatch(parts, /notReady|extra\?: ReactNode|props\.extra/);
  assert.equal(existsSync(new URL("../src/pages/SummaryPage.tsx", import.meta.url)), false);
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

test("the lesson is found in the window's memory, else by its key (lessonByKey); a lesson the core cannot give has «Назад»", () => {
  const src = read("pages/KnowledgeLessonPage.tsx");
  assert.match(src, /const cached = props\.cache\.get\(key\);/);
  // TASK-352: any key goes to the core, a recording and a note-only lecture alike; no recording-id-only path is left.
  assert.match(src, /api\.lessonByKey\(key\)\.then\(/);
  assert.doesNotMatch(src, /api\.lesson\(recordingId\)|recordingIdOfKey/);
  // One component (pages/LessonGone.tsx) for both failures: the banner with the screen's own words (TASK-352), «Повторить»
  // when a repeat may help and «Назад», which is the window's closeLesson (where the student came from).
  const gone = read("pages/LessonGone.tsx");
  assert.match(gone, /<Button variant=\{retry \? "ghost" : undefined\} onClick=\{props\.onBack\}>\s*\{lessonScreenTexts\.back\}/);
  assert.match(src, /<LessonGone\s+problem=\{problem\}/);
  assert.match(src, /onBack=\{props\.onBack\}/);
  assert.doesNotMatch(src, /toSubjects/);
  assert.doesNotMatch(src, /dangerouslySetInnerHTML|innerHTML/);
});

test("a lesson that cannot be read again shows only the error and «Назад»: no tabs, no summary polling", () => {
  const src = read("pages/KnowledgeLessonPage.tsx");
  assert.match(src, /if \(lessonError !== null\) \{\s*return \(\s*<section ref=\{pageRef\} tabIndex=\{-1\} className="page tr sm kb-lesson-page">\s*\{crumbs\}\s*<LessonGone\s+problem=\{lessonError\}/);
  // The early return comes before the head and the tabs.
  assert.ok(src.indexOf("if (lessonError !== null) {\n    return") < src.indexOf("<SegmentTabs"));
  // The polling effect does not start for it.
  assert.match(src, /useEffect\(\(\) => \{\s*if \(lessonError !== null\) \{\s*return;\s*\}\s*let stopped = false;\s*let timer/);
});

test("the crumbs follow the section under the lesson: the window passes the route's page", () => {
  const app = read("App.tsx");
  assert.match(app, /lessonKey=\{route\.lesson\}\s+origin=\{route\.page\}/);
  const src = read("pages/KnowledgeLessonPage.tsx");
  assert.match(src, /case "close":\s*return \{ label: c\.label, onClick: onBack \};/);
  assert.match(src, /const rootGo = openedFromKnowledge\(props\.origin\) \? props\.onBack : props\.nav\.onOpenSubjects;/);
});

// ---- the cards without their headings (the tab names them) ----

const bareView = {
  recordingId: "r1",
  lessonId: "l1",
  title: "Матанализ",
  startedAt: "2026-10-19T06:05:00Z",
  durationSeconds: 2700,
  hasDuration: true,
  transcribed: true,
  noSpeech: false,
  summary: { state: "running", waitReason: "", message: "Конспект составляется.", jobError: "", ready: false, text: "", sections: [], keyPoints: [], updatedAt: "" },
  homework: { state: "done", waitReason: "", message: "", jobError: "", assignments: [] },
  lectureText: { state: "none", waitReason: "", message: "", jobError: "", ready: false, blocks: [], updatedAt: "", partsDone: 0, partsTotal: 0 },
};

test("the cards keep their headings by default (the old overlay) and drop them with heading={false}", () => {
  const withHeading = html(h(SummaryCard, { view: bareView }));
  assert.match(withHeading, /<h2>Конспект<\/h2>/);
  const bare = html(h(SummaryCard, { view: bareView, heading: false }));
  assert.doesNotMatch(bare, /<h2/);
  assert.match(bare, /Конспект составляется\./, "the state line stays");
  const hw = html(h(HomeworkCard, { view: bareView, onOpenAssignment: noop }));
  assert.match(hw, /<h2 class="group-title">Задания<\/h2>/);
  assert.doesNotMatch(html(h(HomeworkCard, { view: bareView, onOpenAssignment: noop, heading: false })), /<h2/);
});

test("the window: the lesson is an overlay over the page, the cache is filled on every open, the opening is one-shot", () => {
  const app = read("App.tsx");
  assert.match(app, /const overlay = route\.lesson !== "";/);
  assert.match(app, /\{route\.lesson !== "" && \(/);
  assert.match(app, /<LessonScreen\s+lessonKey=\{route\.lesson\}\s+origin=\{route\.page\}\s+tab=\{route\.tab\}\s+cache=\{lessons\.current\}/);
  assert.match(app, /lessons\.current\.set\(lesson\.key, lesson\);/);
  assert.match(app, /setRoute\(\(r\) => routeToLesson\(r, key, openingRouteTab\(o\.focus, o\.materialId\)\)\);/);
  assert.match(app, /setRoute\(\(r\) => routeToLessonTab\(r, tab\)\)/);
  assert.match(app, /const lessonBack = useCallback\(\(\) => \{\s*refocusSheet\.current = true;\s*setRoute\(closeLesson\);\s*\}, \[\]\);/);
  assert.match(app, /onBack=\{lessonBack\}/);
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
