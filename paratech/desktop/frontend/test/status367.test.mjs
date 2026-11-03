// TASK-367: quiet statuses. A status is an icon (or the red recording dot) plus coloured text: no tinted fill, no coloured
// border, no double rim. The colour of a status only tints its text and icon; one accent stays for action and selection.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { createElement as h } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { StatusChip } from "../.test-build/ui/StatusChip.js";
import { Banner, ResultLine } from "../.test-build/ui/controls.js";
import { IconSprite } from "../.test-build/ui/Icon.js";
import { jobStatusView, recordingStatusView } from "../.test-build/statusPresentation.js";

const html = (el) => renderToStaticMarkup(el);
const read = (url) => readFileSync(url, "utf8");
const srcUrl = new URL("../src/", import.meta.url);

// Every stylesheet under src/ as [path relative to src, text without comments].
function cssFiles(dir, prefix = "") {
  const out = [];
  for (const name of readdirSync(dir)) {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) out.push(...cssFiles(path, `${prefix}${name}/`));
    else if (name.endsWith(".css")) out.push([`${prefix}${name}`, readFileSync(path, "utf8").replace(/\/\*[\s\S]*?\*\//g, "")]);
  }
  return out;
}
const files = cssFiles(fileURLToPath(srcUrl));

// Every rule of a stylesheet as { selector, decls }; @media wrappers are looked through.
function rules(css) {
  return [...css.matchAll(/([^{}]+)\{([^{}]*)\}/g)].map((m) => ({
    selector: m[1].trim(),
    decls: m[2].split(";").map((d) => d.trim()).filter((d) => d !== "").map((d) => [d.slice(0, d.indexOf(":")).trim(), d.slice(d.indexOf(":") + 1).trim()]),
  }));
}

// The classes of a status: chips, notes, marks, result lines, the kinds, the red and stop buttons, cards that wait.
const statusClass = /\.(chip|note|mark|result|badge|pill|status|danger|stop|s-(rec|run|wait|net|ok|fail|neutral)|ag-confirm|ag-ask|today-lesson)(?![\w-])/;
const surfaceProps = /^(background(-color|-image)?|border(-[a-z]+)*|box-shadow|outline(-color)?)$/;
// Semantic colours (red, green, orange, yellow, blue) and anything derived from the status colour.
const semantic = /--(rec|run|wait|net|ok)\b|--(rec|run|wait|net|ok)-[tb]\b|var\(--b\)|var\(--c\b|color-mix|currentColor|--accent/;

test("no status class has a semantic fill, border, ring or outline", () => {
  const bad = [];
  for (const [file, css] of files) {
    for (const { selector, decls } of rules(css)) {
      for (const one of selector.split(",").map((s) => s.trim())) {
        if (!statusClass.test(one)) continue;
        for (const [prop, value] of decls) {
          if (surfaceProps.test(prop) && semantic.test(value)) bad.push(`${file}: ${one} { ${prop}: ${value} }`);
        }
      }
    }
  }
  assert.deepEqual(bad, []);
});

test("the tinted status tokens are gone; the remaining tints are highlights and the source letter only", () => {
  const dead = /--(rec-b|ok-b|net-t|net-b)\b|var\(--b\)/;
  const tint = /--(run-b|wait-b)\b/;
  const left = [];
  for (const [file, css] of files) {
    for (const { selector, decls } of rules(css)) {
      const text = `${selector} { ${decls.map((d) => d.join(": ")).join("; ")} }`;
      if (dead.test(text) && file !== "ui/tokens.css") left.push(`${file}: ${text}`);
      // A tinted background is allowed on a <mark> highlight only (a search hit, the current paragraph).
      if (tint.test(text) && file !== "ui/tokens.css" && !/(^|[\s>])mark(\.cur)?(?![\w-])/.test(selector)) left.push(`${file}: ${text}`);
    }
  }
  assert.deepEqual(left, []);
  assert.doesNotMatch(read(new URL("ui/tokens.css", srcUrl)), /--(rec-b|ok-b|net-t|net-b):/);
});

test(".chip is text and icon only: no padding box, border, background, ring or dashed variant", () => {
  const css = read(new URL("ui/components.css", srcUrl));
  const chip = css.match(/\n\.chip \{([^}]*)\}/)?.[1] ?? "";
  assert.notEqual(chip, "", ".chip rule");
  assert.doesNotMatch(chip, /border|background|box-shadow|outline/);
  assert.match(chip, /color: var\(--c\);/);
  assert.doesNotMatch(css, /\.chip\.s-(ok|net|fail)/, "no per-kind border or double ring");
  assert.doesNotMatch(css, /--b:/, "no tint variable per kind");
  // Colours of the kinds: text colours only.
  assert.match(css, /\.s-rec, \.s-fail \{ --c: var\(--rec-t\); \}/);
  assert.match(css, /\.s-ok \{ --c: var\(--ok-t\); \}/);
  assert.match(css, /\.s-net, \.s-neutral \{ --c: var\(--muted\); \}/);
  // Forced colours: a status has no box to outline; its icon and word stay.
  const forced = css.slice(css.indexOf("@media (forced-colors: active)"));
  assert.doesNotMatch(forced.split("\n").find((l) => l.includes("border: 1px solid CanvasText")) ?? "", /\.chip/);
});

test("«Остановить» is a normal button; only its square is red", () => {
  const css = read(new URL("ui/components.css", srcUrl));
  assert.match(css, /\.btn\.stop \.i \{ color: var\(--rec-t\); fill: currentColor; \}/);
  assert.doesNotMatch(css, /\.btn\.danger \{[^}]*(box-shadow|border|background)/, "a red button has no red ring either");
  const popupCss = read(new URL("popup/popup.css", srcUrl));
  assert.match(popupCss, /\.btn\.stop \.i \{ color: var\(--rec-t\); fill: currentColor; \}/);
  assert.doesNotMatch(popupCss, /\.btn\.danger \{[^}]*border/);
  for (const file of ["popup/Popup.tsx", "pages/RecordingRow.tsx"]) {
    const code = read(new URL(file, srcUrl));
    assert.match(code, /className="stop"\s+icon="stop"/, file);
    assert.doesNotMatch(code, /variant="danger"\s+icon="stop"/, file);
  }
});

test("StatusChip: the recording dot with the word, the others an icon and the word", () => {
  const rec = html(h(StatusChip, { kind: "rec", icon: null }, "Идёт запись", h("span", { className: "num" }, " · 0:03:48")));
  assert.equal(rec, '<span class="chip s-rec"><span class="dot" aria-hidden="true"></span><span class="chip-text">Идёт запись<span class="num"> · 0:03:48</span></span></span>');

  const ok = html(h(StatusChip, { kind: "ok", icon: "ok" }, "Конспект готов"));
  assert.equal(ok, '<span class="chip s-ok"><svg class="i" aria-hidden="true" focusable="false"><use href="#i-ok"></use></svg><span class="chip-text">Конспект готов</span></span>');

  const run = html(h(StatusChip, { kind: "run", icon: "wave" }, "Пишется конспект"));
  assert.match(run, /^<span class="chip s-run"><svg[^>]*><use href="#i-wave"><\/use><\/svg><span class="chip-text">Пишется конспект<\/span><\/span>$/);
  for (const out of [rec, ok, run]) {
    assert.doesNotMatch(out, /style=|background|border/);
  }
});

test("every known status of a recording and a job has a word and an icon (or the dot): never colour alone", () => {
  const recordings = ["STARTING", "RECORDING", "STOPPING", "FINALIZING", "COMPLETED", "FAILED", "RECOVERY_REQUIRED"];
  const jobs = ["QUEUED", "RUNNING", "COMPLETED", "RETRY_WAIT", "CANCELLED", "FAILED", "WAITING_FOR_NETWORK", "WAITING_FOR_QUOTA", "WAITING_FOR_POWER"];
  const views = [
    ...recordings.map((status) => recordingStatusView({ status, startedAt: "2026-11-03T10:00:00Z", durationSeconds: 0, hasDuration: false }, Date.parse("2026-11-03T10:03:48Z"))),
    ...jobs.map((status) => jobStatusView({ type: "summarize", status, waiting: false, progressPercent: 0 })),
  ];
  for (const v of views) {
    assert.notEqual(v.label, "", JSON.stringify(v));
    assert.ok(v.kind === "rec" || v.icon !== null, `${v.label}: icon or dot`);
  }
  // «● Идёт запись · 0:03:48» and «✓ Конспект готов».
  const recording = recordingStatusView({ status: "RECORDING", startedAt: "2026-11-03T10:00:00Z", durationSeconds: 0, hasDuration: false }, Date.parse("2026-11-03T10:03:48Z"));
  assert.deepEqual([recording.kind, recording.label, recording.value], ["rec", "Идёт запись", "0:03:48"]);
  const done = jobStatusView({ type: "summarize", status: "COMPLETED", waiting: false, progressPercent: 0 });
  assert.deepEqual([done.kind, done.icon, done.label], ["ok", "ok", "Конспект готов"]);
});

test("the source-picker marks and the banners are plain: coloured words and icon, no chip box", () => {
  const picker = read(new URL("sourcePicker/sourcePicker.css", srcUrl));
  const mark = picker.match(/\.picker \.mark \{([^}]*)\}/)?.[1] ?? "";
  assert.notEqual(mark, "");
  assert.doesNotMatch(mark, /border|background|padding/);
  assert.match(picker, /\.picker \.mark\.sounding \{[^}]*color: var\(--ok-t\);/);
  assert.match(picker, /\.picker \.mark\.muted, \.picker \.mark\.gone \{ color: var\(--rec-t\); \}/);
  const note = read(new URL("ui/components.css", srcUrl)).match(/\n\.note \{([^}]*)\}/)?.[1] ?? "";
  // Review: on --raised (not --fill) accent text and the progress track both stay readable inside a banner.
  assert.match(note, /background: var\(--raised\)/);
  assert.match(note, /box-shadow: 0 0 0 1px var\(--edge\)/);
  assert.doesNotMatch(note, /border:|color-mix|background: var\(--fill\)/);
  const popupNote = read(new URL("popup/popup.css", srcUrl)).match(/\n\.note \{([^}]*)\}/)?.[1] ?? "";
  assert.match(popupNote, /background: var\(--raised\)/);
  assert.doesNotMatch(popupNote, /--panel-2|color-mix/);
  const live = read(new URL("pages/today.css", srcUrl));
  assert.doesNotMatch(live, /today-lesson\.live/, "no red tint behind the live lesson");
});

