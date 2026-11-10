// TASK-430: the recorded lessons of a subject as link rows in «Лекции» / «Семинары» / «Лабораторные», counted with the files.
// View logic on the compiled modules; function components without hooks are called and their element tree walked (a click
// is calling the onClick found there); the page wiring (it needs the Wails bindings to render) is checked in the source text.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import {
  FolderLessonsFlow,
  folderCounts,
  folderHeadCount,
  folderLessonMeta,
  folderLessonTexts,
  folderLessonTitle,
  folderLessonsFor,
  foldEmptyFolders,
  isLessonFolder,
  lessonsOfFolder,
  noFolderLessons,
  pairCountText,
  texts,
  treeCount,
} from "../.test-build/libraryView.js";
import { FolderLessonRows, FolderLessonsNotice, lessonRowOpening } from "../.test-build/pages/FolderLessonRows.js";
import { SubjectDetail, SubjectTree } from "../.test-build/pages/SubjectsParts.js";
import { IconAction } from "../.test-build/ui/IconAction.js";
import { lessonChip, lessonMarks } from "../.test-build/subjectTabsView.js";
import { noFocus } from "../.test-build/knowledgeView.js";
import { ariaLabels, buttonTag, visibleText } from "./iconbtn.mjs";

const noop = () => undefined;
const html = (el) => renderToStaticMarkup(el);
const src = (path) => readFileSync(new URL(`../src/${path}`, import.meta.url), "utf8");
const code = (path) => src(path).replace(/\/\*[\s\S]*?\*\/|\/\/.*$/gm, "");

function findAll(node, pred, out = []) {
  if (node === null || node === undefined || typeof node !== "object") {
    return out;
  }
  if (Array.isArray(node)) {
    node.forEach((n) => findAll(n, pred, out));
    return out;
  }
  if (pred(node)) {
    out.push(node);
  }
  return findAll(node.props?.children, pred, out);
}

const folder = (id, system, name = id) => ({ id, name, system });
const subject = {
  id: "s1", name: "Матанализ", aliases: [], semester: "2026-autumn", semesterLabel: "Осень 2026",
  folders: [folder("fl", "lectures"), folder("fs", "seminars"), folder("fb", "labs"), folder("fm", "materials"), folder("fo", "", "Своя")],
};
const file = (over = {}) => ({
  id: "m1", name: "лекция.pdf", type: "pdf", sizeBytes: 10, subjectId: "s1", folderId: "fl", inbox: false, removed: false,
  textStatus: "", textNote: "", createdAt: "", ...over,
});
const lesson = (over = {}) => ({
  key: "recording:r1", noteOnly: false, undated: false, anchor: "recording", scheduleLessonId: "", date: "", notes: [],
  recordingId: "r1", startedAt: "2026-10-21T12:00:00Z", durationSeconds: 4800, title: "", subject: "Матанализ", subjectKey: "ID:s1",
  kind: "lecture", subjectEdited: false, kindEdited: false, transcriptReady: true, summaryReady: true, lectureTextReady: false,
  librarySubjectId: "s1", librarySubjectState: "matched", ...over,
});
const row = (folderId = "fl", system = "lectures", over = {}) => ({ folderId, system, lesson: lesson(over) });
const detail = (over = {}) =>
  h(SubjectDetail, { subject, materials: [], folderForm: false, busy: false, error: "", onCreateFolder: noop, onCancelFolder: noop, onOpenLesson: noop, ...over });

// ---- view logic ----

test("only the three lesson folders hold link rows; the rows of a folder keep the core's order", () => {
  assert.deepEqual(subject.folders.map(isLessonFolder), [true, true, true, false, false]);
  const rows = [row("fl", "lectures", { key: "recording:a", recordingId: "a" }), row("fs", "seminars"), row("fl", "lectures", { key: "recording:b", recordingId: "b" })];
  assert.deepEqual(lessonsOfFolder(rows, "fl").map((r) => r.lesson.recordingId), ["a", "b"]);
  assert.deepEqual(lessonsOfFolder(rows, "fm"), []);
  assert.deepEqual(lessonsOfFolder(null, "fl"), [], "unknown rows are no rows");
  assert.deepEqual(lessonsOfFolder(undefined, "fl"), []);
});

