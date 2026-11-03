// TASK-368 (part 2): the small and secondary buttons of the screens became icon buttons (ui/IconAction): headers, rows,
// toolbars, cards. Their old words are the tooltip (hover and keyboard focus) and the aria-label, and no word is visible.
// Words stay on the main action of a screen or card and on dialog and form buttons; the list at the end is that decision.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import { createElement as h } from "react";
import server from "react-dom/server";
import { IconAction } from "../.test-build/ui/IconAction.js";
import { IconSprite } from "../.test-build/ui/Icon.js";
import { ErrorLine, UndoLines } from "../.test-build/pages/FileRows.js";
import { DropReport } from "../.test-build/pages/DropZone.js";
import { DoneIssueLine } from "../.test-build/pages/AssignmentDoneParts.js";
import { LoadError as TodayLoadError, StopProblem } from "../.test-build/pages/TodayParts.js";
import { JournalList, LoadError as JournalLoadError } from "../.test-build/pages/JournalParts.js";
import { HomeworkCard, SummaryCard, SummaryTools } from "../.test-build/pages/SummaryParts.js";
import { LectureTextCard } from "../.test-build/pages/LectureTextParts.js";
import { LessonList } from "../.test-build/pages/KnowledgeParts.js";
import { PageViewer } from "../.test-build/pages/PageViewer.js";
import { TeachersBlock } from "../.test-build/pages/TeachersBlock.js";
import { SetCard } from "../.test-build/pages/TicketsParts.js";
import { DoneRow, ScheduleCheckFailedLine } from "../.test-build/pages/ScheduleSubjectsReview.js";
import { parseSet } from "../.test-build/bindings/tickets.js";
import { idleTeacher } from "../.test-build/teachersView.js";
import { openPairLabel } from "../.test-build/todayView.js";
import { openTaskLabel } from "../.test-build/summaryView.js";
import { removeModelLabel, cancelDownloadLabel } from "../.test-build/modelsView.js";
import { openVersionLabel } from "../.test-build/homeworkView.js";
import { selectedDeleteLabel } from "../.test-build/recordingDeleteView.js";
import { setSubjectLabel } from "../.test-build/knowledgeView.js";
import { ariaLabels, buttonTag, isOff, isOn, visibleText } from "./iconbtn.mjs";

const { renderToStaticMarkup } = server;
const noop = () => undefined;
const html = (el) => renderToStaticMarkup(el);
const src = (path) => readFileSync(new URL(`../src/${path}`, import.meta.url), "utf8");
const count = (text, part) => text.split(part).length - 1;

/** The button named `label` is an icon button: class ia, the glyph `icon`, no visible words at all. */
function assertIcon(out, label, icon) {
  const tag = buttonTag(out, label);
  assert.ok(tag !== null, `a button named «${label}»`);
  assert.match(tag, /class="ia[ "]/, `${label}: an icon button`);
  assert.ok(out.includes(`<use href="#i-${icon}"></use></svg><span class="ia-tip" aria-hidden="true">`), `${label}: the glyph ${icon}`);
  assert.equal(visibleText(out).includes(label), false, `${label}: the words are not visible`);
}

// ---- the icon set and the component ----

test("the new icons (trash, chevup, copysel) are in the sprite once each; copysel has solid strokes and a text cursor", () => {
  const svg = html(h(IconSprite));
  for (const name of ["trash", "chevup", "copysel"]) {
    assert.equal(count(svg, `id="i-${name}"`), 1, name);
  }
  const copysel = svg.slice(svg.indexOf('id="i-copysel"'), svg.indexOf("</symbol>", svg.indexOf('id="i-copysel"')));
  assert.doesNotMatch(copysel, /dasharray/i, "dashes vanish at 16px");
  assert.match(copysel, /M12\.5 12h4M14\.5 12v5M12\.5 17h4/, "an I-beam on the front sheet");
});

test("IconAction passes data-* attributes (the pages find buttons by them) and onMouseDown (the selection stays)", () => {
  const out = html(h(IconAction, { icon: "chevup", label: "Выше", data: { move: "up", mid: "m1", "page-paste": "" }, onClick: noop, onMouseDown: noop }));
  assert.match(out, /data-move="up"/);
  assert.match(out, /data-mid="m1"/);
  assert.match(out, /data-page-paste=""/);
  assert.doesNotMatch(html(h(IconAction, { icon: "x", label: "Закрыть", onClick: noop })), /data-(?!place)/);
  const code = src("ui/IconAction.tsx");
  assert.match(code, /props\.onMouseDown\?\.\(e\)/, "the handler runs unless the button is aria-disabled");
  assert.match(code, /onMouseDown=\{down\}/);
});

// ---- the library: files ----

