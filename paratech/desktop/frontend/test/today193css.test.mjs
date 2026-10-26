import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

const css = readFileSync(new URL("../src/pages/today.css", import.meta.url), "utf8");

test("«Сегодня»: the right column is wide enough for ordinary titles, still a container query", () => {
  const m = css.match(/@container \(min-width: (\d+)rem\) \{\s*\.today-cols \{ grid-template-columns: minmax\((\d+)rem, 1fr\) minmax\(0, clamp\((\d+)rem, (\d+)%, (\d+)rem\)\); \}/);
  assert.ok(m, "two columns inside the container query");
  const [, minContainer, leftMin, rightMin, , rightMax] = m.map(Number);
  assert.ok(rightMin >= 24 && rightMax > rightMin, "the right column is at least 24rem, not the old 22rem");
  assert.ok(leftMin + 1.5 + rightMin <= minContainer, "both columns fit the narrowest two-column container");
  assert.ok(css.includes(".today { container-type: inline-size; }"));
});

test("«Сегодня»: titles break only at spaces, anywhere only as a fallback", () => {
  assert.match(css, /\.today-t \{[^}]*overflow-wrap: anywhere;/);
  assert.ok(!/word-break:\s*break-all/.test(css));
});
