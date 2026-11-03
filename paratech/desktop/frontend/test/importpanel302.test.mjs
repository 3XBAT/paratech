// Accessibility of «Импортировать запись» (TASK-302): the focus rule, aria-expanded/aria-controls, the mode switch.
// The harness is server rendering (no DOM, no effects), so the moving of the focus itself is tested through its pure rule
// (focusMove) and through the wiring in the sources; the owner checks the real focus on screen.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import server from "react-dom/server";
import { firstControlSelector, focusMove } from "../.test-build/disclosureFocus.js";
import { RecordingImportPanel } from "../.test-build/pages/RecordingImportPanel.js";
import { useDisclosureFocus } from "../.test-build/pages/useDisclosureFocus.js";
import { Button } from "../.test-build/ui/Button.js";
import { Card, Segmented } from "../.test-build/ui/controls.js";
import { importTexts } from "../.test-build/recordingImportView.js";

const { renderToStaticMarkup } = server;
const src = (p) => readFileSync(new URL(`../src/${p}`, import.meta.url), "utf8");

const body = { id: "", isBody: true };
const on = (id) => ({ id, isBody: false });

test("focusMove: opening goes into the panel, closing back to the opener only when the focus is lost or still there", () => {
  assert.equal(focusMove(false, true, on("opener"), "opener"), "panel");
  assert.equal(focusMove(false, true, null, "opener"), "panel");
  // Closed with the Close button: that button left with the panel, the focus is on the body.
  assert.equal(focusMove(true, false, body, "opener"), "opener");
  assert.equal(focusMove(true, false, null, "opener"), "opener");
  // Closed with the opener itself: it already has the focus.
  assert.equal(focusMove(true, false, on("opener"), "opener"), "opener");
  // The student moved on to another control (another form of the same row of buttons): the focus stays there.
  assert.equal(focusMove(true, false, on("other"), "opener"), "none");
});

test("focusMove: a render without a change never moves the focus (no stealing on re-renders)", () => {
  for (const open of [false, true]) {
    for (const active of [null, body, on("opener"), on("x")]) {
      assert.equal(focusMove(open, open, active, "opener"), "none");
    }
  }
});

test("the first enabled control takes the focus: buttons and fields, never a disabled one", () => {
  assert.match(firstControlSelector, /button:not\(:disabled\)/);
  assert.match(firstControlSelector, /input:not\(:disabled\)/);
  assert.match(firstControlSelector, /select:not\(:disabled\)/);
});

function Harness(props) {
  const f = useDisclosureFocus(props.open);
  return h(
    "div",
    null,
    h(Button, f.openerProps, "Открыть"),
    props.open ? h(Card, { id: f.panelId, label: "Панель" }, "тело") : null,
  );
}

test("the opener has aria-expanded; aria-controls points to the panel only while it is open", () => {
  const closed = renderToStaticMarkup(h(Harness, { open: false }));
  assert.match(closed, /aria-expanded="false"/);
  assert.doesNotMatch(closed, /aria-controls/);

  const open = renderToStaticMarkup(h(Harness, { open: true }));
  assert.match(open, /aria-expanded="true"/);
  const controls = /aria-controls="([^"]+)"/.exec(open);
  assert.ok(controls, "aria-controls is set while open");
  assert.ok(open.includes(`id="${controls[1]}"`), "the panel carries the id aria-controls points to");
  assert.notEqual(controls[1], /<button[^>]* id="([^"]+)"/.exec(open)?.[1], "the opener and the panel have different ids");
  assert.ok(/<button[^>]* id="[^"]+"/.test(open), "the opener has an id to get the focus back by");
});

test("the panel takes the id it is given", () => {
  const html = renderToStaticMarkup(
    h(RecordingImportPanel, {
      id: "import-panel-id",
      api: { importRecording: async () => ({}) },
      loadLessons: async () => [],
      onImported: () => undefined,
      onClose: () => undefined,
    }),
  );
  assert.match(html, /<div id="import-panel-id" class="card box rec-import-panel"/);
  // Idle: the mode switch is live.
  assert.doesNotMatch(/<div class="seg"[\s\S]*?<\/div>/.exec(html)[0], /disabled/);
});

test("Segmented: disabled switches every button off, the default and the existing usages stay live", () => {
  const options = [{ value: "a", label: "А" }, { value: "b", label: "Б" }];
  const seg = (extra = {}) => renderToStaticMarkup(h(Segmented, { label: "Режим", options, value: "a", onChange: () => undefined, ...extra }));
  assert.doesNotMatch(seg(), /disabled/);
  assert.doesNotMatch(seg({ disabled: false }), /disabled/);
  const off = seg({ disabled: true });
  assert.equal((off.match(/disabled=""/g) ?? []).length, 2);
  // The selection stays visible while it is off.
  assert.match(off, /aria-pressed="true"/);
});

test("the wiring: the panel switches the mode off while busy, and all three pages use the one hook", () => {
  assert.match(src("pages/RecordingImportPanel.tsx"), /<Segmented[^>]*disabled=\{busy\}/);
  for (const page of ["pages/RecordingsPage.tsx", "pages/SubjectPairsTab.tsx", "pages/KnowledgeBasePage.tsx"]) {
    const text = src(page);
    assert.match(text, /useDisclosureFocus\(importOpen\)/, page);
    assert.match(text, /\.\.\.importFocus\.openerProps/, page);
    assert.match(text, /id=\{importFocus\.panelId\}/, page);
    assert.doesNotMatch(text, /aria-expanded=\{importOpen\}/, `${page}: the hook gives aria-expanded`);
  }
});

test("RecordingsPage: one focus mechanism, the empty-state effect leaves a focus the import panel already placed", () => {
  const text = src("pages/RecordingsPage.tsx");
  // The hook is declared before the effect, so its move comes first.
  assert.ok(text.indexOf("useDisclosureFocus(importOpen)") < text.indexOf("const panelWasOpen"));
  assert.match(text, /if \(!target\?\.contains\(document\.activeElement\)\)/);
});

test("the open button keeps its text; no new visible text on the panel", () => {
  assert.equal(importTexts.open, "Импортировать запись");
});