test("the count of a folder is its files and its link rows together; unknown rows leave the lesson folders without a count", () => {
  const files = [file(), file({ id: "m2", folderId: "fm" }), file({ id: "m3", removed: true }), file({ id: "m4", inbox: true })];
  const counts = folderCounts(subject.folders, files, [row(), row("fl", "lectures", { key: "recording:b" }), row("fb", "labs")]);
  assert.equal(counts.get("fl"), 3, "one file and two rows");
  assert.equal(counts.get("fs"), 0, "known: nothing in the seminars");
  assert.equal(counts.get("fb"), 1, "a lab is only a link row");
  assert.equal(counts.get("fm"), 1);
  // Not read yet or failed: no «0» and no «—» for the lesson folders; the other folders are still counted.
  const unknown = folderCounts(subject.folders, files, null);
  assert.deepEqual([...unknown.keys()], ["fm", "fo"]);
  assert.equal(unknown.get("fm"), 1);
  assert.equal(unknown.has("fl") || unknown.has("fs") || unknown.has("fb"), false);
  // Without the argument nothing is known of link rows and nothing is claimed away: the files only (the old call).
  assert.equal(folderCounts(subject.folders, files).get("fl"), 1);
  assert.equal(folderCounts(subject.folders, files).get("fs"), 0);
});

