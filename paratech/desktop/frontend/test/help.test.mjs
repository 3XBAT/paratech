// Help screen «Как увидеть события в Apple Calendar» (TASK-052). Runs on the modules compiled by `npm run test`.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import server from "react-dom/server";
import { helpApi, helpErrors, helpPageKeys, methods } from "../.test-build/bindings/help.js";
import {
  helpLinks,
  helpOptions,
  helpTexts,
  linksNote,
  openErrorText,
  openHelpLink,
  troublePoints,
  verdictTexts,
} from "../.test-build/appleHelpView.js";
import { HelpView } from "../.test-build/pages/AppleCalendarHelpPage.js";

const { renderToStaticMarkup } = server;
const src = (file) => readFileSync(new URL(file, import.meta.url), "utf8");
const goSrc = src("../../internal/input/wails/help.go");
const method = "paratech/desktop/internal/input/wails.HelpService.OpenHelpPage";

const render = (failure = null) =>
  renderToStaticMarkup(h(HelpView, { failure, onOpen: () => {}, onBack: () => {} }));

// Every string of the screen's copy.
function strings(v) {
  if (typeof v === "string") {
    return [v];
  }
  if (Array.isArray(v)) {
    return v.flatMap(strings);
  }
  if (typeof v === "object" && v !== null) {
    return Object.values(v).flatMap(strings);
  }
  return [];
}

test("the options come in the PRD order: A recommended, B the fallback, C not recommended", () => {
  assert.deepEqual(
    helpOptions.map((o) => [o.id, o.verdict]),
    [["A", "recommended"], ["B", "fallback"], ["C", "notRecommended"]],
  );
  assert.deepEqual(
    helpOptions.map((o) => verdictTexts[o.verdict]),
    ["Рекомендуется", "Запасной вариант", "Не рекомендуется"],
  );
  const markup = render();
  const at = (s) => {
    const i = markup.indexOf(s);
    assert.ok(i >= 0, s);
    return i;
  };
  const a = at("Способ 1. Через аккаунт Google");
  const b = at("Способ 2. Подписка по закрытой ссылке");
  const c = at("Способ 3. Сделать календарь общедоступным");
  assert.ok(a < at("Рекомендуется") && at("Рекомендуется") < b);
  assert.ok(b < at("Запасной вариант") && at("Запасной вариант") < c);
  assert.ok(c < at("Не рекомендуется"));
  assert.ok(at(helpTexts.title) < a);
  assert.ok(at(helpTexts.editTitle) < a, "the editing note comes before the options");
  assert.ok(c < at(helpTexts.troubleTitle));
  assert.equal(markup.split(`>${helpTexts.back}<`).length - 1, 2, "a back button at the top and at the bottom");
});

test("the copy keeps the product rules", () => {
  const all = strings({ helpTexts, helpOptions, troublePoints }).join("\n");
  assert.match(helpTexts.editText, /в приложение не попадают/);
  assert.match(all, /«Закрытый адрес в формате iCal»/);
  assert.match(all, /не просит пароль от Apple ID и не обращается к Apple/);
  assert.match(all, /никогда не делает календарь общедоступным/);
  assert.match(all, /Никому не передавайте ссылку/);
  assert.match(all, /«Сбросить»/);
  assert.match(all, /Совет: выберите один способ/);
  assert.doesNotMatch(all, /Learning Assistant|Секретный/);
});

