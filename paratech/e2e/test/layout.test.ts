// Unit tests of the layout checker (TASK-376). The pure part (evaluateLayout, colours, formatting, the watch with a fake
// page) runs without a browser. The probe is tested on static HTML in the INSTALLED Microsoft Edge, headless
// (`chromium.launch({channel: "msedge"})`: no download, no window); those tests skip with a reason when it cannot start.
import assert from "node:assert/strict";
import { EventEmitter } from "node:events";
import { after, before, describe, test, type TestContext } from "node:test";

import { type Browser, chromium, type Page } from "@playwright/test";

import {
  capPerRule,
  checkAgentThread,
  checkLayout,
  colorAlpha,
  drainConsole,
  emptySnapshot,
  evaluateAgentThread,
  evaluateLayout,
  formatViolations,
  layoutSession,
  layoutViolations,
  type LayoutRule,
  type LayoutSnapshot,
  type LayoutViolation,
  probeLayoutSource,
  resolveOptions,
  watchPage,
} from "../src/checks/layout.js";
import { buildLayoutIcs, icsUtc, layoutLessons, layoutSubjects } from "../src/checks/ics.js";
import { requestRecord, safeUrl } from "../src/checks/watch.js";

const rules = (v: LayoutViolation[]): LayoutRule[] => [...new Set(v.map((x) => x.rule))];

function snap(patch: Partial<LayoutSnapshot>): LayoutSnapshot {
  return { ...emptySnapshot(), ...patch };
}

describe("colorAlpha", () => {
  test("reads the forms Chrome returns", () => {
    assert.equal(colorAlpha("rgba(0, 0, 0, 0)"), 0);
    assert.equal(colorAlpha("transparent"), 0);
    assert.equal(colorAlpha("rgb(10, 20, 30)"), 1);
    assert.equal(colorAlpha("rgba(10, 20, 30, 0.5)"), 0.5);
    assert.equal(colorAlpha("rgb(10 20 30 / 0)"), 0);
    assert.equal(colorAlpha("rgb(10 20 30 / 50%)"), 0.5);
    assert.equal(colorAlpha("color(srgb 0.1 0.2 0.3 / 0)"), 0);
    assert.equal(colorAlpha("color(srgb 0.1 0.2 0.3)"), 1);
    assert.equal(colorAlpha("oklch(0.5 0.1 200)"), 1);
  });
  test("a text that is not a colour is unknown, so it never counts as transparent", () => {
    assert.equal(colorAlpha("var(--x)"), undefined);
    assert.equal(colorAlpha(""), undefined);
    assert.equal(colorAlpha("red"), undefined);
  });
});