// ---- review fixes ----

test("a failure banner and result line have their own glyph (a circle with a cross), a wait keeps the triangle", () => {
  const iconOf = (el) => html(el).match(/<use href="#i-([a-z]+)">/)[1];
  assert.equal(iconOf(h(Banner, { kind: "fail", role: "alert" }, "Не вышло")), "error");
  assert.equal(iconOf(h(Banner, { kind: "wait" }, "Ждём")), "warn");
  assert.equal(iconOf(h(Banner, { kind: "net" }, "Нет сети")), "wave");
  assert.equal(iconOf(h(ResultLine, { kind: "fail" }, "Не вышло")), "error");
  assert.equal(iconOf(h(ResultLine, { kind: "wait" }, "Ждём")), "warn");
  assert.equal(iconOf(h(ResultLine, { kind: "ok" }, "Готово")), "ok");
  // The fail text stays plain --text in a banner; only the icon is red.
  assert.match(html(h(Banner, { kind: "fail" }, "Не вышло")), /^<div class="note s-fail"><svg/);
  const sprite = html(h(IconSprite));
  for (const id of ["i-error", "i-help", "i-warn"]) {
    assert.equal(sprite.split(`id="${id}"`).length - 1, 1, id);
  }
  assert.notEqual(sprite.match(/<symbol id="i-error"[^]*?<\/symbol>/)[0], sprite.match(/<symbol id="i-warn"[^]*?<\/symbol>/)[0]);
});

