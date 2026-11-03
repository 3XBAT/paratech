// TASK-353: the page of a subject with the tabs «Пары · Файлы · Задания», «Билеты к экзамену» and the route around them.
// Pure logic and the route on the compiled modules, the tabs and the head rendered, the pages (no DOM harness) checked in
// the source text.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { parseAssignment, parseOverview } from "../.test-build/bindings/library.js";
import { idleOverview, SubjectOverviewFlow } from "../.test-build/libraryView.js";
import { noFocus, pastLesson } from "../.test-build/knowledgeView.js";
import {
  closeLesson,
  initialRoute,
  routeToAssignment,
  routeToLesson,
  routeToOpenedAssignment,
  routeToPage,
  routeToSubject,
  routeToSubjectTab,
  routeToTickets,
} from "../.test-build/nav.js";
import { currentRoute, goBack, goForward, newHistory, pushRoute, sameRoute } from "../.test-build/navHistory.js";
import {
  addFileId,
  addTaskId,
  defaultTab,
  exportItem,
  folderAddId,
  headTexts,
  knowledgeEntryFor,
  lessonChip,
  lessonMarks,
  loadingDelayMs,
  pairsSubject,
  pairsTexts,
  refocusAllowed,
  startDelay,
  tabSuffix,
  tabTexts,
  taskChip,
  taskRowId,
  taskRows,
  tasksTexts,
  validTab,
} from "../.test-build/subjectTabsView.js";
import { startSubjectId, subjectLine, texts as ticketTexts, ticketsCrumbs } from "../.test-build/ticketsView.js";
import { AddAssignmentDialog } from "../.test-build/pages/HomeworkDialogs.js";
import { SubjectHead } from "../.test-build/pages/SubjectHead.js";
import { loadingPairs, readPairs, SubjectPairsTab } from "../.test-build/pages/SubjectPairsTab.js";
import { SubjectTabs } from "../.test-build/pages/SubjectTabs.js";
import { SubjectTasksTab } from "../.test-build/pages/SubjectTasksTab.js";
import { SubjectDetail, SubjectTree } from "../.test-build/pages/SubjectsParts.js";
import { SegmentTabs } from "../.test-build/ui/controls.js";
import { RowMenu } from "../.test-build/ui/RowMenu.js";
import { AddPastNote, LessonList } from "../.test-build/pages/KnowledgeParts.js";

const noop = () => undefined;
const html = (el) => renderToStaticMarkup(el);
const src = (rel) => readFileSync(new URL(`../src/${rel}`, import.meta.url), "utf8");
const evil = "<img src=x onerror=1>";
const evilEscaped = "&lt;img src=x onerror=1&gt;";

const librarySubject = { id: "s1", name: "Матанализ", aliases: [], semester: "2026-autumn", semesterLabel: "Осень 2026", folders: [], teachers: [] };
const kb = (over = {}) => ({ key: "ID:s1", name: "Матанализ", lessons: 2, latest: "2026-10-20T12:00:00Z", noSubject: false, subjectId: "s1", ...over });
const recorded = (over = {}) => ({
  ...pastLesson(kb(), { kind: "day", date: "2026-10-20" }),
  key: "recording:r1", noteOnly: false, undated: false, anchor: "recording", recordingId: "r1", startedAt: "2026-10-20T12:00:00Z",
  title: "Пределы", kind: "lecture", summaryReady: true, ...over,
});
const noteOnly = (date) => pastLesson(kb(), { kind: "day", date });

// ---- the tab ----

test("a tab of the route is one of three, anything else is «not chosen»", () => {
  assert.deepEqual(["pairs", "files", "tasks", "", "x", "Pairs"].map(validTab), ["pairs", "files", "tasks", "", "", ""]);
  assert.deepEqual([tabTexts.pairs, tabTexts.files, tabTexts.tasks], ["Пары", "Файлы", "Задания"]);
});

test("the count on a tab shows only when there is something to count", () => {
  assert.equal(tabSuffix(0), "");
  assert.equal(tabSuffix(3), " · 3");
});

test("the default tab: «Пары» when the subject has lessons, else «Файлы»; none while the lessons are read; a failed read is «Файлы»", () => {
  assert.equal(defaultTab({ status: "loading", lessons: 0 }), "", "never guessed");
  assert.equal(defaultTab({ status: "ready", lessons: 3 }), "pairs");
  assert.equal(defaultTab({ status: "ready", lessons: 0 }), "files");
  assert.equal(defaultTab({ status: "failed", lessons: 0 }), "files");
});

test("the lessons of the subject are found by its library id, never by a key built here", () => {
  const list = [kb({ key: "free text", subjectId: "" }), kb({ key: "some-key-1", subjectId: "s1" }), kb({ key: "other", subjectId: "s2" })];
  assert.equal(knowledgeEntryFor(list, "s1")?.key, "some-key-1");
  assert.equal(knowledgeEntryFor(list, "s3"), null, "no entry: no lessons");
  assert.equal(knowledgeEntryFor(list, ""), null, "the groups of a free string have an empty id and are nobody's");
  const stand = pairsSubject(null, librarySubject);
  assert.deepEqual([stand.subjectId, stand.name, stand.noSubject, stand.lessons], ["s1", "Матанализ", false, 0]);
  assert.equal(pairsSubject(list[1], librarySubject), list[1]);
  assert.equal(pastLesson(stand, { kind: "undated" }).librarySubjectId, "s1", "a note of a subject without lessons is filed under the subject");
});

test("readPairs: the group gives the key, the key gives the ordered lectures; no group is no second call; a failure is Russian text", async () => {
  const calls = [];
  const lectures = [noteOnly("2026-10-01"), recorded()];
  const api = {
    subjects: async () => (calls.push("subjects"), [kb({ key: "k1" }), kb({ key: "k2", subjectId: "s2" })]),
    lectures: async (key) => (calls.push(`lectures:${key}`), lectures),
  };
  const ok = await readPairs(api, "s1");
  assert.deepEqual(calls, ["subjects", "lectures:k1"]);
  assert.equal(ok.status, "ready");
  assert.equal(ok.entry.key, "k1");
  assert.equal(ok.lessons[0].recordingId, "r1", "newest first (the dated recording before the older note)");
  calls.length = 0;
  const none = await readPairs(api, "s9");
  assert.deepEqual([none.status, none.entry, none.lessons], ["ready", null, []]);
  assert.deepEqual(calls, ["subjects"]);
  const bad = await readPairs({ subjects: async () => { throw new Error("boom"); }, lectures: api.lectures }, "s1");
  assert.equal(bad.status, "failed");
  assert.notEqual(bad.error, "");
  assert.ok(!bad.error.includes("boom"), "only the fixed texts of the page are shown");
});

test("export: off with a visible reason when there is nothing to save, off while it runs", () => {
  const on = { hasEntry: true, nothingToExport: false, exporting: false };
  assert.deepEqual(exportItem(on), { disabled: false, hint: "" });
  assert.deepEqual(exportItem({ ...on, nothingToExport: true }), { disabled: true, hint: pairsTexts.exportEmpty });
  assert.deepEqual(exportItem({ ...on, hasEntry: false }), { disabled: true, hint: pairsTexts.exportEmpty });
  assert.deepEqual(exportItem({ ...on, exporting: true }), { disabled: true, hint: "" });
  assert.equal(pairsTexts.exportEmpty, "Нельзя сохранить: у предмета пока нет пар с конспектом.");
  assert.equal(pairsTexts.export, "Сохранить все конспекты одним файлом");
});