describe("evaluateLayout (pure)", () => {
  test("an empty snapshot passes", () => {
    assert.deepEqual(evaluateLayout(emptySnapshot()), []);
  });

  test("window-scroll: over the window by more than the tolerance fails, by one pixel passes", () => {
    const over = snap({ doc: { scrollHeight: 740, scrollWidth: 1000, bodyScrollWidth: 1000, scrollTop: 0 }, lowest: { el: "div.footer", bottom: 740 } });
    const v = evaluateLayout(over);
    assert.deepEqual(rules(v), ["window-scroll"]);
    assert.match(v[0]?.detail ?? "", /scrollHeight 740 > innerHeight 700/);
    assert.match(v[0]?.detail ?? "", /div\.footer/);
    const edge = snap({ doc: { scrollHeight: 701, scrollWidth: 1000, bodyScrollWidth: 1000, scrollTop: 0 } });
    assert.deepEqual(evaluateLayout(edge), [], "1 px of rounding at a fractional devicePixelRatio is tolerated");
    assert.deepEqual(rules(evaluateLayout(snap({ doc: { scrollHeight: 702, scrollWidth: 1000, bodyScrollWidth: 1000, scrollTop: 0 } }))), ["window-scroll"]);
    assert.deepEqual(evaluateLayout(over, { rules: { "window-scroll": false } }), []);
  });

  test("overflow-x: the body width and each offender", () => {
    const v = evaluateLayout(
      snap({
        doc: { scrollHeight: 700, scrollWidth: 1300, bodyScrollWidth: 1300, scrollTop: 0 },
        overflowX: [{ el: "table.wide", left: 10, right: 1300, width: 1290 }],
      }),
    );
    assert.deepEqual(rules(v), ["overflow-x"]);
    assert.equal(v.length, 2);
    assert.ok(v.some((x) => x.element === "body"));
    assert.ok(v.some((x) => x.element === "table.wide" && /1300/.test(x.detail)));
    assert.deepEqual(evaluateLayout(snap({ doc: { scrollHeight: 700, scrollWidth: 1001, bodyScrollWidth: 1001, scrollTop: 0 } })), []);
  });

  test("icon-button: needs aria-label and a tooltip (title or tip text)", () => {
    const b = (label: string, title: string, tip: string) => ({ el: `button[aria-label="${label}"]`, label, title, tip });
    const v = evaluateLayout(
      snap({
        iconButtons: [b("Закрыть", "Закрыть", ""), b("Сегодня", "", "Сегодня"), b("Удалить", "", ""), b("", "", ""), b("", "Только title", "")],
      }),
    );
    assert.deepEqual(rules(v), ["icon-button"]);
    assert.equal(v.length, 3);
    assert.match(v[0]?.detail ?? "", /нет подсказки/);
    assert.doesNotMatch(v[0]?.detail ?? "", /нет aria-label/);
    assert.match(v[1]?.detail ?? "", /нет aria-label.*нет подсказки/);
    assert.match(v[2]?.detail ?? "", /нет aria-label/);
    assert.doesNotMatch(v[2]?.detail ?? "", /нет подсказки/);
  });

  const quiet = {
    el: "span.chip",
    status: "ok",
    backgroundColor: "rgba(0, 0, 0, 0)",
    backgroundImage: "none",
    boxShadow: "none",
    outlineStyle: "none",
    outlineWidth: "0px",
    focused: false,
    borders: (["top", "right", "bottom", "left"] as const).map((side) => ({ side, style: "none", width: "0px", color: "rgb(0, 0, 0)" })),
  };

  test("status-quiet: transparent, no border, no shadow, no outline passes", () => {
    assert.deepEqual(evaluateLayout(snap({ statuses: [quiet] })), []);
    // a border with zero width or a transparent colour is not a border
    const b = quiet.borders.map((x) => ({ ...x, style: "solid", width: "1px", color: "rgba(255, 0, 0, 0)" }));
    assert.deepEqual(evaluateLayout(snap({ statuses: [{ ...quiet, borders: b }] })), []);
  });

  test("status-quiet: a coloured background, border, shadow, image or resting outline fails, with the values", () => {
    const cases: Array<[Partial<typeof quiet>, RegExp]> = [
      [{ backgroundColor: "rgb(255, 221, 221)" }, /background-color rgb\(255, 221, 221\)/],
      [{ backgroundColor: "rgba(255, 0, 0, 0.1)" }, /background-color rgba\(255, 0, 0, 0\.1\)/],
      [{ backgroundImage: "linear-gradient(red, blue)" }, /background-image linear-gradient/],
      [{ boxShadow: "rgb(0, 0, 0) 0px 1px 2px 0px" }, /box-shadow/],
      [{ outlineStyle: "solid", outlineWidth: "2px" }, /outline 2px solid/],
      [{ borders: quiet.borders.map((x) => ({ ...x, style: "solid", width: "1px", color: "rgb(200, 0, 0)" })) }, /border-top 1px solid rgb\(200, 0, 0\)/],
    ];
    for (const [patch, re] of cases) {
      const v = evaluateLayout(snap({ statuses: [{ ...quiet, ...patch }] }));
      assert.deepEqual(rules(v), ["status-quiet"], JSON.stringify(patch));
      assert.match(v[0]?.detail ?? "", re);
      assert.equal(v[0]?.element, "span.chip");
    }
    // the focus ring of a focused status element is allowed
    assert.deepEqual(evaluateLayout(snap({ statuses: [{ ...quiet, outlineStyle: "solid", outlineWidth: "2px", focused: true }] })), []);
    // an unreadable colour never counts as transparent
    assert.deepEqual(rules(evaluateLayout(snap({ statuses: [{ ...quiet, backgroundColor: "var(--x)" }] }))), ["status-quiet"]);
  });

  test("raw-text: reports the match and its context", () => {
    const v = evaluateLayout(snap({ rawText: [{ el: "p.note", match: "undefined", context: "Всего: undefined записей" }] }));
    assert.deepEqual(rules(v), ["raw-text"]);
    assert.match(v[0]?.detail ?? "", /undefined/);
    assert.match(v[0]?.detail ?? "", /Всего: undefined записей/);
    assert.deepEqual(evaluateLayout(snap({ rawText: [{ el: "p", match: "NaN", context: "NaN" }] }), { rules: { "raw-text": false } }), []);
  });

  const box = (patch: Partial<LayoutSnapshot["textBoxes"][number]>) => ({
    el: "p.t",
    text: "Математический анализ",
    chars: 21,
    width: 40,
    lines: 4,
    clipped: false,
    target: false,
    ...patch,
  });

  test("title-width: a long text wrapped in a narrow box fails; a narrow one-line text (chip, tab) passes", () => {
    assert.deepEqual(rules(evaluateLayout(snap({ textBoxes: [box({})] }))), ["title-width"]);
    assert.deepEqual(evaluateLayout(snap({ textBoxes: [box({ width: 70, lines: 1 })] })), [], "a shrink-wrapped one-liner is fine");
    assert.deepEqual(rules(evaluateLayout(snap({ textBoxes: [box({ width: 70, lines: 1, clipped: true })] }))), ["title-width"], "cut text is squeezed");
    assert.deepEqual(evaluateLayout(snap({ textBoxes: [box({ width: 96, lines: 3 })] })), [], "96 px is wide enough");
    assert.deepEqual(evaluateLayout(snap({ textBoxes: [box({ width: 120, lines: 3 })] })), []);
  });

  test("title-width: more than 4 lines in a box narrower than 160 px fails even when wider than 96", () => {
    const v = evaluateLayout(snap({ textBoxes: [box({ width: 140, lines: 5 })] }));
    assert.deepEqual(rules(v), ["title-width"]);
    assert.match(v[0]?.detail ?? "", /5 строках/);
    assert.deepEqual(evaluateLayout(snap({ textBoxes: [box({ width: 170, lines: 5 })] })), []);
    assert.deepEqual(evaluateLayout(snap({ textBoxes: [box({ width: 140, lines: 4 })] })), []);
  });

  test("title-width: a targeted row title of 3+ characters is checked, an untargeted short text is not", () => {
    const short = box({ text: "Физика 2", chars: 8, width: 30, lines: 2 });
    assert.deepEqual(evaluateLayout(snap({ textBoxes: [{ ...short, target: false }] })), []);
    const v = evaluateLayout(snap({ textBoxes: [{ ...short, target: true }] }));
    assert.deepEqual(rules(v), ["title-width"]);
    assert.match(v[0]?.detail ?? "", /заголовок строки списка/);
    assert.deepEqual(evaluateLayout(snap({ textBoxes: [{ ...short, chars: 2, target: true }] })), [], "under 3 characters is never a title");
  });

  test("console: every record is a violation unless an ignore with a reason matches", () => {
    const s = snap({
      console: [
        { kind: "console", text: "Failed to load resource", location: "http://x/a.js:3" },
        { kind: "pageerror", text: "TypeError: x is not a function" },
      ],
    });
    const v = evaluateLayout(s);
    assert.deepEqual(rules(v), ["console"]);
    assert.equal(v.length, 2);
    assert.match(v[0]?.detail ?? "", /a\.js:3/);
    const left = evaluateLayout(s, { ignoreConsole: [{ pattern: "^Failed to load resource", reason: "the favicon of the dev server" }] });
    assert.equal(left.length, 1);
    assert.match(left[0]?.detail ?? "", /TypeError/);
    assert.throws(() => resolveOptions({ ignoreConsole: [{ pattern: "x", reason: " " }] }), /needs a reason/);
    assert.deepEqual(evaluateLayout(s, { rules: { console: false } }), []);
  });

  test("maxPerRule keeps the output short and counts the rest", () => {
    const many = Array.from({ length: 20 }, (_, i) => ({ el: `p#${i}`, match: "NaN", context: "NaN" }));
    const v = evaluateLayout(snap({ rawText: many }), { maxPerRule: 5 });
    assert.equal(v.length, 6);
    assert.match(v[5]?.detail ?? "", /и ещё 15/);
    assert.deepEqual(capPerRule([], 3), []);
  });

  test("the message names the screen, the rule and the element", () => {
    const v = evaluateLayout(snap({ rawText: [{ el: "main > p.note «Итого»", match: "undefined", context: "Итого undefined" }], iconButtons: [{ el: "button[aria-label=\"Х\"]", label: "Х", title: "", tip: "" }] }));
    const text = formatViolations("Записи · панель открыта", v);
    const lines = text.split("\n");
    assert.ok(lines[0]?.startsWith("Раскладка «Записи · панель открыта»: 2 нарушения"), lines[0]);
    assert.match(text, /\[raw-text\] main > p\.note «Итого» — /);
    assert.match(text, /\[icon-button\] button\[aria-label="Х"\] — /);
    assert.equal(lines.length, 3);
    assert.ok(formatViolations("Z", [v[0] as LayoutViolation]).startsWith("Раскладка «Z»: 1 нарушение,"));
  });
});

