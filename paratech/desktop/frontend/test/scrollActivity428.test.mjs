// TASK-428: scrollbars show only on activity. jsdom is not used (no layout): the document, the scrollers and their geometry
// are fakes, the timers are Node's fake timers, the animation frame is a queue the test flushes.
import { afterEach, beforeEach, mock, test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { activeClass, hideAfterMs, installScrollActivity } from "../.test-build/ui/scrollActivity.js";

beforeEach(() => mock.timers.enable({ apis: ["setTimeout"] }));
afterEach(() => mock.timers.reset());

/** A scroller with a 10px vertical scrollbar on its right edge (box 0..410 x 0..300), nested inside `parent`. */
function scroller(over = {}, parent = null) {
  const classes = new Set();
  return {
    classList: { add: (c) => classes.add(c), remove: (c) => classes.delete(c), contains: (c) => classes.has(c) },
    parentElement: parent,
    offsetWidth: 410, clientWidth: 400, clientLeft: 0,
    offsetHeight: 300, clientHeight: 300, clientTop: 0,
    getBoundingClientRect: () => ({ left: 0, right: 410, top: 0, bottom: 300 }),
    borders: { right: "0px", bottom: "0px" },
    on: () => classes.has(activeClass),
    ...over,
  };
}
/** A box with no scrollbar. */
const plain = (parent) =>
  scroller({ offsetWidth: 400, clientWidth: 400, offsetHeight: 300, clientHeight: 300, getBoundingClientRect: () => ({ left: 0, right: 400, top: 0, bottom: 300 }) }, parent);

function fakeDoc() {
  const listeners = new Map();
  const frames = [];
  const root = { classList: { add() {}, remove() {}, contains: () => false }, parentElement: null };
  const doc = {
    documentElement: root,
    scrollingElement: scroller(),
    defaultView: { getComputedStyle: (el) => ({ borderRightWidth: el.borders?.right ?? "0px", borderBottomWidth: el.borders?.bottom ?? "0px" }) },
    addEventListener: (type, fn, opts) => {
      assert.deepEqual(opts, { capture: true, passive: true }, `${type}: capture and passive`);
      listeners.set(type, [...(listeners.get(type) ?? []), fn]);
    },
    removeEventListener: (type, fn) => listeners.set(type, (listeners.get(type) ?? []).filter((f) => f !== fn)),
  };
  const fire = (type, ev) => (listeners.get(type) ?? []).forEach((fn) => fn({ type, ...ev }));
  const count = () => [...listeners.values()].reduce((n, l) => n + l.length, 0);
  const raf = (run) => frames.push(run);
  const flush = () => frames.splice(0).forEach((run) => run());
  return { doc, fire, count, raf, flush, root };
}
const install = (d) => installScrollActivity(d.doc, d.raf);

test("a scroll puts the class on the scroller and removes it 1000 ms after the last scroll", () => {
  assert.equal(hideAfterMs, 1000);
  const d = fakeDoc();
  install(d);
  const el = scroller();
  assert.equal(el.on(), false, "nothing at rest");
  d.fire("scroll", { target: el });
  assert.equal(el.on(), true);
  mock.timers.tick(999);
  assert.equal(el.on(), true);
  mock.timers.tick(1);
  assert.equal(el.on(), false);
});

test("a second scroll restarts the countdown; every scroller has its own", () => {
  const d = fakeDoc();
  install(d);
  const a = scroller();
  const b = scroller();
  d.fire("scroll", { target: a });
  mock.timers.tick(600);
  d.fire("scroll", { target: b });
  d.fire("scroll", { target: a });
  mock.timers.tick(600);
  assert.equal(a.on(), true, "restarted at 600");
  mock.timers.tick(400);
  assert.equal(a.on(), false);
  assert.equal(b.on(), false, "b started at 600 as well, with a");
});

test("a scroll of the document itself goes to the scrolling element", () => {
  const d = fakeDoc();
  install(d);
  d.fire("scroll", { target: d.doc });
  assert.equal(d.doc.scrollingElement.on(), true);
  mock.timers.tick(1000);
  assert.equal(d.doc.scrollingElement.on(), false);
});

test("the pointer over the scrollbar strip shows the thumb and holds it while it rests there; leaving lets the countdown run", () => {
  const d = fakeDoc();
  install(d);
  const el = scroller();
  d.fire("pointermove", { target: el, clientX: 405, clientY: 100, buttons: 0 });
  assert.equal(el.on(), false, "handled once per frame, not at the event");
  d.flush();
  assert.equal(el.on(), true);
  mock.timers.tick(5000);
  assert.equal(el.on(), true, "a resting pointer on the bar keeps it");
  d.fire("pointermove", { target: el, clientX: 200, clientY: 100, buttons: 0 });
  d.flush();
  assert.equal(el.on(), true, "left the strip: the countdown starts now");
  mock.timers.tick(999);
  assert.equal(el.on(), true);
  mock.timers.tick(1);
  assert.equal(el.on(), false);
});

test("the pointer outside any strip does nothing; a child of a scroller finds the scroller, not its own box", () => {
  const d = fakeDoc();
  install(d);
  const el = scroller();
  const child = plain(el);
  d.fire("pointermove", { target: child, clientX: 200, clientY: 100, buttons: 0 });
  d.flush();
  assert.equal(el.on(), false);
  assert.equal(child.on(), false);
  // The strip of the scroller is reached through its child as the target too (geometry decides, not the target).
  d.fire("pointermove", { target: child, clientX: 405, clientY: 100, buttons: 0 });
  d.flush();
  assert.equal(el.on(), true);
  assert.equal(child.on(), false);
});

test("the strip is measured without borders: a left border and a right border are not part of it", () => {
  const d = fakeDoc();
  install(d);
  // .sheet: 1px left border; 411 - 400 - 1 = 10px strip at the right edge 411.
  const sheet = scroller({ offsetWidth: 411, clientLeft: 1, getBoundingClientRect: () => ({ left: 0, right: 411, top: 0, bottom: 300 }) });
  d.fire("pointermove", { target: sheet, clientX: 0.5, clientY: 100, buttons: 0 });
  d.flush();
  assert.equal(sheet.on(), false, "the left border is not the bar");
  d.fire("pointermove", { target: sheet, clientX: 406, clientY: 100, buttons: 0 });
  d.flush();
  assert.equal(sheet.on(), true);
  // A 2px right border: the bar is 8..10px inside it, the border itself is outside.
  const framed = scroller({ offsetWidth: 412, borders: { right: "2px", bottom: "0px" }, getBoundingClientRect: () => ({ left: 0, right: 412, top: 0, bottom: 300 }) });
  d.fire("pointermove", { target: framed, clientX: 411, clientY: 100, buttons: 0 });
  d.flush();
  assert.equal(framed.on(), false, "in the border");
  d.fire("pointermove", { target: framed, clientX: 405, clientY: 100, buttons: 0 });
  d.flush();
  assert.equal(framed.on(), true);
});

test("a horizontal strip counts too", () => {
  const d = fakeDoc();
  install(d);
  const el = scroller({ offsetWidth: 400, offsetHeight: 310, getBoundingClientRect: () => ({ left: 0, right: 400, top: 0, bottom: 310 }) });
  d.fire("pointermove", { target: el, clientX: 100, clientY: 305, buttons: 0 });
  d.flush();
  assert.equal(el.on(), true);
});

test("pointer moves are throttled to one per frame: the last position of the frame wins", () => {
  const d = fakeDoc();
  install(d);
  const el = scroller();
  d.fire("pointermove", { target: el, clientX: 405, clientY: 100, buttons: 0 });
  d.fire("pointermove", { target: el, clientX: 200, clientY: 100, buttons: 0 });
  d.flush();
  assert.equal(el.on(), false, "the first move was dropped, the last one is outside");
});

test("a thumb drag keeps the class on, scrolls included, until the release", () => {
  const d = fakeDoc();
  install(d);
  const el = scroller();
  d.fire("pointerdown", { target: el, clientX: 405, clientY: 100, buttons: 1 });
  assert.equal(el.on(), true);
  d.fire("scroll", { target: el });
  mock.timers.tick(10_000);
  assert.equal(el.on(), true, "held while dragged");
  d.fire("pointerup", { target: el });
  mock.timers.tick(999);
  assert.equal(el.on(), true);
  mock.timers.tick(1);
  assert.equal(el.on(), false);
});

test("a press outside the strip is no drag; pointercancel ends one", () => {
  const d = fakeDoc();
  install(d);
  const el = scroller();
  d.fire("pointerdown", { target: el, clientX: 100, clientY: 100, buttons: 1 });
  assert.equal(el.on(), false);
  d.fire("pointerdown", { target: el, clientX: 405, clientY: 100, buttons: 1 });
  d.fire("pointercancel", { target: el });
  mock.timers.tick(1000);
  assert.equal(el.on(), false);
});

test("a release outside the window is not seen: the next move without a button ends the drag", () => {
  const d = fakeDoc();
  install(d);
  const el = scroller();
  d.fire("pointerdown", { target: el, clientX: 405, clientY: 100, buttons: 1 });
  d.fire("pointermove", { target: el, clientX: 200, clientY: 100, buttons: 0 });
  d.flush();
  mock.timers.tick(1000);
  assert.equal(el.on(), false);
});

test("the pointer leaving the window from the bar ends the hold", () => {
  const d = fakeDoc();
  install(d);
  const el = scroller();
  d.fire("pointermove", { target: el, clientX: 405, clientY: 100, buttons: 0 });
  d.flush();
  d.fire("pointerout", { target: el, relatedTarget: {} });
  mock.timers.tick(5000);
  assert.equal(el.on(), true, "moved to another element of the page: still on the bar's strip");
  d.fire("pointerout", { target: el, relatedTarget: null });
  mock.timers.tick(1000);
  assert.equal(el.on(), false);
});

test("uninstall removes every listener, the classes and the timers", () => {
  const d = fakeDoc();
  const off = install(d);
  assert.ok(d.count() >= 5);
  const el = scroller();
  d.fire("scroll", { target: el });
  assert.equal(el.on(), true);
  off();
  assert.equal(d.count(), 0);
  assert.equal(el.on(), false);
  d.fire("scroll", { target: el });
  assert.equal(el.on(), false, "no listener left");
  mock.timers.tick(2000);
});

// ---- CSS and wiring guards ----

const strip = (s) => s.replace(/\/\*[\s\S]*?\*\//g, "");
const read = (p) => strip(readFileSync(new URL(p, import.meta.url), "utf8"));
const css = read("../src/ui/scrollbar.css");

test("scrollbar.css: the thumb is transparent at rest and visible only under .scroll-active", () => {
  const rest = /(?:^|\})\s*::-webkit-scrollbar-thumb\s*\{([^}]*)\}/.exec(css);
  assert.ok(rest);
  assert.match(rest[1], /background:\s*transparent/);
  assert.doesNotMatch(rest[1], /var\(--scroll/);
  const on = /\.scroll-active::-webkit-scrollbar-thumb\s*\{([^}]*)\}/.exec(css);
  assert.ok(on);
  assert.match(on[1], /background:\s*var\(--scroll\)/);
  const hover = /\.scroll-active::-webkit-scrollbar-thumb:hover,\s*\.scroll-active::-webkit-scrollbar-thumb:active\s*\{([^}]*)\}/.exec(css);
  assert.ok(hover);
  assert.match(hover[1], /var\(--scroll-hover\)/);
  // The coloured values appear nowhere else (a rule without the class would show the thumb at rest).
  const outside = css.replace(/\.scroll-active::-webkit-scrollbar-thumb[^{]*\{[^}]*\}/g, "");
  assert.doesNotMatch(outside, /var\(--scroll/);
  assert.doesNotMatch(css, /overflow:\s*overlay|scrollbar-(?:width|color)\s*:/);
});

