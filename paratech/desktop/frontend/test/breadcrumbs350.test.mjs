// Breadcrumbs above the title on the nested screens (TASK-350, ADR-0034 §2): the component, and the pages that use it
// instead of their back buttons. The pages need the Wails bindings to render, so their source is checked.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { Breadcrumbs, crumbsLabel } from "../.test-build/ui/Breadcrumbs.js";

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

test("tickets: «Предметы / Билеты» opens the subjects as the old button did; the empty state keeps a way on", () => {
  const page = read("pages/TicketsPage.tsx");
  assert.doesNotMatch(page, /icon="back"/);
  assert.doesNotMatch(page, /<span className="label"/);
  assert.match(page, /<Breadcrumbs items=\{\[\{ label: texts\.back, onClick: \(\) => props\.onOpen\("subjects"\) \}, \{ label: texts\.title \}\]\} \/>/);
  // The crumb is shown in every state (loading, failed, no subjects), the head only holds the title.
  assert.match(page, /<Breadcrumbs[^\n]*\n\s*<div className="head">\s*<h1>\{texts\.title\}<\/h1>\s*<\/div>/);
  // No subjects: not a back button but the next step.
  assert.match(page, /<EmptyState[^>]*actions=\{<Button onClick=\{\(\) => props\.onOpen\("subjects"\)\}>\{addSubjectAction\}<\/Button>\}/);
  assert.equal(readFileSync(new URL("../src/ticketsView.ts", import.meta.url), "utf8").match(/back: "Предметы"/).length, 1);
});

test("knowledge base: the subject and the lesson have crumbs; each level does what a step up did", () => {
  const page = read("pages/KnowledgeBasePage.tsx");
  assert.doesNotMatch(page, backButtons);
  assert.ok(!page.includes("hw-back"));
  // The subject: «База знаний» is the old «Назад» (back to the list, the counts read again).
  assert.match(page, /<Breadcrumbs items=\{\[\{ label: rootCrumb, onClick: props\.onBack \}, \{ label: subject\.name \}\]\} \/>/);
  assert.match(page, /onBack=\{leaveSubject\}/);
  assert.match(page, /const leaveSubject = \(\) => \{\s*setSubject\(null\);\s*setReload\(\(n\) => n \+ 1\);\s*\};/);
  // The lesson: the same step up as before, and the subject level when the lesson was opened from a subject.
  assert.match(page, /onBack=\{closeLesson\}/);
  assert.match(page, /subject === null\s*\?\s*\[\{ label: rootCrumb, onClick: closeLesson \}\]/);
  assert.match(page, /\{ label: subject\.name, onClick: closeLesson \}/);
  assert.match(page, /closeLesson\(\);\s*leaveSubject\(\);/);
  // The found lessons are asked again after a change, as before.
  assert.match(page, /const closeLesson = \(\) => \{\s*setOpen\(null\);\s*if \(changed\.current\) \{[^}]*runSearch\(query, true\)/);
  assert.match(page, /<NoteLecturePage[^>]*trail=\{lessonTrail\}/);
  assert.match(page, /<KnowledgeLessonPage[\s\S]*?trail=\{lessonTrail\}/);
});

test("lesson pages: the lesson is the last level after the trail they get, and they have no back button", () => {
  const src = read("pages/KnowledgeLessonPage.tsx");
  assert.doesNotMatch(src, backButtons);
  assert.ok(!src.includes("hw-back"));
  assert.match(src, /<Breadcrumbs items=\{\[\.\.\.props\.trail, \{ label: lessonHeading\(lesson\) \}\]\} \/>/);
  assert.match(src, /<Breadcrumbs items=\{\[\.\.\.props\.trail, \{ label: noteLectureHeading\(l\) \}\]\} \/>/);
  // The embedded transcript still gets the step up (it draws no head of its own).
  assert.match(src, /embedded[\s\S]*?onBack=\{props\.onBack\}/);
});

test("summary and transcript: the first level is the old «Назад» (onBack), shown while loading too", () => {
  for (const [file, last] of [
    ["pages/SummaryPage.tsx", "Конспект"],
    ["pages/TranscriptPage.tsx", "Расшифровка"],
  ]) {
    const src = read(file);
    assert.doesNotMatch(src, backButtons, file);
    assert.ok(!src.includes("hw-back"), file);
    assert.match(src, /<Breadcrumbs items=\{\[\{ label: parentCrumb, onClick: props\.onBack \}, \{ label: pageCrumb \}\]\} \/>/, file);
    assert.ok(src.includes(`const pageCrumb = "${last}";`), file);
    assert.match(src, /<h1>\{pageCrumb\}<\/h1>/, file);
  }
  // The window still closes the overlay to the section under it (the route's step), the page only calls it.
  const app = read("App.tsx");
  assert.equal((app.match(/onBack=\{\(\) => setRoute\(closeTranscript\)\}/g) ?? []).length, 2);
});

test("subjects: the over-lines are gone, the semester is one muted line under the name", () => {
  const src = read("pages/SubjectsPage.tsx");
  assert.doesNotMatch(src, /<span className="label"/);
  assert.match(src, /<h1>\{subject\.name\}<\/h1>\s*<p className="small muted sub-line">\{subject\.semesterLabel \|\| subject\.semester\}<\/p>/);
  assert.doesNotMatch(src, backButtons);
});
