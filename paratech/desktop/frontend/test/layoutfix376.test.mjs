// BUG-20261104-e2e-layout (found by the layout pack of TASK-376): the narrow lesson row of «Сегодня», cut texts without
// a tooltip, the squeezed current crumb, and the quiet "no work yet" reply of the turnkey block.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { Breadcrumbs, crumbLongFrom } from "../.test-build/ui/Breadcrumbs.js";
import { BurningSection, WaitingSection } from "../.test-build/pages/TodayParts.js";
import { SubjectTree } from "../.test-build/pages/SubjectsParts.js";
import { loadTurnkeyBlock } from "../.test-build/homeworkTurnkey.js";
import { methods, turnkeyApi } from "../.test-build/bindings/turnkey.js";

const read = (path) => readFileSync(new URL(`../src/${path}`, import.meta.url), "utf8");
const noop = () => undefined;

/** The body of the first rule whose selector is exactly `selector`, inside `css`. */
function rule(css, selector) {
  const esc = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const m = new RegExp(`(?:^|\\n)\\s*${esc}\\s*\\{([^}]*)\\}`).exec(css);
  assert.ok(m, `rule ${selector}`);
  return m[1];
}

// ---- «Сегодня»: the actions of a lesson row ----

test("narrow lesson row: the actions take columns 2-3 of their own row and are not auto-placed into the time column", () => {
  const css = read("pages/today.css");
  const cut = css.indexOf("@container (min-width: 38rem)");
  assert.ok(cut > 0);
  const narrow = css.slice(0, cut);
  const n = rule(narrow, ".today-lesson .today-ac");
  assert.match(n, /grid-column: 2 \/ -1/);
  assert.match(n, /justify-self: start/);
  // The status keeps its place, the link panel still spans the same columns.
  assert.match(rule(narrow, ".today-lesson .today-st"), /grid-column: 2 \/ -1/);
  assert.match(rule(narrow, ".today-link"), /grid-column: 2 \/ -1/);
});

test("narrow: the quiet ghost button is pulled left by its own padding; the wide row resets it (it is end-aligned there)", () => {
  const css = read("pages/today.css");
  const comp = read("ui/components.css");
  const pad = /\.btn\.sm \{[^}]*padding: 0 (\d+)px/.exec(comp);
  assert.ok(pad, "the sm button padding");
  const cut = css.indexOf("@container (min-width: 38rem)");
  const sel = ".today-lesson .today-ac > .btn.ghost:first-child";
  assert.match(rule(css.slice(0, cut), sel), new RegExp(`margin-left: -${pad[1]}px`));
  assert.match(rule(css.slice(cut, css.indexOf("@container (min-width: 62rem)")), sel), /margin-left: 0/);
});

test("wide lesson row: four columns as before, the actions are auto-placed in the last one and end-aligned", () => {
  const css = read("pages/today.css");
  const wide = css.slice(css.indexOf("@container (min-width: 38rem)"), css.indexOf("@container (min-width: 62rem)"));
  assert.match(wide, /\.today-lesson \{ grid-template-columns: 4\.5rem minmax\(0, 1fr\) minmax\(0, 14rem\) auto; \}/);
  const w = rule(wide, ".today-lesson .today-ac");
  assert.match(w, /grid-column: auto/);
  assert.match(w, /justify-self: end/);
  assert.match(w, /justify-content: flex-end/);
});

// ---- «Сегодня»: the clamped subject ----

test("the clamped .today-subject has the whole text as a tooltip, in the deadlines and in the waiting rows", () => {
  const long = "Теория вероятностей и математическая статистика для инженеров, групп 1-4 и вечернего отделения";
  const burning = renderToStaticMarkup(
    h(BurningSection, { burning: [{ assignmentId: "a", title: "Реферат", subject: long, due: "", daysLeft: 2, urgent: false }], onOpenAssignment: noop }),
  );
  assert.ok(burning.includes(`class="small muted today-subject" title="${long}"`));
  const w = { proposalId: "p", kind: "send_work_email", assignmentId: "", title: "Реферат", subject: long, due: "", recipient: "", createdAt: "" };
  assert.ok(renderToStaticMarkup(h(WaitingSection, { waiting: [w] })).includes(`class="small muted today-subject" title="${long}"`));
  // Every use in the source carries the title (no third, untitled place).
  const src = read("pages/TodayParts.tsx");
  const uses = src.match(/<div className="small muted today-subject"[^>]*>/g) ?? [];
  assert.equal(uses.length, 2);
  for (const u of uses) {
    assert.match(u, /title=\{m\.subject\}/);
  }
});

// ---- «Предметы»: the tree ----

test("tree buttons carry the whole name as a tooltip, the toggle has the same text as its aria-label", () => {
  const name = "Теория вероятностей и математическая статистика";
  const sem = "Осень 2026";
  const s = {
    id: "s1", name, aliases: [], semester: "2026-autumn", semesterLabel: sem,
    folders: [{ id: "f1", name: "Очень длинное название папки для лабораторных работ", system: "" }],
  };
  const out = renderToStaticMarkup(
    h(SubjectTree, { subjects: [s], currentId: "s1", counts: null, busy: false, onOpen: noop, onAddSubject: noop, onAddFolder: noop }),
  );
  assert.match(out, new RegExp(`class="ti sem"[^>]* title="${sem}"`), "semester head");
  assert.match(out, new RegExp(`data-subject-id="s1" title="${name}"`), "subject");
  assert.match(out, /class="ti fold" title="Очень длинное название папки для лабораторных работ"/, "folder");
  const tog = /<button[^>]*class="tog"[^>]*>/.exec(out);
  assert.ok(tog);
  const aria = /aria-label="([^"]+)"/.exec(tog[0]);
  assert.ok(aria);
  assert.ok(tog[0].includes(`title="${aria[1]}"`), "the toggle's tooltip is its aria-label");
  assert.ok(aria[1].includes(name));
});