describe("evaluateAgentThread (pure)", () => {
  const b = (l: number, t: number, r: number, bo: number) => ({ left: l, top: t, right: r, bottom: bo });
  const pos = (blockerBox: ReturnType<typeof b>, textBox: ReturnType<typeof b>, related: number[] = []) => ({
    label: "низ",
    scrollTop: 100,
    blockers: [{ el: ".ag-thread > div.ag-think", kind: "row" as const, position: "sticky", box: blockerBox }],
    texts: [{ el: ".ag-thread > p.ag-text", text: "Ответ агента", box: textBox, related }],
  });

  test("an overlap of more than 2 px both ways fails", () => {
    const v = evaluateAgentThread({ rowSeen: true, positions: [pos(b(0, 100, 300, 130), b(10, 120, 200, 140))] });
    assert.deepEqual(rules(v), ["agent-overlap"]);
    assert.match(v[0]?.detail ?? "", /строка «Агент отвечает…\/Стоп» перекрывает текст «Ответ агента»/);
    assert.match(v[0]?.detail ?? "", /прокрутка ленты: низ/);
  });
  test("touching, a 2 px overlap, or an ancestor/descendant is not an overlap", () => {
    assert.deepEqual(evaluateAgentThread({ rowSeen: true, positions: [pos(b(0, 100, 300, 130), b(10, 130, 200, 150))] }), []);
    assert.deepEqual(evaluateAgentThread({ rowSeen: true, positions: [pos(b(0, 100, 300, 130), b(10, 128, 200, 150))] }), []);
    assert.deepEqual(evaluateAgentThread({ rowSeen: true, positions: [pos(b(0, 100, 300, 130), b(10, 100, 200, 130), [0])] }), []);
  });
  test("a floating element is named as such, and the rule can be off", () => {
    const p = pos(b(0, 100, 300, 130), b(10, 110, 200, 125));
    const first = p.blockers[0];
    assert.ok(first);
    const f = { rowSeen: false, positions: [{ ...p, blockers: [{ ...first, kind: "floating" as const }] }] };
    assert.match(evaluateAgentThread(f)[0]?.detail ?? "", /плавающий элемент \(position: sticky\)/);
    assert.deepEqual(evaluateAgentThread(f, { rules: { "agent-overlap": false } }), []);
  });
});

