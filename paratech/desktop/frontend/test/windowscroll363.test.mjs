// TASK-363: the whole window must not scroll. jsdom has no layout, so the rules that cause/prevent it are checked as CSS text.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync } from "node:fs";
import { fileURLToPath } from "node:url";

const strip = (s) => s.replace(/\/\*[\s\S]*?\*\//g, "");
const read = (p) => strip(readFileSync(new URL(p, import.meta.url), "utf8"));
const styles = read("../src/styles.css");
const agent = read("../src/agent/agent.css");
const base = read("../src/ui/base.css");
const scrollbar = read("../src/ui/scrollbar.css");
const tokens = read("../src/ui/tokens.css");

function walk(dir, out = []) {
  for (const e of readdirSync(dir, { withFileTypes: true })) {
    const p = `${dir}/${e.name}`;
    if (e.isDirectory()) walk(p, out);
    else out.push(p);
  }
  return out;
}
const srcFiles = walk(fileURLToPath(new URL("../src", import.meta.url)));

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

test("agent scrollers are positioned: the absolute .ag-vh inside them cannot stretch the panel", () => {
  assert.match(rule(agent, ".ag-vh"), /position:\s*absolute/);
  for (const sel of [".ag-thread", ".ag-plan-scroll"]) {
    assert.match(rule(agent, sel), /position:\s*relative/, sel);
  }
});

test("html, body and #root are not scrollable themselves", () => {
  for (const css of [styles, base]) {
    assert.doesNotMatch(css, /(?:^|\})\s*(?:html|body|#root)\b[^{]*\{[^}]*overflow:\s*(?:auto|scroll)/);
  }
});

test("one scrollbar approach: ::-webkit-scrollbar, no scrollbar-width/-color anywhere in src", () => {
  assert.match(scrollbar, /::-webkit-scrollbar-thumb/);
  assert.match(scrollbar, /::-webkit-scrollbar-thumb:active/);
  assert.match(scrollbar, /@media \(forced-colors: active\)/);
  const files = srcFiles.filter((f) => /\.(css|tsx)$/.test(f));
  assert.ok(files.length > 20);
  for (const f of files) {
    const text = strip(readFileSync(f, "utf8"));
    if (f.endsWith(".css")) assert.doesNotMatch(text, /scrollbar-(?:width|color)\s*:/, f);
    else assert.doesNotMatch(text, /scrollbarWidth|scrollbarColor/, f);
  }
});

test("the scrollbar css is loaded by the main window and by the popup", () => {
  for (const entry of ["../src/main.tsx", "../src/popup/main.tsx"]) {
    assert.match(readFileSync(new URL(entry, import.meta.url), "utf8"), /import "\.\.?\/(?:ui\/)?scrollbar\.css"/, entry);
  }
});

test("scrollbar thumb tokens exist in the light, system-dark and data-theme dark blocks", () => {
  assert.equal([...tokens.matchAll(/--scroll:/g)].length, 3);
  assert.equal([...tokens.matchAll(/--scroll-hover:/g)].length, 3);
});
