// TASK-363: the whole window must not scroll. jsdom has no layout, so the rules that cause/prevent it are checked as CSS text.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const read = (p) => readFileSync(new URL(p, import.meta.url), "utf8").replace(/\/\*[\s\S]*?\*\//g, "");
const styles = read("../src/styles.css");
const agent = read("../src/agent/agent.css");
const base = read("../src/ui/base.css");
const tokens = read("../src/ui/tokens.css");

function rule(css, selector) {
  const esc = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const m = css.match(new RegExp(`(?:^|\\})\\s*${esc}\\s*\\{([^}]*)\\}`));
  assert.ok(m, `no rule for ${selector}`);
  return m[1];
}

test(".sheet is positioned: it is the containing block of the page's absolute .vh, so they cannot stretch the document", () => {
  assert.match(rule(styles, ".sheet"), /position:\s*relative/);
  assert.match(rule(styles, ".vh"), /position:\s*absolute/);
  assert.match(rule(agent, ".ag"), /display:\s*flex/);
  assert.match(agent, /\.ag\s*\{\s*position:\s*relative/);
});

test("html, body and #root are not scrollable themselves", () => {
  for (const css of [styles, base]) {
    assert.doesNotMatch(css, /(?:^|\})\s*(?:html|body|#root)\b[^{]*\{[^}]*overflow:\s*(?:auto|scroll)/);
  }
});

test("one scrollbar approach: ::-webkit-scrollbar, no scrollbar-width/-color that would disable it", () => {
  assert.match(base, /::-webkit-scrollbar-thumb/);
  for (const css of [styles, agent, base]) assert.doesNotMatch(css, /scrollbar-(?:width|color)\s*:/);
});

test("scrollbar thumb tokens exist in the light, system-dark and data-theme dark blocks", () => {
  assert.equal([...tokens.matchAll(/--scroll:/g)].length, 3);
  assert.equal([...tokens.matchAll(/--scroll-hover:/g)].length, 3);
});