describe("watchPage with a fake page", () => {
  function fakePage() {
    const ee = new EventEmitter();
    return ee as unknown as Page & EventEmitter;
  }
  const msg = (type: string, text: string) => ({ type: () => type, text: () => text, location: () => ({ url: "http://app/x.js", lineNumber: 7, columnNumber: 0 }) });

  test("records console errors and page errors only, idempotently, and drains them", () => {
    const page = fakePage();
    watchPage(page);
    watchPage(page);
    assert.equal(page.listenerCount("console"), 1);
    assert.equal(page.listenerCount("pageerror"), 1);
    page.emit("console", msg("log", "hello"));
    page.emit("console", msg("warning", "careful"));
    page.emit("console", msg("error", "boom"));
    page.emit("pageerror", new Error("unhandled rejection: nope"));
    const got = drainConsole(page);
    assert.deepEqual(
      got.map((r) => [r.kind, r.text]),
      [
        ["console", "boom"],
        ["pageerror", "unhandled rejection: nope"],
      ],
    );
    assert.equal(got[0]?.location, "http://app/x.js:7");
    assert.deepEqual(drainConsole(page), [], "drained once");
    page.emit("console", msg("error", "later"));
    assert.deepEqual(
      drainConsole(page).map((r) => r.text),
      ["later"],
      "an error after the drain belongs to the next check",
    );
  });

  test("a page nobody watched has nothing to drain", () => {
    assert.deepEqual(drainConsole(fakePage()), []);
  });

  // TASK-377 (follow-up of TASK-376): «Failed to load resource: 404» names its URL.
  const req = (method: string, url: string, failure?: string) => ({ method: () => method, url: () => url, failure: () => (failure === undefined ? null : { errorText: failure }) });
  const res = (status: number, url: string, method = "GET") => ({ status: () => status, url: () => url, request: () => req(method, url) });

  test("a failed request and an answer with status >= 400 are recorded with their URL, a good answer is not", () => {
    const page = fakePage();
    watchPage(page);
    assert.equal(page.listenerCount("requestfailed"), 1);
    assert.equal(page.listenerCount("response"), 1);
    watchPage(page);
    assert.equal(page.listenerCount("response"), 1, "idempotent");
    page.emit("response", res(200, "http://127.0.0.1:34115/index.html"));
    page.emit("response", res(399, "http://127.0.0.1:34115/moved"));
    page.emit("response", res(404, "http://127.0.0.1:34115/favicon.ico"));
    page.emit("response", res(500, "http://127.0.0.1:34115/wails/runtime", "POST"));
    page.emit("requestfailed", req("GET", "http://127.0.0.1:34115/wails/custom.js", "net::ERR_CONNECTION_REFUSED"));
    page.emit("requestfailed", req("GET", "http://127.0.0.1:34115/x.js"));
    assert.deepEqual(
      drainConsole(page).map((r) => [r.kind, r.text]),
      [
        ["request", "GET http://127.0.0.1:34115/favicon.ico → 404"],
        ["request", "POST http://127.0.0.1:34115/wails/runtime → 500"],
        ["request", "GET http://127.0.0.1:34115/wails/custom.js — net::ERR_CONNECTION_REFUSED"],
        ["request", "GET http://127.0.0.1:34115/x.js — сбой запроса"],
      ],
    );
  });

  test("a URL is recorded without its query, fragment and user info (a token may travel there)", () => {
    assert.equal(safeUrl("http://u:pw@127.0.0.1:1/a/b?key=pthub_secret&x=1#frag"), "http://127.0.0.1:1/a/b");
    assert.equal(safeUrl("https://hub.example.invalid/rpc?token=abc"), "https://hub.example.invalid/rpc");
    assert.equal(safeUrl("data:text/plain;base64,AAAA"), "data:…");
    assert.equal(safeUrl("blob:http://x/uuid"), "blob:…");
    assert.equal(safeUrl("not a url?secret=1"), "not a url");
    assert.equal(requestRecord("GET", "http://h/p?pthub_aaaa=1", { status: 404 }).text, "GET http://h/p → 404");
  });
});

describe("request records in the layout verdict", () => {
  const failedLoad = { kind: "console" as const, text: "Failed to load resource: the server responded with a status of 404 ()", location: "http://127.0.0.1:34115/favicon.ico:0" };
  const request = { kind: "request" as const, text: "GET http://127.0.0.1:34115/favicon.ico → 404" };

  test("the console violation «Failed to load resource» names the URL of the failed request", () => {
    const v = evaluateLayout(snap({ console: [request, failedLoad] }));
    assert.equal(v.length, 1, "the request record is context, not a second finding");
    assert.equal(v[0]?.rule, "console");
    assert.match(v[0]?.detail ?? "", /Failed to load resource/);
    assert.match(v[0]?.detail ?? "", /запросы с ошибкой: GET http:\/\/127\.0\.0\.1:34115\/favicon\.ico → 404/);
  });

  test("a request record alone is no violation, and other console errors do not get the list", () => {
    assert.deepEqual(evaluateLayout(snap({ console: [request] })), []);
    const v = evaluateLayout(snap({ console: [request, { kind: "pageerror", text: "TypeError: x" }] }));
    assert.equal(v.length, 1);
    assert.ok(!/запросы с ошибкой/.test(v[0]?.detail ?? ""));
  });

  test("an ignore with a reason still applies to the console record, and then nothing is reported", () => {
    const v = evaluateLayout(snap({ console: [request, failedLoad] }), { ignoreConsole: [{ pattern: "^Failed to load resource", reason: "the favicon of the dev server" }] });
    assert.deepEqual(v, []);
  });
});