test("scrollbar.css: forced colors always show the thumb (CanvasText, forced-color-adjust none), not conditional on the class", () => {
  const block = /@media \(forced-colors: active\)\s*\{([\s\S]*?)\n\}/.exec(css);
  assert.ok(block);
  assert.match(block[1], /background:\s*CanvasText/);
  assert.match(block[1], /forced-color-adjust:\s*none/);
  assert.match(block[1], /(?:^|\s)::-webkit-scrollbar-thumb,/, "the rest selector is in the block");
  assert.match(block[1], /\.scroll-active::-webkit-scrollbar-thumb\s*\{/);
  assert.match(css, /@media \(prefers-reduced-motion: reduce\)/);
});

// Every scroll container has a stable gutter (the track keeps its width, nothing moves when the thumb shows), except these.
const exempt = new Map([
  [".template", "a horizontal-only pre: a gutter would add a pointless strip at its right edge"],
  [".ag-input", "the textarea of the agent: the gutter would eat the width of the text; it rarely scrolls"],
]);

test("every scroll container has scrollbar-gutter: stable or a listed exemption", () => {
  const files = ["../src/styles.css", "../src/agent/agent.css", "../src/popup/popup.css", "../src/sourcePicker/sourcePicker.css"];
  let seen = 0;
  for (const f of files) {
    for (const m of read(f).matchAll(/(?<=^|\})\s*([^{}@]+)\{([^}]*)\}/g)) {
      const body = m[2];
      if (!/overflow(?:-y)?:\s*(?:auto|scroll)/.test(body)) continue;
      const selector = m[1].trim();
      seen++;
      if (exempt.has(selector)) {
        assert.ok(exempt.get(selector).length > 10);
        continue;
      }
      assert.match(body, /scrollbar-gutter:\s*stable/, `${f}: ${selector}`);
    }
  }
  assert.ok(seen >= 10, `found ${seen} scroll containers`);
  // The exemptions are real (a stale entry would hide a missing gutter later).
  assert.match(read("../src/styles.css"), /\.template\s*\{[^}]*overflow-x:\s*auto/);
  assert.match(read("../src/agent/agent.css"), /\.ag-input\s*\{[^}]*overflow-y:\s*auto/);
});

