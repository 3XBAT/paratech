// TASK-347: «Записи», «Предметы» and the dialogs with less noise and the Windows order of buttons.
// Pure view logic on the compiled modules; the pages have no DOM harness, so their wiring is checked in the source text.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { foldEmptyFolders, inFolderText, texts } from "../.test-build/libraryView.js";
import { initialListState, stateOf } from "../.test-build/listView.js";
import { OverviewSections, SubjectDetail } from "../.test-build/pages/SubjectsParts.js";
import {
  backgroundCount,
  backgroundFailedText,
  backgroundKey,
  backgroundShowFailed,
  backgroundState,
  backgroundTitle,
  backgroundVisible,
  emptyTexts,
  headActionsVisible,
  pageGroups,
  tasksKey,
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
  assert.match(page, /\{headActions && \(\s*<div className="head-acts">\s*\{importButton\}\s*\{startControl\}/);
  // The empty state: the text, [Начать запись] [Загрузить расписание] [Импортировать запись], the muted privacy line.
  assert.match(
    page,
    /<div ref=\{emptyRef\} className="rec-empty">\s*<p className="empty-text">\{emptyTexts\.text\}<\/p>\s*<div className="acts start">\s*\{startControl\}[^]*?onOpen\("schedule"\)[^]*?\{emptyTexts\.schedule\}[^]*?\{importButton\}/,
  );
  assert.match(page, /<p className="small muted rec-empty-privacy">\s*<Icon name="lock" \/>\s*\{emptyTexts\.privacy\}/);
  // One accent: the start button is always primary; an open panel replaces the empty state instead of sitting above it.
  // TASK-348: while the start panel is open the head button is the default variant; the panel's own button is the accent.
  assert.match(page, /const startControl = \(\s*<div className="rec-start">\s*<Button\s+(\/\/[^\n]*\n\s*)?variant=\{panelOpen \? undefined : "primary"\}/);
  assert.doesNotMatch(page, /secondary/);
  assert.match(page, /\) : noRecordings \? \(\s*\/\/[^\n]*\n\s*!panelOpen && \(/);
  assert.match(page, /const panelOpen = startOpen \|\| importOpen;/);
  // The privacy line is gone from the status bar.
  assert.doesNotMatch(page, /<span className="r">/);
  assert.doesNotMatch(page, /Звук обрабатывается только на этом компьютере/);
  assert.match(page, /\{queue && \(\s*<>\s*<div className="gap" \/>\s*<div className="statusbar">/, "the bar is drawn only for the queue line");
});

test("the empty state: a disabled start says why in a visible line; the focus follows the panel and comes back", () => {
  const page = src("../src/pages/RecordingsPage.tsx");
  // The reason is a visible muted line under the buttons (the head keeps the hidden one for screen readers).
  assert.match(page, /\{startState\.reason && \(\s*<p id=\{reasonId\} className="small muted rec-empty-reason">\s*\{startState\.reason\}/);
  // With an empty list and an open panel the visible line is gone, so the hidden span must exist for aria-describedby.
  assert.match(page, /\{startState\.reason && \(!noRecordings \|\| panelOpen\) && \(\s*<span id=\{reasonId\} className="vh">/);
  // The clicked button leaves with the empty state: the focus goes into the panels, and back to the first button.
  assert.match(page, /noRecordings && panelOpen !== panelWasOpen\.current/);
  assert.match(page, /const target = panelOpen \? panelsRef\.current : emptyRef\.current;/);
  assert.match(page, /<div ref=\{panelsRef\} className="rec-panels">/);
  assert.match(src("../src/styles.css"), /\.rec-panels \{ display: contents; \}/);
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
  assert.match(page, /\{backgroundTitle\}\s*\{backgroundN > 0 && <span className="clist-count"> \(\{backgroundN\}\)<\/span>\}/);
  assert.match(page, /withState\(l, backgroundKey, \{ \.\.\.background, collapsed: !background\.collapsed \}\)/);
  assert.match(page, /\{!background\.collapsed && sectionGroups\("tasks"/);
  assert.doesNotMatch(page, /<h2 className="group-title">/);
});

// The jobs as the page gets them from the core; pageGroups splits them the way the page does.
const coreJob = (id, status, over = {}) => ({
  id, type: "send_email", status, waiting: false, progressPercent: 0, runNowOffered: false, assignmentId: "",
  createdAt: "2026-10-01T10:00:00Z", updatedAt: "2026-10-01T10:00:00Z", ...over,
});
const folded = { collapsed: true, showAll: false };
const unfolded = { collapsed: false, showAll: false };

test("the number in «Фоновая обработка (N)» counts the unfinished and the failed jobs only; none, no number", () => {
  const count = (...jobs) => backgroundCount(pageGroups([], jobs).tasks);
  assert.equal(count(coreJob("a", "COMPLETED"), coreJob("b", "COMPLETED")), 0, "finished ones have their own «Завершённые (N)»");
  assert.equal(count(coreJob("a", "QUEUED"), coreJob("b", "RUNNING"), coreJob("c", "COMPLETED")), 2);
  assert.equal(count(coreJob("a", "QUEUED"), coreJob("b", "FAILED"), coreJob("c", "COMPLETED")), 2, "a failed one is counted");
  assert.equal(count(coreJob("a", "FAILED", { runNowOffered: true })), 1, "a failed job in progress is counted once");
  // The block is drawn for any job (the finished ones sit in its «Завершённые»), not for none.
  assert.equal(backgroundVisible([]), false);
  assert.equal(backgroundVisible([coreJob("a", "COMPLETED")]), true);
});

test("a failed job is not hidden by the fold: «С ошибкой: N — показать» unfolds the block and «Завершённые»", () => {
  const tasks = pageGroups([], [coreJob("a", "COMPLETED"), coreJob("b", "FAILED"), coreJob("c", "QUEUED"), coreJob("d", "FAILED")]).tasks;
  assert.deepEqual(tasks.finished.map((j) => j.id).sort(), ["a", "b", "d"], "failed jobs are classed as finished");
  const text = (bg, inner) => backgroundFailedText(tasks, bg, inner);
  assert.equal(text(folded, folded), "С ошибкой: 2 — показать");
  assert.equal(text(unfolded, folded), "С ошибкой: 2 — показать", "the block is open but its «Завершённые» hides them");
  assert.equal(text(folded, unfolded), "С ошибкой: 2 — показать", "the inner list is open but the block is folded");
  assert.equal(text(unfolded, unfolded), "", "visible: the line goes");
  assert.doesNotMatch(text(folded, folded), /задач/i, "the glossary has no «задачи»");

  // No failed job, nothing to show; a failed job in progress sits in the open block itself.
  const calm = pageGroups([], [coreJob("a", "QUEUED"), coreJob("b", "COMPLETED")]).tasks;
  assert.equal(backgroundFailedText(calm, folded, folded), "");
  const active = pageGroups([], [coreJob("a", "FAILED", { runNowOffered: true })]).tasks;
  assert.equal(backgroundFailedText(active, folded, folded), "С ошибкой: 1 — показать");
  assert.equal(backgroundFailedText(active, unfolded, folded), "", "in progress: the open block is enough");

  // The click: the block and the inner list unfold, with every row; other lists keep their state.
  const next = backgroundShowFailed({ recordings: folded });
  assert.deepEqual(backgroundState(next), unfolded);
  assert.deepEqual(stateOf(next, tasksKey), { collapsed: false, showAll: true });
  assert.deepEqual(stateOf(next, "recordings"), folded);
  assert.equal(tasksKey, "tasks", "the key of sectionGroups(\"tasks\", …)");
  assert.equal(text(next[backgroundKey], next[tasksKey]), "", "after the click the line is gone");

  const page = src("../src/pages/RecordingsPage.tsx");
  assert.match(page, /const failed = backgroundFailedText\(groups\.tasks, background, stateOf\(lists, tasksKey\)\);/);
  assert.match(page, /\{failed !== "" && \(\s*<div className="background-failed">\s*<Button size="sm" onClick=\{\(\) => setLists\(backgroundShowFailed\)\}>\s*\{failed\}/);
  assert.doesNotMatch(page, /<p className="small row-problem background-failed">/, "no longer a plain paragraph");
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
  // A removed file or a file of «Входящие» does not fill a folder.
  assert.deepEqual(foldEmptyFolders(subject.folders, [file({ removed: true }), file({ id: "m2", inbox: true })]).shown, []);
  // Unknown files (loading, failed): nothing is called empty.
  assert.deepEqual(foldEmptyFolders(subject.folders, null).empty, []);
  assert.equal(foldEmptyFolders(subject.folders, null).shown.length, 4);
});

test("a folder just made (or marked «новое») stays a section while empty; the other empty ones fold", () => {
  const split = foldEmptyFolders(subject.folders, [file()], new Set(["f3"]));
  assert.deepEqual(split.shown.map((f) => f.id), ["f1", "f3"], "the kept one keeps its place in the order");
  assert.deepEqual(split.empty.map((f) => f.id), ["f2", "f6"]);
  // Without the set, or with an unknown id, nothing changes.
  assert.deepEqual(foldEmptyFolders(subject.folders, [file()], new Set()).empty.map((f) => f.id), ["f2", "f3", "f6"]);
  assert.deepEqual(foldEmptyFolders(subject.folders, [file()], new Set(["nope"])).shown.map((f) => f.id), ["f1"]);
  // A kept folder that gets a file is an ordinary section, listed once.
  assert.deepEqual(foldEmptyFolders(subject.folders, [file(), file({ id: "m3", folderId: "f3" })], new Set(["f3"])).shown.map((f) => f.id), ["f1", "f3"]);

  // On the page: «Пока пусто» and the «+» under the folder's own header, the anchor of the tree's click, no line for it.
  const out = html(h(SubjectDetail, { ...detailProps, materials: [file()], onAddFile: noop, created: new Set(["f3"]) }));
  assert.equal((out.match(/class="sec-head"/g) ?? []).length, 2, "«Лекции» and the new «Лабораторные»");
  assert.match(out, /<section class="section" id="folder-f3">[^]*?Лабораторные[^]*?aria-label="Добавить файл в папку «Лабораторные»"[^]*?<p class="small muted folder-empty">Пока пусто<\/p>/);
  assert.equal(texts.folderEmpty, "Пока пусто");
  assert.match(out, /Пусто:<\/span><span class="empty-folder-name" id="folder-f2">Семинары<button[^>]*>[^]*?<\/button><\/span><span class="empty-folder-name" id="folder-f6">Билеты/);
  assert.equal((out.match(/id="folder-f3"/g) ?? []).length, 1, "the anchor exists once");
  assert.doesNotMatch(out, /Лабораторные<button/, "not in the line");
  // «новое» marks keep a folder too.
  const marked = html(h(SubjectDetail, { ...detailProps, materials: [file()], fresh: { folders: new Set(["f2"]), materials: new Set() } }));
  assert.match(marked, /<section class="section" id="folder-f2">/);

  // The page keeps the ids of folders made on this visit, forgets them with the subject and scrolls to the new one.
  const page = src("../src/pages/SubjectsPage.tsx");
  assert.match(page, /setCreatedFolders\(\(created\) => new Set\(created\)\.add\(f\.id\)\);/);
  assert.match(page, /setCreatedFolders\(noFolders\);\s*flow\.reset\(\);/);
  assert.match(page, /scrollToFolder\(document, scrollTo\.current\);/);
  assert.match(page, /created=\{createdFolders\}/);
});

test("the results of a folder in the «Пусто» line name it; a folder with a header needs no name", () => {
  assert.equal(inFolderText("Семинары", "Файл добавлен."), "«Семинары»: Файл добавлен.");
  assert.equal(inFolderText("Семинары", ""), "", "nothing to say, nothing to name");
  const seen = [];
  const extra = (id, named) => {
    seen.push([id, named]);
    return null;
  };
  html(h(SubjectDetail, { ...detailProps, materials: [file()], onAddFile: noop, folderExtra: extra }));
  assert.deepEqual(seen, [["f1", undefined], ["f2", "Семинары"], ["f3", "Лабораторные"], ["f6", "Билеты"]]);
  const page = src("../src/pages/SubjectsPage.tsx");
  assert.match(page, /folderExtra=\{\(folderId, named\) => addLines\(folderId, named\)\}/);
  assert.match(page, /note=\{added\.origin === origin \? namedIn\(folder, added\.note\) : ""\}/);
  assert.match(page, /namedIn\(folder, addError\.text\)/);
});

test("the folded line keeps a «+» per folder as a real button, named by the folder, not hover-only", () => {
  const out = html(h(SubjectDetail, { ...detailProps, materials: [file()], onAddFile: noop }));
  assert.equal((out.match(/class="sec-head"/g) ?? []).length, 1, "one section header: «Лекции»");
  // The names are set apart by the gap of the line: no comma ends up after a «+» button.
  assert.match(out, /<span class="empty-folders-label">Пусто:<\/span><span class="empty-folder-name" id="folder-f2">Семинары<button[^>]*aria-label="Добавить файл в папку «Семинары»"[^>]*>[^]*?<\/button><\/span><span/);
  assert.doesNotMatch(out, /<\/button>,|,<button/);
  for (const name of ["Семинары", "Лабораторные", "Билеты"]) {
    const button = out.match(new RegExp(`<button[^>]*aria-label="Добавить файл в папку «${name}»"[^>]*>`))?.[0] ?? "";
    assert.match(button, /type="button"/, name);
    assert.doesNotMatch(button, /tabindex|hidden/i, `${name}: reachable by Tab`);
  }
  assert.doesNotMatch(out, /В этой папке пока нет файлов|· 0 файлов/);
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
  // A button holds phrasing content only: the body of the row is spans (blocks again by CSS), no div or p inside.
  for (const button of out.match(/<button[^>]*>[^]*?<\/button>/g) ?? []) {
    assert.doesNotMatch(button, /<(div|p|h[1-6]|ul|li)[\s>]/, button);
    assert.match(button, /<span class="d num">[^]*?<span class="tx"><span class="t">/);
  }
  // TASK-352: the rows of «Пары» share the rule, their meta line is a span too.
  assert.match(src("../src/styles.css"), /\.drow \.tx, \.drow \.t, \.drow \.meta \{ display: block; \}/);
  assert.match(out, new RegExp(`title="${texts.openAssignment}"`));
  // Without the handler the rows only show.
  const plain = html(h(OverviewSections, { overview }));
  assert.doesNotMatch(plain, /<button/);

  const page = src("../src/pages/SubjectsPage.tsx");
  assert.match(page, /onOpenAssignment\?: \(assignmentId: string\) => void;/);
  assert.match(page, /<OverviewSections overview=\{overview\} onOpenAssignment=\{props\.onOpenAssignment\} onOpenLesson=\{props\.onOpenLesson\} \/>/);
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
  assert.match(page, /const exportHint = "Сохранить все пары предмета в один файл \.md — по датам, с конспектом и текстом лекции\.";/);
  // Off, the same place says why (title for the pointer, aria-describedby for the screen reader).
  assert.match(page, /const exportEmptyHint = "Экспортировать нечего: у предмета пока нет пар с конспектом\.";/);
  assert.match(page, /const nothingToExport = lessons !== null && lessons\.every\(isNoteOnly\);/);
  assert.match(page, /const exportTip = nothingToExport \? exportEmptyHint : exportHint;/);
  assert.match(page, /disabled=\{exporting \|\| lessons === null \|\| nothingToExport\}\s+title=\{exportTip\}\s+aria-describedby=\{exportHintId\}/);
  assert.match(page, /<span id=\{exportHintId\} className="vh">\s*\{exportTip\}/);
  assert.doesNotMatch(page, /<p className="small muted kb-sub">\s*Экспорт — один файл/, "the paragraph is gone");
  assert.equal(page.split("Сохраняется только по кнопке").length - 1, 0, "the old text is gone");
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
  assert.match(dialog, /d\.showModal\(\);[^]*?const button = safe\.current;\s*\(button !== null && !button\.disabled \? button : d\)\.focus\(\);/);
  // A disabled «Отмена» cannot take the focus: it goes to the dialog (tabindex -1), not to the action showModal() chose.
  assert.match(dialog, /className="dialog"\s+tabIndex=\{-1\}/);
  assert.match(src("../src/styles.css"), /dialog\.dialog:focus \{ outline: none; \}/);
  assert.match(dialog, /<button ref=\{safe\} type="button" className="btn" disabled=\{props\.cancelDisabled\} onClick=\{props\.onCancel\}>/);
  assert.ok(dialog.indexOf("onClick={props.onConfirm}") < dialog.indexOf("ref={safe}"), "the ref is on the second button");
  // No autofocus or submit button that Enter could press on the danger one.
  assert.doesNotMatch(dialog, /autoFocus|type="submit"/);
});
