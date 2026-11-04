// Tests of the layout rules added after the first real run (TASK-376 fix round): evalInPage under a CSP, pane scroll,
// clipped content, truncated texts, raw text in attributes and tooltips, glyph buttons, the counts of what was examined,
// the session hooks. Pure parts run without a browser; the page parts run in the INSTALLED Microsoft Edge, headless, and
// skip with a reason when it cannot start (like layout.test.ts).
import assert from "node:assert/strict";
import { EventEmitter } from "node:events";
import { after, before, describe, test, type TestContext } from "node:test";

import { type Browser, chromium, type Page } from "@playwright/test";

import {
  consoleViolations,
  defaultRawText,
  drainConsole,
  emptyCounts,
  emptySnapshot,
  evalInPage,
  evaluateLayout,
  formatCounts,
  layoutReport,
  layoutSession,
  layoutViolations,
  type LayoutRule,
  type LayoutSnapshot,
  type LayoutViolation,
  watchPage,
} from "../src/checks/layout.js";
import { clipboardWrites, installPageStubs } from "../src/steps/stubs.js";

const rules = (v: LayoutViolation[]): LayoutRule[] => [...new Set(v.map((x) => x.rule))];

function snap(patch: Partial<LayoutSnapshot>): LayoutSnapshot {
  return { ...emptySnapshot(), ...patch };
}

describe("pane-scroll (pure)", () => {
  const pane = { selector: "main.sheet", el: "main.sheet", scrollWidth: 1200, clientWidth: 900 };

  test("a pane wider than itself is a violation with both widths; the rule can be off", () => {
    const v = evaluateLayout(snap({ panes: [pane] }));
    assert.deepEqual(rules(v), ["pane-scroll"]);
    assert.match(v[0]?.detail ?? "", /main\.sheet прокручивается вбок: scrollWidth 1200 > clientWidth 900/);
    assert.deepEqual(evaluateLayout(snap({ panes: [pane] }), { rules: { "pane-scroll": false } }), []);
  });

  test("a pane on the explicit allow-list is not a violation; the others still are", () => {
    const other = { ...pane, selector: ".ag-thread", el: "aside > div.ag-thread" };
    const v = evaluateLayout(snap({ panes: [pane, other] }), { sidewaysPanes: ["main.sheet"] });
    assert.equal(v.length, 1);
    assert.equal(v[0]?.element, "aside > div.ag-thread");
  });
});

describe("clipped and truncated (pure)", () => {
  test("a clipped control and a clipped text say how and by what", () => {
    const v = evaluateLayout(
      snap({
        clipped: [
          { kind: "control", el: "button «Сохранить»", by: "div.card", axis: "x", cutPx: 60 },
          { kind: "control", el: "button «Ниже»", by: "div.card", axis: "y", cutPx: 18 },
          { kind: "text", el: "div.note", text: "Первая строка", scrollHeight: 60, clientHeight: 20 },
        ],
      }),
    );
    assert.deepEqual(rules(v), ["clipped"]);
    assert.equal(v.length, 3);
    assert.match(v[0]?.detail ?? "", /на 60 px по горизонтали предком div\.card/);
    assert.match(v[1]?.detail ?? "", /по вертикали/);
    assert.match(v[2]?.detail ?? "", /scrollHeight 60 > clientHeight 20.*без line-clamp/);
    assert.deepEqual(evaluateLayout(snap({ clipped: [{ kind: "text", el: "p", text: "t", scrollHeight: 60, clientHeight: 20 }] }), { rules: { clipped: false } }), []);
  });

  test("a truncated text needs a tooltip: only those without one are violations", () => {
    const v = evaluateLayout(
      snap({
        truncated: [
          { el: "div.nm «Длинное название»", text: "Длинное название", axis: "x", hasTip: false },
          { el: "div.nm «С подсказкой»", text: "С подсказкой", axis: "x", hasTip: true },
          { el: "p.clamp", text: "Абзац", axis: "y", hasTip: false },
        ],
      }),
    );
    assert.deepEqual(rules(v), ["truncated"]);
    assert.equal(v.length, 2);
    assert.match(v[0]?.detail ?? "", /обрезан по ширине.*подсказки с полным текстом нет/);
    assert.match(v[1]?.detail ?? "", /по высоте/);
    assert.deepEqual(evaluateLayout(snap({ truncated: [{ el: "p", text: "t", axis: "x", hasTip: false }] }), { rules: { truncated: false } }), []);
  });
});

