import { test } from "node:test";
import assert from "node:assert/strict";
import {
  clampWidth, closeBelow, defaultWidth, dragPref, effectiveWidth, keyPref, keyStep, maxWidth, maxWidthLimit, minWidth,
  openKey, readPref, togglePref, widthKey, writePref,
} from "../.test-build/agent/panelWidth.js";

const vp = 1200;
const closed = { open: false, width: 340 };
const open = { open: true, width: 340 };
const memory = (init = {}) => {
  const m = new Map(Object.entries(init));
  return { m, getItem: (k) => m.get(k) ?? null, setItem: (k, v) => m.set(k, v) };
};

test("dragging left from a closed panel opens it and follows the pointer", () => {
  assert.deepEqual(dragPref(closed, vp - 400, vp), { open: true, width: 400 });
  assert.deepEqual(dragPref({ open: true, width: 400 }, vp - 450, vp), { open: true, width: 450 });
});

test("dragging right past the threshold closes the panel and keeps its width", () => {
  assert.deepEqual(dragPref(open, vp - (closeBelow - 1), vp), { open: false, width: 340 });
  assert.equal(dragPref(open, vp - closeBelow, vp).open, true);
  assert.deepEqual(dragPref(closed, vp - 100, vp), closed);
});

test("width is clamped to the minimum and to what leaves the main screen room", () => {
  assert.equal(dragPref(closed, vp - closeBelow, vp).width, minWidth);
  assert.equal(dragPref(closed, 0, vp).width, maxWidth(vp));
  assert.equal(maxWidth(5000), maxWidthLimit);
  assert.equal(maxWidth(700), minWidth);
  assert.equal(maxWidth(1000), 1000 - 64 - 420);
  assert.equal(clampWidth(NaN, vp), defaultWidth);
  assert.equal(effectiveWidth({ open: true, width: 600 }, 900), 416);
  assert.equal(effectiveWidth(open, vp), 340);
});

test("a temporarily narrow window narrows only the drawn width, the stored one survives", () => {
  const s = memory();
  const pref = { open: true, width: 600 };
  writePref(s, pref);
  assert.equal(effectiveWidth(pref, 900), 416);
  assert.equal(effectiveWidth(pref, 700), minWidth);
  assert.deepEqual(readPref(s, 700), pref);
  assert.equal(effectiveWidth(readPref(s, 700), 1400), 600);
});

test("the stored width and open state come back, bad values give the default", () => {
  const s = memory();
  writePref(s, { open: false, width: 420 });
  assert.equal(s.m.get(widthKey), "420");
  assert.equal(s.m.get(openKey), "0");
  assert.deepEqual(readPref(s, vp), { open: false, width: 420 });
  assert.deepEqual(readPref(memory(), vp), { open: true, width: defaultWidth });
  assert.deepEqual(readPref(memory({ [widthKey]: "abc" }), vp), { open: true, width: defaultWidth });
  assert.equal(readPref(memory({ [widthKey]: "9999" }), vp).width, maxWidth(vp));
  assert.deepEqual(readPref(null, vp), { open: true, width: defaultWidth });
  const broken = { getItem: () => { throw new Error("denied"); }, setItem: () => { throw new Error("denied"); } };
  assert.deepEqual(readPref(broken, vp), { open: true, width: defaultWidth });
  writePref(broken, open);
});

test("arrow keys change the width, Left opens a closed panel, Right narrows until it closes", () => {
  assert.equal(keyPref(open, "ArrowLeft", vp).width, 340 + keyStep);
  assert.equal(keyPref(open, "ArrowRight", vp).width, 340 - keyStep);
  assert.deepEqual(keyPref(closed, "ArrowLeft", vp), { open: true, width: 340 });
  assert.deepEqual(keyPref({ open: false, width: 500 }, "ArrowLeft", vp), { open: true, width: 500 });
  assert.deepEqual(keyPref(closed, "ArrowRight", vp), closed);
  assert.deepEqual(keyPref({ open: true, width: minWidth }, "ArrowRight", vp), { open: false, width: minWidth });
  assert.equal(keyPref({ open: true, width: maxWidth(vp) }, "ArrowLeft", vp).width, maxWidth(vp));
  assert.equal(keyPref(open, "Home", vp).width, minWidth);
  assert.equal(keyPref(open, "End", vp).width, maxWidth(vp));
  assert.equal(keyPref(open, "Escape", vp).open, false);
  assert.equal(keyPref(open, "a", vp), null);
});

test("double click toggles and keeps the width", () => {
  assert.deepEqual(togglePref(open), { open: false, width: 340 });
  assert.deepEqual(togglePref(closed), { open: true, width: 340 });
});