test("in-progress statuses are muted (icon and verb carry them), never the orange of the accent", () => {
  const css = read(new URL("ui/components.css", srcUrl));
  assert.match(css, /\.s-run, \.s-net, \.s-neutral \{ --c: var\(--muted\); \}/);
  assert.doesNotMatch(css, /\.s-run \{/);
  // --run-t is left to highlights (<mark>, a diff insertion); no status text uses it.
  for (const [file, text] of files) {
    for (const { selector, decls } of rules(text)) {
      if (/(^|[\s,])\.(chip|s-run|result|note)(?![\w-])/.test(selector) && decls.some(([, v]) => /--run-t/.test(v))) {
        assert.fail(`${file}: ${selector} uses --run-t`);
      }
    }
  }
});

test("an urgent deadline is amber (wait) with the clock; the overdue assignment in a reply is the failure red", () => {
  const code = read(new URL("pages/TodayParts.tsx", srcUrl));
  assert.match(code, /m\.overdue \? "fail" : m\.urgent \? "wait" : "neutral"/);
  assert.doesNotMatch(code, /m\.urgent \? "run"/);
  assert.match(read(new URL("agent/agent.css", srcUrl)), /\.ag-asg-late \{ color: var\(--rec-t\);/);
});

test("the question cards: a plain-text caption with a question icon, not the accent colour, still no frame", () => {
  const css = read(new URL("agent/agent.css", srcUrl));
  const cap = css.match(/\n\.ag-ask > \.ag-card-h \{([^}]*)\}/)[1];
  assert.match(cap, /color: var\(--text\)/);
  assert.match(cap, /font-weight: 600/);
  assert.doesNotMatch(cap, /accent/);
  assert.doesNotMatch(css, /\n\.ag-ask \{/, "no frame for the question card");
  for (const f of ["agent/AgentCards.tsx", "agent/TurnkeyCards.tsx"]) {
    assert.match(read(new URL(f, srcUrl)), /<section className="ag-card ag-ask">\s*<h3 className="ag-card-h">\s*<Icon name="help" \/>/, f);
  }
});

test("«Сегодня»: the problem chip button shows it is pressable (underline on hover and focus, a focus ring)", () => {
  const css = read(new URL("pages/today.css", srcUrl));
  assert.match(css, /\.today-problem:hover \.chip-text, \.today-problem:focus-visible \.chip-text \{ text-decoration: underline; \}/);
  assert.match(css, /\.today-problem:focus-visible \{ outline: 2px solid var\(--accent\);/);
});

test("a busy round icon button spins the way the retry arrow points (counter-clockwise)", () => {
  const css = read(new URL("ui/components.css", srcUrl));
  assert.match(css, /\.btn\.icon-btn\[aria-busy="true"\] \.i \{ animation: icon-spin 1s linear infinite; \}/);
  assert.match(css, /\.btn\.icon-btn\[aria-busy="true"\] \.i:has\(use\[href="#i-retry"\]\) \{ animation-direction: reverse; \}/);
});