test("files: the only way out of a refusal keeps its words («Повторить» / «Обновить список»), the undo of a placement is an undo icon", () => {
  const retry = html(h(ErrorLine, { text: "Файл больше 100 МБ.", onRefresh: noop, onRetry: noop }));
  assert.deepEqual(ariaLabels(retry), []);
  assert.match(retry, /class="btn sm"[^>]*>Повторить</);
  const refresh = html(h(ErrorLine, { text: "Папка не найдена: обновите список.", onRefresh: noop, onRetry: noop }));
  assert.deepEqual(ariaLabels(refresh), []);
  assert.match(refresh, /class="btn sm"[^>]*>Обновить список</);
  const item = { id: 1, materialId: "m1", text: "Положено в «Лекции».", back: { kind: "inbox" }, error: "", expires: 0 };
  const undo = html(h(UndoLines, { items: [item], busy: false, onUndo: noop, onRefresh: noop }));
  assertIcon(undo, "Вернуть", "undo");
  assert.ok(isOff(html(h(UndoLines, { items: [item], busy: true, onUndo: noop, onRefresh: noop })), "Вернуть"));
  const stale = html(h(UndoLines, { items: [{ ...item, error: "Папка не найдена: обновите список." }], busy: false, onUndo: noop, onRefresh: noop }));
  assert.deepEqual(ariaLabels(stale), [], "a refused «Вернуть» offers the word «Обновить список», as ErrorLine does");
  assert.match(stale, /class="btn sm"[^>]*>Обновить список</);
});

test("the report of a drop: «Закрыть» is a cross", () => {
  const out = html(h(DropReport, { lines: [{ ok: true, text: "Файл добавлен." }], onClose: noop }));
  assertIcon(out, "Закрыть", "x");
  assert.equal(html(h(DropReport, { lines: [], onClose: noop })), "");
});

// ---- homework and Today ----

test("a refusal of «Выполнено»: retry and open are icons, «Закрыть» a cross, «К заданиям» (the only way out of a gone task) a word", () => {
  const issue = (way, over = {}) => ({ text: "Не получилось.", pageText: "Не получилось.", way, openId: "a1", gone: false, ...over });
  const retry = html(h(DoneIssueLine, { issue: issue("retry"), where: "list", onRetry: noop, onOpen: noop, onClose: noop }));
  assert.deepEqual(ariaLabels(retry), ["Повторить", "Закрыть"]);
  assertIcon(retry, "Повторить", "retry");
  assertIcon(retry, "Закрыть", "x");
  const open = html(h(DoneIssueLine, { issue: issue("open"), where: "list", onRetry: noop, onOpen: noop }));
  assert.deepEqual(ariaLabels(open), ["Открыть задание"]);
  const gone = html(h(DoneIssueLine, { issue: issue("none", { gone: true }), where: "page", onRetry: noop, onOpen: noop, onBack: noop }));
  assert.deepEqual(ariaLabels(gone), []);
  assert.match(visibleText(gone), /Все задания/);
  assert.match(gone, /<button type="button" class="btn sm">/);
});

test("Today: a failed read and a failed stop keep the words (the only way out); the rows of the day open with a chevron that names the pair", () => {
  const load = html(h(TodayLoadError, { onRetry: noop }));
  assert.deepEqual(ariaLabels(load), []);
  assert.match(load, /class="btn sm"[^>]*>Повторить</);
  const stop = html(h(StopProblem, { text: "Не удалось остановить.", retrying: false, canRetry: true, onRetry: noop, onOpenRecordings: noop }));
  assert.deepEqual(ariaLabels(stop), []);
  assert.match(visibleText(stop), /Повторить .*Перейти к записям/);
  const busy = html(h(StopProblem, { text: "Не удалось остановить.", retrying: true, canRetry: true, onRetry: noop, onOpenRecordings: noop }));
  assert.match(busy, /disabled=""[^>]*>[^]*Остановка…/);
  const parts = src("pages/TodayParts.tsx");
  assert.equal(count(parts, "<IconAction"), 2, "the two «Открыть» of the summary rows");
  assert.equal(count(parts, 'icon="chevr"'), 2, "opening a row of the app is a chevron");
  assert.equal(count(parts, "openPairLabel("), 2, "and its name says which pair");
  assert.equal(openPairLabel("Матанализ"), "Открыть пару: Матанализ");
  assert.equal(openPairLabel("  "), "Открыть");
  assert.equal(count(parts, "tipText={texts.openLesson}"), 2, "the hover tooltip is the short «Открыть пару»");
  assert.equal(openPairLabel("я".repeat(300)), `Открыть пару: ${"я".repeat(120)}…`, "a long title is cut in the name");
});

test("Journal: «Повторить» keeps its word; the row «Отменить» is an icon; the filter reset of an empty list keeps its words", () => {
  const retry = html(h(JournalLoadError, { text: "Не удалось.", onRetry: noop }));
  assert.deepEqual(ariaLabels(retry), []);
  assert.match(retry, /class="btn sm"[^>]*>Повторить</, "every load-error «Повторить» is the default secondary button");
  const line = { kind: "action", at: "2026-10-22T10:00:00Z", updatedAt: "2026-10-22T10:00:00Z", subjectId: "", actionId: "a1", threadId: "t1", turn: 1, tool: "create_subject", jobId: "", jobType: "", status: "DONE", undoable: true, cannotUndo: false, noUndo: "" };
  const out = html(h(JournalList, { lines: [line], subjects: new Map(), busyAction: "", onUndo: noop }));
  assertIcon(out, "Отменить", "undo");
  assert.match(out, /data-place="left"/, "the row action opens its tooltip to the left, away from the card edge");
  const empty = html(h(JournalList, { lines: [], subjects: new Map(), busyAction: "", onUndo: noop, filtered: true, onResetFilters: noop }));
  assert.match(visibleText(empty), /Сбросить/);
  assert.deepEqual(ariaLabels(empty), []);
});

