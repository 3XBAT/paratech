// BUG 2026-11-04: the recording title was one letter / one word per line with the agent panel open. jsdom has no
// layout, so the rules that prevent it are checked as CSS text.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const strip = (s) => s.replace(/\/\*[\s\S]*?\*\//g, "");
const read = (p) => strip(readFileSync(new URL(p, import.meta.url), "utf8"));
const styles = read("../src/styles.css");
const agent = read("../src/agent/agent.css");

test("@container app measures the sheet: the agent panel lives in the shell's padding, not in a grid column", () => {
  assert.match(agent, /\.shell\.with-agent \{[^}]*padding-right:\s*var\(--ag-w, 340px\)/);
  assert.match(agent, /\.shell\.with-agent \{[^}]*grid-template-columns:\s*64px minmax\(0, 1fr\) 0;/);
  assert.match(agent, /\.shell\.with-agent > \.ag \{[^}]*width:\s*var\(--ag-w, 340px\)/);
  assert.doesNotMatch(agent, /\.shell\.with-agent \{[^}]*grid-template-columns:[^;}]*var\(--ag-w/);
});

test("«Записи» rows: the title keeps at least 16rem and the fixed status and actions tracks stay slim", () => {
  assert.match(styles, /\.arows > \.row \{[^}]*grid-template-columns:\s*minmax\(min\(16rem, 100%\), 1fr\) var\(--arow-st\)/);
  const m = styles.match(/\.arows \{ --arow-st: ([\d.]+)rem; --arow-btn: ([\d.]+)rem;/);
  assert.ok(m, "tracks declared");
  assert.ok(Number(m[1]) <= 14 && Number(m[2]) <= 11, "status <= 14rem, button <= 11rem");
});
