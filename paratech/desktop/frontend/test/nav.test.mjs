import { test } from "node:test";
import assert from "node:assert/strict";
import { defaultPage, pageById, pages } from "../.test-build/nav.js";

test("navigation has the five sections in order", () => {
  assert.deepEqual(
    pages.map((p) => p.label),
    ["Записи", "Модели", "Домашние задания", "Расписание", "Настройки"],
  );
  assert.equal(new Set(pages.map((p) => p.id)).size, pages.length);
});

test("an unknown page falls back to the default", () => {
  assert.equal(pageById("settings").id, "settings");
  assert.equal(pageById("nope").id, defaultPage);
});
