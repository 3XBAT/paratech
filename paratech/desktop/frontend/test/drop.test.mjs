// The drop zone (TASK-138): Go imports the dropped files and emits "library:dropped"; the page shows the outcome.
// The page never sees or sends a path; names are plain text.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import {
  dropTargetAttr,
  droppedEvent,
  dropTexts,
  hasFiles,
  nextDepth,
  parseDrop,
  subscribeDropped,
} from "../.test-build/dropFiles.js";

const material = { id: "m1", name: "a.pdf" };

test("each file of a drop gets its own line: added, already there, or the refusal text", () => {
  const out = parseDrop({
    error: "",
    files: [
      { name: "a.pdf", existing: false, material, error: "" },
      { name: "b.pdf", existing: true, material, error: "" },
      { name: "c.exe", existing: false, material: null, error: "Подходят только DOCX, PPTX, PDF и текст в UTF-8." },
      { name: "", existing: false, material: null, error: "Этот файл добавить нельзя: перетащите файл с диска этого компьютера." },
    ],
  });
  assert.equal(out.imported, true);
  assert.deepEqual(out.lines, [
    { ok: true, text: `a.pdf: ${dropTexts.added}` },
    { ok: true, text: `b.pdf: ${dropTexts.existing}` },
    { ok: false, text: "c.exe: Подходят только DOCX, PPTX, PDF и текст в UTF-8." },
    { ok: false, text: "Этот файл добавить нельзя: перетащите файл с диска этого компьютера." },
  ]);
  assert.match(dropTexts.existing, /уже есть/);
});

test("a refusal of the whole drop is shown as it is; no files then", () => {
  const out = parseDrop({ error: "За раз можно перетащить не больше 10 файлов.", files: [] });
  assert.deepEqual(out, { lines: [{ ok: false, text: "За раз можно перетащить не больше 10 файлов." }], imported: false });
});

test("an unknown error text is replaced by the generic one; a payload in a list of one is read", () => {
  const out = parseDrop([{ error: "", files: [{ name: "x.txt", material: null, error: "read C:\\Users\\me\\x.txt: denied" }] }]);
  assert.deepEqual(out.lines, [{ ok: false, text: "x.txt: Не удалось выполнить действие." }]);
  assert.equal(out.imported, false);
  assert.ok(!JSON.stringify(out).includes("Users"));
});

test("a malformed or empty payload is no import", () => {
  for (const bad of [null, undefined, 5, "x"]) {
    const out = parseDrop(bad);
    assert.equal(out.imported, false);
    assert.equal(out.lines.length, 1);
    assert.equal(out.lines[0].ok, false);
  }
  assert.deepEqual(parseDrop({ error: "", files: [] }).lines, [{ ok: false, text: dropTexts.nothing }]);
});

test("a long file name is cut", () => {
  const out = parseDrop({ error: "", files: [{ name: "n".repeat(500), existing: false, material, error: "" }] });
  assert.ok(out.lines[0].text.length < 200);
});

test("the subscription listens to library:dropped, passes the outcome on, and does nothing without a Wails runtime", async () => {
  assert.equal(droppedEvent, "library:dropped");
  let handler;
  let name;
  const got = [];
  const off = await subscribeDropped(async (n, h) => ((name = n), (handler = h), () => got.push("off")), (o) => got.push(o));
  assert.equal(name, "library:dropped");
  handler({ error: "", files: [{ name: "a.pdf", existing: false, material, error: "" }] });
  assert.equal(got[0].imported, true);
  off();
  assert.equal(got.at(-1), "off");

  const absent = await subscribeDropped(async () => { throw new Error("Failed to fetch /wails/runtime.js"); }, () => assert.fail("no event"));
  assert.equal(typeof absent, "function");
  absent();
});

test("body is the persistent Wails drop target, not the overlay; the page has no path or import binding", () => {
  assert.equal(dropTargetAttr, "data-file-drop-target");
  const zone = readFileSync(new URL("../src/pages/DropZone.tsx", import.meta.url), "utf8");
  assert.match(zone, /document\.body\.setAttribute\(dropTargetAttr/);
  assert.match(zone, /document\.body\.removeAttribute\(dropTargetAttr/);
  const overlay = zone.match(/<div className="drop-overlay"[^>]*>/)?.[0] ?? "";
  assert.ok(overlay && !/dropTargetAttr|data-file-drop-target/.test(overlay));
  const css = readFileSync(new URL("../src/styles.css", import.meta.url), "utf8");
  assert.match(css, /html, body, #root \{ height: 100%/);
  for (const f of [zone, readFileSync(new URL("../src/dropFiles.ts", import.meta.url), "utf8")]) {
    assert.ok(!/importDroppedFile|files:dropped|dataTransfer\.files/.test(f));
  }
});

test("hasFiles tells a file drag from text or a link", () => {
  assert.equal(hasFiles({ types: ["text/plain", "Files"] }), true);
  assert.equal(hasFiles({ types: ["text/uri-list"] }), false);
  assert.equal(hasFiles(undefined), false);
});

test("the drag depth counts enter and leave of child elements and resets on drop", () => {
  let d = 0;
  d = nextDepth(d, "enter");
  d = nextDepth(d, "enter");
  d = nextDepth(d, "leave");
  assert.equal(d, 1);
  assert.equal(nextDepth(0, "leave"), 0);
  assert.equal(nextDepth(3, "reset"), 0);
});
