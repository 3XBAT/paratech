// Breadcrumbs above the title on the nested screens (TASK-350, ADR-0034 §2): the component, and the pages that use it
// instead of their back buttons. The pages need the Wails bindings to render, so their source is checked.
import { test } from "node:test";
import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import { createElement as h } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { Breadcrumbs, crumbsLabel } from "../.test-build/ui/Breadcrumbs.js";
import { subjectSubLine } from "../.test-build/libraryView.js";
import { texts } from "../.test-build/ticketsView.js";

const read = (path) => readFileSync(new URL(`../src/${path}`, import.meta.url), "utf8");
const noop = () => undefined;

// Walks the element tree of a function component without rendering it: a click is calling the onClick found there.
function findAll(node, pred, out = []) {
  if (Array.isArray(node)) {
    node.forEach((n) => findAll(n, pred, out));
  } else if (node !== null && typeof node === "object" && "props" in node) {
    if (pred(node)) {
      out.push(node);
    }
    findAll(node.props.children, pred, out);
  }
  return out;
}

test("every level but the last is a button in the tab order, the last is plain text with aria-current", () => {
  const html = renderToStaticMarkup(
    h(Breadcrumbs, { items: [{ label: "Предметы", onClick: noop }, { label: "Матанализ", onClick: noop }, { label: "Билеты" }] }),
  );
  assert.match(html, /<nav class="crumbs" aria-label="Путь">/);
  assert.equal(crumbsLabel, "Путь");
  assert.equal(html.match(/<button /g).length, 2, "two links, not the last level");
  assert.equal(html.match(/<li>/g).length, 3);
  assert.match(html, /<button type="button" class="crumb link">Предметы<\/button>/);
  assert.match(html, /<span class="crumb cur" aria-current="page">Билеты<\/span>/);
  assert.equal(html.match(/aria-current/g).length, 1, "only the last level is the current page");
  assert.ok(!html.includes("tabindex"), "buttons are in the tab order as they are");
  // The separator is decoration for the eye; the list already says how deep the screen is.
  assert.equal(html.match(/<span class="crumb-sep" aria-hidden="true">\/<\/span>/g).length, 2);
});

test("a click on a level calls its handler; the last level has none", () => {
  const calls = [];
  const tree = Breadcrumbs({
    items: [
      { label: "Задания", onClick: () => calls.push("list") },
      { label: "Реферат по истории" },
    ],
  });
  const buttons = findAll(tree, (n) => n.type === "button");
  assert.equal(buttons.length, 1);
  buttons[0].props.onClick();
  assert.deepEqual(calls, ["list"]);
  const current = findAll(tree, (n) => n.props["aria-current"] === "page");
  assert.equal(current.length, 1);
  assert.equal(current[0].type, "span");
  assert.equal(current[0].props.onClick, undefined);
});

test("one level is only the current page; a long name keeps the whole text in a tooltip; text is not HTML", () => {
  const one = renderToStaticMarkup(h(Breadcrumbs, { items: [{ label: "Задания" }] }));
  assert.ok(!one.includes("<button") && !one.includes("crumb-sep"));
  const long = "Реферат по истории Древнего Востока и античности";
  const html = renderToStaticMarkup(h(Breadcrumbs, { items: [{ label: "Задания", onClick: noop }, { label: long }] }));
  assert.ok(html.includes(`title="${long}"`), "cut by the line, whole in the tooltip");
  assert.ok(!html.includes('title="Задания"'), "a short name needs no tooltip");
  const evil = renderToStaticMarkup(h(Breadcrumbs, { items: [{ label: "Предметы", onClick: noop }, { label: "<img src=x onerror=1>" }] }));
  assert.ok(!evil.includes("<img"));
  assert.ok(evil.includes("&lt;img"));
});

test("the style keeps the line on one row, cuts long names and leaves the focus ring to the global rule", () => {
  const css = read("styles.css");
  assert.match(css, /\.crumbs ol \{[^}]*flex-wrap: nowrap/);
  assert.match(css, /\.crumb \{[^}]*text-overflow: ellipsis[^}]*white-space: nowrap/);
  assert.match(css, /\.crumb\.link \{[^}]*color: var\(--accent\)/);
  assert.ok(!/\.crumb[^{]*\{[^}]*outline: none/.test(css), "the focus ring of the buttons stays visible");
  assert.ok(!css.includes(".hw-back"), "the back button's style is gone with it");
});