test("the tree shows «Лекции 1» for one processed lecture and no number at all while the rows are unknown", () => {
  const tree = (counts) =>
    html(h(SubjectTree, { subjects: [subject], currentId: "s1", counts, busy: false, onOpen: noop, onAddSubject: noop, onAddFolder: noop }));
  const known = tree(folderCounts(subject.folders, [], [row()]));
  assert.match(known, /Лекции<\/span><span class="cnt num">1<\/span>/);
  assert.match(known, /Семинары<\/span><span class="cnt num">—<\/span>/, "known and empty: the dash");
  const unknown = tree(folderCounts(subject.folders, [], null));
  assert.doesNotMatch(unknown, /Лекции<\/span><span class="cnt/, "no dash, no zero");
  assert.doesNotMatch(unknown, /Семинары<\/span><span class="cnt/);
  assert.doesNotMatch(unknown, /Лабораторные<\/span><span class="cnt/);
  assert.match(unknown, /Материалы<\/span><span class="cnt num">—<\/span>/, "the other folders keep their count");
  assert.equal(treeCount(undefined), "—");
});

test("«Пусто:» counts the link rows; while they are unknown the lesson folders are not called empty", () => {
  const empties = (r) => r.empty.map((f) => f.id);
  assert.deepEqual(empties(foldEmptyFolders(subject.folders, [], new Set(), [row()])), ["fs", "fb", "fm", "fo"], "a lecture row: «Лекции» is a section");
  assert.deepEqual(foldEmptyFolders(subject.folders, [], new Set(), [row()]).shown.map((f) => f.id), ["fl"]);
  assert.deepEqual(empties(foldEmptyFolders(subject.folders, [], new Set(), [row("fs", "seminars")])), ["fl", "fb", "fm", "fo"]);
  assert.deepEqual(empties(foldEmptyFolders(subject.folders, [], new Set(), [])), ["fl", "fs", "fb", "fm", "fo"], "known: none");
  const unknown = foldEmptyFolders(subject.folders, [], new Set(), null);
  assert.deepEqual(unknown.shown.map((f) => f.id), ["fl", "fs", "fb"], "no claim for the three");
  assert.deepEqual(empties(unknown), ["fm", "fo"]);
  assert.deepEqual(foldEmptyFolders(subject.folders, [file()], new Set(), null).shown.map((f) => f.id), ["fl", "fs", "fb"]);
  // The old call (no rows argument) behaves as before.
  assert.deepEqual(empties(foldEmptyFolders(subject.folders, [file()])), ["fs", "fb", "fm", "fo"]);
  // Files not read yet: everything stays a section, as before.
  assert.equal(foldEmptyFolders(subject.folders, null, new Set(), [row()]).empty.length, 0);
});

test("the head of a section: only the non-zero parts, with the plurals of «пара» and «файл»", () => {
  assert.deepEqual([1, 2, 4, 5, 11, 12, 21, 22, 25].map(pairCountText), ["1 пара", "2 пары", "4 пары", "5 пар", "11 пар", "12 пар", "21 пара", "22 пары", "25 пар"]);
  assert.equal(folderHeadCount(2, 1), "2 пары · 1 файл");
  assert.equal(folderHeadCount(1, 0), "1 пара");
  assert.equal(folderHeadCount(0, 3), "3 файла");
  assert.equal(folderHeadCount(5, 5), "5 пар · 5 файлов");
  assert.equal(folderHeadCount(0, 0), "");
});

test("the title and the muted line of a row: the title or «Запись 21.10», «21.10 · 1 ч 20 мин»", () => {
  assert.equal(folderLessonTitle(lesson({ title: "Пределы" })), "Пределы");
  assert.equal(folderLessonTitle(lesson({ title: "  " })), "Запись 21.10");
  assert.equal(folderLessonTitle(lesson({ title: "", startedAt: "" })), "Запись");
  assert.equal(folderLessonMeta(lesson()), "21.10 · 1 ч 20 мин");
  assert.equal(folderLessonMeta(lesson({ durationSeconds: 0 })), "21.10", "an unknown length is left out");
  assert.equal(folderLessonMeta(lesson({ startedAt: "", durationSeconds: 600 })), "10 мин");
  assert.equal(folderLessonMeta(lesson({ startedAt: "", durationSeconds: 0 })), "");
});

// ---- the section on the page ----

test("one processed lecture: the section «Лекции · 1 пара» has the row; «Пусто:» no longer lists «Лекции»", () => {
  const out = html(detail({ lessons: [row()] }));
  assert.match(out, /<h2 class="sec-h">Лекции <span class="cnt num">· 1 пара<\/span>/);
  assert.match(out, />Запись 21\.10</);
  assert.match(out, /21\.10 · 1 ч 20 мин/);
  const line = out.slice(out.indexOf("Пусто:"));
  assert.doesNotMatch(line, /Лекции/);
  assert.match(line, /Семинары[\s\S]*Лабораторные/);
  assert.doesNotMatch(out, /Пока пусто/);
});

test("a seminar row is in «Семинары», a lab row in «Лабораторные»; each head counts its rows", () => {
  const out = html(detail({ lessons: [row("fs", "seminars", { kind: "seminar" }), row("fb", "labs", { kind: "lab", key: "recording:l", recordingId: "l" })] }));
  assert.match(out, /Семинары <span class="cnt num">· 1 пара<\/span>/);
  assert.match(out, /Лабораторные <span class="cnt num">· 1 пара<\/span>/);
  assert.match(out.slice(out.indexOf("Пусто:")), /^Пусто:<\/span><span[^>]*>Лекции/);
});

test("link rows come before the files of the folder; the head says both counts", () => {
  const out = html(detail({ materials: [file()], lessons: [row(), row("fl", "lectures", { key: "recording:b", recordingId: "b", title: "Ряды" })] }));
  assert.match(out, /Лекции <span class="cnt num">· 2 пары · 1 файл<\/span>/);
  assert.ok(out.indexOf("lrow-lesson") < out.indexOf("лекция.pdf"), "rows first");
  assert.ok(out.indexOf("Запись 21.10") < out.indexOf("Ряды"), "the core's order (newest first) is kept");
  assert.equal((out.match(/class="lrow lrow-lesson"/g) ?? []).length, 2);
  // One card holds both lists.
  assert.equal((out.match(/class="card lrows"/g) ?? []).length, 1);
});

test("while the rows are not read: the lesson folders are sections that claim nothing; the files still show", () => {
  const out = html(detail({ materials: [file({ folderId: "fm" })], lessons: null }));
  assert.match(out, /<h2 class="sec-h">Лекции /);
  assert.match(out, /<h2 class="sec-h">Семинары /);
  assert.match(out, /<h2 class="sec-h">Лабораторные /);
  assert.doesNotMatch(out, /Пока пусто/);
  assert.doesNotMatch(out.slice(out.indexOf("Пусто:")), /Лекции|Семинары|Лабораторные/);
  assert.match(out, /Пусто:<\/span><span[^>]*>Своя/);
  assert.match(out, /лекция\.pdf/);
  // A folder that cannot hold rows still says «Пока пусто» when it is kept open.
  const created = html(detail({ materials: [], lessons: null, created: new Set(["fm"]) }));
  assert.match(created, /Материалы[\s\S]*Пока пусто/);
});

test("known and empty: the lesson folders fold into «Пусто:»; a kept-open one says «Пока пусто»", () => {
  const out = html(detail({ lessons: [] }));
  assert.match(out, /Пусто:<\/span><span[^>]*>Лекции<[\s\S]*Семинары[\s\S]*Лабораторные/);
  assert.match(html(detail({ lessons: [], created: new Set(["fl"]) })), /<h2 class="sec-h">Лекции <\/h2>[\s\S]*Пока пусто/);
  // A caller that knows nothing of rows (no `lessons`) draws no row and claims as before.
  assert.doesNotMatch(html(detail({ lessons: undefined })), /lrow-lesson/);
});

test("the rows need the handler that opens the lesson screen; without it none is drawn", () => {
  assert.doesNotMatch(html(detail({ lessons: [row()], onOpenLesson: undefined })), /lrow-lesson/);
});

// ---- the row ----

test("a row is one button that opens the lesson screen on its default tab", () => {
  const opened = [];
  const rows = [row()];
  const tree = FolderLessonRows({ rows, onOpen: (l, o) => opened.push([l.key, o]) });
  const buttons = findAll(tree, (n) => n.type === "button");
  assert.equal(buttons.length, 1, "the row itself");
  buttons[0].props.onClick();
  assert.deepEqual(opened, [["recording:r1", { focus: noFocus, materialId: "", editing: false }]]);
  assert.deepEqual(noFocus, { tab: "notes", index: -1 }, "the screen chooses its tab (openingTab)");
});

test("«Открыть конспект» opens the tab «Конспект», «Открыть запись» the tab «Расшифровка»; both are icons with tooltips", () => {
  const opened = [];
  const tree = FolderLessonRows({ rows: [row()], onOpen: (l, o) => opened.push([l.key, o.focus, o.materialId, o.editing]) });
  const actions = findAll(tree, (n) => n.type === IconAction);
  assert.deepEqual(actions.map((a) => a.props.label), ["Открыть конспект", "Открыть запись"]);
  assert.deepEqual(actions.map((a) => a.props.icon), ["doc", "play"]);
  for (const a of actions) {
    a.props.onClick();
  }
  assert.deepEqual(opened, [
    ["recording:r1", { tab: "summary", index: -1 }, "", false],
    ["recording:r1", { tab: "transcript", index: -1 }, "", false],
  ]);
  assert.deepEqual(lessonRowOpening("transcript").focus, { tab: "transcript", index: -1 });
  assert.deepEqual(lessonRowOpening().focus, noFocus);
  // Markup: the name is the aria-label, the same words are the tooltip, nothing but the icon is visible on them.
  const out = html(h(FolderLessonRows, { rows: [row()], onOpen: noop }));
  assert.deepEqual(ariaLabels(out), ["Открыть конспект", "Открыть запись"]);
  assert.match(out, /<span class="ia-tip" aria-hidden="true">Открыть конспект<\/span>/);
  assert.match(out, /<span class="ia-tip" aria-hidden="true">Открыть запись<\/span>/);
  assert.equal(folderLessonTexts.openSummary, "Открыть конспект");
  assert.equal(folderLessonTexts.openRecording, "Открыть запись");
  assert.doesNotMatch(visibleText(out), /Открыть/);
});

test("the keyboard reaches the row and both icons as plain buttons, and no button sits inside another", () => {
  const out = html(h(FolderLessonRows, { rows: [row(), row("fl", "lectures", { key: "recording:b", recordingId: "b", summaryReady: false })], onOpen: noop }));
  assert.equal((out.match(/<button\b/g) ?? []).length, 5, "row + 2 icons, row + 1 icon");
  assert.doesNotMatch(out, /tabindex/, "no tab stop is taken away");
  assert.doesNotMatch(out, /<button\b(?:(?!<\/button>)[^])*<button\b/, "the icons are siblings of the row button");
  const tag = out.match(/<button type="button" class="lrow-open">/);
  assert.ok(tag, "the whole row is a type=button");
  // A button is activated by Enter and Space by the browser; the row adds no key handler of its own.
  assert.doesNotMatch(code("pages/FolderLessonRows.tsx"), /onKeyDown|onKeyUp|onKeyPress|role="button"/);
});

test("«Открыть конспект» only when the summary is ready; «Открыть запись» always", () => {
  const out = html(h(FolderLessonRows, { rows: [row("fl", "lectures", { summaryReady: false, transcriptReady: false })], onOpen: noop }));
  assert.deepEqual(ariaLabels(out), ["Открыть запись"]);
});

test("no «⋯», no drag, no checkbox, no move or delete on a row", () => {
  const out = html(h(FolderLessonRows, { rows: [row()], onOpen: noop }));
  assert.doesNotMatch(out, /draggable|type="checkbox"|mmenu|aria-haspopup/);
  assert.doesNotMatch(code("pages/FolderLessonRows.tsx"), /RowMenu|draggable|onDrag|checkbox|Checkbox/);
  assert.deepEqual(ariaLabels(out).filter((l) => /Ещё|Удалить|Переместить|Разложить/.test(l)), []);
});

test("the quiet status shows only while the lesson is not processed: icon and text, no fill", () => {
  const marks = lessonMarks([
    { recordingId: "r1", status: "recorded", summaryNew: false },
    { recordingId: "r2", status: "failed", summaryNew: false },
    { recordingId: "r3", status: "summaryReady", summaryNew: false },
  ]);
  const rows = [row("fl", "lectures", { recordingId: "r1", key: "recording:r1" }), row("fl", "lectures", { recordingId: "r2", key: "recording:r2" }), row("fl", "lectures", { recordingId: "r3", key: "recording:r3" }), row("fl", "lectures", { recordingId: "r4", key: "recording:r4" })];
  const out = html(h(FolderLessonRows, { rows, marks, onOpen: noop }));
  assert.match(out, /<span class="chip s-neutral" data-status="neutral"><svg[^>]*><use href="#i-clock"><\/use><\/svg><span class="chip-text">В обработке<\/span><\/span>/);
  assert.match(out, /<span class="chip s-fail" data-status="fail"><svg[^>]*><use href="#i-warn"><\/use><\/svg><span class="chip-text">Сбой обработки<\/span><\/span>/);
  assert.equal((out.match(/class="chip /g) ?? []).length, 2, "a processed or unknown lesson has no status");
  // It is the status of the row of «Пары»: the same helper.
  assert.equal(lessonChip("recorded").label, "В обработке");
  assert.equal(out.includes("lrow-open"), true);
  // The chip lives inside the row button, so a click on it opens the lesson too (spans only).
  assert.match(out, /<button type="button" class="lrow-open">[^]*?chip-text[^]*?<\/button>/);
});

test("the title is a text node: markup in it stays text", () => {
  const evil = "<img src=x onerror=alert(1)>";
  const out = html(h(FolderLessonRows, { rows: [row("fl", "lectures", { title: evil })], onOpen: noop }));
  assert.ok(!out.includes("<img"));
  assert.ok(out.includes("&lt;img src=x onerror=alert(1)&gt;"));
  assert.doesNotMatch(code("pages/FolderLessonRows.tsx"), /dangerouslySetInnerHTML|innerHTML/);
});

// ---- the failure and the wait ----

test("a failed read: a quiet line with an icon «Повторить» (tooltip and aria-label), the click retries", () => {
  const calls = [];
  const out = html(h(FolderLessonsNotice, { failed: true, slow: false, onRetry: noop }));
  assert.equal(folderLessonTexts.failed, "Не удалось загрузить пары предмета.");
  assert.match(out, /<p class="result s-fail" role="alert"><svg[^>]*>[^]*?<\/svg><span>Не удалось загрузить пары предмета\.<\/span><\/p>/);
  assert.deepEqual(ariaLabels(out), ["Повторить"]);
  assert.match(buttonTag(out, "Повторить"), /class="ia"/);
  assert.match(out, /<use href="#i-retry"><\/use><\/svg><span class="ia-tip" aria-hidden="true">Повторить<\/span>/);
  assert.equal(visibleText(out), "Не удалось загрузить пары предмета.", "the words of the button are not visible");
  const tree = FolderLessonsNotice({ failed: true, slow: false, onRetry: () => calls.push("retry") });
  findAll(tree, (n) => n.type === IconAction)[0].props.onClick();
  assert.deepEqual(calls, ["retry"]);
});

test("a slow read shows a small «Загрузка пар…»; a quick read and a settled one show nothing", () => {
  assert.match(html(h(FolderLessonsNotice, { failed: false, slow: true, onRetry: noop })), /<p class="small muted" role="status">Загрузка пар…<\/p>/);
  assert.equal(html(h(FolderLessonsNotice, { failed: false, slow: false, onRetry: noop })), "");
  // The page keeps the files under the failure line: the files are drawn from their own state.
  const out = html(detail({ materials: [file()], lessons: null }));
  assert.match(out, /лекция\.pdf/);
});

// ---- reading the rows, and a late reply ----

function deferred() {
  const d = {};
  d.promise = new Promise((resolve, reject) => {
    d.resolve = resolve;
    d.reject = reject;
  });
  return d;
}
const settle = () => new Promise((resolve) => setImmediate(resolve));

test("the flow reads the rows of its subject: unknown first, then the rows; a failure makes them unknown again and says so", async () => {
  const states = [];
  const replies = [];
  const api = { folderLessons: (id) => { const d = deferred(); replies.push([id, d]); return d.promise; } };
  const flow = new FolderLessonsFlow(api, "s1", (s) => states.push(s));
  flow.open();
  assert.deepEqual(states, [{ subjectId: "s1", rows: null, failed: false }]);
  replies[0][1].resolve([row()]);
  await settle();
  assert.deepEqual(states[1], { subjectId: "s1", rows: [row()], failed: false });
  // A re-read keeps what is shown meanwhile (no state until the reply).
  flow.reread();
  assert.equal(states.length, 2);
  replies[1][1].reject(new Error("x"));
  await settle();
  assert.deepEqual(states[2], { subjectId: "s1", rows: null, failed: true });
  // «Повторить» is a first reading: unknown again, then the answer.
  flow.open();
  assert.deepEqual(states[3], { subjectId: "s1", rows: null, failed: false });
  replies[2][1].resolve([]);
  await settle();
  assert.deepEqual(states[4], { subjectId: "s1", rows: [], failed: false });
  assert.deepEqual(replies.map((r) => r[0]), ["s1", "s1", "s1"]);
});

test("a late reply of another subject never shows: a closed flow and an older read are dropped", async () => {
  const states = [];
  const replies = {};
  const api = { folderLessons: (id) => { const d = deferred(); (replies[id] ??= []).push(d); return d.promise; } };
  const onChange = (s) => states.push(s);
  const first = new FolderLessonsFlow(api, "s1", onChange);
  first.open();
  // The student opens another subject: the flow of the first is closed, a new one starts.
  first.close();
  const second = new FolderLessonsFlow(api, "s2", onChange);
  second.open();
  replies.s2[0].resolve([row("fl", "lectures", { key: "recording:second" })]);
  await settle();
  replies.s1[0].resolve([row("fl", "lectures", { key: "recording:first" })]);
  await settle();
  const last = states.at(-1);
  assert.equal(last.subjectId, "s2");
  assert.equal(last.rows[0].lesson.key, "recording:second");
  assert.equal(states.some((s) => s.rows?.[0]?.lesson.key === "recording:first"), false);
  // A failure of the closed flow is dropped too, and an older read of the same flow loses to the newer one.
  const third = new FolderLessonsFlow(api, "s3", onChange);
  third.open();
  third.reread();
  const before = states.length;
  replies.s3[1].resolve([row("fl", "lectures", { key: "recording:new" })]);
  await settle();
  replies.s3[0].resolve([row("fl", "lectures", { key: "recording:old" })]);
  await settle();
  assert.equal(states.length, before + 1);
  assert.equal(states.at(-1).rows[0].lesson.key, "recording:new");
  third.close();
  const fourth = new FolderLessonsFlow(api, "s4", onChange);
  fourth.open();
  fourth.close();
  const count = states.length;
  replies.s4[0].reject(new Error("late"));
  await settle();
  assert.equal(states.length, count);
});

test("the page shows only the rows read for the subject on screen", () => {
  const read = { subjectId: "s1", rows: [row()], failed: false };
  assert.equal(folderLessonsFor(read, "s1"), read);
  assert.deepEqual(folderLessonsFor(read, "s2"), { subjectId: "s2", rows: null, failed: false });
  assert.deepEqual(folderLessonsFor({ subjectId: "s1", rows: null, failed: true }, "s2"), { subjectId: "s2", rows: null, failed: false }, "a failure of another subject is not shown");
  assert.deepEqual(folderLessonsFor(noFolderLessons, ""), noFolderLessons);
});

// ---- the page ----

test("the page reads the rows next to the files on the same triggers and after the lesson screen, and counts them", () => {
  const page = src("pages/SubjectsPage.tsx");
  assert.match(page, /knowledgeBaseApi\(callByName\)/);
  assert.match(page, /new FolderLessonsFlow\(lessonsApi, subject\.id, setLessonsRead\)/);
  assert.match(page, /flow\.open\(\);[^]*?\} else \{\s*flow\.reread\(\);/, "another subject: a first reading; the same one: a re-read");
  assert.match(page, /\[lessonsApi, subject\?\.id, props\.reload, changes, props\.lessonsSeen, props\.lessonsEdited, filesTab\]/);
  assert.match(page, /const folderLessons = folderLessonsFor\(lessonsRead, subject\?\.id \?\? ""\);/);
  assert.match(page, /folderCounts\(subject\.folders, materials, lessonRows\)/);
  assert.match(page, /<FolderLessonsLine state=\{folderLessons\} onRetry=\{\(\) => lessonsFlow\.current\?\.open\(\)\} \/>/);
  assert.match(page, /lessons=\{lessonRows\}\s+lessonMarks=\{lessonStatuses\}\s+onOpenLesson=\{\(lesson, opening\) => props\.onOpenKnowledgeLesson\?\.\(lesson, opening\)\}/);
  assert.match(page, /lessonsFlow\.current\?\.close\(\);/, "the flow is closed when the page goes");
  // The files effect is unchanged beside it.
  assert.match(page, /api\.materials\(subject\.id\)\.then\(/);
});

test("the section head shows the counts as num text and the row layout adds no fill or coloured border", () => {
  const parts = src("pages/SubjectsParts.tsx");
  assert.match(parts, /\{folderTitle\(f\)\} \{head !== "" && <span className="cnt num">· \{head\}<\/span>\}/);
  const css = src("styles.css");
  const rules = [...css.replace(/\/\*[\s\S]*?\*\//g, "").matchAll(/([^{}]+)\{([^{}]*)\}/g)].map((m) => ({ selector: m[1].trim(), decls: m[2].trim() }));
  for (const r of rules.filter((x) => /\.lrow-(open|lesson|tx)/.test(x.selector))) {
    assert.doesNotMatch(r.decls, /border(?!: 0)[^;]*:|box-shadow|background: var\(--(accent|ok|rec|wait)/, r.selector);
  }
  assert.match(css, /\.lrow-open:hover \{ background: var\(--fill\); \}/);
  assert.equal(texts.emptyFolders, "Пусто:");
});
