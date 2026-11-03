// TASK-347: «Записи», «Предметы» and the dialogs with less noise and the Windows order of buttons.
// Pure view logic on the compiled modules; the pages have no DOM harness, so their wiring is checked in the source text.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { emptyFoldersText, foldEmptyFolders, texts } from "../.test-build/libraryView.js";
import { initialListState } from "../.test-build/listView.js";
import { OverviewSections, SubjectDetail } from "../.test-build/pages/SubjectsParts.js";
import {
  backgroundFailedText,
  backgroundKey,
  backgroundState,
  backgroundTitle,
  backgroundVisible,
  emptyTexts,
  headActionsVisible,
} from "../.test-build/recordingRowView.js";
import { Dialog } from "../.test-build/ui/controls.js";

const noop = () => undefined;
const html = (el) => renderToStaticMarkup(el);
const src = (path) => readFileSync(new URL(path, import.meta.url), "utf8");

// ---- «Записи» ----

test("the empty «Записи» says where to start and keeps the privacy line apart from the text", () => {
  assert.equal(
    emptyTexts.text,
    "Здесь будут записи пар. Записать можно сейчас — кнопкой «Начать запись», или загрузить расписание, и paratech сам предложит запись в начале пары.",
  );
  assert.equal(emptyTexts.schedule, "Загрузить расписание");
  assert.equal(emptyTexts.privacy, "Звук обрабатывается только на этом компьютере");
  assert.ok(!emptyTexts.text.includes(emptyTexts.privacy), "the line is not inside the paragraph");
});

test("the head buttons show with a list that has rows, wait while it loads, stay after a failed load", () => {
  assert.equal(headActionsVisible([], false), false, "empty: the empty state has them");
  assert.equal(headActionsVisible([{}], false), true);
  assert.equal(headActionsVisible(null, false), false, "loading: nothing to jump");
  assert.equal(headActionsVisible(null, true), true, "a failed load still lets the student start");
});