test("no scroll container hides in a tsx inline style", () => {
  const dir = fileURLToPath(new URL("../src", import.meta.url));
  const walk = (p, out = []) => {
    for (const e of readdirSync(p, { withFileTypes: true })) {
      if (e.isDirectory()) walk(`${p}/${e.name}`, out);
      else if (e.name.endsWith(".tsx")) out.push(`${p}/${e.name}`);
    }
    return out;
  };
  for (const f of walk(dir)) {
    assert.doesNotMatch(readFileSync(f, "utf8"), /overflow(?:Y|X)?:\s*["'](?:auto|scroll)/, f);
  }
});

test("«Ссылка на пару» keeps a 24px gap to the tabs: the margin is on the block, so every state of it has it", () => {
  const styles = read("../src/styles.css");
  const m = /\.section\.calllink\s*\{([^}]*)\}/.exec(styles);
  assert.ok(m);
  assert.match(m[1], /margin-bottom:\s*24px/);
  // Everything the block shows (link, form, refusal, result line) is inside the one <section>; the tabs follow it.
  const parts = readFileSync(new URL("../src/pages/CallLinkParts.tsx", import.meta.url), "utf8");
  assert.match(parts, /<section className="section calllink"/);
  assert.match(parts, /\{line !== null && <Line [\s\S]*?\/>\}\s*<\/section>/);
  // No narrow-window rule takes the margin away.
  assert.doesNotMatch(styles, /@(?:container|media)[^{]*\{\s*\.section\.calllink/);
});

test("both entries install the scroll activity once, on their document", () => {
  for (const entry of ["../src/main.tsx", "../src/popup/main.tsx"]) {
    const text = readFileSync(new URL(entry, import.meta.url), "utf8");
    assert.match(text, /import \{ installScrollActivity \} from "\.\.?\/(?:ui\/)?scrollActivity"/, entry);
    assert.equal((text.match(/installScrollActivity\(document\)/g) ?? []).length, 1, entry);
  }
});