// ---- Breadcrumbs ----

test("every crumb carries its whole label as a tooltip", () => {
  const html = renderToStaticMarkup(
    h(Breadcrumbs, { items: [{ label: "Предметы", onClick: noop }, { label: "Матанализ", onClick: noop }, { label: "Билеты к экзамену" }] }),
  );
  for (const label of ["Предметы", "Матанализ", "Билеты к экзамену"]) {
    assert.ok(html.includes(`title="${label}"`), label);
  }
  assert.equal((html.match(/title="/g) ?? []).length, 3);
  assert.doesNotMatch(read("ui/Breadcrumbs.tsx"), /tipFrom/);
});

test("only an ancestor crumb with a label longer than the threshold gets the floor class; the last crumb never does", () => {
  const longLabel = "Теория вероятностей"; // 19 letters
  const edge = "а".repeat(crumbLongFrom); // exactly at the threshold: still short
  const html = renderToStaticMarkup(
    h(Breadcrumbs, {
      items: [
        { label: "Задания", onClick: noop },
        { label: edge, onClick: noop },
        { label: longLabel, onClick: noop },
        { label: "Очень длинное название текущего экрана" },
      ],
    }),
  );
  const lis = html.match(/<li[^>]*>/g) ?? [];
  assert.deepEqual(lis, ["<li>", "<li>", '<li class="long">', "<li>"]);
  // The tooltips stay on every crumb, with or without the class.
  assert.equal((html.match(/title="/g) ?? []).length, 4);
});

test("the crumb line is never wider than the pane: the ancestors give way first down to a floor, the last crumb takes the rest", () => {
  const css = read("styles.css");
  // The containers cannot be pushed wider than the pane by their content.
  for (const sel of [".crumbs", ".crumbs ol"]) {
    const box = rule(css, sel);
    assert.match(box, /min-width: 0/, `${sel} may be narrower than its content`);
    assert.match(box, /max-width: 100%/, `${sel} is not wider than the pane`);
  }
  const ol = rule(css, ".crumbs ol");
  assert.match(ol, /display: flex/);
  assert.match(ol, /flex-wrap: nowrap/);

  // Ancestors: a huge shrink weight, so the deficit goes to them first. A short one has no floor (it keeps its own width,
  // so no empty gap appears after its «/»); only a .long one is frozen at a floor of a few letters.
  const li = rule(css, ".crumbs li");
  const anc = /flex: 0 (\d+) auto/.exec(li);
  assert.ok(anc && Number(anc[1]) >= 100, "ancestors shrink with a big weight");
  assert.match(li, /min-width: auto/, "a short ancestor has no floor to be stretched to");
  assert.doesNotMatch(li, /min-width: min\(/);
  const floor = /min-width: min\(100%, (\d+)ch\)/.exec(rule(css, ".crumbs li.long"));
  assert.ok(floor, "the floor of a long ancestor, never above the pane");
  assert.equal(Number(floor[1]), crumbLongFrom, "the floor and the class threshold are the same number");

  // The last crumb: a normal shrink factor (so it does shrink, the line cannot overflow) and no floor (an ellipsis).
  const last = rule(css, ".crumbs li:last-child");
  assert.match(last, /flex: 0 1 auto/);
  assert.match(last, /min-width: 0/);
  assert.match(last, /max-width: 100%/);
  assert.ok(Number(anc[1]) > 1, "ancestors are weighted above the last crumb, so they give way first");

  // Scenarios: (1) short last: nothing shrinks, content fits. (2) long last, short ancestors: the ancestors keep their
  // own width (auto floor), the whole deficit goes to the last, which is cut by .crumb's ellipsis. (3) long both: the
  // long ancestors go down to the floor first (weight 1000), then the last takes what is left.
  const crumb = rule(css, ".crumb");
  assert.match(crumb, /min-width: 0/);
  assert.match(crumb, /overflow: hidden/);
  assert.match(crumb, /text-overflow: ellipsis/);
  assert.match(crumb, /white-space: nowrap/);
  assert.match(rule(css, ".crumb.cur"), /max-width: none/, "the current crumb is limited by the line, not by 28ch");
});

// ---- Turnkey: "no work yet" is a quiet null ----

test("loadTurnkeyBlock: a null reply is the button without an error; other failures still show their text", async () => {
  assert.deepEqual(await loadTurnkeyBlock({ workOfAssignment: async () => null }, "a1"), { phase: "none", error: "" });
  const broken = await loadTurnkeyBlock({ workOfAssignment: async () => Promise.reject(new Error("raw internal text")) }, "a1");
  assert.equal(broken.phase, "none");
  assert.notEqual(broken.error, "");
});

test("turnkeyApi.workOfAssignment passes null through and parses a work", async () => {
  const calls = [];
  const reply = (v) => async (name, ...args) => (calls.push([name, ...args]), v);
  assert.equal(await turnkeyApi(reply(null)).workOfAssignment("a1"), null);
  assert.deepEqual(calls[0], [methods.workOfAssignment, "a1"]);
  const w = await turnkeyApi(reply({ id: "w1", status: "triage", version: 1, problem: "", message: "" })).workOfAssignment("a1");
  assert.equal(w.id, "w1");
  await assert.rejects(turnkeyApi(reply({ id: 5 })).workOfAssignment("a1"), "a malformed reply is still refused");
});
