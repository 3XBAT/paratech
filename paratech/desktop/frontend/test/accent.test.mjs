import { test } from "node:test";
import assert from "node:assert/strict";
import { accents, applyAccent, defaultAccent, parseAccent, readAccent, writeAccent, accentKey } from "../.test-build/ui/accent.js";

function root() {
  const attrs = new Map();
  const props = new Map();
  return {
    attrs,
    props,
    setAttribute: (n, v) => attrs.set(n, v),
    removeAttribute: (n) => attrs.delete(n),
    style: { setProperty: (n, v) => props.set(n, v), removeProperty: (n) => props.delete(n) },
  };
}

test("an unknown stored accent is the default", () => {
  assert.equal(parseAccent("blue").id, "blue");
  assert.equal(parseAccent("nope"), defaultAccent);
  assert.equal(parseAccent(null), defaultAccent);
  assert.equal(readAccent(null), defaultAccent);
  assert.equal(readAccent({ getItem: () => { throw new Error("denied"); }, setItem: () => undefined }), defaultAccent);
});

test("the default accent clears the variable, another one sets it; a refusing storage is tolerated", () => {
  const r = root();
  applyAccent(r, accents[2]);
  assert.equal(r.attrs.get("data-accent"), "green");
  assert.equal(r.props.get("--acc"), "#3E8A5A");
  applyAccent(r, defaultAccent);
  assert.equal(r.attrs.has("data-accent"), false);
  assert.equal(r.props.has("--acc"), false);
  const saved = [];
  writeAccent({ getItem: () => null, setItem: (k, v) => saved.push([k, v]) }, accents[1]);
  assert.deepEqual(saved, [[accentKey, "blue"]]);
  writeAccent({ getItem: () => null, setItem: () => { throw new Error("quota"); } }, accents[1]);
});

// The text accent of accent.css (78 % swatch + black on light, 60 % + white on dark) must reach 4.5:1 for every swatch.
function rgb(hex) {
  return [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16));
}
function mix(a, b, pa) {
  return a.map((v, i) => v * pa + b[i] * (1 - pa));
}
function lum(c) {
  const [r, g, b] = c.map((v) => {
    const x = v / 255;
    return x <= 0.04045 ? x / 12.92 : ((x + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}
function ratio(a, b) {
  const [hi, lo] = [lum(a), lum(b)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
}

test("every accent keeps 4.5:1 as text on the light and dark surfaces", () => {
  const light = ["#EBEBE7", "#F4F4F1", "#FFFFFF"].map(rgb);
  const dark = ["#121315", "#18191B", "#212225"].map(rgb);
  for (const a of accents) {
    const onLight = mix(rgb(a.hex), [0, 0, 0], 0.78);
    const onDark = mix(rgb(a.hex), [255, 255, 255], 0.6);
    for (const bg of light) {
      assert.ok(ratio(onLight, bg) >= 4.5, `${a.id} on light ${ratio(onLight, bg).toFixed(2)}`);
    }
    for (const bg of dark) {
      assert.ok(ratio(onDark, bg) >= 4.5, `${a.id} on dark ${ratio(onDark, bg).toFixed(2)}`);
    }
  }
});