test("the export item carries what it does as its tooltip, and the reason of a disabled one is visible under it", () => {
  // The static render has the menu closed, so the item's title and hint are checked on the sources.
  const items = [{ key: "export", label: pairsTexts.export, title: pairsTexts.exportHint }];
  assert.doesNotMatch(html(h(RowMenu, { label: "x", title: "y", items, onPick: noop })), /Сохранить все/);
  assert.match(src("ui/RowMenu.tsx"), /title=\{i\.title\}/);
  assert.match(src("ui/RowMenu.tsx"), /\{hint && \(\s*<span id=\{hintId\} className="small muted mmenu-hint">/, "a disabled item's reason is visible text");
  assert.match(src("pages/SubjectPairsTab.tsx"), /title: pairsTexts\.exportHint,/);
  assert.match(src("pages/SubjectPairsTab.tsx"), /exportItem\(\{ hasEntry: data\.entry !== null, nothingToExport, exporting \}\)/);
});

test("the two «⋯» have their own tooltips, and «Удалить предмет» says why it is off", () => {
  assert.equal(headTexts.moreTitle, "Ещё действия с предметом");
  assert.equal(pairsTexts.moreTitle, "Ещё действия с парами");
  assert.equal(headTexts.busy, "Нельзя удалить, пока идёт другое действие с предметом. Удалить можно, когда оно закончится.");
  assert.match(html(h(SubjectHead, { subject: librarySubject, schedule: "", busy: false, onTickets: noop, onRemove: noop })), /title="Ещё действия с предметом"/);
  assert.match(src("pages/SubjectPairsTab.tsx"), /title=\{pairsTexts\.moreTitle\}/);
  assert.doesNotMatch(src("pages/SubjectPairsTab.tsx") + src("pages/SubjectHead.tsx"), /title="Ещё"/);
});

// ---- the status of a row of «Пары» ----

const overviewLesson = (recordingId, status, summaryNew = false) => ({ recordingId, status, summaryNew });

test("lessonMarks: a chip only for «в обработке» and «не обработано» (what the ready marks cannot say), «новое» from summaryNew, joined by recordingId", () => {
  const chips = Object.fromEntries(["recorded", "transcribed", "summaryReady", "failed"].map((s) => [s, lessonChip(s)]));
  assert.deepEqual([chips.recorded.label, chips.recorded.kind], ["В обработке", "neutral"]);
  assert.deepEqual([chips.failed.label, chips.failed.kind], ["Сбой обработки", "fail"]);
  assert.equal(chips.transcribed, null, "«✓ Конспект» and the like say it: no second word for the same fact");
  assert.equal(chips.summaryReady, null);
  const marks = lessonMarks([
    overviewLesson("r1", "failed"),
    overviewLesson("r2", "summaryReady", true),
    overviewLesson("r3", "transcribed"),
    overviewLesson("r4", "recorded"),
    overviewLesson("", "recorded"),
  ]);
  assert.deepEqual([...marks.keys()], ["r1", "r2", "r4"], "no recording, or nothing to say: no mark");
  assert.deepEqual([marks.get("r1").chip.label, marks.get("r1").fresh], ["Сбой обработки", false]);
  assert.deepEqual([marks.get("r2").chip, marks.get("r2").fresh], [null, true], "«новое» stays without a chip");
  assert.equal(marks.get("r4").chip.label, "В обработке");
  assert.equal(marks.get("r9"), undefined);
});

test("the rows of «Пары» carry the chip and «новое» of their recording, in spans inside the row button; the rest carry none", () => {
  const lessons = [
    recorded({ key: "k1", recordingId: "r1" }),
    recorded({ key: "k2", recordingId: "r2", title: "Новая" }),
    recorded({ key: "k3", recordingId: "old" }),
    noteOnly("2026-10-01"),
    recorded({ key: "k5", recordingId: "r5" }),
    recorded({ key: "k6", recordingId: "r6" }),
  ];
  const marks = lessonMarks([
    overviewLesson("r1", "failed"),
    overviewLesson("r2", "summaryReady", true),
    overviewLesson("r5", "recorded"),
    overviewLesson("r6", "transcribed"),
  ]);
  const out = html(pairsProps(ready(lessons), { marks }));
  const rows = out.match(/<button[^>]*class="kb-item"[^>]*>[^]*?<\/button>/g) ?? [];
  assert.equal(rows.length, 6);
  assert.match(rows[0], /<span class="chip s-fail"[^>]*><svg[^>]*>[^]*?<\/svg><span class="chip-text">Сбой обработки<\/span>/);
  assert.doesNotMatch(rows[0], /class="fresh"/);
  assert.match(rows[1], /<span class="fresh">новое<\/span>/);
  assert.doesNotMatch(rows[1], /chip|Конспект готов/, "a ready summary has no chip: «новое» only");
  assert.doesNotMatch(rows[2], /class="chip|class="fresh"|kb-line/, "older than the overview's latest: no chip");
  assert.doesNotMatch(rows[3], /Сбой обработки|В обработке|kb-line/);
  assert.match(rows[4], /chip s-neutral[^>]*>[^]*?В обработке/, "in the work: a quiet chip");
  assert.doesNotMatch(rows[5], /class="chip|kb-line/, "transcribed: nothing to add");
  for (const r of [rows[0], rows[1], rows[4]]) {
    assert.match(r, /<span class="kb-line"><span class="kb-name">/);
    assert.doesNotMatch(r, /<(div|p|h[1-6]|ul|li)[\s>]/, "phrasing content only inside a button");
  }
  // Without marks the list is what it was.
  assert.doesNotMatch(html(pairsProps(ready(lessons))), /kb-line|В обработке|Сбой обработки/);
});

test("LessonList without `extra` keeps its markup (the knowledge base page is unchanged)", () => {
  const out = html(h(LessonList, { lessons: [recorded()], onOpen: noop }));
  assert.match(out, /<button type="button" class="kb-item"><span class="kb-name">/);
  assert.doesNotMatch(out, /kb-line/);
  const withNull = html(h(LessonList, { lessons: [recorded()], onOpen: noop, extra: () => null }));
  assert.equal(withNull, out);
});

test("the overview's lessons reach the rows: SubjectTabs joins them and gives them to the tab", () => {
  const tabs = src("pages/SubjectTabs.tsx");
  assert.match(tabs, /lessonMarks\(props\.overview\.overview\?\.lessons \?\? \[\]\)/);
  assert.match(tabs, /marks=\{marks\}/);
  assert.match(src("pages/SubjectPairsTab.tsx"), /extra=\{\(l\) => lessonMarkView\(props\.marks\?\.get\(l\.recordingId\)\)\}/);
});

// ---- one form at a time on «Пары» ----

test("«Импортировать запись» and «Добавить конспект прошедшей пары» are never open together; the import is the accent only on an empty tab", () => {
  const tab = src("pages/SubjectPairsTab.tsx");
  assert.match(tab, /const \[form, setForm\] = useState<"none" \| "import" \| "past">\("none"\);/);
  assert.match(tab, /onClick=\{\(\) => setForm\(importOpen \? "none" : "import"\)\}/);
  assert.match(tab, /open=\{form === "past"\}\s+busyReason=\{[^}]*\}\s+onOpenChange=\{\(o\) => setForm\(o \? "past" : "none"\)\}/);
  assert.match(tab, /variant=\{empty && !importOpen \? "primary" : undefined\}/);
  // With lessons nothing on the tab is the accent.
  const withLessons = html(pairsProps(ready([recorded()])));
  assert.equal((withLessons.match(/btn primary/g) ?? []).length, 0);
  // AddPastNote: the page can own the open state; without it the block keeps its own (the knowledge base page).
  const past = (extra) => html(h(AddPastNote, { subject: { subjectId: "s1", noSubject: false }, lectures: [], onChoose: noop, today: "2026-11-03", ...extra }));
  assert.doesNotMatch(past({}), /Выберите|kb-form/);
  assert.match(past({ open: true }), /kb-form/);
  assert.doesNotMatch(past({ open: false }), /kb-form/);
});

test("while a recording is imported the forms of «Пары» do not switch (the panel would be lost with its result), and they say why", () => {
  assert.equal(pairsTexts.importBusy, "Недоступно, пока идёт импорт записи.");
  const past = (extra) => html(h(AddPastNote, { subject: { subjectId: "s1", noSubject: false }, lectures: [], onChoose: noop, today: "2026-11-03", ...extra }));
  const idle = past({});
  assert.doesNotMatch(idle, /disabled|Недоступно/);
  const busy = past({ busyReason: pairsTexts.importBusy });
  assert.match(busy, /<button[^>]*aria-describedby="[^"]*"[^>]*disabled=""[^>]*>[^]*?Добавить конспект прошедшей пары/);
  assert.match(busy, /<span id="[^"]*" class="small muted">Недоступно, пока идёт импорт записи\.<\/span>/, "the reason is visible text");
  // The panel tells the page; the page turns both buttons off.
  const tab = src("pages/SubjectPairsTab.tsx");
  assert.match(tab, /onBusyChange=\{setImportBusy\}/);
  assert.match(tab, /busyReason=\{importBusy \? pairsTexts\.importBusy : undefined\}/);
  assert.match(tab, /disabled=\{importBusy\}\s+title=\{importBusy \? pairsTexts\.importBusy : undefined\}/);
  const panel = src("pages/RecordingImportPanel.tsx");
  assert.match(panel, /onBusyChange\?: \(busy: boolean\) => void;/);
  assert.match(panel, /onBusyChange\?\.\(busy\);/);
  assert.match(panel, /useEffect\(\(\) => \(\) => onBusyChange\?\.\(false\), \[onBusyChange\]\);/, "a panel that goes away is not busy any more");
});

// ---- the delayed «Загрузка…» and the first tab stop ----

test("startDelay fires after the wait and never after cancel", () => {
  const timers = [];
  const fake = { set: (fn, ms) => (timers.push({ fn, ms, cleared: false }), timers.length - 1), clear: (i) => (timers[i].cleared = true) };
  let fired = 0;
  const cancel = startDelay(loadingDelayMs, () => fired++, fake);
  assert.equal(timers[0].ms, 800);
  assert.equal(fired, 0, "nothing at once");
  timers[0].fn();
  assert.equal(fired, 1);
  const cancel2 = startDelay(loadingDelayMs, () => fired++, fake);
  cancel2();
  assert.equal(timers[1].cleared, true);
  assert.equal(typeof cancel, "function");
  assert.match(src("pages/useAfterDelay.ts"), /return startDelay\(ms, \(\) => setLate\(true\)\);/);
});

test("the segmented tabs: with no tab selected the first one is the tab stop; with one selected only it is", () => {
  const options = [{ value: "a", label: "A" }, { value: "b", label: "B" }, { value: "c", label: "C" }];
  const bar = (value) => html(h(SegmentTabs, { label: "x", base: "id", options, value, onChange: noop }));
  const stops = (out) => [...out.matchAll(/<button[^>]*id="id-tab-(\w)"[^>]*tabindex="(-?\d)"/g)].map((m) => [m[1], m[2]]);
  assert.deepEqual(stops(bar("")), [["a", "0"], ["b", "-1"], ["c", "-1"]]);
  assert.deepEqual(stops(bar("zzz")), [["a", "0"], ["b", "-1"], ["c", "-1"]]);
  assert.deepEqual(stops(bar("b")), [["a", "-1"], ["b", "0"], ["c", "-1"]]);
});

// ---- «Задания» ----

const dated = (id, daysLeft, over = {}) => ({ assignmentId: id, title: `Задание ${id}`, due: "2026-10-27T12:00:00Z", daysLeft, ...over });
const undated = (id) => ({ assignmentId: id, title: `Без даты ${id}`, due: "", daysLeft: 0 });

test("a task chip: overdue is the warning, today and tomorrow and N days as elsewhere, no deadline has none", () => {
  assert.deepEqual(taskChip(dated("a", -3)), { kind: "fail", label: "Просрочено" });
  assert.deepEqual(taskChip(dated("a", 0)), { kind: "wait", label: "сегодня" });
  assert.equal(taskChip(dated("a", 1)).label, "завтра");
  assert.equal(taskChip(dated("a", 6)).label, "6 дней");
  assert.equal(taskChip(undated("u")), null, "daysLeft of an undated one means nothing");
  assert.equal(taskChip({ due: "", daysLeft: -5 }), null);
});

test("the rows keep the order the core sent and carry the date block only with a deadline", () => {
  const rows = taskRows([dated("late", -2), dated("soon", 1), undated("u1"), undated("u2")]);
  assert.deepEqual(rows.map((r) => r.id), ["late", "soon", "u1", "u2"], "not sorted again");
  assert.match(rows[0].day, /^\d\d\.\d\d$/);
  assert.notEqual(rows[0].weekday, "");
  assert.deepEqual([rows[2].weekday, rows[2].day, rows[2].chip], ["", "", null]);
  assert.equal(rows[1].title, "Задание soon");
  assert.equal(taskRowId("a1"), "task-open-a1");
});

const tasksState = (assignments, over = {}) => ({
  status: "ready",
  error: "",
  overview: { subjectId: "s1", name: "Матанализ", openedAt: "", deadlines: [], schedule: [], lessons: [], newMaterials: [], assignments },
  ...over,
});
const tasksTab = (state, props = {}) =>
  h(SubjectTasksTab, { subjectId: "s1", state, onRetry: noop, onCreated: noop, api: {}, library: {}, ...props });

test("the tab «Задания»: rows are whole-row buttons with a date block, the title and a chip; an undated one says «Без срока»", () => {
  const out = html(tasksTab(tasksState([dated("a1", -1, { title: evil }), dated("a2", 1), undated("a3")]), { onOpenAssignment: noop }));
  const rows = out.match(/<button[^>]*drow-open[^>]*>[^]*?<\/button>/g) ?? [];
  assert.equal(rows.length, 3);
  assert.match(rows[0], /id="task-open-a1"/);
  assert.match(rows[0], /<span class="chip s-fail"[^>]*><span class="chip-text">Просрочено<\/span>/);
  assert.ok(rows[0].includes(evilEscaped) && !out.includes(evil), "the title is a text node");
  assert.match(rows[1], /chip s-wait[^>]*><span class="chip-text">завтра</);
  assert.match(rows[2], /<span class="small muted">Без срока<\/span>/);
  assert.doesNotMatch(rows[2], /chip/);
  assert.match(rows[2], /<span class="d num" aria-hidden="true">/, "the date block keeps the column but is hidden from a reader");
  // Spans only inside the button.
  for (const r of rows) {
    assert.doesNotMatch(r, /<(div|p|h[1-6]|ul|li)[\s>]/);
  }
  // One accent: «Добавить задание» is the only primary button.
  assert.equal((out.match(/btn primary/g) ?? []).length, 1);
  assert.match(out, new RegExp(`id="${addTaskId}"`));
  assert.ok(out.indexOf(addTaskId) < out.indexOf("task-open-a1"), "the add button is above the list");
});

test("the tab «Задания»: empty says so and keeps the button under the text; loading and failed have their own line", () => {
  const empty = html(tasksTab(tasksState([])));
  assert.match(empty, /Открытых заданий по предмету нет\. Они появляются из конспектов пар; можно добавить своё\./);
  assert.ok(empty.indexOf("Открытых заданий") < empty.indexOf("Добавить задание"), "text first, the button under it");
  assert.doesNotMatch(empty, /Загрузка/);
  const loading = html(tasksTab({ status: "loading", overview: null, error: "" }));
  assert.doesNotMatch(loading, /Загрузка/, "a quick read leaves no flash: the text comes after loadingDelayMs only");
  assert.match(src("pages/SubjectTasksTab.tsx"), /const slow = useAfterDelay\(loading, loadingDelayMs\);/);
  assert.match(src("pages/SubjectTasksTab.tsx"), /\{loading && slow && \(\s*<p className="muted small" role="status">\s*\{tabTexts\.loading\}/);
  assert.match(loading, /Добавить задание/, "the add button works without the list");
  // The tab says it in its own words, whatever text the flow carries.
  const failed = html(tasksTab({ status: "failed", overview: null, error: "Не удалось загрузить сведения о предмете." }));
  assert.match(failed, /Не удалось загрузить задания предмета\./);
  assert.doesNotMatch(failed, /сведения о предмете/);
  assert.match(failed, />Повторить</);
  assert.doesNotMatch(failed, /Загрузка/);
  assert.equal(tasksTexts.failed, "Не удалось загрузить задания предмета.");
});

test("a failed re-read after «Добавить задание» keeps the list and says so in its own words, with «Повторить»", () => {
  assert.equal(tasksTexts.rereadFailed, "Список не обновился: новое задание может пока не отображаться.");
  const kept = html(tasksTab(tasksState([dated("a1", 1)], { rereadFailed: true }), { onOpenAssignment: noop }));
  assert.match(kept, /task-open-a1/, "the previous list stays");
  assert.match(kept, /Список не обновился: новое задание может пока не отображаться\./);
  assert.doesNotMatch(kept, /Не удалось загрузить/, "the list is on screen: the line must not say it failed to load");
  assert.match(kept, />Повторить</);
  // Nothing to show yet and the re-read failed: not «нет заданий», the new one may be missing.
  const none = html(tasksTab(tasksState([], { rereadFailed: true })));
  assert.doesNotMatch(none, /Открытых заданий по предмету нет/);
  assert.match(none, /Список не обновился/);
  assert.doesNotMatch(html(tasksTab(tasksState([dated("a1", 1)]))), /Не удалось|не обновился/);
});

test("«Добавить задание» opens the form with the subject of the page chosen; by default no subject", () => {
  const subjects = [
    { id: "s1", name: "Матанализ", aliases: [], semester: "", semesterLabel: "", folders: [] },
    { id: "s2", name: "Физика", aliases: [], semester: "", semesterLabel: "", folders: [] },
  ];
  const dialog = (extra) =>
    html(h(AddAssignmentDialog, { api: {}, library: {}, initialSubjects: subjects, onCreated: noop, onClose: noop, ...extra }));
  assert.match(dialog({ initialSubjectId: "s2" }), /<option value="s2" selected="">Физика<\/option>/);
  assert.doesNotMatch(dialog({ initialSubjectId: "s2" }), /<option value="s1" selected/);
  assert.doesNotMatch(dialog({}), /<option value="s[12]" selected/);
  assert.match(dialog({}), /<option value="" selected="">/, "the default is unchanged: no subject");
  const tab = src("pages/SubjectTasksTab.tsx");
  assert.match(tab, /initialSubjectId=\{props\.subjectId\}/);
});

test("the tab «Задания»: after an add the overview is read again and the focus goes to the new row, else to «Добавить задание»", () => {
  const tab = src("pages/SubjectTasksTab.tsx");
  assert.match(tab, /pending\.current = \{ id: a\.id, seen: overview \};\s*setAdding\(false\);\s*props\.onCreated\(\);/);
  assert.match(tab, /\(document\.getElementById\(taskRowId\(p\.id\)\) \?\? document\.getElementById\(addTaskId\)\)\?\.focus\(\)/);
  assert.match(tab, /if \(wasAdding\.current && !adding\) \{\s*document\.getElementById\(addTaskId\)\?\.focus\(\);/, "the closed dialog gives the focus back");
  const page = src("pages/SubjectsPage.tsx");
  assert.match(page, /onOverviewChanged=\{rereadOverview\}/);
  assert.match(page, /const rereadOverview = \(\) => overviewFlow\.current\?\.refresh\(shownId\);/);
});

test("the overview carries every open assignment; the binding reads it strictly like its neighbours", () => {
  const base = { subjectId: "s1", name: "Матанализ", openedAt: "", deadlines: [], schedule: [], lessons: [], newMaterials: [] };
  const a = { assignmentId: "a1", title: "Эссе", due: "2026-10-29T12:00:00Z", daysLeft: -2 };
  assert.deepEqual(parseOverview({ ...base, assignments: [a, { ...a, assignmentId: "a2", due: "", daysLeft: 0 }] }).assignments.map((x) => [x.assignmentId, x.due, x.daysLeft]), [
    ["a1", "2026-10-29T12:00:00Z", -2],
    ["a2", "", 0],
  ]);
  assert.deepEqual(parseOverview({ ...base, assignments: null }).assignments, []);
  assert.deepEqual(parseOverview(base).assignments, [], "an older core without the list: an empty tab");
  assert.throws(() => parseAssignment({ ...a, daysLeft: 1.5 }));
  assert.throws(() => parseAssignment({ ...a, title: 5 }));
  assert.throws(() => parseAssignment({ ...a, due: null }));
  assert.throws(() => parseAssignment({ ...a, assignmentId: undefined }));
  assert.throws(() => parseOverview({ ...base, assignments: [{ ...a, daysLeft: "1" }] }));
  assert.throws(() => parseOverview({ ...base, assignments: {} }));
});

// ---- the overview flow ----

const settle = () => new Promise((r) => setTimeout(r, 0));

test("a re-read of the overview shows no «Загрузка…», records no new opening; when it fails the previous overview stays and the state says so", async () => {
  const calls = [];
  let fail = false;
  let reads = 0;
  const api = {
    subjectOverview: async (id) => {
      calls.push(["overview", id]);
      if (fail) throw new Error("boom");
      return { subjectId: id, name: "x", openedAt: "", deadlines: [], assignments: [], schedule: [], lessons: [], newMaterials: [], read: ++reads };
    },
    markSubjectOpened: async (id) => void calls.push(["mark", id]),
  };
  const states = [];
  const flow = new SubjectOverviewFlow(api, (s) => states.push(s));
  flow.open("s1");
  await settle();
  flow.refresh("s1");
  await settle();
  assert.deepEqual(calls, [["overview", "s1"], ["mark", "s1"], ["overview", "s1"]], "one opening recorded, two reads");
  assert.deepEqual(states.map((s) => s.status), ["loading", "ready", "ready"]);
  const shown = states[2].overview;
  assert.equal(shown.read, 2);
  fail = true;
  flow.refresh("s1");
  await settle();
  assert.equal(states.length, 4, "a failed re-read is told, not swallowed");
  assert.equal(states[3].status, "ready");
  assert.equal(states[3].overview, shown, "the previous overview is kept");
  assert.equal(states[3].rereadFailed, true);
  // «Повторить» re-reads (no new opening, no «Загрузка…»); success clears the flag.
  fail = false;
  flow.retry("s1");
  await settle();
  assert.equal(states.length, 5);
  assert.equal(states[4].rereadFailed, undefined);
  assert.equal(states[4].overview.read, 3);
  assert.equal(calls.filter((c) => c[0] === "mark").length, 1);
  flow.refresh("s1");
  flow.close();
  await settle();
  assert.equal(states.length, 5, "a closed flow shows no late reply");
});

test("«Повторить» of a first reading that failed opens again; the overview line of the head is gone from the other tabs", async () => {
  let fail = true;
  const api = {
    subjectOverview: async (id) => {
      if (fail) throw new Error("boom");
      return { subjectId: id, name: "x", openedAt: "", deadlines: [], assignments: [], schedule: [], lessons: [], newMaterials: [] };
    },
    markSubjectOpened: async () => undefined,
  };
  const states = [];
  const flow = new SubjectOverviewFlow(api, (s) => states.push(s.status));
  flow.open("s1");
  await settle();
  assert.deepEqual(states, ["loading", "failed"]);
  fail = false;
  flow.retry("s1");
  await settle();
  assert.deepEqual(states, ["loading", "failed", "loading", "ready"], "a first reading, as at the opening");
  const page = src("pages/SubjectsPage.tsx");
  assert.doesNotMatch(page, /overviewState\.status === "failed"/, "no vague line above the tabs");
  assert.match(page, /const retryOverview = \(\) => overviewFlow\.current\?\.retry\(shownId\);/);
});

// ---- the route ----

test("every builder clears the subject tab; routeToSubjectTab sets it on the subjects page only", () => {
  const on = { ...routeToSubject(routeToPage("subjects"), "s1"), subjectTab: "tasks" };
  assert.deepEqual(routeToSubject(on, "s2"), { ...routeToPage("subjects"), subject: "s2" });
  assert.equal(routeToSubject(on, "s2").subjectTab, "");
  assert.equal(routeToPage("subjects").subjectTab, "");
  assert.equal(routeToAssignment("a1").subjectTab, "");
  assert.equal(routeToOpenedAssignment(on, "a1").subjectTab, "");
  assert.equal(initialRoute.subjectTab, "");
  assert.deepEqual(routeToSubjectTab(routeToSubject(routeToPage("subjects"), "s1"), "files"), { ...routeToSubject(routeToPage("subjects"), "s1"), subjectTab: "files" });
  assert.equal(routeToSubjectTab(on, "tasks"), on, "the same tab is the same route");
  const other = routeToPage("today");
  assert.equal(routeToSubjectTab(other, "files"), other, "no subject page, no tab");
});

test("«Билеты к экзамену» is the page «Билеты» with the subject in the route", () => {
  const r = routeToTickets({ ...routeToSubject(routeToPage("subjects"), "s1"), subjectTab: "pairs" }, "s1");
  assert.deepEqual(r, { ...routeToPage("tickets"), subject: "s1" });
  assert.equal(r.subjectTab, "");
});

test("the tab is not a step of the history; a lesson, an assignment or the tickets opened from a tab are, and «Назад» returns to the tab", () => {
  let hist = newHistory(routeToPage("subjects"));
  hist = pushRoute(hist, routeToSubject(currentRoute(hist), "s1"));
  assert.equal(hist.entries.length, 2);
  // The page decides the default tab and puts it into the route: the entry gets it, no new entry.
  hist = pushRoute(hist, routeToSubjectTab(currentRoute(hist), "pairs"));
  assert.equal(hist.entries.length, 2);
  assert.equal(currentRoute(hist).subjectTab, "pairs");
  hist = pushRoute(hist, routeToSubjectTab(currentRoute(hist), "tasks"));
  assert.equal(hist.entries.length, 2, "switching tabs adds nothing");
  assert.equal(currentRoute(hist).subjectTab, "tasks");
  assert.equal(sameRoute(routeToSubjectTab(currentRoute(hist), "files"), currentRoute(hist)), true);

  // A lesson from the tab: its own entry, over the same tab.
  hist = pushRoute(hist, routeToLesson(currentRoute(hist), "recording:r1", "summary"));
  assert.equal(hist.entries.length, 3);
  assert.equal(currentRoute(hist).subjectTab, "tasks");
  assert.equal(currentRoute(goBack(hist)).subjectTab, "tasks", "Alt+← lands on the tab the lesson was opened from");
  assert.deepEqual(closeLesson(currentRoute(hist)), currentRoute(goBack(hist)));
  hist = goBack(hist);

  // The tickets from the head: an entry of its own; back returns to the subject on its tab.
  hist = pushRoute(hist, routeToTickets(currentRoute(hist), "s1"));
  assert.equal(currentRoute(hist).page, "tickets");
  assert.equal(currentRoute(hist).subject, "s1");
  hist = goBack(hist);
  assert.deepEqual([currentRoute(hist).page, currentRoute(hist).subject, currentRoute(hist).subjectTab], ["subjects", "s1", "tasks"]);

  // An assignment from the tab, then back; then forward returns to the assignment.
  hist = pushRoute(hist, routeToAssignment("a1"));
  hist = goBack(hist);
  assert.equal(currentRoute(hist).subjectTab, "tasks");
  assert.equal(currentRoute(goForward(hist)).assignment, "a1");

  // Another tab chosen after coming back replaces the tab of the entry, «Вперёд» is still the assignment's.
  hist = pushRoute(hist, routeToSubjectTab(currentRoute(hist), "files"));
  assert.equal(currentRoute(hist).subjectTab, "files");
  assert.equal(currentRoute(goForward(hist)).assignment, "a1");
});

// ---- the tabs rendered ----

const pairsProps = (data, extra = {}) =>
  h(SubjectPairsTab, { api: {}, subject: librarySubject, data, onRetry: noop, onImported: noop, onOpenLesson: noop, ...extra });
const ready = (lessons, entry = kb()) => ({ status: "ready", entry, lessons, error: "" });

test("the tabs: a bar with three tabs and a tabpanel; until the default is decided none is chosen, the panel is empty (no flash) and the first tab is the tab stop", () => {
  const props = {
    subject: librarySubject, tab: "", onTab: noop, overview: idleOverview, onRetryOverview: noop, onOverviewChanged: noop,
    onOpenLesson: noop, lessonsSeen: 0, lessonsEdited: 0, files: h("p", null, "ФАЙЛЫ"),
  };
  const out = html(h(SubjectTabs, props));
  assert.match(out, /role="tablist" aria-label="Разделы предмета"/);
  assert.equal((out.match(/role="tab"/g) ?? []).length, 3);
  assert.deepEqual([...out.matchAll(/role="tab"[^>]*>(?:<[^>]+>)*([А-Яа-я]+)/g)].map((m) => m[1]), ["Пары", "Файлы", "Задания"]);
  assert.equal((out.match(/aria-selected="true"/g) ?? []).length, 0);
  assert.match(out, /<div role="tabpanel" aria-busy="true" class="tab-panel"><\/div>/, "an empty box of a stable height, no «Загрузка…» at once");
  assert.doesNotMatch(out, /Загрузка/);
  assert.doesNotMatch(out, /ФАЙЛЫ/);
  assert.equal((out.match(/tabindex="0"/g) ?? []).length, 1, "the first tab can be reached by keyboard");
  assert.match(out, /<button[^>]*id="[^"]*-tab-pairs"[^>]*tabindex="0"/);
  // The «Загрузка…» comes after loadingDelayMs only.
  const tabs = src("pages/SubjectTabs.tsx");
  assert.match(tabs, /const slow = useAfterDelay\(!decided, loadingDelayMs\);/);
  assert.match(tabs, /slow && \(\s*<p className="muted small" role="status">/);
  assert.equal(loadingDelayMs, 800);
  assert.match(src("styles.css"), /\.tab-panel \{ min-height: \d+px; \}/);

  const files = html(h(SubjectTabs, { ...props, tab: "files" }));
  assert.match(files, /<button[^>]*role="tab"[^>]*id="[^"]*-tab-files"[^>]*aria-selected="true"/);
  assert.match(files, /role="tabpanel" id="[^"]*-panel-files" aria-labelledby="[^"]*-tab-files"/);
  assert.match(files, /ФАЙЛЫ/);
  assert.doesNotMatch(files, /aria-busy/);
  assert.equal((files.match(/tabindex="0"/g) ?? []).length, 1, "one tab stop; arrows move");
  // The other tabs wait the same way (the lessons are read when the page opens): an empty box first, no flash.
  const pairs = html(h(SubjectTabs, { ...props, tab: "pairs" }));
  assert.match(pairs, /id="[^"]*-panel-pairs"[^>]*><\/div>/);
  assert.doesNotMatch(pairs, /Загрузка/);
  assert.match(html(h(SubjectTabs, { ...props, tab: "tasks" })), /id="[^"]*-panel-tasks"[^>]*>[^]*Добавить задание/);
  // An unknown value of the route is «not chosen».
  assert.match(html(h(SubjectTabs, { ...props, tab: "bogus" })), /aria-busy="true"/);
});

test("«Пары» stays mounted once shown, hidden and not exposed while another tab is chosen; the other tabs have one panel", () => {
  const tabs = src("pages/SubjectTabs.tsx");
  assert.match(tabs, /const \[pairsShown, setPairsShown\] = useState\(false\);\s*if \(chosen === "pairs" && !pairsShown\) \{\s*setPairsShown\(true\);/);
  assert.match(tabs, /\{pairsShown && \(\s*<div\s+role="tabpanel"\s+id=\{segmentPanelId\(base, "pairs"\)\}[^>]*hidden=\{chosen !== "pairs"\}/);
  assert.match(tabs, /\{chosen !== "pairs" && \(\s*<div\s+role="tabpanel"/);
  const props = {
    subject: librarySubject, onTab: noop, overview: idleOverview, onRetryOverview: noop, onOverviewChanged: noop,
    onOpenLesson: noop, lessonsSeen: 0, lessonsEdited: 0, files: h("p", null, "ФАЙЛЫ"),
  };
  const pairs = html(h(SubjectTabs, { ...props, tab: "pairs" }));
  assert.equal((pairs.match(/role="tabpanel"/g) ?? []).length, 1);
  assert.doesNotMatch(pairs, /hidden/);
  // Not visited yet: no pairs panel at all, so the other tab's panel is the only one.
  const files = html(h(SubjectTabs, { ...props, tab: "files" }));
  assert.equal((files.match(/role="tabpanel"/g) ?? []).length, 1);
  assert.doesNotMatch(files, /-panel-pairs/);
});

test("the counts on the bar wait for the decision: nothing to count while the tab is undecided", () => {
  const props = {
    subject: librarySubject, tab: "", onTab: noop, onRetryOverview: noop, onOverviewChanged: noop,
    onOpenLesson: noop, lessonsSeen: 0, lessonsEdited: 0, files: null,
  };
  assert.doesNotMatch(html(h(SubjectTabs, { ...props, overview: tasksState([dated("a", 1)]) })), /class="num"/);
});

test("the count of open assignments is `num` text on the tab «Задания», none when there are none", () => {
  const props = {
    subject: librarySubject, tab: "files", onTab: noop, onRetryOverview: noop, onOverviewChanged: noop,
    onOpenLesson: noop, lessonsSeen: 0, lessonsEdited: 0, files: null,
  };
  const withTasks = html(h(SubjectTabs, { ...props, overview: tasksState([dated("a", 1), dated("b", 2), undated("c")]) }));
  assert.match(withTasks, />Задания<span class="num"> · 3<\/span><\/button>/);
  assert.match(html(h(SubjectTabs, { ...props, overview: tasksState([]) })), />Задания<\/button>/);
  assert.match(html(h(SubjectTabs, { ...props, overview: idleOverview })), />Задания<\/button>/);
});

test("the tab «Пары»: loading shows its line only after the delay, a failure has the text and «Повторить»", () => {
  assert.doesNotMatch(html(pairsProps(loadingPairs)), /Загрузка/, "Back from a lesson re-reads quickly: no flash");
  const tab = src("pages/SubjectPairsTab.tsx");
  assert.match(tab, /const slow = useAfterDelay\(data\.status === "loading", loadingDelayMs\);/);
  assert.match(tab, /return slow \? \(\s*<p className="muted small" role="status">\s*\{tabTexts\.loading\}/);
  assert.ok(tab.indexOf("useAfterDelay(data.status") < tab.indexOf('if (data.status === "loading")'), "the hook comes before the early returns");
  const failed = html(pairsProps({ status: "failed", entry: null, lessons: [], error: "Не удалось открыть базу знаний." }));
  assert.match(failed, /Не удалось открыть базу знаний\./);
  assert.match(failed, />Повторить</);
  assert.doesNotMatch(failed, /Загрузка/);
});

test("the tab «Пары», empty: the text, then «Импортировать запись» (the accent) and «Добавить конспект прошедшей пары»", () => {
  const out = html(pairsProps(ready([], null)));
  const text = out.indexOf("Записей по предмету пока нет.");
  assert.ok(text >= 0);
  assert.ok(text < out.indexOf("Импортировать запись"), "the buttons are under the text");
  assert.ok(out.indexOf("Импортировать запись") < out.indexOf("Добавить конспект прошедшей пары"));
  assert.equal((out.match(/btn primary/g) ?? []).length, 1, "one accent");
  assert.match(out, /<button[^>]*class="btn primary"[^>]*aria-expanded="false"[^>]*>(<svg.*?<\/svg>)?Импортировать запись/);
  // The past note works for a subject without lessons: the button is on (the library subject stands in).
  assert.doesNotMatch(out, /<button[^>]*disabled=""[^>]*>[^<]*(<svg.*?<\/svg>)?Добавить конспект/);
  assert.doesNotMatch(out, /aria-label="Пары"/, "no empty list");
  // «⋯» holds the export; the closed menu shows nothing of it.
  assert.match(out, /aria-label="Ещё действия с парами"/);
  assert.doesNotMatch(out, /Сохранить все конспекты/);
});

test("the tab «Пары» with lessons: rows that open the lesson, no heading over the list, titles as text", () => {
  const opened = [];
  const lessons = [recorded({ title: evil }), noteOnly("2026-10-01")];
  const el = pairsProps(ready(lessons), { onOpenLesson: (l, o) => opened.push([l.key, o]) });
  const out = html(el);
  assert.match(out, /<ul class="card kb-list" aria-label="Пары">/);
  assert.equal((out.match(/class="kb-item"/g) ?? []).length, 2);
  assert.ok(out.includes(evilEscaped) && !out.includes(evil));
  assert.doesNotMatch(out, /<h2/, "the tab name is the heading");
  assert.doesNotMatch(out, /Записей по предмету пока нет/);
  // A row opens the lesson screen with no tab and no place named; the editing form is not asked for.
  const tab = src("pages/SubjectPairsTab.tsx");
  assert.match(tab, /const open = \(lesson: KnowledgeLesson, editing = false\) => props\.onOpenLesson\(lesson, \{ focus: noFocus, materialId: "", editing \}\);/);
  assert.deepEqual(noFocus, { tab: "notes", index: -1 });
  assert.match(tab, /<LessonList lessons=\{data\.lessons\} onOpen=\{\(l\) => open\(l\)\} extra=\{/);
  assert.match(tab, /onChoose=\{\(c: PastChoice\) => open\(pastLesson\(kbSubject, c\)\)\}/, "a past pair opens the lesson screen too");
});

test("the tab «Пары»: «Импортировать запись» opens the panel with the subject prefilled and the lessons are read again after an import", () => {
  const tab = src("pages/SubjectPairsTab.tsx");
  assert.match(tab, /subject=\{subject\.name\}/);
  assert.match(tab, /onImported=\{props\.onImported\}/);
  assert.match(tab, /variant=\{empty && !importOpen \? "primary" : undefined\}/, "the accent only on an empty tab, and not while the panel is open");
  const tabs = src("pages/SubjectTabs.tsx");
  assert.match(tabs, /onImported=\{\(\) => setImports\(\(n\) => n \+ 1\)\}/);
  assert.match(tabs, /useSubjectPairs\(kb, props\.subject\.id, props\.lessonsSeen \+ props\.lessonsEdited \+ imports\)/);
});

test("the export of all notes is in the «⋯» of the tab, with the result line and the same texts as the base", () => {
  const tab = src("pages/SubjectPairsTab.tsx");
  assert.match(tab, /<RowMenu label=\{pairsTexts\.more\} title=\{pairsTexts\.moreTitle\} items=\{items\} onPick=\{\(\) => void onExport\(\)\} \/>/);
  assert.match(tab, /props\.api\.exportSubject\(data\.entry\.key\)/);
  assert.match(tab, /setMessage\(exportResultLine\(r\)\)/);
  assert.match(tab, /disabled: exportState\.disabled,\s*hint: exportState\.hint,/);
  assert.equal(pairsTexts.exportHint, "Сохранить все пары предмета в один файл .md — по датам, с конспектом и текстом лекции.");
});

// ---- the head ----

test("the head: name and sub-lines, «Билеты к экзамену» and «⋯»; no «Добавить файл», no «Удалить предмет» button, no «База знаний»", () => {
  const out = html(h(SubjectHead, { subject: { ...librarySubject, name: evil, aliases: ["МА"] }, schedule: "Лекции — вт, 10:30", busy: false, onTickets: noop, onRemove: noop }));
  assert.ok(out.includes(`<h1>${evilEscaped}</h1>`));
  assert.match(out, /Осень 2026 · Также: МА/);
  assert.match(out, /Лекции — вт, 10:30/);
  assert.match(out, /<button[^>]*class="btn"[^>]*>(<svg.*?<\/svg>)?Билеты к экзамену<\/button>/, "secondary, not the accent");
  assert.match(out, /<button[^>]*class="btn ghost mmenu-btn"[^>]*aria-label="Ещё действия с предметом"/);
  assert.doesNotMatch(out, /Добавить файл|Удалить предмет|База знаний|primary|danger/);
  assert.ok(out.indexOf("Билеты к экзамену") < out.indexOf("mmenu-btn"), "tickets, then «⋯»");
});

test("«Билеты к экзамену» opens the tickets page of this subject from the window", () => {
  const page = src("pages/SubjectsPage.tsx");
  assert.match(page, /onTickets=\{\(\) => props\.onOpenTickets\?\.\(subject\.id\)\}/);
  const app = src("App.tsx");
  assert.match(app, /const openTickets = useCallback\(\(subjectId: string\) => setRoute\(\(r\) => routeToTickets\(r, subjectId\)\), \[\]\);/);
  assert.match(app, /<TicketsPage onOpen=\{props\.onOpen\} subjectId=\{props\.subject\} onOpenSubjects=\{props\.onOpenSubjects\} onOpenSubject=\{props\.onSelectSubject\} \/>/);
  assert.match(app, /onOpenSubjects=\{lessonNav\.onOpenSubjects\}/);
});

// ---- the tickets page ----

test("the tickets page opens on the subject of the route while it is listed, else on the first", () => {
  const list = [{ id: "a" }, { id: "b" }];
  assert.equal(startSubjectId(list, "b"), "b");
  assert.equal(startSubjectId(list, ""), "a");
  assert.equal(startSubjectId(list, "gone"), "a");
  assert.equal(startSubjectId([], "b"), "");
  const page = src("pages/TicketsPage.tsx");
  assert.match(page, /setSubjectId\(\(id\) => \(id !== "" \? id : startSubjectId\(list, wanted\.current\)\)\)/);
  assert.match(page, /subjectId\?: string;/);
});

test("the crumbs of the tickets: «Предметы / <предмет> / Билеты»; «Предметы» opens the section, the subject opens its page", () => {
  const calls = [];
  const nav = { onSubjects: () => calls.push("subjects"), onSubject: (id) => calls.push(`subject:${id}`) };
  const crumbs = ticketsCrumbs({ id: "s1", name: "Матанализ" }, nav);
  assert.deepEqual(crumbs.map((c) => c.label), ["Предметы", "Матанализ", "Билеты к экзамену"]);
  crumbs[0].onClick();
  crumbs[1].onClick();
  assert.deepEqual(calls, ["subjects", "subject:s1"]);
  assert.equal(crumbs[2].onClick, undefined, "the last level is the page");
  // Before the subject is known no crumb names a wrong one.
  assert.deepEqual(ticketsCrumbs(null, nav).map((c) => c.label), ["Предметы", "Билеты к экзамену"]);
  assert.equal(ticketTexts.back, "Предметы");
});

test("the tickets page has one name, and opened from a subject it hides the select behind «Сменить предмет»", () => {
  assert.equal(ticketTexts.title, "Билеты к экзамену");
  assert.equal(ticketTexts.change, "Сменить предмет");
  assert.equal(ticketTexts.noSubjectsTitle, "Сначала нужен предмет", "the empty state keeps its title");
  const page = src("pages/TicketsPage.tsx");
  assert.match(page, /const pinned = wanted\.current !== "" && subjectId === wanted\.current && !changing;/);
  assert.match(page, /\{pinned \? \([^]*?<span>\{subjectLine\(subject\.name\)\}<\/span>[^]*?disabled=\{blocked\}\s+onClick=\{\(\) => setChanging\(true\)\}[^]*?\{texts\.changeShort\}[^]*?\) : \(\s*<Field label=\{texts\.subject\}>/);
  assert.match(page, /pick\.current\?\.querySelector\("select"\)\?\.focus\(\)/, "the pressed button is gone: the select takes the focus");
});

test("opened from a subject the tickets page says «Предмет: …» with «Сменить» on one line; busy and a vanished subject are handled", () => {
  assert.equal(subjectLine("Матанализ"), "Предмет: Матанализ");
  assert.equal(ticketTexts.changeShort, "Сменить");
  assert.equal(ticketTexts.changeBusy, "Нельзя сменить предмет, пока идёт другое действие.");
  const page = src("pages/TicketsPage.tsx");
  assert.match(page, /<p className="tk-subject">/);
  assert.match(page, /aria-label=\{texts\.change\}/, "the short word keeps the full accessible name");
  assert.match(page, /title=\{blocked \? texts\.changeBusy : undefined\}\s+disabled=\{blocked\}/, "a disabled button says why");
  // The route's subject must be the one shown: if it vanished from the list, startSubjectId picked another and the select is shown.
  assert.match(page, /const pinned = wanted\.current !== "" && subjectId === wanted\.current && !changing;/);
  assert.equal(startSubjectId([{ id: "a" }], "gone"), "a", "so subjectId !== wanted and pinned is false");
  assert.match(src("pages/tickets.css"), /\.tk-subject \{ display: flex;/);
});

// ---- the tree ----

test("«База знаний» is a global button of the tree under «Предмет» and «Папка»; the click on a folder goes through the page", () => {
  const props = { subjects: [{ ...librarySubject, folders: [{ id: "f1", name: "Материалы", system: "materials" }] }], currentId: "s1", counts: null, busy: false, onOpen: noop, onAddSubject: noop, onAddFolder: noop };
  const out = html(h(SubjectTree, { ...props, onOpenKnowledge: noop }));
  const acts = out.slice(out.indexOf('class="tree-acts"'));
  assert.ok(acts.indexOf("Добавить предмет") < acts.indexOf("Новая папка") && acts.indexOf("Новая папка") < acts.indexOf("База знаний"));
  assert.match(acts, /<button[^>]*class="btn sm"[^>]*>(<svg.*?<\/svg>)?База знаний<\/button>/);
  assert.doesNotMatch(html(h(SubjectTree, props)), /База знаний/, "no handler, no button");
  const page = src("pages/SubjectsPage.tsx");
  assert.match(page, /onOpenKnowledge=\{\(\) => props\.onOpen\("knowledge"\)\}/);
  assert.equal((page.match(/onOpenKnowledge=/g) ?? []).length, 1);
  assert.match(src("pages/SubjectsParts.tsx"), /onClick=\{\(\) => \(props\.onOpenFolder \?\? scrollToFolder\)\(f\.id\)\}/);
});

test("a click on a folder of the tree shows the tab «Файлы» first, then scrolls; «Новая папка» opens the tab with the form", () => {
  const page = src("pages/SubjectsPage.tsx");
  assert.match(page, /const openFolder = \(folderId: string\) => \{\s*if \(tab === "files" \|\| onSubjectTab === undefined\) \{\s*scrollToFolder\(document, folderId\);\s*return;\s*\}\s*folderJump\.current = folderId;\s*onSubjectTab\("files"\);/);
  assert.match(page, /if \(tab === "files" && folderJump\.current !== ""\) \{\s*scrollToFolder\(document, folderJump\.current\);\s*folderJump\.current = "";\s*\}\s*\}, \[tab\]\);/);
  assert.match(page, /setForm\("folder"\);\s*onSubjectTab\?\.\("files"\);/);
  assert.match(page, /onOpenFolder=\{openFolder\}/);
});

// ---- the focus after «Добавить файл» ----

test("one id finds the «+» of a folder wherever it is drawn: in the «Пусто: …» line or in the header of its section", () => {
  const folders = [{ id: "f1", name: "", system: "lectures" }, { id: "f2", name: "", system: "seminars" }];
  const subject = { ...librarySubject, folders };
  const detail = (materials) => html(h(SubjectDetail, { subject, materials, folderForm: false, busy: false, error: "", onCreateFolder: noop, onCancelFolder: noop, onAddFile: noop }));
  const file = (folderId) => ({ id: `m-${folderId}`, name: "x.pdf", type: "pdf", sizeBytes: 1, subjectId: "s1", folderId, inbox: false, removed: false, textStatus: "", textNote: "", createdAt: "" });
  const id = folderAddId("f2");
  assert.equal(id, "folder-add-f2");
  // Empty: the button is in the line.
  const before = detail([file("f1")]);
  assert.equal((before.match(new RegExp(`id="${id}"`, "g")) ?? []).length, 1);
  assert.match(before, new RegExp(`empty-folder-name" id="folder-f2">Семинары<button[^>]*id="${id}"`));
  // The first file landed: the folder is a section and the button is in its header, still the only one with that id.
  const after = detail([file("f1"), file("f2")]);
  assert.equal((after.match(new RegExp(`id="${id}"`, "g")) ?? []).length, 1);
  assert.match(after, new RegExp(`<div class="sec-head"><h2[^>]*>Семинары[^]*?<button[^>]*id="${id}"`));
  assert.doesNotMatch(after, /empty-folder-name" id="folder-f2"/);
});

test("the focus goes back only when it is lost or still on the button", () => {
  assert.equal(refocusAllowed(null, "x"), true);
  assert.equal(refocusAllowed({ id: "", isBody: true }, "x"), true, "the page body: the button is gone");
  assert.equal(refocusAllowed({ id: "x", isBody: false }, "x"), true);
  assert.equal(refocusAllowed({ id: "other", isBody: false }, "x"), false, "the student moved on");
  assert.equal(addFileId, "subject-add-file");
});

test("the page arms the focus on «Добавить файл» and waits for the files to be read again when the file landed", () => {
  const page = src("pages/SubjectsPage.tsx");
  assert.match(page, /const focusBack = useFocusBack\(busy, materials, filesFailed\);/);
  assert.match(page, /focusBack\.arm\(folderId === "" \? addFileId : folderAddId\(folderId\)\);/);
  assert.match(page, /focusBack\.reloading\(\);\s*refresh\(\);/);
  assert.match(page, /folderJump\.current = "";\s*focusBack\.cancel\(\);/, "another subject forgets it");
  const hook = src("pages/useFocusBack.ts");
  assert.match(hook, /\(p\.reload && files === p\.seen && !failed\)/, "waits until the list is replaced, or reading failed");
  assert.match(hook, /document\.getElementById\(p\.id\)\?\.focus\(\)/);
  assert.match(hook, /refocusAllowed\(/);
  const parts = src("pages/SubjectsParts.tsx");
  assert.equal((parts.match(/<IconButton id=\{folderAddId\(f\.id\)\}/g) ?? []).length, 2, "the header «+» and the line «+»");
  assert.match(src("ui/Button.tsx"), /id\?: string;/);
});

// ---- the files tab on the page ----

test("«Добавить файл» is the first thing of the tab «Файлы» (the accent), «+ Папка» stays in the tree; the head lost both", () => {
  const page = src("pages/SubjectsPage.tsx");
  const files = page.slice(page.indexOf("files={"), page.indexOf("<SubjectDetail"));
  assert.match(files, /<Button id=\{addFileId\} variant="primary"/);
  assert.match(files, /\{filesFailed && <ErrorLine text=\{texts\.filesFailed\} onRefresh=\{refresh\} onRetry=\{refresh\} \/>\}\s*\{addLines\(""\)\}/);
  assert.doesNotMatch(src("pages/SubjectHead.tsx"), /Добавить файл|addFile/);
  assert.doesNotMatch(page, /OverviewSections|RemoveSubjectButton|onOpenLesson=\{props\.onOpenLesson\}/);
  assert.match(page, /<SubjectTabs\s+key=\{subject\.id\}/, "another subject: the tabs start afresh");
});

test("a failed overview is told on «Задания» only (its own line and «Повторить»); «Пары» and «Файлы» go on without it", () => {
  const page = src("pages/SubjectsPage.tsx");
  assert.doesNotMatch(page, /overviewState\.error|overviewState\.status === "failed"/);
  assert.match(page, /onRetryOverview=\{retryOverview\}/);
  const tab = src("pages/SubjectTasksTab.tsx");
  assert.match(tab, /state\.status === "failed" && <ErrorLine text=\{tasksTexts\.failed\}/);
  assert.match(tab, /state\.status !== "failed" && state\.rereadFailed === true && \(\s*<ErrorLine text=\{tasksTexts\.rereadFailed\}/);
});

test("App puts the tab into the route, and only the tab", () => {
  const app = src("App.tsx");
  assert.match(app, /const selectSubjectTab = useCallback\(\(tab: SubjectTab\) => setRoute\(\(r\) => routeToSubjectTab\(r, tab\)\), \[\]\);/);
  assert.match(app, /subjectTab=\{route\.subjectTab\}\s+onSubjectTab=\{selectSubjectTab\}/);
  assert.match(app, /subjectTab=\{props\.subjectTab\}\s+onSubjectTab=\{props\.onSubjectTab\}\s+onOpenTickets=\{props\.onOpenTickets\}\s+onOpenKnowledgeLesson=\{props\.onOpenLesson\}\s+lessonsSeen=\{props\.lessonsSeen\}\s+lessonsEdited=\{props\.lessonsEdited\}/);
  assert.match(src("navHistory.ts"), /now\.tab === r\.tab && now\.subjectTab === r\.subjectTab/);
});

test("the tab bar and the actions of a tab have their style; the focus ring stays the global one", () => {
  const css = src("styles.css");
  assert.match(css, /\.tab-acts \{ display: flex; flex-wrap: wrap;/);
  assert.match(css, /\.tab-acts \.kb-past \{ display: contents; \}/);
  assert.match(css, /\.tab-acts \.kb-form \{ order: 9; flex: 1 0 100%; \}/);
  assert.doesNotMatch(css, /\.tab-acts[^{]*:focus[^{]*\{[^}]*outline:\s*(none|0)/);
});
