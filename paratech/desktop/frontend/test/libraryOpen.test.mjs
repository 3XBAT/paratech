// «Открыть в Word / PowerPoint» (TASK-177): the binding passes only the ID and shows only the fixed texts.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { libraryApi, methods, openTexts, parseOpen } from "../.test-build/bindings/library.js";

test("the open texts match the Go binding", () => {
  const go = readFileSync(new URL("../../internal/input/wails/library_open.go", import.meta.url), "utf8");
  const text = (name) => go.match(new RegExp(`${name}\\s*=\\s*"([^"]*)"`))[1];
  assert.equal(openTexts.opened, text("msgOpenOpened"));
  assert.equal(openTexts.not_found, text("msgOpenNotFound"));
  assert.equal(openTexts.refused, text("msgOpenRefused"));
  assert.equal(openTexts.failed, text("msgOpenFailed"));
  assert.equal(methods.openMaterial, "paratech/desktop/internal/input/wails.LibraryService.OpenMaterial");
});

test("openMaterial sends only the ID and reads the status", async () => {
  const calls = [];
  const api = libraryApi(async (...args) => {
    calls.push(args);
    return { status: "not_found", message: "Файл не найден: возможно, его удалили." };
  });
  assert.deepEqual(await api.openMaterial("m1"), { status: "not_found", message: openTexts.not_found });
  assert.deepEqual(calls, [[methods.openMaterial, "m1"]]);
});

test("a reply is never shown as it came: unknown or failed calls are «failed» with the fixed text", async () => {
  assert.deepEqual(parseOpen({ status: "opened", message: "C:\\Users\\me\\secret.docx" }), { status: "opened", message: openTexts.opened });
  for (const raw of [null, undefined, "opened", [], { status: "toString" }, { status: "weird" }, { message: "x" }]) {
    assert.deepEqual(parseOpen(raw), { status: "failed", message: openTexts.failed }, JSON.stringify(raw));
  }
  const api = libraryApi(async () => {
    throw new Error("open C:\\secret.docx: boom");
  });
  assert.deepEqual(await api.openMaterial("m1"), { status: "failed", message: openTexts.failed });
});