// The old back buttons «Все задания», «Назад», «Предметы» are gone; their callbacks are behind the first level.
const backButtons = />\s*(Все задания|Назад|Все предметы)\s*</;

test("assignment page: «Задания» is the old «Все задания» (the same onBack), also while loading, failed or deleted", () => {
  const page = read("pages/HomeworkPage.tsx");
  assert.match(page, /import \{ Breadcrumbs \} from "\.\.\/ui\/Breadcrumbs"/);
  assert.doesNotMatch(page, backButtons);
  assert.ok(!page.includes("hw-back"));
  assert.match(page, /<Breadcrumbs items=\{\[\{ label: listCrumb, onClick: props\.onBack \}, \{ label: title \}\]\} \/>/);
  // The route stays the owner of the choice: the list is "" in the route (Alt+← walks the history, TASK-202).
  assert.match(page, /onBack=\{\(\) => select\(""\)\}/);
  // The loading/failed, the deleted and the open assignment all show the crumb, over the title.
  assert.match(page, /crumbs\(loadingCrumb\)/);
  assert.equal((page.match(/\{crumbs\(a\.title\)\}/g) ?? []).length, 2);
  assert.match(page, /\{crumbs\(a\.title\)\}\s*<div className="head">\s*<h1 className="hw-title"/, "the open page: the crumb is right above the head");
  assert.doesNotMatch(page, /<span className="label"/);
});

test("tickets: «Предметы / <предмет> / Билеты» (TASK-353; «Предметы» opens the subjects as the old button did); the empty state keeps a way on", () => {
  const page = read("pages/TicketsPage.tsx");
  assert.doesNotMatch(page, /icon="back"/);
  assert.doesNotMatch(page, /<span className="label"/);
  assert.match(page, /<Breadcrumbs\s+items=\{ticketsCrumbs\(subject, \{\s*onSubjects: \(\) => \(props\.onOpenSubjects \?\? \(\(\) => props\.onOpen\("subjects"\)\)\)\(\),\s*onSubject: \(id\) => props\.onOpenSubject\?\.\(id\),\s*\}\)\}\s*\/>/);
  // The crumb is shown in every state (loading, failed, no subjects), the head only holds the title.
  assert.match(page, /\}\)\}\s*\/>\s*<div className="head">\s*<h1>\{texts\.title\}<\/h1>\s*<\/div>/);
  // No subjects: not a back button but the next step.
  // The button only opens «Предметы», so it says so; no over-line, and the title does not repeat «Билеты».
  assert.match(page, /<EmptyState title=\{texts\.noSubjectsTitle\} actions=\{<Button onClick=\{\(\) => props\.onOpen\("subjects"\)\}>\{texts\.goSubjects\}<\/Button>\}/);
  assert.equal(texts.noSubjectsTitle, "Сначала нужен предмет");
  assert.equal(texts.noSubjects, "Билеты собираются по предмету. Предмет можно добавить в разделе «Предметы».");
  assert.equal(texts.goSubjects, "Перейти к предметам");
  assert.ok(!("label" in texts), "no «Экзамен» over-line");
  assert.equal(readFileSync(new URL("../src/ticketsView.ts", import.meta.url), "utf8").match(/back: "Предметы"/).length, 1);
});

