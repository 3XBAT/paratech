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
  assert.match(note, /background: var\(--fill\)/);
  assert.doesNotMatch(note, /border:|color-mix/);
  const live = read(new URL("pages/today.css", srcUrl));
  assert.doesNotMatch(live, /today-lesson\.live/, "no red tint behind the live lesson");
});