describe("raw text and icon buttons (pure)", () => {
  test("the place of the raw text is named: attribute, tooltip, or the screen", () => {
    const v = evaluateLayout(
      snap({
        rawText: [
          { el: "p", match: "null", context: "a null b" },
          { el: "p", match: "null", context: "x", source: "text" },
          { el: "button", match: "undefined", context: "undefined", source: "title" },
          { el: "span.tip", match: "Invalid Date", context: "Invalid Date", source: "tooltip" },
        ],
      }),
    );
    assert.deepEqual(
      v.map((x) => x.detail.split(" служебный")[0]),
      ["на экране", "на экране", "в атрибуте title", "в подсказке"],
    );
  });

  test("a glyph-only button is named by its glyph", () => {
    const v = evaluateLayout(snap({ iconButtons: [{ el: "button", label: "", title: "", tip: "", glyph: "×" }] }));
    assert.match(v[0]?.detail ?? "", /кнопка только с символом «×»: нет aria-label; нет подсказки/);
  });

  test("the default pattern knows JS leftovers and the forms of backend errors", () => {
    const re = new RegExp(defaultRawText.source, defaultRawText.flags);
    for (const bad of [
      "undefined",
      "NaN минут",
      "[object Object]",
      "Error: x",
      "TypeError",
      "Значение: null",
      "Invalid Date",
      "[canceled] request",
      "[deadline_exceeded] слишком долго",
      "[unavailable]",
      "[failed_precondition]",
      "permanent failure: hub: boom",
      "hub: unavailable",
      "context canceled",
      "context deadline exceeded",
      "dial tcp 127.0.0.1:8080",
      "connection refused",
      "sql: no rows in result set",
    ]) {
      assert.ok(re.test(bad), `should match: ${bad}`);
    }
    for (const fine of ["nullable", "Дата не указана", "[draft]", "[canceled", "context menu", "dial", "permanent", "mysql", "Нет подключения к Hub"]) {
      assert.ok(!re.test(fine), `should not match: ${fine}`);
    }
  });
});