// ---- the lesson: summary, lecture text, transcript ----

const step = (state, over = {}) => ({ state, waitReason: "", message: `msg:${state}`, jobError: "", ...over });
const lectureStep = (over = {}) => ({ ...step("done"), ready: true, blocks: [{ heading: "Пределы", startSeconds: 725, time: "12:05", paragraphs: ["Абзац."] }], updatedAt: "2026-10-19T07:10:00Z", partsDone: 0, parts: 0, canStart: false, retryWork: "", ...over });
const summaryView = (over = {}, lt = {}, summary = {}) => ({
  recordingId: "r1", lessonId: "l1", title: "Матанализ", startedAt: "2026-10-19T06:05:00Z", durationSeconds: 2700, hasDuration: true, transcribed: true, noSpeech: false,
  summary: { ...step("done"), ready: true, text: "Конспект.", sections: [{ heading: "", text: "Конспект.", points: [] }], keyPoints: [], updatedAt: "2026-10-19T07:00:00Z", ...summary },
  homework: { ...step("done"), assignments: [] },
  lectureText: lectureStep(lt),
  ...over,
});

test("the summary: «Скопировать» is a copy icon; «Сделать конспект заново» (it spends quota) and «Повторить» keep words; «Открыть задание» is a chevron naming the task", () => {
  const tools = html(h(SummaryTools, { ready: true, idle: true, busy: "", message: null, onCopy: noop, onSave: noop }));
  assertIcon(tools, "Скопировать", "copy");
  const redo = html(h(SummaryCard, { view: summaryView(), onRetry: noop, retrying: "" }));
  assert.deepEqual(ariaLabels(redo), []);
  assert.match(visibleText(redo), /Сделать конспект заново/);
  const failed = html(h(SummaryCard, { view: summaryView({}, {}, { ...step("failed"), ready: false }), onRetry: noop, retrying: "" }));
  assert.deepEqual(ariaLabels(failed), []);
  assert.match(visibleText(failed), /Повторить/);
  const running = html(h(SummaryCard, { view: summaryView(), onRetry: noop, retrying: "summary" }));
  assert.match(running, /disabled=""[^>]*>[^]*Запуск…/);
  assert.equal(html(h(SummaryCard, { view: summaryView() })).includes("<button"), false, "no handler, no button");
  const task = { id: "a1", title: "Задача", description: "", due: "2026-10-25T20:59:00Z", userEdited: false, subject: "Матанализ", subjectSource: "schedule", kind: "lecture", teacher: "", teacherSource: "", lessonDate: "2026-10-19T06:00:00Z" };
  const withTask = summaryView({ homework: { ...step("done"), assignments: [task] } });
  const opened = html(h(HomeworkCard, { view: withTask, onOpenAssignment: noop }));
  assertIcon(opened, "Открыть задание: Задача", "chevr");
  assert.equal(openTaskLabel("Задача"), "Открыть задание: Задача");
  assert.equal(openTaskLabel(""), "Открыть задание");
  assert.match(opened, /<span class="ia-tip" aria-hidden="true">Открыть задание<\/span>/, "the hover tooltip is short; the title stays in the name");
  assert.equal(openTaskLabel("я".repeat(300)), `Открыть задание: ${"я".repeat(120)}…`);
  assert.doesNotMatch(opened, /#i-open/, "the arrow out of a box is only for what opens outside the app");
});

const lectureCard = (v, over = {}) =>
  html(h(LectureTextCard, { view: v, busy: "", disabled: false, format: "txt", onFormat: noop, onCopy: noop, onSave: noop, onStart: noop, onOpenTranscript: noop, message: null, ...over }));

test("the lecture text: copy and save are icons, the start and the retry keep words with «Исходная расшифровка»", () => {
  const ready = lectureCard(summaryView());
  assert.deepEqual(ariaLabels(ready), ["Скопировать", "Сохранить как…"]);
  assertIcon(ready, "Скопировать", "copy");
  assertIcon(ready, "Сохранить как…", "down");
  assert.match(visibleText(ready), /Исходная расшифровка/);
  const first = lectureCard(summaryView({}, { ...step("none"), ready: false, blocks: [], canStart: true, retryWork: "lectureText" }));
  assert.deepEqual(ariaLabels(first), []);
  assert.match(visibleText(first), /Сделать текст лекции/);
  const failed = lectureCard(summaryView({}, { ...step("failed"), ready: false, blocks: [], canStart: false, retryWork: "lectureText" }));
  assert.deepEqual(ariaLabels(failed), []);
  assert.match(visibleText(failed), /Повторить/);
  assert.ok(isOff(lectureCard(summaryView(), { busy: "copy" }), "Копирование…"));
  assert.ok(isOff(lectureCard(summaryView(), { busy: "save" }), "Сохранение…"));
});

test("the transcript tools: search steps, two different copy glyphs and save are icons; the copy of a selection keeps the selection", () => {
  const code = src("pages/TranscriptPage.tsx");
  assert.equal(count(code, "<Button"), 0, "no word button left on the transcript page");
  assert.equal(count(code, "<IconAction"), 5);
  for (const icon of ['icon="chevup"', 'icon="chev"', 'icon="copy"', 'icon="copysel"', 'icon="down"']) {
    assert.equal(count(code, icon), 1, icon);
  }
  assert.match(code, /label=\{toolTexts\.prev\}\s+tipText=\{toolTexts\.prevTip\}/, "the key stays in the tooltip");
  assert.match(code, /onMouseDown=\{\(e\) => e\.preventDefault\(\)\}\s+onClick=\{copySelection\}/);
  const texts = src("transcriptView.ts");
  for (const word of ["Предыдущее", "Следующее", "Скопировать всё", "Скопировать выделенное", "Сохранить как…"]) {
    assert.ok(texts.includes(`"${word}"`), `${word} lives in transcriptView.ts`);
    assert.equal(code.includes(`>${word}<`), false);
  }
});

// ---- the knowledge base, notes, pages ----

test("«База знаний»: «Указать предмет» of a lesson row is a folder naming the lesson, «Экспорт в .md» a download icon with the hint as its tooltip", () => {
  const lesson = {
    key: "recording:r1", noteOnly: false, undated: false, anchor: "recording", scheduleLessonId: "", date: "", notes: [], recordingId: "r1", startedAt: "2026-10-19T12:00:00Z",
    durationSeconds: 5400, title: "Пределы", subject: "", subjectKey: "", kind: "lecture", subjectEdited: false, kindEdited: false, transcriptReady: true, summaryReady: true,
    lectureTextReady: false, librarySubjectId: "", librarySubjectState: "none",
  };
  const out = html(h(LessonList, { lessons: [lesson], onOpen: noop, onEdit: noop }));
  assert.equal(ariaLabels(out).length, 1);
  assert.match(ariaLabels(out)[0], /^Указать предмет: /, "the name says which lesson");
  assertIcon(out, ariaLabels(out)[0], "folder");
  assert.equal(setSubjectLabel("Пределы"), "Указать предмет: Пределы");
  assert.equal(setSubjectLabel(" "), "Указать предмет");
  const page = src("pages/KnowledgeBasePage.tsx");
  assert.match(page, /<IconAction\s+icon="down"\s+place="below"/);
  assert.match(page, /tipText=\{exportTip\}/);
  assert.doesNotMatch(page, />\s*Экспорт в \.md\s*</, "the words come from knowledgeView.ts");
});

test("own notes: the four row actions have four glyphs of their own (none used elsewhere for another meaning) and keep the attributes the focus code reads; the dark viewer has its colours", () => {
  const code = src("pages/OwnNotesParts.tsx");
  const row = code.slice(code.indexOf('<span className="own-part-acts">'), code.indexOf("{pasting === r.materialId && ("));
  const icons = [...row.matchAll(/icon="([a-z]+)"/g)].map((m) => m[1]);
  assert.deepEqual(icons, ["arrowup", "arrowdown", "swap", "paste"]);
  assert.equal(new Set(icons).size, 4, "a row of four icons is never ambiguous");
  // chev/chevup mean show/hide, retry means repeat, doc means a document: the row uses none of them.
  for (const taken of ["chev", "chevup", "retry", "doc", "up", "down"]) {
    assert.equal(icons.includes(taken), false, taken);
  }
  assert.match(row, /data=\{\{ move: "up", mid: r\.materialId \}\}/);
  assert.match(row, /data=\{\{ move: "down", mid: r\.materialId \}\}/);
  assert.match(row, /data=\{\{ "page-paste": "", mid: r\.materialId \}\}/);
  assert.match(code, /data=\{\{ undo: "" \}\}/);
  assert.match(code, /<IconAction icon="trash" variant="danger"/);
  // The focus code finds them as before.
  assert.match(code, /button\[data-move\]/);
  assert.match(code, /button\[data-undo\]/);
  const css = src("pages/ownNotes.css");
  assert.match(css, /\.page-viewer \.ia \{ color: #fff; \}/);
  assert.match(css, /\.page-viewer \.ia-tip \{ outline:/);
});

test("the page viewer: previous, next and close are icons; close stays the element the viewer focuses and returns from", () => {
  const pages = [1, 2, 3].map((n) => ({ materialId: `m${n}`, src: `/p${n}.png`, number: n, noText: false, alt: `Страница ${n}` }));
  const first = html(h(PageViewer, { pages, start: 0, onClose: noop }));
  assert.equal(count(first, "<button"), 3);
  assert.equal(count(first, 'class="btn'), 0, "no word button");
  assert.equal(count(first, 'class="ia"'), 3);
  // A mirrored pair: left and right chevrons, then the cross.
  assert.deepEqual([...first.matchAll(/<use href="#i-([a-z]+)">/g)].map((m) => m[1]).slice(-3), ["chevl", "chevr", "x"]);
  assert.match(buttonTag(first, ariaLabels(first)[2]), /data-close=""/);
  const prev = ariaLabels(first)[0];
  assert.ok(isOff(first, prev), "the first page has no previous");
  const last = html(h(PageViewer, { pages, start: 2, onClose: noop }));
  assert.ok(isOff(last, ariaLabels(last)[1]), "the last page has no next");
  assert.match(src("pages/PageViewer.tsx"), /button\[data-close\]/);
});

test("homework: opening a version switches it in place: a chevron that names the version, not the arrow out of a box", () => {
  const page = src("pages/HomeworkPage.tsx");
  assert.match(page, /icon="chevr"\s+place="left"\s+label=\{openVersionLabel\(v\.number\)\}\s+tipText=\{versionTexts\.open\}/);
  assert.equal(openVersionLabel(3), "Открыть версию 3");
});

// ---- settings, tickets, schedule ----

test("teachers: the row button is a plus for an empty row and a pencil for a filled one, named «Указать/Изменить: <роль>»; the undo is an icon", () => {
  const on = { open: noop, type: noop, copy: noop, save: noop, cancel: noop, clear: noop, restore: noop };
  const out = html(h(TeachersBlock, { teachers: { lecture: { name: "Иванов", email: "i@example.test" }, practice: null, lab: null }, state: idleTeacher, on }));
  assert.deepEqual(ariaLabels(out), ["Изменить: лектор", "Указать: семинары и практика", "Указать: лабораторные"]);
  assert.equal(count(out, '<use href="#i-pencil">'), 1);
  assert.equal(count(out, '<use href="#i-plus">'), 2);
  assert.match(out, /id="teacher-open-lecture"/, "the focus returns to the button by its id");
  const undo = html(h(TeachersBlock, { teachers: { lecture: null, practice: null, lab: null }, state: { ...idleTeacher, removed: { role: "lecture", name: "Иванов", email: "i@example.test" } }, on }));
  assertIcon(undo, "Вернуть", "undo");
});

function ticketSet(over) {
  return parseSet({
    id: "ts-1", subjectId: "sub-1", sourceId: "", status: "done", tickets: [{ number: 1, title: "Первый", answered: true, coverage: "covered", answerId: "a-1" }], done: 1, total: 1,
    budget: { tickets: 1, estimateMicroUsd: 90000, capMicroUsd: 3000000, overCap: false }, costAccepted: true, report: { covered: 1, partial: 0, notCovered: 0, failed: 0, pending: 0 },
    materialId: "m-1", result: "", job: "", jobStatus: "", stalled: false, version: 1, ...over,
  });
}

test("tickets: undo, stop and show/hide are icons; «Открыть .docx» and the «no» of a proposal keep their words", () => {
  const card = (set, over = {}) =>
    html(h(SetCard, { set, subjectName: "Физика", busy: false, blocked: false, error: "", openBusy: false, openResult: null, onConfirm: noop, onCancel: noop, onUndo: noop, onOpen: noop, ...over }));
  const done = card(ticketSet());
  assertIcon(done, "Вернуть файл во «Входящие»", "undo");
  assert.match(visibleText(done), /Открыть \.docx/);
  assert.match(buttonTag(done, "Скрыть билеты") ?? buttonTag(done, "Показать билеты"), /aria-expanded="(true|false)"/);
  const shown = card(ticketSet({ status: "answering", materialId: "" }));
  assertIcon(shown, "Отменить", "x");
  assert.match(buttonTag(shown, "Отменить"), /ia-danger/);
  const proposed = card(ticketSet({ status: "proposed", materialId: "", done: 0, costAccepted: false }));
  assert.equal(buttonTag(proposed, "Отменить"), null, "the «no» of «Подтвердить» is a word button");
  assert.match(visibleText(proposed), /Отменить/);
  assert.ok(isOff(card(ticketSet({ status: "answering", materialId: "" }), { busy: true }), "Отменяется…"));
});

test("schedule: the outcome of «Создать предметы» has undo (default colour) and close icons; «Развернуть все»/«Свернуть все» and the check retry keep words", () => {
  const out = html(h(DoneRow, { text: "Создано предметов: 2", phase: "idle", onUndo: noop, onClose: noop }));
  assertIcon(out, "Отменить всё", "undo");
  assertIcon(out, "Закрыть", "x");
  assert.doesNotMatch(buttonTag(out, "Отменить всё"), /ia-danger/, "an undo is not red");
  assert.ok(isOff(html(h(DoneRow, { text: "x", phase: "undoing", onUndo: noop })), "Отмена…") || isOff(html(h(DoneRow, { text: "x", phase: "undoing", onUndo: noop })), "Отменяется…"));
  const review = src("pages/ScheduleSubjectsReview.tsx");
  assert.match(review, /rest: null,/, "«Показать остальные» keeps its words");
  const page = src("pages/SchedulePage.tsx");
  assert.match(page, /\{folded \? listTexts\.expandAll : listTexts\.collapseAll\}/);
  assert.doesNotMatch(page, /IconAction/);
  // The check of the schedule failed: «Повторить» is a word (the empty list of subjects is covered by scheduleSubjects312).
  const failed = html(h(ScheduleCheckFailedLine, { onRetry: noop }));
  assert.deepEqual(ariaLabels(failed), []);
  assert.match(failed, /class="btn sm"[^>]*>Повторить</);
  assert.doesNotMatch(review, /variant="ghost" onClick=\{(props|offer)\.onRetry\}/, "the offer retry buttons use the default style too");
});

test("models and the recordings selection: a model row's delete is a trash icon and its cancel a cross, both naming the model; «Удалить выбранные (N)» is a red trash (TASK-371), the clear is a cross", () => {
  const models = src("pages/ModelsPage.tsx");
  assert.equal((models.match(/icon="trash"\s+variant="danger"/g) ?? []).length, 2, "an installed row and a catalog row");
  assert.equal(count(models, "removeModelLabel(fullName(r))"), 2);
  assert.equal(count(models, "cancelDownloadLabel(fullName(r))"), 1);
  assert.match(models, /icon="x"\s+variant="danger"\s+place="left"\s+label=\{cancelDownloadLabel/);
  assert.match(models, /onClick=\{props\.onAskDelete\}/, "the row still asks first");
  assert.equal(removeModelLabel("small · q5"), "Удалить модель small · q5");
  assert.equal(cancelDownloadLabel("small · q5"), "Отменить скачивание: small · q5");
  const recs = src("pages/RecordingsPage.tsx");
  assert.match(recs, /<IconAction icon="trash" variant="danger-solid" label=\{selectedDeleteLabel\(selected\.size\)\} disabled=\{deleting !== null\} onClick=\{\(\) => askDelete\(\[\.\.\.selected\]\)\}/);
  assert.match(recs, /<IconAction icon="x" label=\{deleteTexts\.selectedClear\}/);
  assert.equal(selectedDeleteLabel(3), "Удалить выбранные (3)");
  // The icon only asks: the confirmation dialog that lists the recordings stays, and it is off while one is open.
  assert.match(recs, /askDelete\(\[\.\.\.selected\]\)/);
  assert.match(recs, /disabled=\{deleting !== null\}/);
});

// ---- the source: the word buttons that remain are the decision ----

// The body of every <Button ...>body</Button>: the opening tag is read with its braces and quotes balanced.
function buttonBodies(code) {
  const out = [];
  let from = 0;
  for (;;) {
    const at = code.indexOf("<Button", from);
    if (at < 0) {
      return out;
    }
    if (/[A-Za-z]/.test(code[at + 7] ?? "")) {
      from = at + 7;
      continue;
    }
    let i = at + 7;
    let depth = 0;
    let quote = "";
    for (; i < code.length; i++) {
      const c = code[i];
      if (quote !== "") {
        if (c === quote) quote = "";
        continue;
      }
      if (c === '"' || c === "'" || c === "`") quote = c;
      else if (c === "{") depth++;
      else if (c === "}") depth--;
      else if (c === ">" && depth === 0 && code[i - 1] !== "=") break;
    }
    if (code[i - 1] === "/") {
      from = i;
      continue;
    }
    const end = code.indexOf("</Button>", i);
    out.push(code.slice(i + 1, end).replace(/\s+/g, " ").trim());
    from = end;
  }
}

// Every <Button> that still shows words, by file, with the reason it keeps them:
//  - the main action of its screen, card or line («Записать», «Добавить», «Скачать», «Подключиться»-like: the one thing to do);
//  - a dialog, a panel or a form button («Сохранить», «Создать», «Отмена», «Закрыть» next to a primary, a yes/no pair);
//  - the way out of a refusal that must name its target («Вернуть», «Разложить», «Открыть подключения», «К заданиям»);
//  - an empty-state call to action; a navigation or a disclosure whose words are the content («Исходная расшифровка»,
//    the shown count of failed jobs, a suggestion chip, a link with a date).
// Anything else (open, close, copy, save-as, undo, delete, cancel in a row, edit, show/hide, and a retry that sits inside a
// populated block) is an icon. A retry, refresh or reload that is the ONLY way out of an error state keeps its words
// (the LoadError lines, the cards that could not be read, «Сделать конспект заново»), and so do the bulk «Развернуть все»,
// «Удалить выбранные» and the commit buttons of inline forms.
const wordButtons = {
  "pages/AppleCalendarHelpPage.tsx": ["{helpTexts.backToConnections}", "{l.caption}"],
  "pages/AssignmentDoneParts.tsx": ["{pageDoneLabel(props.done)}", "{button.label}", "Открыть"],
  "pages/CalendarSync.tsx": ["{calendarTexts.sync}"],
  // TASK-371: the call-link card keeps «Подключиться», «Добавить ссылку», the field's «Сохранить»/«Отмена» and the two «Открыть «Предметы»»
  // (the only way out of a refusal that the subject's page fixes); «Изменить», «Удалить», «Вернуть», «Повторить» are icons.
  "pages/CallLinkParts.tsx": [
    "{reviving ? callLinkTexts.revive : saveLabel(card)}", "{callLinkTexts.cancel}", "{callLinkTexts.openSubjects}",
    "{callLinkTexts.join}", "{callLinkTexts.add}", "{callLinkTexts.openSubjects}", "{callLinkTexts.add}", "{callLinkTexts.join}",
  ],
  "pages/ConnectionsPage.tsx": ["Сохранить адрес", "Сохранить", "Проверить почту"],
  "pages/DiagnosticsPage.tsx": ["{check.label}", "{save.label}"],
  "pages/FileRows.tsx": ["{fileTexts.refresh}", "{fileTexts.retry}", "{addFileTexts.moveHere}", "{addFileTexts.place}", "{fileTexts.createSubject}", "{fileTexts.cancel}", "{fileTexts.putHere}", "{fileTexts.cancel}", "{line.action}", "{primary.label}", "{fileTexts.refresh}"],
  "pages/GoogleBlock.tsx": ["{googleTexts.save}", "{googleTexts.remove}", "{googleTexts.cancel}", "{googleTexts.signIn}", "{googleTexts.signOut}"],
  "pages/HomeworkDialogs.tsx": ["{changeTexts.restore}", "{changeTexts.remove}", "{changeTexts.cancel}", "{changeTexts.openSend}"],
  "pages/HomeworkMenus.tsx": ["{firstRunTexts.manual}", "{draftButtonTexts.markSent}", "{draftButtonTexts.sendAgain}", '{draftButtonTexts.save} <span aria-hidden="true"> ▾</span>'],
  "pages/HomeworkPage.tsx": ["{addTexts.button}", "{empty.action.label}", "{filterReset}", "Сохранить правку", "Отменить правку", "Взять этот текст", "К последней версии", "{draftButtonTexts.send}", "{offer.label}"],
  "pages/HomeworkTurnkey.tsx": ["{turnkeyTexts.button}"],
  "pages/JournalParts.tsx": ["{texts.retry}", "{texts.resetFilters}"],
  "pages/KnowledgeBasePage.tsx": ["Найти", "{importTexts.open}"],
  "pages/KnowledgeParts.tsx": ["{t.addPast}", "{t.choose}", "{t.choose}", "{t.pickUndated}", "{t.close}", "Сохранить", "Закрыть"],
  "pages/LectureTextParts.tsx": ["{start}", "Исходная расшифровка"],
  "pages/LessonCorrection.tsx": ["Сохранить", "Закрыть"],
  "pages/LessonGone.tsx": ["{lessonScreenTexts.retry}", "{lessonScreenTexts.back}"],
  "pages/ModelsPage.tsx": ["Импортировать файл модели…", "SHA-256 совпадает, установить", "Отмена", "Открыть страницу моделей на Hugging Face", "Скачать"],
  "pages/NameForm.tsx": ["{texts.create}", "{texts.cancel}"],
  "pages/OwnNotesParts.tsx": ["{t.pasteSave}", "{t.pasteCancel}", "{t.addFiles}", "{t.paste}", "{t.pasteSave}", "{t.pasteCancel}"],
  "pages/RecipientParts.tsx": ["{action.label}"],
  "pages/RecordingImportPanel.tsx": ["{importTexts.choose}", "{importTexts.close}"],
  "pages/RecordingRow.tsx": ["{i.label}", "{i.label}", "{i.label}", "{i.label}", "{fixTexts.connections}", "{fixTexts.retry}", "{i.label}", "{i.label}"],
  "pages/RecordingsPage.tsx": ["{importTexts.open}", "{manualStartTexts.open}", "{manualStartTexts.start}", "{manualStartTexts.cancel}", "{emptyTexts.schedule}", "{failed}"],
  "pages/SchedulePage.tsx": ["Выбрать файл…", "{reviewTexts.open}", "{folded ? listTexts.expandAll : listTexts.collapseAll}", "{applyLabel(p)}", "Отмена"],
  "pages/ScheduleSubjectsReview.tsx": ["{stepLabels[s]}", "{main.label}", "{reviewTexts.close}", "{label}", '{reviewTexts.open} {props.groups !== undefined && <span className="sched-cnt num">{props.groups}</span>}', "{reviewTexts.retry}", "{props.addLabel}", "{reviewTexts.retry}", "{reviewTexts.importSchedule}"],
  "pages/SecretSlot.tsx": ["{labels.save}", "{labels.remove}"],
  "pages/SettingsPage.tsx": ["Сохранить профиль"],
  "pages/SpeechComponentNotice.tsx": ["{speechTexts.install}", "{speechTexts.cancel}"],
  "pages/SubjectHead.tsx": ["{headTexts.tickets}"],
  "pages/SubjectPairsTab.tsx": ["{importTexts.open}"],
  "pages/SubjectRemoval.tsx": ['{v.button === "restore" ? removalTexts.restore : removalTexts.retry}'],
  "pages/SubjectsPage.tsx": ["{texts.addFile}"],
  "pages/SubjectsParts.tsx": ["{texts.addFile}", "{texts.addSubject}", "{texts.addFolder}", "{texts.knowledge}"],
  "pages/SubjectTasksTab.tsx": ["{tasksTexts.add}"],
  "pages/SummaryParts.tsx": ["{label}", "{checkSoundLabel}"],
  "pages/TeachersBlock.tsx": ["{copyLabels[source].button}", "{teacherTexts.save}", "{teacherTexts.cancel}", "{teacherTexts.clear}"],
  "pages/TicketsPage.tsx": ["{texts.goSubjects}", "{texts.pasteAction}", "{texts.fileAction}"],
  "pages/TicketsParts.tsx": ["{props.restart ? texts.confirmAgain : texts.confirm}", "{texts.open}", "{texts.cancel}"],
  "pages/TodayCalendar.tsx": ["{calendarTexts.button}"],
  "pages/TodayParts.tsx": [
    "{c.text}",
    "{texts.retry}",
    "{p}",
    '{b.kind === "stop" ? ( <> {recordTexts.running} {b.time !== "" && ( <> {" "} <span className="num">{b.time}</span> </> )} {` · ${recordTexts.stop}`} </> ) : ( b.label )}',
    "{recordTexts.retry}",
    "{recordTexts.openRecordings}",
    "{callLinkTexts.retry}", // TASK-364's lesson-card button, not converted here
    "{texts.record}",
    "{addTexts.button}",
  ],
  "pages/TranscriptLinks.tsx": ["{links.length === 1 ? openPairTexts.one : openPairTexts.many(formatTime(l.startedAt))}"],
  // The popup is one prompt window: every button answers the prompt («Записать», «Пропустить», «Остановить», «Скрыть»);
  // TASK-370: «Подключиться» (the meeting of the lesson on offer) is its second main action and stays a word.
  "popup/Popup.tsx": ['{advicePending === "switch" ? adviceLabels.switching : adviceLabels.switch}', "{adviceLabels.keep}", "{recordingTexts.stop}", "{recordingTexts.hide}", '{id === "start" ? m.startLabel : buttonLabels[id]}', "{callLinkTexts.join}"],
  "sourcePicker/SourceAdvice.tsx": ["{adviceLabels.switch}", "{adviceLabels.keep}"],
};

test("every <Button> that still shows words in the screens and the popup is a main action, a dialog or form button, or a named way out (the list is the decision)", () => {
  const found = {};
  for (const dir of ["pages", "popup", "sourcePicker", "ui"]) {
    for (const file of readdirSync(new URL(`../src/${dir}/`, import.meta.url)).filter((f) => f.endsWith(".tsx"))) {
      const bodies = buttonBodies(src(`${dir}/${file}`));
      if (bodies.length > 0) {
        found[`${dir}/${file}`] = bodies;
      }
    }
  }
  assert.deepEqual(found, wordButtons);
});

test("no raw <button className=\"btn…\"> is drawn in the screens outside the dialogs and the filters disclosure", () => {
  const raw = [];
  for (const dir of ["pages", "popup", "sourcePicker"]) {
    for (const file of readdirSync(new URL(`../src/${dir}/`, import.meta.url)).filter((f) => f.endsWith(".tsx"))) {
      const code = src(`${dir}/${file}`);
      if (/<button\b[^>]*className=\{?"btn/.test(code)) {
        raw.push(`${dir}/${file}`);
      }
    }
  }
  assert.deepEqual(raw, ["pages/JournalParts.tsx", "pages/SubjectRemoval.tsx"], "«Ещё фильтры» is a disclosure with a counter; SubjectRemoval is a native <dialog> with two dialog buttons");
});

test("the converted places draw their small buttons through IconAction and their words come from the text modules", () => {
  for (const file of [
    "pages/FileRows.tsx", "pages/DropZone.tsx", "pages/AssignmentDoneParts.tsx", "pages/TodayParts.tsx", "pages/JournalParts.tsx", "pages/SummaryParts.tsx",
    "pages/LectureTextParts.tsx", "pages/TranscriptPage.tsx", "pages/KnowledgeParts.tsx", "pages/KnowledgeBasePage.tsx", "pages/OwnNotesParts.tsx", "pages/PageViewer.tsx",
    "pages/TeachersBlock.tsx", "pages/TicketsPage.tsx", "pages/TicketsParts.tsx", "pages/ModelsPage.tsx", "pages/ScheduleSubjectsReview.tsx",
    "pages/SubjectRemoval.tsx", "pages/RecordingsPage.tsx", "pages/ConnectionsPage.tsx", "pages/HomeworkPage.tsx", "pages/CallLinkParts.tsx",
  ]) {
    const code = src(file);
    assert.ok(count(code, "<IconAction") > 0, file);
    assert.match(code, /import \{ IconAction \} from "\.\.\/ui\/IconAction(\.js)?";/, file);
    for (const m of code.matchAll(/<IconAction\b[\s\S]*?\/>/g)) {
      assert.doesNotMatch(m[0], /label="/, `${file}: the label of an icon button comes from a text module`);
    }
  }
});
