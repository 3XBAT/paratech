import { test } from "node:test";
import assert from "node:assert/strict";
import { createElement as h } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { defaultPage, pageById, pages, rail, railItemOf, settingsPages, routeToPage } from "../.test-build/nav.js";
import { Rail } from "../.test-build/ui/Rail.js";

test("the rail has six icons in order", () => {
  assert.deepEqual(
    rail.map((r) => r.label),
    ["Сегодня", "Предметы", "Задания", "Расписание", "Журнал", "Настройки"],
  );
  assert.equal(new Set(rail.map((r) => r.id)).size, rail.length);
  assert.equal(new Set(pages.map((p) => p.id)).size, pages.length);
});

test("every page belongs to exactly one rail item; the moved screens are inside Настройки", () => {
  for (const p of pages) {
    assert.equal(rail.filter((r) => r.covers.includes(p.id)).length, 1, p.id);
  }
  for (const id of ["models", "connections", "diagnostics", "profile", "appearance"]) {
    assert.equal(railItemOf(id).id, "settings", id);
  }
  assert.equal(railItemOf("knowledge").id, "subjects");
  assert.deepEqual(settingsPages, ["settings", "models", "connections", "diagnostics", "profile", "appearance"]);
});

test("an unknown page falls back to the default", () => {
  assert.equal(pageById("settings").id, "settings");
  assert.equal(pageById("homework").label, "Задания");
  assert.equal(pageById("nope").id, defaultPage);
  assert.equal(routeToPage("journal").page, "journal");
});

test("the rail shows icons with the name as label and tooltip, and marks the current item", () => {
  const out = renderToStaticMarkup(h(Rail, { current: "models", onOpen: () => undefined }));
  assert.equal((out.match(/<button/g) ?? []).length, 6);
  for (const label of ["Сегодня", "Предметы", "Задания", "Расписание", "Журнал", "Настройки"]) {
    assert.match(out, new RegExp(`aria-label="${label}"`));
    assert.match(out, new RegExp(`<span class="tip" aria-hidden="true">${label}</span>`));
  }
  // A screen inside Настройки keeps the Настройки icon current, and only that one.
  assert.equal((out.match(/aria-current="page"/g) ?? []).length, 1);
  assert.match(out, /aria-label="Настройки" aria-current="page"/);
  assert.match(out, /class="low"/);
});