describe("counts and the console (pure)", () => {
  test("formatCounts is the line the spec prints", () => {
    assert.equal(
      formatCounts({ ...emptyCounts(), iconButtons: 7, statuses: 3, titles: 12, textNodes: 140 }),
      "кнопок-значков 7, статусов 3, заголовков 12, текстов 140",
    );
    assert.deepEqual(emptySnapshot().counts, emptyCounts());
  });

  function fakePage() {
    return new EventEmitter() as unknown as Page & EventEmitter;
  }
  const msg = (url: string, lineNumber: number, text = "boom") => ({ type: () => "error", text: () => text, location: () => ({ url, lineNumber, columnNumber: 0 }) });

  test("the watch keeps url:line of a message; a message without a source has no location", () => {
    const page = fakePage();
    watchPage(page);
    page.emit("console", msg("http://app/assets/index.js", 42));
    page.emit("console", msg("", 0, "Note that 'script-src' was not explicitly set"));
    page.emit("console", msg("", 7));
    const got = drainConsole(page);
    assert.deepEqual(
      got.map((r) => r.location),
      ["http://app/assets/index.js:42", undefined, "?:7"],
    );
  });

  test("consoleViolations drains the records of the page without a probe", () => {
    const page = fakePage();
    watchPage(page);
    page.emit("console", msg("http://app/a.js", 3, "Failed to fetch"));
    const v = consoleViolations(page);
    assert.deepEqual(rules(v), ["console"]);
    assert.match(v[0]?.detail ?? "", /Failed to fetch \(http:\/\/app\/a\.js:3\)/);
    assert.deepEqual(consoleViolations(page), [], "drained once");
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
const lines = (v: LayoutViolation[]): string[] => v.map((x) => `${x.rule} | ${x.element} | ${x.detail}`);

describe("evalInPage under the CSP of the app", () => {
  const csp = "default-src 'self'";
  const html = "<!doctype html><html><head><meta charset=\"utf-8\"></head><body><p>страница под CSP</p><button>Кнопка</button></body></html>";

  async function cspPage<T>(t: TestContext, fn: (page: Page) => Promise<T>): Promise<T | undefined> {
    if (!browser) {
      t.skip(skipReason ?? "no browser");
      return undefined;
    }
    const context = await browser.newContext({ viewport: { width: 800, height: 600 } });
    try {
      const page = await context.newPage();
      watchPage(page);
      await page.route("http://csp.test/**", (route) =>
        route.fulfill({ status: 200, contentType: "text/html; charset=utf-8", headers: { "content-security-policy": csp }, body: html }),
      );
      await page.goto("http://csp.test/");
      return await fn(page);
    } finally {
      await context.close();
    }
  }

  test("a value comes back and the console watch records nothing", async (t) => {
    await cspPage(t, async (page) => {
      drainConsole(page);
      assert.equal(await evalInPage<number>(page, "() => 6 * 7"), 42);
      assert.equal(await evalInPage<string>(page, (a: { w: string }) => `${a.w}:${document.title === "" ? "no title" : "title"}`, { w: "x" }), "x:no title");
      assert.equal(await evalInPage<string>(page, "async () => { await new Promise((r) => setTimeout(r, 20)); return 'later'; }"), "later");
      assert.equal(await evalInPage<undefined>(page, "() => undefined"), undefined);
      assert.deepEqual(await evalInPage<{ a: number[] }>(page, "(o) => ({ a: [o.n, o.n + 1] })", { n: 1 }), { a: [1, 2] });
      assert.equal(await evalInPage<string>(page, "() => document.querySelector('p').textContent"), "страница под CSP");
      await new Promise((r) => setTimeout(r, 150));
      assert.deepEqual(drainConsole(page), [], "inspector evaluation is exempt from the CSP and prints nothing to the page console");
    });
  });

  test("the clipboard stubs install and read back under the CSP without a console record (TASK-377)", async (t) => {
    await cspPage(t, async (page) => {
      drainConsole(page);
      await installPageStubs(page.context());
      await evalInPage(page, "() => navigator.clipboard.writeText('привет')");
      await evalInPage(page, "async () => { await navigator.clipboard.writeText('мир'); }");
      assert.deepEqual(await clipboardWrites(page), ["привет", "мир"]);
      await new Promise((r) => setTimeout(r, 150));
      assert.deepEqual(
        drainConsole(page).filter((r) => r.kind !== "request"),
        [],
        "page.evaluate of a string would have logged the CSP note here",
      );
    });
  });

  test("the reason evalInPage exists: Playwright's waitForFunction polls by eval, which the CSP blames on the page", async (t) => {
    // The first run of an evaluate is exempt (it is started by the inspector); the polling re-runs come from page timers.
    await cspPage(t, async (page) => {
      drainConsole(page);
      await page.waitForFunction("window.__ready === true", undefined, { timeout: 600, polling: 50 }).catch(() => undefined);
      await new Promise((r) => setTimeout(r, 300));
      const got = drainConsole(page);
      assert.ok(got.some((r) => /script-src/.test(r.text) && r.kind === "console"), JSON.stringify(got));
    });
  });

  test("a page that throws gives a readable error; a closed page too", async (t) => {
    await cspPage(t, async (page) => {
      await assert.rejects(evalInPage(page, "() => { throw new Error('сломано внутри'); }"), /evalInPage: код страницы упал: .*сломано внутри/);
      await assert.rejects(evalInPage(page, "() => nope.nope"), /код страницы упал: .*nope/);
      await assert.rejects(evalInPage(page, "async () => { await Promise.reject(new Error('отклонено')); }"), /отклонено/);
      await page.close();
      await assert.rejects(evalInPage(page, "() => 1"), /evalInPage:/);
    });
  });

  test("the whole layout probe runs under the CSP without a console violation", async (t) => {
    await cspPage(t, async (page) => {
      const r = await layoutReport(page);
      assert.deepEqual(r.violations, []);
      assert.ok(r.counts.visibleElements >= 2, JSON.stringify(r.counts));
      assert.ok(r.counts.textNodes >= 2);
      const s = layoutSession(page);
      assert.deepEqual(await s.check("под CSP"), []);
      await s.checkAgent("лента под CSP");
      assert.deepEqual(await layoutViolations(page), []);
    });
  });

  test("a running finite animation is waited for without a console error (this is where the polling used to eval)", async (t) => {
    await cspPage(t, async (page) => {
      await evalInPage(page, "() => { document.body.animate([{ opacity: 1 }, { opacity: 1 }], { duration: 500 }); }");
      const started = Date.now();
      const v = await layoutViolations(page);
      assert.ok(Date.now() - started >= 250, `the check should wait for the animation (${Date.now() - started} ms)`);
      assert.deepEqual(v, [], "no console violation from the waiting");
    });
  });
});

describe("pane-scroll in headless Edge", () => {
  const wide = `<main class="sheet" style="width:400px;height:150px;overflow:auto"><div style="width:900px">широкая таблица внутри</div></main>`;

  test("a pane that scrolls sideways fails; a fitting one passes; the allow-list and the switch work", async (t) => {
    await inPage(t, wide, async (page) => {
      const v = await layoutViolations(page);
      assert.deepEqual(rules(v), ["pane-scroll"], lines(v).join("\n"));
      assert.match(v[0]?.detail ?? "", /main\.sheet прокручивается вбок: scrollWidth 9\d\d > clientWidth \d+/);
      assert.deepEqual(await layoutViolations(page, { sidewaysPanes: ["main.sheet"] }), []);
      assert.deepEqual(await layoutViolations(page, { rules: { "pane-scroll": false } }), []);
      assert.deepEqual(await layoutViolations(page, { paneSelectors: ["#nothing"] }), []);
    });
    await inPage(t, `<main class="sheet" style="width:400px;height:150px;overflow:auto"><div style="width:300px;height:400px">узкая таблица</div></main>`, async (page) => {
      assert.deepEqual(await layoutViolations(page), [], "a vertical scroll is fine");
    });
  });

  test("the agent panel and its thread are panes too; a closed (hidden) panel is not examined", async (t) => {
    await inPage(
      t,
      `<aside class="ag" style="width:300px;overflow:auto"><div class="ag-thread" style="width:200px;height:100px;overflow:auto"><div style="width:500px">длинная строка ленты</div></div></aside>`,
      async (page) => {
        const v = await layoutViolations(page);
        assert.deepEqual(rules(v), ["pane-scroll"]);
        assert.equal(v.length, 1, lines(v).join("\n"));
        assert.match(v[0]?.detail ?? "", /\.ag-thread прокручивается вбок/);
      },
    );
    await inPage(t, `<aside class="ag" style="visibility:hidden;width:300px;overflow:auto"><div style="width:900px">закрыта</div></aside>`, async (page) => {
      assert.deepEqual(await layoutViolations(page), []);
    });
  });
});

describe("clipped controls and texts in headless Edge", () => {
  const cutButton = (w: number, wrapperStyle = "width:100px;overflow:hidden") =>
    `<div style="${wrapperStyle}"><button style="width:${w}px;white-space:nowrap">Сохранить изменения</button></div>`;

  test("a button cut by an overflow:hidden ancestor is reported with the cut; 2 px of slack passes, 3 px fails", async (t) => {
    await inPage(t, cutButton(160), async (page) => {
      const v = await layoutViolations(page);
      assert.deepEqual(rules(v), ["clipped"], lines(v).join("\n"));
      assert.match(v[0]?.detail ?? "", /обрезан на 60 px по горизонтали/);
      assert.match(v[0]?.element ?? "", /button/);
      assert.deepEqual(await layoutViolations(page, { rules: { clipped: false } }), []);
    });
    await inPage(t, cutButton(102), async (page) => assert.deepEqual(await layoutViolations(page), [], "cut by 2 px"));
    await inPage(t, cutButton(103), async (page) => assert.deepEqual(rules(await layoutViolations(page)), ["clipped"], "cut by 3 px"));
    await inPage(t, cutButton(103), async (page) => assert.deepEqual(await layoutViolations(page, { clipSlackPx: 3 }), [], "the slack is an option"));
  });

  test("a scrolling ancestor between the control and the hider makes it reachable", async (t) => {
    await inPage(t, `<div style="width:100px;overflow:hidden"><div style="width:100px;overflow:auto">${cutButton(160, "")}</div></div>`, async (page) => {
      assert.deepEqual(await layoutViolations(page), []);
    });
  });

  test("a control below the visible part of a hider is reported on the vertical axis", async (t) => {
    await inPage(t, `<div style="height:20px;overflow:hidden"><p style="margin:0;height:30px">строка</p><button>Кнопка внизу</button></div>`, async (page) => {
      const v = await layoutViolations(page);
      assert.ok(v.some((x) => x.rule === "clipped" && /по вертикали/.test(x.detail) && x.element.includes("button")), lines(v).join("\n"));
    });
  });

  test("links, tabs, inputs, selects and textareas count as controls", async (t) => {
    await inPage(
      t,
      `<div style="width:60px;overflow:hidden;white-space:nowrap">
         <a href="#x" style="display:inline-block;width:120px">ссылка длинная</a>
         <div role="tab" tabindex="0" style="width:120px">вкладка длинная</div>
         <input aria-label="поле" style="width:120px">
         <select aria-label="выбор" style="width:120px"><option>один</option></select>
         <textarea aria-label="текст" style="width:120px"></textarea>
       </div>`,
      async (page) => {
        const v = await layoutViolations(page);
        const clipped = v.filter((x) => x.rule === "clipped");
        assert.equal(clipped.length, 5, lines(v).join("\n"));
      },
    );
  });

  test("not clipped: aria-hidden / inert subtrees, hidden and invisible inputs, a native select that fits, fixed and escaping absolute controls", async (t) => {
    await inPage(
      t,
      `${cutButton(160, "width:100px;overflow:hidden").replace("<div ", '<div aria-hidden="true" ')}
       ${cutButton(160, "width:100px;overflow:hidden").replace("<div ", "<div inert ")}
       <div style="width:50px;overflow:hidden"><input type="hidden" value="x"><label>Файл<input type="file" style="position:absolute;opacity:0;width:300px"></label></div>
       <div style="width:300px;overflow:hidden"><select aria-label="Один"><option>Один</option></select></div>
       <div style="width:100px;overflow:hidden"><button style="position:fixed;left:0;top:500px;width:200px;white-space:nowrap">Закреплена снизу</button></div>
       <div style="position:relative"><div style="width:100px;overflow:hidden"><button style="position:absolute;left:0;top:200px;width:200px;white-space:nowrap">Вне обрезки</button></div></div>`,
      async (page) => {
        const v = await layoutViolations(page);
        assert.deepEqual(v, [], lines(v).join("\n"));
      },
    );
  });

  test("an absolute control inside its containing block that hides it is reported", async (t) => {
    await inPage(
      t,
      `<div style="position:relative;width:100px;height:30px;overflow:hidden"><button style="position:absolute;left:80px;top:0;width:120px;white-space:nowrap">Кнопка далеко</button></div>`,
      async (page) => {
        const v = await layoutViolations(page);
        assert.deepEqual(rules(v), ["clipped"], lines(v).join("\n"));
      },
    );
  });

  test("a text block that hides its lines is reported; scrolling, a line clamp and a box without text are not", async (t) => {
    const three = "Первая строка<br>Вторая строка<br>Третья строка";
    await inPage(t, `<div style="height:20px;overflow:hidden;line-height:20px">${three}</div>`, async (page) => {
      const v = await layoutViolations(page);
      assert.deepEqual(rules(v), ["clipped"], lines(v).join("\n"));
      assert.match(v[0]?.detail ?? "", /текст «Первая строкаВторая строкаТретья строка» обрезан навсегда: scrollHeight 60 > clientHeight 20/);
    });
    await inPage(t, `<div style="height:20px;overflow:auto;line-height:20px">${three}</div>`, async (page) => assert.deepEqual(await layoutViolations(page), []));
    await inPage(
      t,
      `<p title="Первая строка Вторая строка Третья строка" style="margin:0;width:200px;line-height:20px;display:-webkit-box;-webkit-box-orient:vertical;-webkit-line-clamp:1;overflow:hidden">${three}</p>`,
      async (page) => assert.deepEqual(await layoutViolations(page), [], "a clamp cuts on purpose and has its tooltip"),
    );
    await inPage(t, `<div style="height:20px;overflow:hidden"><div style="height:60px;background:#ccc"></div></div>`, async (page) => assert.deepEqual(await layoutViolations(page), [], "no text, nothing lost"));
  });
});

describe("truncated texts in headless Edge", () => {
  const e = "width:120px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap";
  const long = "Очень длинное название предмета";

  test("an ellipsis text without a tooltip fails; a title on itself or within two ancestors, aria-describedby and a tooltip element pass", async (t) => {
    await inPage(
      t,
      `<div id="a" style="${e}">${long}</div>
       <div id="b" style="${e}" title="${long}">${long}</div>
       <div title="Название"><div id="c" style="${e}">${long}</div></div>
       <div title="Название"><div><div id="d" style="${e}">${long}</div></div></div>
       <div title="Название"><div><div><div id="e" style="${e}">${long}</div></div></div></div>
       <div id="f" style="${e}" aria-describedby="tipf">${long}</div><span id="tipf" hidden>Полный текст названия</span>
       <div id="g" style="${e}">Короткий</div>
       <div id="h" style="${e}">${long}<span class="ia-tip" style="display:none">Полное название предмета</span></div>
       <div id="i" style="${e}" title="  ">${long}</div>
       <div id="j" style="${e}">${long}</div><span class="tip" style="display:none">Полное название предмета</span>`,
      async (page) => {
        const v = await layoutViolations(page);
        assert.deepEqual(rules(v), ["truncated"], lines(v).join("\n"));
        const ids = v.map((x) => /div#(\w)/.exec(x.element)?.[1]).sort();
        assert.deepEqual(ids, ["a", "e", "i"], lines(v).join("\n"));
        assert.match(v[0]?.detail ?? "", /обрезан по ширине/);
        assert.deepEqual(await layoutViolations(page, { rules: { truncated: false } }), []);
      },
    );
  });

  test("a line clamp that cuts the text needs a tooltip; a clamp that does not cut does not", async (t) => {
    const clamp = "margin:0;width:200px;line-height:20px;display:-webkit-box;-webkit-box-orient:vertical;-webkit-line-clamp:2;overflow:hidden";
    const body = "Раз два три четыре пять шесть семь восемь девять десять одиннадцать двенадцать тринадцать четырнадцать пятнадцать";
    await inPage(t, `<p id="cut" style="${clamp}">${body}</p><p id="fits" style="${clamp}">Коротко</p><p id="tip" style="${clamp}" title="${body}">${body}</p>`, async (page) => {
      const v = await layoutViolations(page);
      assert.deepEqual(rules(v), ["truncated"], lines(v).join("\n"));
      assert.equal(v.length, 1);
      assert.match(v[0]?.element ?? "", /p#cut/);
      assert.match(v[0]?.detail ?? "", /обрезан по высоте/);
    });
  });

  test("a row title (title selector) that is cut without an ellipsis is checked too; an uncut or overflowing-visible one is not; any width", async (t) => {
    await inPage(
      t,
      `<div class="tx" style="width:260px"><p class="t" id="cut" style="margin:0;overflow:hidden;white-space:nowrap;width:130px">${long}</p></div>
       <div class="tx" style="width:260px"><p class="t" id="visible" style="margin:0;white-space:nowrap;width:130px">${long}</p></div>
       <div class="tx" style="width:260px"><p class="t" id="fits" style="margin:0;overflow:hidden;white-space:nowrap">Коротко</p></div>
       <div style="width:130px;overflow:hidden;white-space:nowrap"><span id="plain">${long}</span></div>`,
      async (page) => {
        const v = await layoutViolations(page);
        const t2 = v.filter((x) => x.rule === "truncated");
        assert.equal(t2.length, 1, lines(v).join("\n"));
        assert.match(t2[0]?.element ?? "", /p#cut/);
      },
      { width: 1400, height: 600 },
    );
  });
});

describe("raw text in attributes and tooltips in headless Edge", () => {
  test("title, aria-label, placeholder and tooltip text are scanned; hidden, closed and data-raw-ok ones are not", async (t) => {
    await inPage(
      t,
      `<button id="t" title="permanent failure: hub: unavailable">Повторить</button>
       <input id="p" placeholder="Значение: null" aria-label="Поле">
       <button id="a" aria-label="Закрыть [canceled]">Закрыть</button>
       <span class="tip" style="opacity:0">Invalid Date</span>
       <span class="ia-tip" style="display:none">dial tcp 127.0.0.1:1</span>
       <div style="display:none"><span class="tip">sql: no rows</span><button title="undefined">x</button></div>
       <button title="undefined" style="display:none">скрыта</button>
       <div data-raw-ok><p>Error: строка журнала</p><button title="undefined">журнал</button><span role="tooltip">connection refused</span></div>
       <p title="Обычная подсказка">ok</p>`,
      async (page) => {
        const v = await layoutViolations(page);
        assert.deepEqual(rules(v), ["raw-text"], lines(v).join("\n"));
        assert.equal(v.length, 5, lines(v).join("\n"));
        const detail = v.map((x) => x.detail).join("\n");
        assert.match(detail, /в атрибуте title служебный текст «permanent failure»/);
        assert.match(detail, /в атрибуте placeholder служебный текст «null»/);
        assert.match(detail, /в атрибуте aria-label служебный текст «\[canceled\]»/);
        assert.match(detail, /в подсказке служебный текст «Invalid Date»/);
        assert.match(detail, /в подсказке служебный текст «dial tcp»/);
      },
    );
  });

  test("the new backend forms are caught in visible text, and data-raw-ok skips it", async (t) => {
    await inPage(
      t,
      `<p>Запись: null</p><p>Дата Invalid Date</p><p>[unavailable] не удалось</p><p>context deadline exceeded</p><p>sql: no rows in result set</p>
       <div data-raw-ok><p>hub: журнал сервера, connection refused, sql: x</p></div>
       <p>Нет подключения к Hub</p>`,
      async (page) => {
        const v = await layoutViolations(page);
        assert.deepEqual(rules(v), ["raw-text"]);
        assert.equal(v.length, 5, lines(v).join("\n"));
      },
    );
  });
});

describe("icon buttons: glyphs, tabs, menu items in headless Edge", () => {
  test("a button whose only text is a glyph needs a name and a tooltip like an icon button", async (t) => {
    await inPage(
      t,
      `<button id="g1">×</button><button id="g2" aria-label="Закрыть" title="Закрыть">×</button><button id="g3">⋯</button><button id="g4">+</button><button id="g5">›</button>
       <button id="g6" aria-label="Ещё" title="Ещё">…</button><button id="g8">🗑</button>
       <button>Ещё…</button><button>12</button><button>+ Добавить</button>`,
      async (page) => {
        const v = await layoutViolations(page);
        assert.deepEqual(rules(v), ["icon-button"], lines(v).join("\n"));
        assert.equal(v.length, 5, lines(v).join("\n"));
        const ids = v.map((x) => /button#(g\d)/.exec(x.element)?.[1]).sort();
        assert.deepEqual(ids, ["g1", "g3", "g4", "g5", "g8"]);
        assert.match(v.find((x) => x.element.includes("g1"))?.detail ?? "", /кнопка только с символом «×»/);
      },
    );
  });

  test("tabs and menu items without text are checked like buttons; with text they are not", async (t) => {
    await inPage(
      t,
      `<div role="tablist"><button role="tab" id="t1">${svg}</button><div role="tab" tabindex="0" id="t2">${svg}</div>
         <div role="tab" tabindex="0" aria-label="Файлы" title="Файлы">${svg}</div><div role="tab" tabindex="0">Конспект</div></div>
       <div role="menu"><div role="menuitem" tabindex="0" id="m1">${svg}</div><div role="menuitem" tabindex="0">Удалить</div></div>`,
      async (page) => {
        const v = await layoutViolations(page);
        assert.deepEqual(rules(v), ["icon-button"], lines(v).join("\n"));
        assert.equal(v.length, 3, lines(v).join("\n"));
        assert.ok(v.every((x) => /\[role=(tab|menuitem)\]/.test(x.element)), lines(v).join("\n"));
      },
    );
  });
});

describe("counts and session hooks in headless Edge", () => {
  const page1 = `
    <nav aria-label="Разделы"><button aria-label="Сегодня" title="Сегодня">${svg}</button><button aria-label="Записи" title="Записи">${svg}</button></nav>
    <main>
      <h1>Записи занятий</h1>
      <p class="t">Математический анализ и аналитическая геометрия</p>
      <span class="chip" data-status="ok" style="background:transparent">Готово</span>
      <span class="chip" data-status="run" style="background:transparent">Идёт обработка…</span>
      <button>Сохранить изменения</button>
    </main>`;

  test("the report says what was examined: icon buttons, statuses, titles, text nodes", async (t) => {
    await inPage(t, page1, async (page) => {
      const r = await layoutReport(page);
      assert.deepEqual(r.violations, []);
      assert.equal(r.counts.iconButtons, 2);
      assert.equal(r.counts.buttons, 3);
      assert.equal(r.counts.statuses, 2);
      assert.equal(r.counts.titles, 4, "text runs of 12+ characters: h1, the paragraph, «Идёт обработка…», the button");
      assert.ok(r.counts.textNodes >= 5, JSON.stringify(r.counts));
      assert.ok(r.counts.visibleElements >= 9, JSON.stringify(r.counts));
      assert.match(formatCounts(r.counts), /^кнопок-значков 2, статусов 2, заголовков 4, текстов \d+$/);
    });
    await inPage(t, "", async (page) => {
      const r = await layoutReport(page);
      assert.equal(r.counts.statuses, 0, "an empty page examines nothing: the line shows it");
      assert.equal(r.counts.textNodes, 0);
    });
  });

  test("the session records the counts per state and calls the hook after every check, clean or not", async (t) => {
    await inPage(t, page1, async (page) => {
      const seen: Array<[string, number, number | undefined]> = [];
      const s = layoutSession(page, {}, { onChecked: (name, v, counts) => void seen.push([name, v.length, counts?.statuses]) });
      await s.check("чистое");
      await page.evaluate(() => {
        document.querySelector("main")!.insertAdjacentHTML("beforeend", "<p>Итог: NaN</p>");
      });
      await s.check("с ошибкой");
      await s.checkAgent("лента");
      assert.deepEqual(seen, [
        ["чистое", 0, 2],
        ["с ошибкой", 1, 2],
        ["лента", 0, undefined],
      ]);
      assert.equal(s.findings[0]?.counts?.statuses, 2);
      assert.equal(s.findings[2]?.counts, undefined);
      assert.throws(() => s.assertAll(), /Раскладка «с ошибкой»/);
    });
  });

  test("a hook that throws or rejects never hides a finding", async (t) => {
    await inPage(t, `<p>Итог: NaN</p>`, async (page) => {
      const s = layoutSession(page, {}, {
        onChecked: () => {
          throw new Error("снимок не получился");
        },
      });
      const v = await s.check("состояние");
      assert.equal(v.length, 1);
      assert.throws(() => s.assertAll(), /Раскладка «состояние»/);
      const s2 = layoutSession(page, {}, { onChecked: () => Promise.reject(new Error("async hook")) });
      assert.equal((await s2.check("второе")).length, 1);
    });
  });
});
