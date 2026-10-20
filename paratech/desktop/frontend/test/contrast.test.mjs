// WCAG 2.x contrast of the palette in src/ui/tokens.css, the single source of the colours (both themes).
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const css = readFileSync(new URL("../src/ui/tokens.css", import.meta.url), "utf8").replace(/\/\*[\s\S]*?\*\//g, "");

function block(re, what) {
  const m = css.match(re);
  assert.ok(m, `tokens.css: no ${what} block`);
  const tokens = {};
  for (const [, name, value] of m[1].matchAll(/--([\w-]+)\s*:\s*([^;]+);/g)) {
    tokens[name] = value.trim();
  }
  return tokens;
}

const light = block(/(?:^|\n):root\s*\{([^}]*)\}/, "light :root");
const systemDark = block(/:root:not\(\[data-theme="light"\]\)\s*\{([^}]*)\}/, "prefers-color-scheme dark");
const dark = block(/:root\[data-theme="dark"\]\s*\{([^}]*)\}/, "data-theme dark");
const themes = { light, dark };

const colours = [
  "bg", "panel", "raised", "fill", "fill-2", "panel-2", "line", "control", "text", "muted", "accent", "accent-fill", "prim", "on-prim", "rec",
  "rec-t", "rec-b", "run-t", "run-b", "wait-t", "wait-b", "net-t", "net-b", "ok-t", "ok-b", "neu-t", "neu-b",
];

function hex(theme, name) {
  const v = themes[theme][name];
  assert.ok(v !== undefined, `${theme}: --${name} is missing`);
  assert.match(v, /^#[0-9a-f]{6}$/i, `${theme}: --${name} is not #RRGGBB`);
  return v;
}

function luminance(colour) {
  const [r, g, b] = [1, 3, 5].map((i) => {
    const c = parseInt(colour.slice(i, i + 2), 16) / 255;
    return c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

function ratio(a, b) {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
}

function check(theme, fg, bg, min) {
  const r = ratio(hex(theme, fg), hex(theme, bg));
  assert.ok(r >= min, `${theme}: --${fg} on --${bg} is ${r.toFixed(2)}:1, needs ${min}:1`);
}

test("the formula matches known WCAG values", () => {
  assert.equal(ratio("#000000", "#FFFFFF").toFixed(1), "21.0");
  assert.equal(ratio("#777777", "#FFFFFF").toFixed(2), "4.48");
  assert.equal(ratio("#FFFFFF", "#FFFFFF"), 1);
});

test("both themes define every colour; the two dark blocks are the same", () => {
  for (const theme of Object.keys(themes)) {
    for (const name of colours) {
      hex(theme, name);
    }
  }
  // Hairlines and outlines are translucent, so they sit on any surface.
  for (const theme of Object.keys(themes)) {
    for (const name of ["sep", "edge", "btn-line"]) {
      assert.match(themes[theme][name] ?? "", /^rgba\(\d+, \d+, \d+, \.\d+\)$/, `${theme}: --${name}`);
    }
  }
  assert.deepEqual(systemDark, dark);
  assert.notEqual(light.bg, dark.bg);
  assert.match(light.font, /^"Golos Text", "Segoe UI Variable Text", "Segoe UI", sans-serif$/);
});

test("text and controls have enough contrast in both themes", () => {
  for (const theme of Object.keys(themes)) {
    for (const surface of ["bg", "panel", "raised"]) {
      for (const fg of ["text", "muted", "accent"]) {
        check(theme, fg, surface, 4.5);
      }
      check(theme, "control", surface, 3); // non-text UI (WCAG 1.4.11)
    }
    check(theme, "on-prim", "prim", 4.5);
    for (const chip of ["rec", "run", "wait", "net", "ok", "neu"]) {
      check(theme, `${chip}-t`, `${chip}-b`, 4.5);
      check(theme, "text", `${chip}-b`, 4.5); // banners: plain text on the tint
    }
    // Hovered ghost buttons of the popup are on --panel-2.
    for (const fg of ["muted", "accent"]) {
      check(theme, fg, "panel-2", 4.5);
    }
    // Segment tracks, hovered rows and buttons are on --fill, pressed ones on --fill-2. Accent text never goes there
    // (4.37:1 in light): a hovered link-like button turns to --text.
    for (const fg of ["text", "muted"]) {
      check(theme, fg, "fill", 4.5);
    }
    check(theme, "text", "fill-2", 4.5);
    check(theme, "muted", "fill-2", 4.5); // a pressed segment or button keeps its muted label readable
    // Non-text parts (WCAG 1.4.11): the progress fill on its track, a switch outline on its --fill track.
    check(theme, "accent-fill", "fill", 3);
    check(theme, "control", "fill", 3);
    // Error and warning texts outside a chip (danger buttons, problems, field errors, «Может работать медленно»).
    for (const fg of ["rec-t", "wait-t"]) {
      for (const surface of ["raised", "panel"]) {
        check(theme, fg, surface, 4.5);
      }
    }
  }
});

// TASK-100: the surfaces of both themes are the strict mockup's (docs/design/concept-strict.html, the token table).
test("the surfaces, hairlines and shadows are the strict mockup's in light and dark", () => {
  const mockup = {
    light: {
      bg: "#EBEBE7", panel: "#F4F4F1", raised: "#FFFFFF", fill: "#E8E8E4", "fill-2": "#DFDFDA",
      sep: "rgba(26, 27, 30, .10)", edge: "rgba(26, 27, 30, .07)", "btn-line": "rgba(26, 27, 30, .20)",
      "sh-thumb": "0 1px 2px rgba(20, 20, 24, .10), 0 0 0 .5px rgba(20, 20, 24, .06)",
    },
    dark: {
      bg: "#121315", panel: "#18191B", raised: "#212225", fill: "#2A2B2F", "fill-2": "#323338",
      sep: "rgba(236, 235, 231, .09)", edge: "rgba(236, 235, 231, .06)", "btn-line": "rgba(236, 235, 231, .16)",
      "sh-btn": "0 .5px 1px rgba(0, 0, 0, .4)", "sh-thumb": "0 1px 2px rgba(0, 0, 0, .45)",
    },
  };
  for (const [theme, tokens] of Object.entries(mockup)) {
    for (const [name, value] of Object.entries(tokens)) {
      assert.equal(themes[theme][name], value, `${theme}: --${name}`);
    }
    // Groups and the segment thumb lie on the sheet: lighter than it in both themes, as in the mockup.
    assert.ok(luminance(hex(theme, "raised")) > luminance(hex(theme, "panel")), `${theme}: groups stand out of the sheet`);
    assert.ok(luminance(hex(theme, "panel")) !== luminance(hex(theme, "bg")), `${theme}: the sheet differs from the window`);
    assert.match(themes[theme].scrim ?? "", /^rgba\(\d+, \d+, \d+, \.\d+\)$/, `${theme}: --scrim`);
  }
});