// TASK-351: the lesson is a screen of its own (route.lesson), not rendered inside «База знаний» any more; its crumbs are
// «Предметы / <предмет> / <пара>» (lessonCrumbs of knowledgeView.ts; from «База знаний»: «База знаний / <пара>»), the
// levels above the lesson are routes of the window.
test("knowledge base: the subject has crumbs, the old «Назад» of its step up stays; the lesson is not drawn inside it", () => {
  const page = read("pages/KnowledgeBasePage.tsx");
  assert.doesNotMatch(page, backButtons);
  assert.ok(!page.includes("hw-back"));
  // The subject: «База знаний» is the old «Назад» (back to the list, the counts read again).
  assert.match(page, /<Breadcrumbs items=\{\[\{ label: rootCrumb, onClick: props\.onBack \}, \{ label: subject\.name \}\]\} \/>/);
  assert.match(page, /onBack=\{leaveSubject\}/);
  assert.match(page, /const leaveSubject = \(\) => \{\s*setSubject\(null\);\s*setReload\(\(n\) => n \+ 1\);\s*\};/);
  // The lesson levels moved to the lesson screen with the route.
  assert.doesNotMatch(page, /KnowledgeLessonPage|NoteLecturePage|lessonTrail|closeLesson|setOpen\(/);
});

test("lesson pages: the crumbs are the lesson's own, the lesson is the last level, and there is no back button", () => {
  const src = read("pages/KnowledgeLessonPage.tsx");
  assert.doesNotMatch(src, backButtons);
  assert.ok(!src.includes("hw-back"));
  // TASK-351 review: the crumbs follow the section under the lesson (origin) and can close the lesson (onBack).
  assert.match(src, /<Breadcrumbs items=\{crumbItems\(lesson, lessonHeading\(lesson\), props\.origin, props\.nav, props\.onBack\)\} \/>/);
  assert.match(src, /<Breadcrumbs items=\{crumbItems\(l, noteLectureHeading\(l\), props\.origin, props\.nav, props\.onBack\)\} \/>/);
  // The transcript tab draws no head and no crumbs of its own (TASK-352: the overlay pages of the summary and the
  // transcript are gone, the lesson screen has the only crumbs).
  const transcript = read("pages/TranscriptPage.tsx");
  assert.doesNotMatch(transcript, /Breadcrumbs|<h1|onBack|embedded|defaultParentCrumb/);
});

test("the lesson screen is the only page with crumbs for a recording: the old overlays and their wiring are gone", () => {
  assert.equal(existsSync(new URL("../src/pages/SummaryPage.tsx", import.meta.url)), false);
  const app = read("App.tsx");
  assert.doesNotMatch(app, /closeTranscript|routeToSummary|routeToTranscript|<SummaryPage|<TranscriptPage|pageById/);
  // The window closes the lesson to the section under it (the route's step); the screen only calls it.
  assert.equal((app.match(/onBack=\{lessonBack\}/g) ?? []).length, 1);
  assert.match(app, /setRoute\(closeLesson\);/);
});

test("subjects: the over-lines are gone, the semester and the aliases are one muted line under the name", () => {
  // TASK-353: the head of a subject is SubjectHead (name, the muted lines, «Билеты к экзамену», «⋯»).
  const src = read("pages/SubjectHead.tsx");
  assert.doesNotMatch(read("pages/SubjectsPage.tsx"), /<span className="label"/);
  assert.match(src, /<h1>\{subject\.name\}<\/h1>\s*\{sub !== "" && <p className="small muted sub-line">\{sub\}<\/p>\}/);
  assert.match(src, /const sub = subjectSubLine\(subject\);/);
  assert.doesNotMatch(src, backButtons);
  const s = { semester: "2026-autumn", semesterLabel: "Осень 2026", aliases: ["матан", "МА"] };
  assert.equal(subjectSubLine(s), "Осень 2026 · Также: матан, МА");
  assert.equal(subjectSubLine({ ...s, aliases: [] }), "Осень 2026");
  assert.equal(subjectSubLine({ ...s, semesterLabel: "" }), "2026-autumn · Также: матан, МА");
  assert.equal(subjectSubLine({ semester: "", semesterLabel: "", aliases: ["МА"] }), "Также: МА");
  assert.equal(subjectSubLine({ semester: "", semesterLabel: "", aliases: [] }), "");
});

test("the crumb is a ~32px target aligned with the title: padding 4px 6px, margin -6px", () => {
  const css = read("styles.css");
  assert.match(css, /\.crumbs \{[^}]*margin: 0 0 8px -6px/);
  assert.match(css, /\.crumb \{[^}]*padding: 4px 6px/);
});
