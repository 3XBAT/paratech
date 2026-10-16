// Theme choice (src/ui/theme.ts). Runs on the modules compiled by `npm run test`.
import { test } from "node:test";
import assert from "node:assert/strict";
import { applyTheme, parseTheme, readTheme, themeChoices, themeKey, writeTheme } from "../.test-build/ui/theme.js";

function memory(initial = {}) {
  const data = new Map(Object.entries(initial));
  return { data, getItem: (k) => (data.has(k) ? data.get(k) : null), setItem: (k, v) => data.set(k, v) };
}

function root() {
  const attrs = new Map();
  return {
    attrs,
    setAttribute: (n, v) => attrs.set(n, v),
    removeAttribute: (n) => attrs.delete(n),
  };
}

test("an unknown or missing value is the system theme", () => {
  assert.deepEqual(themeChoices, ["system", "light", "dark"]);
  assert.equal(themeKey, "ui.theme");
  for (const v of ["light", "dark", "system"]) {
    assert.equal(parseTheme(v), v);
  }
  for (const v of [null, undefined, "", "Dark", "auto", 1, {}]) {
    assert.equal(parseTheme(v), "system", String(v));
  }
});

test("the choice is read and saved under its key; a failing storage gives the system theme", () => {
  const s = memory({ [themeKey]: "dark" });
  assert.equal(readTheme(s), "dark");
  writeTheme(s, "light");
  assert.equal(s.data.get(themeKey), "light");
  assert.equal(readTheme(memory({ [themeKey]: "neon" })), "system");
  assert.equal(readTheme(memory()), "system");
  assert.equal(readTheme(null), "system");
  const broken = { getItem: () => { throw new Error("denied"); }, setItem: () => { throw new Error("quota"); } };
  assert.equal(readTheme(broken), "system");
  assert.doesNotThrow(() => writeTheme(broken, "dark"));
});

test("a forced theme sets data-theme, the system theme removes it", () => {
  const r = root();
  applyTheme(r, "dark");
  assert.equal(r.attrs.get("data-theme"), "dark");
  applyTheme(r, "light");
  assert.equal(r.attrs.get("data-theme"), "light");
  applyTheme(r, "system");
  assert.equal(r.attrs.has("data-theme"), false);
});