describe("the synthetic schedule", () => {
  const now = new Date("2026-11-04T10:07:31Z");

  test("is one calendar with CRLF lines, UTC times and unique UIDs", () => {
    const text = buildLayoutIcs(now);
    assert.ok(text.startsWith("BEGIN:VCALENDAR\r\n") && text.endsWith("END:VCALENDAR\r\n"));
    assert.equal(text.split("BEGIN:VCALENDAR").length - 1, 1);
    assert.doesNotMatch(text.replace(/\r\n/g, ""), /[\n\r]/);
    const lessons = layoutLessons(now);
    assert.equal(text.split("BEGIN:VEVENT").length - 1, lessons.length);
    assert.equal(new Set(lessons.map((l) => l.uid)).size, lessons.length);
    assert.match(text, /X-WR-TIMEZONE:Europe\/Moscow/);
    for (const m of text.matchAll(/^DTSTART:(.*)$/gm)) {
      assert.match(m[1] ?? "", /^\d{8}T\d{6}Z$/);
    }
    for (const m of text.matchAll(/^SUMMARY:(.*)$/gm)) {
      assert.doesNotMatch(m[1] ?? "", /[,;\\]/, "a comma or semicolon would need ICS escaping");
    }
  });

  test("has a lesson in 25 minutes, one earlier and one later today, and long subject names", () => {
    const lessons = layoutLessons(now);
    const soon = lessons.find((l) => l.uid === "layout-soon");
    assert.equal(soon?.start.toISOString(), "2026-11-04T10:32:00.000Z");
    assert.ok(lessons.some((l) => l.start < now && now.getTime() - l.start.getTime() < 6 * 3_600_000));
    assert.ok(lessons.some((l) => l.start > now && l.start.getTime() - now.getTime() < 6 * 3_600_000 && l.uid !== "layout-soon"));
    assert.ok(Object.values(layoutSubjects).filter((n) => n.length > 60).length >= 3);
    assert.equal(icsUtc(new Date("2026-01-02T03:04:05Z")), "20260102T030400Z");
  });

  test("every lesson is inside the import window of the app (from the start of yesterday UTC, 366 days)", () => {
    for (const l of layoutLessons(now)) {
      assert.ok(l.start.getTime() >= Date.UTC(2026, 10, 3) && l.start.getTime() < Date.UTC(2026, 10, 3) + 366 * 86_400_000, l.uid);
    }
  });
});

