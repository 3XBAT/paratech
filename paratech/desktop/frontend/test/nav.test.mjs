import { test } from "node:test";
import assert from "node:assert/strict";
import { createElement as h } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import {
  bottomGroupStart,
  defaultPage,
  pageById,
  pages,
  rail,
  railItemOf,
  routeToPage,
  settingsAdvancedCaption,
  settingsAdvancedPages,
  settingsMainPages,
  settingsPages,
  settingsPermissionsSlot,
} from "../.test-build/nav.js";
import { Rail } from "../.test-build/ui/Rail.js";

// TASK-348: «Записи» is a section of its own; the rail has seven icon-only items.
test("the rail has seven items in order: five on top, «Журнал» and «Настройки» below", () => {
  assert.deepEqual(
    rail.map((r) => r.label),
    ["Сегодня", "Записи", "Предметы", "Задания", "Расписание", "Журнал", "Настройки"],
  );
  assert.equal(bottomGroupStart, "journal");
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
  assert.equal(railItemOf("recordings").id, "recordings");
  // TASK-356: the main group first, then the «Дополнительно» group; every page is still in the menu once.
  assert.deepEqual(settingsPages, ["settings", "connections", "profile", "appearance", "models", "diagnostics"]);
  assert.deepEqual(settingsAdvancedPages, ["models", "diagnostics"]);
  assert.equal(settingsAdvancedCaption, "Дополнительно");
  assert.equal(new Set(settingsPages).size, settingsPages.length);
  for (const id of settingsPages) {
    assert.equal(railItemOf(id).id, "settings", id);
  }
});

test("the order leaves the slot for «Разрешения» right after «Основные»", () => {
  assert.equal(settingsPermissionsSlot, 1);
  assert.equal(settingsMainPages[settingsPermissionsSlot - 1], "settings");
  assert.equal(settingsMainPages[settingsPermissionsSlot], "connections");
  // The item itself is TASK-361's: no page of that name yet.
  assert.ok(!pages.some((p) => p.label === "Разрешения"));
  assert.ok(!settingsPages.includes("permissions"));
});

test("an unknown page falls back to the default", () => {
  assert.equal(pageById("settings").id, "settings");
  assert.equal(pageById("homework").label, "Задания");
  assert.equal(pageById("recordings").label, "Записи");
  assert.equal(pageById("nope").id, defaultPage);
  assert.equal(routeToPage("journal").page, "journal");
});

test("the rail shows icons only: the name is a tooltip and the aria-label, never visible text; marks the current item", () => {
  const out = renderToStaticMarkup(h(Rail, { current: "models", onOpen: () => undefined }));
  assert.equal((out.match(/<button/g) ?? []).length, 7);
  for (const label of ["Сегодня", "Записи", "Предметы", "Задания", "Расписание", "Журнал", "Настройки"]) {
    assert.match(out, new RegExp(`aria-label="${label}"`));
    // The tooltip holds the name; it is hidden from the accessibility tree (the button is named by aria-label).
    assert.match(out, new RegExp(`<span class="tip" aria-hidden="true">${label}</span>`));
  }
  assert.equal((out.match(/class="tip"/g) ?? []).length, 7, "one tooltip per item");
  // No visible label under the icon.
  assert.doesNotMatch(out, /rail-label/);
  // A screen inside Настройки keeps the Настройки item current, and only that one.
  assert.equal((out.match(/aria-current="page"/g) ?? []).length, 1);
  assert.match(out, /aria-label="Настройки" aria-current="page"/);
  // The bottom group starts at «Журнал»: only that button carries the push-down class.
  assert.equal((out.match(/class="low"/g) ?? []).length, 1);
  assert.match(out, /class="low" aria-label="Журнал"/);
  // «Записи» is current on its own page.
  const rec = renderToStaticMarkup(h(Rail, { current: "recordings", onOpen: () => undefined }));
  assert.match(rec, /aria-label="Записи" aria-current="page"/);
});