test("the empty state offers start (primary), the schedule and the import; the head has no duplicate", () => {
  const page = src("../src/pages/RecordingsPage.tsx");
  // The head carries the buttons only for a list with rows.
  assert.match(page, /const headActions = headActionsVisible\(recordings, recordingsError !== ""\);/);
  assert.match(page, /\{headActions && \(\s*<div className="head-acts">\s*\{importButton\}\s*\{startControl\("primary"\)\}/);
  // The empty state: the text, [Начать запись] [Загрузить расписание] [Импортировать запись], the muted privacy line.
  assert.match(
    page,
    /<div className="rec-empty">\s*<p className="empty-text">\{emptyTexts\.text\}<\/p>\s*<div className="acts start">\s*\{startControl\(startOpen \? "secondary" : "primary"\)\}[^]*?onOpen\("schedule"\)[^]*?\{emptyTexts\.schedule\}[^]*?\{importButton\}/,
  );
  assert.match(page, /<p className="small muted rec-empty-privacy">\s*<Icon name="lock" \/>\s*\{emptyTexts\.privacy\}/);
  // One accent: the empty state's start steps back to secondary while the start panel (with its own primary) is open.
  assert.match(page, /const startControl = \(variant: "primary" \| "secondary"\) => \(/);
  // The privacy line is gone from the status bar.
  assert.doesNotMatch(page, /<span className="r">/);
  assert.doesNotMatch(page, /Звук обрабатывается только на этом компьютере/);
  assert.match(page, /\{queue && \(\s*<>\s*<div className="gap" \/>\s*<div className="statusbar">/, "the bar is drawn only for the queue line");
});

test("«Фоновая обработка» is folded by default, keeps the state the student sets, hides when empty", () => {
  assert.equal(backgroundTitle, "Фоновая обработка");
  assert.deepEqual(backgroundState({}), { collapsed: true, showAll: false });
  const opened = { [backgroundKey]: { collapsed: false, showAll: false } };
  assert.deepEqual(backgroundState(opened), { collapsed: false, showAll: false });
  assert.deepEqual(backgroundState({ tasks: initialListState }), { collapsed: true, showAll: false }, "another list's state does not unfold it");
  assert.equal(backgroundVisible([]), false);
  assert.equal(backgroundVisible([{}]), true);

  const page = src("../src/pages/RecordingsPage.tsx");
  assert.match(page, /<CollapsibleList[^]*?title=\{backgroundTitle\}[^]*?count=\{visibleJobs\.length\}[^]*?state=\{background\}/);
  assert.match(page, /withState\(l, backgroundKey, s\)/);
  assert.doesNotMatch(page, /<h2 className="group-title">/);
});

test("a failed job is not hidden by the fold: the count of failures is named under it", () => {
  const job = (status) => ({ type: "send_email", status, waiting: false, progressPercent: 0 });
  assert.equal(backgroundFailedText([job("QUEUED"), job("COMPLETED")]), "");
  assert.equal(backgroundFailedText([job("FAILED"), job("QUEUED"), job("FAILED")]), "Задач с ошибкой: 2");
  assert.match(src("../src/pages/RecordingsPage.tsx"), /\{background\.collapsed && failed !== "" && <p className="small row-problem background-failed">\{failed\}<\/p>\}/);
});

// ---- «Предметы» ----

const folder = (id, system, name = id) => ({ id, name, system });
const subject = {
  id: "s1", name: "Матанализ", aliases: [], semester: "2026-autumn", semesterLabel: "Осень 2026",
  folders: [folder("f1", "lectures"), folder("f2", "seminars"), folder("f3", "labs"), folder("f6", "tickets")],
};
const file = (over = {}) => ({
  id: "m1", name: "лекция.pdf", type: "pdf", sizeBytes: 10, subjectId: "s1", folderId: "f1", inbox: false, removed: false,
  textStatus: "", textNote: "", createdAt: "", ...over,
});
const detailProps = { subject, folderForm: false, busy: false, error: "", onCreateFolder: noop, onCancelFolder: noop };

test("folders with no files fold into one «Пусто: …» line, the others stay sections", () => {
  const split = foldEmptyFolders(subject.folders, [file()]);
  assert.deepEqual(split.shown.map((f) => f.id), ["f1"]);
  assert.deepEqual(split.empty.map((f) => f.id), ["f2", "f3", "f6"]);
  assert.equal(emptyFoldersText(split.empty), "Пусто: Семинары, Лабораторные, Билеты");
  assert.equal(emptyFoldersText([]), "");
  // A removed file or a file of «Входящие» does not fill a folder.
  assert.deepEqual(foldEmptyFolders(subject.folders, [file({ removed: true }), file({ id: "m2", inbox: true })]).shown, []);
  // Unknown files (loading, failed): nothing is called empty.
  assert.deepEqual(foldEmptyFolders(subject.folders, null).empty, []);
  assert.equal(foldEmptyFolders(subject.folders, null).shown.length, 4);
});

test("the folded line keeps a «+» per folder as a real button, named by the folder, not hover-only", () => {
  const out = html(h(SubjectDetail, { ...detailProps, materials: [file()], onAddFile: noop }));
  assert.equal((out.match(/class="sec-head"/g) ?? []).length, 1, "one section header: «Лекции»");
  assert.match(out, /Пусто:<\/span> <span class="empty-folder-name" id="folder-f2">Семинары<button[^>]*aria-label="Добавить файл в папку «Семинары»"[^>]*>[^]*?<\/button>,<\/span> <span/);
  for (const name of ["Семинары", "Лабораторные", "Билеты"]) {
    const button = out.match(new RegExp(`<button[^>]*aria-label="Добавить файл в папку «${name}»"[^>]*>`))?.[0] ?? "";
    assert.match(button, /type="button"/, name);
    assert.doesNotMatch(button, /tabindex|hidden/i, `${name}: reachable by Tab`);
  }
  assert.doesNotMatch(out, /В этой папке пока нет файлов|· 0 файлов/);
  // The last name has no comma.
  assert.match(out, /Билеты<button[^>]*>[^]*?<\/button><\/span>/);
  // Without the handler there are names but no buttons.
  const plain = html(h(SubjectDetail, { ...detailProps, materials: [file()] }));
  assert.match(plain, /Пусто:/);
  assert.doesNotMatch(plain, /Добавить файл/);
  // A busy page disables them.
  assert.match(html(h(SubjectDetail, { ...detailProps, materials: [file()], onAddFile: noop, busy: true })), /<button[^>]*aria-label="Добавить файл в папку «Семинары»"[^>]*disabled/);
});

test("when every folder is empty the line alone is shown; the result of a «+» sits under it", () => {
  const extra = (id) => (id === "f3" ? h("p", { className: "marker" }, "итог") : null);
  const out = html(h(SubjectDetail, { ...detailProps, materials: [], onAddFile: noop, folderExtra: extra }));
  assert.equal((out.match(/class="sec-head"/g) ?? []).length, 0, "no folder section");
  assert.equal((out.match(/class="empty-folders-line|class="small muted empty-folders-line/g) ?? []).length, 1);
  assert.match(out, /Пусто:<\/span>/);
  assert.equal((out.match(/aria-label="Добавить файл в папку/g) ?? []).length, 4);
  assert.ok(out.indexOf('class="marker"') > out.indexOf("</div>"), "after the line");
  assert.equal((out.match(/class="marker"/g) ?? []).length, 1);
  // Each folder still has its anchor for the tree's click.
  for (const id of ["f1", "f2", "f3", "f6"]) {
    assert.ok(out.includes(`id="folder-${id}"`), id);
  }
});

test("a folder name stays a text node in the line", () => {
  const evil = "<img src=x onerror=alert(1)>";
  const s = { ...subject, folders: [folder("f9", "", evil)] };
  const out = html(h(SubjectDetail, { ...detailProps, subject: s, materials: [], onAddFile: noop }));
  assert.ok(!out.includes("<img"));
  assert.ok(out.includes("&lt;img src=x onerror=alert(1)&gt;"));
});

const overview = {
  subjectId: "s1", name: "Матанализ", openedAt: "", schedule: [], lessons: [], newMaterials: [],
  deadlines: [
    { assignmentId: "a1", title: "Задачи 3, 5", due: "2026-10-27T12:00:00Z", daysLeft: 6 },
    { assignmentId: "a2", title: "Эссе", due: "2026-10-29T12:00:00Z", daysLeft: 8 },
  ],
};

test("«Ближайшее» rows are whole-row buttons that open their assignment", () => {
  const out = html(h(OverviewSections, { overview, onOpenAssignment: noop }));
  assert.equal((out.match(/<button type="button" class="lrow drow drow-open"/g) ?? []).length, 2);
  assert.equal((out.match(/role="listitem"/g) ?? []).length, 2, "the list items stay");
  assert.ok(out.includes("Задачи 3, 5") && out.includes("Эссе"));
  assert.match(out, new RegExp(`title="${texts.openAssignment}"`));
  // Without the handler the rows only show.
  const plain = html(h(OverviewSections, { overview }));
  assert.doesNotMatch(plain, /<button/);

  const page = src("../src/pages/SubjectsPage.tsx");
  assert.match(page, /onOpenAssignment\?: \(assignmentId: string\) => void;/);
  assert.match(page, /<OverviewSections overview=\{overview\} onOpenAssignment=\{props\.onOpenAssignment\} \/>/);
  const app = src("../src/App.tsx");
  assert.match(app, /onSelect=\{props\.onSelectSubject\}\s+onOpenAssignment=\{props\.onOpenAssignment\}/);

  const css = src("../src/styles.css");
  assert.match(css, /\.drow-open \{ width: 100%; border: 0; background: none;[^}]*cursor: pointer; \}/);
  assert.match(css, /\.drow-item \+ \.drow-item > \.lrow::before/);
  assert.doesNotMatch(css, /\.drow-open:focus[^{]*\{[^}]*outline:\s*(none|0)/, "the global focus ring stays");
});

// ---- «База знаний» ----

test("the export paragraph of a subject is the tooltip of the export button", () => {
  const page = src("../src/pages/KnowledgeBasePage.tsx");
  assert.match(page, /const exportHint = "Один файл \.md: пары по датам, с конспектом и текстом лекции\. Сохраняется только по кнопке\.";/);
  assert.match(page, /title=\{exportHint\}\s+aria-describedby=\{exportHintId\}/);
  assert.match(page, /<span id=\{exportHintId\} className="vh">\s*\{exportHint\}/);
  assert.doesNotMatch(page, /<p className="small muted kb-sub">\s*Экспорт — один файл/, "the paragraph is gone");
  assert.equal(page.split("Сохраняется только по кнопке").length - 1, 1, "the text only once");
});

// ---- Dialog ----

test("Dialog puts the action left and «Отмена» right, Tab goes action then cancel", () => {
  const out = html(h(Dialog, { open: false, title: "Удалить?", confirmLabel: "Удалить", cancelLabel: "Отмена", danger: true, onConfirm: noop, onCancel: noop }, h("p", null, "Текст")));
  assert.ok(out.indexOf(">Удалить</button>") > 0 && out.indexOf(">Удалить</button>") < out.indexOf(">Отмена</button>"));
  assert.match(out, /<div class="acts"><button type="button" class="btn danger">Удалить<\/button><button type="button" class="btn">Отмена<\/button><\/div>/);
  const ok = html(h(Dialog, { open: false, title: "Т", confirmLabel: "Сохранить", cancelLabel: "Отмена", onConfirm: noop, onCancel: noop }, h("p", null, "Текст")));
  assert.match(ok, /<button type="button" class="btn primary">Сохранить<\/button><button type="button" class="btn">Отмена<\/button>/);
  // The buttons are right-aligned (.acts: justify-content: end), so the pair reads «Действие Отмена» at the right edge.
  assert.match(src("../src/ui/components.css"), /\.acts \{[^}]*justify-content: end;/);
});

test("Dialog's focus starts on the safe, last button; the danger button is not the default", () => {
  const code = src("../src/ui/controls.tsx");
  const dialog = code.slice(code.indexOf("export function Dialog"));
  assert.match(dialog, /d\.showModal\(\);\s*safe\.current\?\.focus\(\);/);
  assert.match(dialog, /<button ref=\{safe\} type="button" className="btn" disabled=\{props\.cancelDisabled\} onClick=\{props\.onCancel\}>/);
  assert.ok(dialog.indexOf("onClick={props.onConfirm}") < dialog.indexOf("ref={safe}"), "the ref is on the second button");
  // No autofocus or submit button that Enter could press on the danger one.
  assert.doesNotMatch(dialog, /autoFocus|type="submit"/);
});