test("every link is a key of help.go and every key of help.go is used", () => {
  const goKeys = [...goSrc.matchAll(/"([a-z-]+)":\s+"https:\/\//g)].map((m) => m[1]).sort();
  assert.equal(goKeys.length, 7);
  assert.deepEqual([...helpPageKeys].sort(), goKeys);
  const used = helpLinks().map((l) => l.key);
  for (const k of used) {
    assert.ok(goKeys.includes(k), k);
  }
  assert.deepEqual([...new Set(used)].sort(), goKeys);
  for (const l of helpLinks()) {
    assert.ok(l.caption.length > 0 && !/https?:|www\./.test(l.caption), l.caption);
  }
  for (const text of Object.values(helpErrors)) {
    assert.ok(goSrc.includes(`"${text}"`), text);
  }
});

test("the block ids are unique, so an error shows under the right buttons", () => {
  const ids = helpOptions.flatMap((o) => o.blocks.map((b) => b.id));
  assert.equal(new Set(ids).size, ids.length);
});

test("the opened key reaches the binding", async () => {
  assert.deepEqual(methods, { openHelpPage: method });
  const calls = [];
  const api = helpApi(async (m, ...args) => {
    calls.push([m, ...args]);
    return null;
  });
  for (const k of helpPageKeys) {
    assert.equal(await openHelpLink(api, k), "");
  }
  assert.deepEqual(calls, helpPageKeys.map((k) => [method, k]));
});

test("an error of the binding is shown, an unknown one as the generic text", async () => {
  const failing = (err) => helpApi(async () => { throw err; });
  assert.equal(await openHelpLink(failing(new Error(helpErrors.unknown)), "google-syncselect"), helpErrors.unknown);
  assert.equal(await openHelpLink(failing({ message: helpErrors.failed }), "google-syncselect"), helpErrors.failed);
  assert.equal(await openHelpLink(failing(helpErrors.unknown), "google-syncselect"), helpErrors.unknown);
  for (const err of [new Error("OpenHelpPage: panic: C:\\Users\\x"), "x", null, undefined, 5, {}]) {
    assert.equal(openErrorText(err), helpErrors.failed);
  }

  const markup = renderToStaticMarkup(
    h(HelpView, { failure: { block: "a-missing", text: helpErrors.failed }, onOpen: () => {}, onBack: () => {} }),
  );
  assert.equal(markup.split(helpErrors.failed).length - 1, 1);
  const err = markup.indexOf(helpErrors.failed);
  assert.ok(markup.lastIndexOf('role="alert"', err) > markup.indexOf("Страница синхронизации Google Календаря"));
  assert.ok(err < markup.indexOf("Когда появятся события"), "under the buttons of its block");
  assert.ok(!render().includes('role="alert"'));
});

test("links are buttons with a browser note; the page opens no URL itself", () => {
  const markup = render();
  assert.ok(!markup.includes("<a "));
  assert.ok(!/https?:\/\//.test(markup));
  for (const l of helpLinks()) {
    assert.ok(markup.includes(`>${l.caption}</button>`), l.caption);
  }
  assert.equal(linksNote(1), "Открывается в браузере");
  assert.equal(linksNote(2), "Открываются в браузере");
  for (const file of [
    "../src/appleHelpView.ts",
    "../src/pages/AppleCalendarHelpPage.tsx",
    "../src/bindings/help.ts",
  ]) {
    const s = src(file);
    for (const bad of ["href=", "window.open", "dangerouslySetInnerHTML", "innerHTML", "https://", "http://"]) {
      assert.ok(!s.includes(bad), `${file}: ${bad}`);
    }
  }
});

test("the copy has no first person", () => {
  // Quoted UI labels («Мои календари», «На моем Mac») are Apple's and Google's words, not the app's.
  const all = strings({ helpTexts, helpOptions, troublePoints, verdictTexts }).map((s) => s.replace(/«[^»]*»/g, ""));
  const firstPerson = /(^|[^а-яё])(я|мне|меня|мной|мой|моя|моё|мое|мои|моего|моей|моем|моём|мы|нам|нас|наш|наша|наше|наши)(?![а-яё])/iu;
  for (const s of all) {
    assert.doesNotMatch(s, firstPerson, s);
  }
  assert.ok(all.length > 40);
});

test("the entry on «Подключения» opens the screen and the rail stays as it is", () => {
  const page = src("../src/pages/ConnectionsPage.tsx");
  assert.ok(page.includes("<AppleCalendarHelpPage onBack="));
  assert.ok(page.includes("{helpTexts.entryButton}"));
  assert.equal(helpTexts.entryButton, "Как увидеть события в Apple Calendar");
  assert.equal(helpTexts.entryTitle, "Календарь на iPhone, iPad и Mac");
  assert.ok(!src("../src/nav.ts").includes("help"));
});

test("no UI source names the app by its old names (TASK-069)", async () => {
  const { readdirSync } = await import("node:fs");
  const files = readdirSync(new URL("../src/", import.meta.url), { recursive: true })
    .filter((f) => /\.(ts|tsx|css|html)$/.test(f))
    .map((f) => `../src/${f.replaceAll("\\", "/")}`);
  assert.ok(files.length > 20, "the walk found the sources");
  for (const file of [...files, "../index.html", "../popup.html"]) {
    const text = src(file);
    assert.doesNotMatch(text, /learning.?assistant|Учебн\S* помощник/i, file);
    assert.doesNotMatch(text, /Paratech/, file); // the name is lowercase; binding paths are lowercase too
  }
  assert.match(src("../index.html"), /<title>paratech<\/title>/);
});