describe("the probe source", () => {
  test("is one self-contained function expression", () => {
    assert.match(probeLayoutSource, /^function probeLayout\(/);
    assert.doesNotMatch(probeLayoutSource, /__name\(|__publicField|require\(|import\(/);
    // it must be callable from a string with only a global scope: evaluate it with a fake minimal DOM-less scope fails fast,
    // so just prove it parses as an expression
    assert.doesNotThrow(() => new Function(`return (${probeLayoutSource});`));
  });
});

// ---------- real browser: headless Edge ----------

let browser: Browser | undefined;
let skipReason: string | undefined;

before(async () => {
  try {
    browser = await chromium.launch({ channel: "msedge", headless: true, timeout: 20_000 });
  } catch (err) {
    skipReason = `headless Microsoft Edge cannot start here: ${err instanceof Error ? err.message.split("\n")[0] : String(err)}`;
  }
});
after(async () => {
  await browser?.close().catch(() => undefined);
});

async function inPage<T>(t: TestContext, html: string, fn: (page: Page) => Promise<T>, size = { width: 800, height: 600 }): Promise<T | undefined> {
  if (!browser) {
    t.skip(skipReason ?? "no browser");
    return undefined;
  }
  const context = await browser.newContext({ viewport: size });
  try {
    const page = await context.newPage();
    watchPage(page);
    await page.setContent(`<!doctype html><html><head><meta charset="utf-8"><style>html,body{margin:0}body{font:14px Arial,sans-serif}</style></head><body>${html}</body></html>`);
    return await fn(page);
  } finally {
    await context.close();
  }
}

const svg = '<svg width="16" height="16" viewBox="0 0 16 16"><path d="M0 0h16v16z"/></svg>';

// A page that follows every rule, with the shapes the app has: rail buttons with a .tip, IconAction with .ia-tip, an
// inner pane that scrolls sideways, quiet statuses, short chips and tab labels, a narrow column with short text.
const goodPage = `
<style>
  .rail{position:fixed;left:0;top:0;width:60px}
  .rail button{position:relative;width:40px;height:40px;display:block}
  .tip{position:absolute;left:calc(100% + 8px);top:50%;opacity:0;white-space:nowrap}
  .rail button:hover .tip{opacity:1}
  .ia{position:relative}
  .ia-tip{display:none;position:absolute}
  .ia:hover .ia-tip{display:block}
  .vh{position:absolute;width:1px;height:1px;overflow:hidden;clip:rect(0 0 0 0)}
  main{margin-left:80px;height:560px;overflow:auto}
  .chip{display:inline-flex;white-space:nowrap;background:transparent;border:0;font-size:11px}
  .tabs button{white-space:nowrap}
  .pane{width:200px;overflow-x:auto}
  .pane div{width:900px;height:20px}
</style>
<nav class="rail" aria-label="Разделы">
  <button aria-label="Сегодня">${svg}<span class="tip" aria-hidden="true">Сегодня</span></button>
  <button aria-label="Записи">${svg}<span class="tip" aria-hidden="true">Записи</span></button>
</nav>
<main>
  <h1>Записи занятий</h1>
  <p class="t">Математический анализ и аналитическая геометрия</p>
  <button class="ia" aria-label="Удалить">${svg}<span class="ia-tip" aria-hidden="true">Удалить</span></button>
  <button aria-label="Закрыть" title="Закрыть">${svg}</button>
  <button>Сохранить изменения</button>
  <a href="#x" aria-label="Справка" title="Справка">${svg}</a>
  <span class="vh">служебный текст для чтения с экрана undefined</span>
  <div style="display:none">Скрыто: undefined NaN</div>
  <p data-note="NaN undefined">Служебные data-атрибуты не считаются</p>
  <span class="chip" data-status="ok">Не распознано</span>
  <span class="chip" data-status="run">Идёт обработка…</span>
  <div class="tabs"><button>Конспект</button><button>Расшифровка занятия</button></div>
  <div style="width:60px"><p>Дата</p></div>
  <div class="pane"><div>ширина внутри панели</div></div>
</main>`;

describe("probe in headless Edge", () => {
  test("a page that follows every rule has no violations (also with the mouse over a rail button)", async (t) => {
    await inPage(t, goodPage, async (page) => {
      assert.deepEqual(await layoutViolations(page), []);
      await page.hover(".rail button");
      assert.deepEqual(await layoutViolations(page), [], "a visible .tip is a tooltip, not the text of the button");
      await checkLayout(page, "Хорошая страница");
    });
  });

  test("window-scroll: a page taller than the window", async (t) => {
    await inPage(t, `<div style="height:900px">высокий</div><p id="last">Конец страницы</p>`, async (page) => {
      const v = await layoutViolations(page);
      assert.deepEqual(rules(v), ["window-scroll"]);
      assert.match(v[0]?.detail ?? "", /scrollHeight \d+ > innerHeight 600/);
      assert.match(v[0]?.detail ?? "", /Конец страницы/, "the lowest element is named");
    });
  });

  test("window-scroll: an inner pane that scrolls is fine", async (t) => {
    await inPage(t, `<div style="height:300px;overflow:auto"><div style="height:2000px">внутри</div></div>`, async (page) => {
      assert.deepEqual(await layoutViolations(page), []);
    });
  });

  test("overflow-x: a wide element is named; clipped, hidden and off-canvas ones are not", async (t) => {
    await inPage(t, `<div id="wide" style="width:1500px;height:20px;background:#ccc">слишком широкий блок</div>`, async (page) => {
      const v = await layoutViolations(page);
      assert.deepEqual(rules(v), ["overflow-x"]);
      assert.ok(v.some((x) => x.element === "body"), "body.scrollWidth");
      assert.ok(v.some((x) => x.element.includes("div#wide") && x.element.includes("слишком широкий")), JSON.stringify(v));
    });
    const quietPage = `
      <div style="width:300px;overflow:hidden"><div style="width:1500px">обрезан предком</div></div>
      <div style="position:fixed;left:2000px;top:0;white-space:nowrap" aria-hidden="true">за краем по замыслу</div>
      <div style="display:none;width:3000px">нет</div>
      <div style="width:100px;height:5px;overflow:hidden"><div style="visibility:hidden;width:3000px;height:5px"></div></div>
      <div style="position:relative;width:300px;overflow:hidden"><div style="position:absolute;left:900px;top:0;white-space:nowrap">внутри обрезающего</div></div>`;
    await inPage(t, quietPage, async (page) => {
      assert.deepEqual(await layoutViolations(page), []);
    });
  });

  test("overflow-x: an absolute child escapes a clipping ancestor that is not its containing block", async (t) => {
    const html = `<div style="width:300px;overflow:hidden"><div style="position:absolute;left:900px;top:0;width:50px;height:20px">убежал</div></div>`;
    await inPage(t, html, async (page) => {
      const v = await layoutViolations(page);
      assert.ok(v.some((x) => x.rule === "overflow-x" && x.element.includes("убежал")), JSON.stringify(v));
    });
  });

  test("icon-button: aria-label without a title or tip fails; neither fails twice; .tip, .ia-tip and title pass", async (t) => {
    await inPage(
      t,
      `<button id="a" aria-label="Удалить">${svg}</button>
       <button id="b">${svg}</button>
       <a href="#z" id="c">${svg}</a>
       <div role="button" id="d" tabindex="0">${svg}</div>
       <button aria-label="Сегодня">${svg}<span class="tip">Сегодня</span></button>
       <button aria-label="Копировать">${svg}<span class="ia-tip" style="display:none">Копировать</span></button>
       <button aria-label="Новый чат" title="Новый чат">${svg}</button>
       <button aria-label="Роль подсказки">${svg}<span role="tooltip">Роль подсказки</span></button>
       <button aria-label="Пустая подсказка">${svg}<span class="tip">  </span></button>
       <button>Подпись есть</button>
       <button style="display:none">${svg}</button>`,
      async (page) => {
        const v = await layoutViolations(page);
        assert.deepEqual(rules(v), ["icon-button"]);
        const lines = v.map((x) => `${x.element} :: ${x.detail}`);
        assert.equal(v.length, 5, lines.join("\n"));
        assert.ok(lines.some((l) => l.includes('aria-label="Удалить"') && /нет подсказки/.test(l) && !/нет aria-label/.test(l)));
        assert.ok(lines.some((l) => l.includes("button#b") && /нет aria-label.*нет подсказки/.test(l)));
        assert.ok(lines.some((l) => l.includes("a#c")));
        assert.ok(lines.some((l) => l.includes("[role=button]") && l.includes("#d")));
        assert.ok(lines.some((l) => l.includes('aria-label="Пустая подсказка"')), "an empty tooltip is no tooltip");
      },
    );
  });

  test("icon-button: aria-labelledby that resolves to text counts as the name", async (t) => {
    await inPage(t, `<span id="n">Закрыть окно</span><button aria-labelledby="n" title="Закрыть окно">${svg}</button>`, async (page) => {
      assert.deepEqual(await layoutViolations(page), []);
    });
  });

  test("status-quiet: a coloured background and a coloured border fail with the computed values; quiet ones pass", async (t) => {
    await inPage(
      t,
      `<span data-status="fail" id="bg" style="background:rgb(255,221,221)">Сбой</span>
       <span data-status="wait" id="bd" style="border:1px solid rgb(200,0,0)">Ожидание</span>
       <span data-status="rec" id="sh" style="box-shadow:0 0 0 2px rgb(0,0,255)">Запись</span>
       <span data-status="ok" id="ok" style="background:transparent;border:0">Готово</span>
       <span data-status="net" id="hid" style="display:none;background:red">Скрыт</span>`,
      async (page) => {
        const v = await layoutViolations(page);
        assert.deepEqual(rules(v), ["status-quiet"]);
        assert.equal(v.length, 3, JSON.stringify(v));
        assert.ok(v.some((x) => x.element.includes("span#bg") && /background-color rgb\(255, 221, 221\)/.test(x.detail)));
        assert.ok(v.some((x) => x.element.includes("span#bd") && /border-top 1px solid rgb\(200, 0, 0\)/.test(x.detail)));
        assert.ok(v.some((x) => x.element.includes("span#sh") && /box-shadow/.test(x.detail)));
      },
    );
  });

  test("raw-text: visible undefined / NaN / [object / Error: / TypeError fail; data-attributes, hidden and screen-reader-only text do not", async (t) => {
    await inPage(
      t,
      `<p id="a">Всего записей: undefined</p><p id="b">Итог NaN минут</p><p id="c">Значение [object Object]</p>
       <p id="d">Error: connection refused</p><p id="e">TypeError при открытии</p>
       <p data-x="undefined">data-атрибут</p><div hidden>undefined</div>
       <p>слово undefinedX и NaNa — не совпадают по границе слова</p>`,
      async (page) => {
        const v = await layoutViolations(page);
        assert.deepEqual(rules(v), ["raw-text"]);
        assert.equal(v.length, 5, JSON.stringify(v));
        assert.ok(v.some((x) => x.element.includes("p#a") && x.detail.includes("Всего записей: undefined")));
      },
    );
  });

  test("title-width: a title squeezed into a 40 px column fails; a short chip, a tab and a short word in a narrow box do not", async (t) => {
    await inPage(
      t,
      `<div style="width:40px"><p id="sq" class="t">Математический анализ</p></div>
       <span class="chip" style="display:inline-block;white-space:nowrap;font-size:11px">Не распознано</span>
       <button style="white-space:nowrap">Расшифровка занятия</button>
       <div style="width:60px"><p>Дата</p></div>`,
      async (page) => {
        const v = await layoutViolations(page);
        assert.deepEqual(rules(v), ["title-width"]);
        assert.equal(v.length, 1, JSON.stringify(v));
        assert.ok(v[0]?.element.includes("p#sq") || v[0]?.element.includes("p.t"), v[0]?.element);
        assert.match(v[0]?.detail ?? "", /Математический анализ/);
      },
    );
  });

  test("title-width: a text of 12+ characters wrapping into more than 4 lines in a box under 160 px fails", async (t) => {
    await inPage(t, `<div style="width:120px"><p id="tall">Раз два три четыре пять шесть семь восемь девять десять Раз два три четыре пять шесть семь восемь девять десять</p></div>`, async (page) => {
      const v = await layoutViolations(page, { rules: {} });
      assert.deepEqual(rules(v), ["title-width"]);
      assert.match(v[0]?.detail ?? "", /строках/);
    });
  });

  test("title-width: the targeted selectors check a short row title (3+ characters) too", async (t) => {
    await inPage(
      t,
      `<div class="tx" style="width:30px"><p class="t">Физика 2</p></div><div style="width:30px"><p class="other">Физика 2</p></div>`,
      async (page) => {
        const v = await layoutViolations(page);
        assert.deepEqual(rules(v), ["title-width"]);
        assert.equal(v.length, 1);
        assert.match(v[0]?.detail ?? "", /заголовок строки списка/);
      },
    );
  });

  test("every rule can be switched off", async (t) => {
    await inPage(t, `<div style="height:900px;width:1200px"><p>undefined</p><button>${svg}</button></div>`, async (page) => {
      const all = await layoutViolations(page);
      assert.deepEqual(rules(all).sort(), ["icon-button", "overflow-x", "raw-text", "window-scroll"]);
      const off = await layoutViolations(page, {
        rules: { "window-scroll": false, "overflow-x": false, "icon-button": false, "raw-text": false },
      });
      assert.deepEqual(off, []);
    });
  });

  test("console: an error and an unhandled rejection are attributed to the check that follows them, once", async (t) => {
    await inPage(t, `<p>страница</p>`, async (page) => {
      assert.deepEqual(await layoutViolations(page), []);
      const pe = page.waitForEvent("pageerror");
      await page.evaluate(() => {
        console.error("сломалось: boom");
        setTimeout(() => {
          void Promise.reject(new Error("rejected late"));
        }, 0);
      });
      await pe;
      const v = await layoutViolations(page);
      assert.deepEqual(rules(v), ["console"]);
      assert.equal(v.length, 2, JSON.stringify(v));
      assert.ok(v.some((x) => x.detail.includes("сломалось: boom")));
      assert.ok(v.some((x) => x.detail.includes("rejected late")));
      assert.deepEqual(await layoutViolations(page), [], "the next check starts clean");
      await page.evaluate(() => console.error("second screen"));
      await assert.rejects(checkLayout(page, "Вторая страница"), (e: Error) => e.message.startsWith("Раскладка «Вторая страница»") && e.message.includes("second screen"));
    });
  });

  test("checkLayout throws one error that names the screen and lists every violation", async (t) => {
    await inPage(t, `<div style="height:900px"><p>undefined</p><button>${svg}</button></div>`, async (page) => {
      await assert.rejects(checkLayout(page, "Журнал"), (e: Error) => {
        const lines = e.message.split("\n");
        assert.ok(lines[0]?.startsWith("Раскладка «Журнал»: 3 нарушения"), lines[0]);
        assert.match(e.message, /\[window-scroll\]/);
        assert.match(e.message, /\[raw-text\] .*p.*undefined/);
        assert.match(e.message, /\[icon-button\] div > button/);
        return true;
      });
    });
  });

  test("layoutSession collects the findings of several states and fails once at the end", async (t) => {
    await inPage(t, `<p id="msg">чисто</p>`, async (page) => {
      const s = layoutSession(page);
      assert.deepEqual(await s.check("панель закрыта"), []);
      await page.evaluate(() => {
        document.getElementById("msg")!.textContent = "ошибка: undefined";
      });
      const bad = await s.check("панель открыта");
      assert.deepEqual(rules(bad), ["raw-text"]);
      await page.evaluate(() => {
        document.body.insertAdjacentHTML("beforeend", "<p>и ещё NaN</p>");
      });
      await s.check("панель открыта", { rules: { "raw-text": true } });
      assert.equal(s.findings.length, 2, "the same state name is one entry");
      assert.throws(
        () => s.assertAll(),
        (e: Error) => {
          assert.ok(e.message.startsWith("Раскладка «панель открыта»: 3 нарушения (состояний: 1)"), e.message.split("\n")[0]);
          assert.match(e.message, /\[панель открыта\]/);
          assert.doesNotMatch(e.message, /\[панель закрыта\]/, "clean states are not listed");
          return true;
        },
      );
    });
    await inPage(t, `<p>всё хорошо</p>`, async (page) => {
      const s = layoutSession(page);
      await s.check("a");
      await s.check("b");
      assert.doesNotThrow(() => s.assertAll());
      s.add("перенесено из другого теста", [{ rule: "raw-text", element: "p", detail: "текст «NaN»" }]);
      assert.throws(() => s.assertAll(), /^Error: Раскладка «перенесено из другого теста»: 1 нарушение/);
    });
  });
});

// ---------- agent thread in headless Edge ----------

const threadCss = `
  .ag{position:relative;width:300px}
  .ag-thread{position:relative;height:220px;overflow-y:auto;display:flex;flex-direction:column;gap:12px;padding:4px 12px 16px 16px}
  .ag-thread > *{flex:none}
  p{margin:0}
  .ag-think{display:flex;align-items:center;gap:8px;min-height:28px}
`;
const messages = Array.from({ length: 12 }, (_, i) => `<p class="ag-text">Сообщение номер ${i + 1}: достаточно длинный ответ помощника</p>`).join("");

describe("agent thread in headless Edge", () => {
  test("a row in the flow of the thread never overlaps the text, at the top, the middle and the bottom", async (t) => {
    await inPage(t, `<style>${threadCss}</style><aside class="ag"><div class="ag-thread">${messages}<div class="ag-think"><span>Агент отвечает…</span><button aria-label="Стоп" title="Стоп">${svg}</button></div></div></aside>`, async (page) => {
      const r = await checkAgentThread(page);
      assert.equal(r.rowSeen, true);
      assert.deepEqual(r.violations, []);
      assert.equal(await page.evaluate(() => document.querySelector(".ag-thread")!.scrollTop), 0, "the scroll position is restored");
    });
  });

  test("a sticky strip floating over the reply text is reported", async (t) => {
    await inPage(t, `<style>${threadCss} .ag-think{position:sticky;bottom:0;background:#fff}</style><aside class="ag"><div class="ag-thread">${messages}<div class="ag-think"><span>Агент отвечает…</span><button aria-label="Стоп" title="Стоп">${svg}</button></div></div></aside>`, async (page) => {
      const r = await checkAgentThread(page);
      assert.equal(r.rowSeen, true);
      assert.ok(r.violations.length > 0);
      assert.deepEqual(rules(r.violations), ["agent-overlap"]);
      assert.match(r.violations[0]?.detail ?? "", /перекрывает текст «Сообщение номер/);
      assert.match(r.violations[0]?.element ?? "", /ag-think/);
    });
  });

  test("a sticky element that is not the row is reported too", async (t) => {
    await inPage(t, `<style>${threadCss} .head{position:sticky;top:0;background:#eee}</style><aside class="ag"><div class="ag-thread"><div class="head">Липкая шапка</div>${messages}</div></aside>`, async (page) => {
      const r = await checkAgentThread(page);
      assert.equal(r.rowSeen, false);
      assert.ok(r.violations.some((v) => /плавающий элемент \(position: sticky\)/.test(v.detail)), JSON.stringify(r.violations));
    });
  });

  test("without the row it says so; without a thread it says why", async (t) => {
    await inPage(t, `<style>${threadCss}</style><aside class="ag"><div class="ag-thread">${messages}</div></aside>`, async (page) => {
      assert.deepEqual(await checkAgentThread(page), { rowSeen: false, violations: [] });
    });
    await inPage(t, `<p>нет панели</p>`, async (page) => {
      const r = await checkAgentThread(page);
      assert.equal(r.rowSeen, false);
      assert.match(r.note ?? "", /нет ленты помощника/);
    });
    await inPage(t, `<style>${threadCss} .ag{visibility:hidden}</style><aside class="ag"><div class="ag-thread">${messages}</div></aside>`, async (page) => {
      assert.match((await checkAgentThread(page)).note ?? "", /панель помощника закрыта/);
    });
  });

  test("session.checkAgent counts toward assertAll", async (t) => {
    await inPage(t, `<style>${threadCss} .ag-think{position:sticky;bottom:0;background:#fff}</style><aside class="ag"><div class="ag-thread">${messages}<div class="ag-think"><span>Агент отвечает…</span></div></div></aside>`, async (page) => {
      const s = layoutSession(page);
      await s.checkAgent("Помощник · лента");
      assert.throws(() => s.assertAll(), /^Error: Раскладка «Помощник · лента»/);
    });
  });
});
