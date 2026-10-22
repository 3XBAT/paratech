// The drop zone (TASK-124): the injectable onFilesDropped, the native event, the DOM fallback. A fake binding stands for
// LibraryService.ImportDroppedFile; paths are untrusted and never appear in the report.
import { test } from "node:test";
import assert from "node:assert/strict";
import {
  createFilesDropped,
  droppedEvent,
  dropTexts,
  hasFiles,
  maxDropped,
  nextDepth,
  parseDroppedPaths,
  pathsFromTransfer,
  subscribeDropped,
} from "../.test-build/dropFiles.js";

const material = { id: "m1", name: "a.pdf" };
const imported = { cancelled: false, existing: false, material };

function run(api, paths) {
  const reports = [];
  let done = 0;
  const fn = createFilesDropped({ importDroppedFile: api }, (l) => reports.push(l), () => done++);
  return fn(paths).then(() => ({ reports, done }));
}

test("a dropped file is imported through the binding and reported in fixed Russian text", async () => {
  const seen = [];
  const { reports, done } = await run(async (p) => (seen.push(p), imported), ["C:\\Users\\me\\Docs\\secret-plan.pdf"]);
  assert.deepEqual(seen, ["C:\\Users\\me\\Docs\\secret-plan.pdf"]);
  assert.deepEqual(reports, [[{ ok: true, text: dropTexts.added }]]);
  assert.equal(done, 1);
  assert.ok(!JSON.stringify(reports).includes("secret-plan"));
});

test("a file that was in the library already is said so", async () => {
  const { reports } = await run(async () => ({ ...imported, existing: true }), ["C:\\a.txt"]);
  assert.deepEqual(reports[0], [{ ok: true, text: dropTexts.existing }]);
});

test("a refusal of the core is shown as its Russian text; an unknown error is generic; neither echoes the path", async () => {
  const known = await run(async () => { throw new Error("Подходят только DOCX, PPTX, PDF и текст в UTF-8."); }, ["C:\\a.exe"]);
  assert.deepEqual(known.reports[0], [{ ok: false, text: "Подходят только DOCX, PPTX, PDF и текст в UTF-8." }]);
  const odd = await run(async () => { throw new Error("read C:\\Users\\me\\a.pdf: sharing violation"); }, ["C:\\Users\\me\\a.pdf"]);
  assert.deepEqual(odd.reports[0], [{ ok: false, text: "Не удалось выполнить действие." }]);
  assert.ok(!JSON.stringify(odd.reports).includes("Users"));
});

test("one failed file does not stop the next; each gets its own line", async () => {
  let n = 0;
  const { reports } = await run(async () => {
    if (n++ === 0) {
      throw new Error("Файл пустой.");
    }
    return imported;
  }, ["C:\\1.txt", "C:\\2.txt"]);
  assert.deepEqual(reports[0], [{ ok: false, text: "Файл пустой." }, { ok: true, text: dropTexts.added }]);
});

test("a drop without a path says to use «Добавить файл» and calls nothing", async () => {
  let calls = 0;
  const { reports, done } = await run(async () => (calls++, imported), []);
  assert.equal(calls, 0);
  assert.equal(done, 0);
  assert.deepEqual(reports, [[{ ok: false, text: dropTexts.noPath }]]);
  assert.match(dropTexts.noPath, /«Добавить файл»/);
  const blank = await run(async () => (calls++, imported), ["", 5]);
  assert.equal(calls, 0);
  assert.deepEqual(blank.reports, [[{ ok: false, text: dropTexts.noPath }]]);
});

test("a cancelled result is not reported as added; too many files are cut", async () => {
  const cancelled = await run(async () => ({ cancelled: true, existing: false, material: null }), ["C:\\a.txt"]);
  assert.deepEqual(cancelled.reports[0], [{ ok: false, text: dropTexts.nothing }]);
  let calls = 0;
  const many = await run(async () => (calls++, imported), Array.from({ length: maxDropped + 3 }, (_, i) => `C:\\f${i}.txt`));
  assert.equal(calls, maxDropped);
  assert.equal(many.reports[0].at(-1).text, dropTexts.tooMany);
});

test("the event payload is read from a list, a list in a list or one path; anything else is no path", () => {
  assert.deepEqual(parseDroppedPaths(["C:\\a.txt", "C:\\b.txt"]), ["C:\\a.txt", "C:\\b.txt"]);
  assert.deepEqual(parseDroppedPaths([["C:\\a.txt"]]), ["C:\\a.txt"]);
  assert.deepEqual(parseDroppedPaths("C:\\a.txt"), ["C:\\a.txt"]);
  assert.deepEqual(parseDroppedPaths(["C:\\a.txt", 7, null, ""]), ["C:\\a.txt"]);
  for (const bad of [null, undefined, 5, {}, ""]) {
    assert.deepEqual(parseDroppedPaths(bad), []);
  }
});

test("the subscription passes the paths of files:dropped on, and does nothing without a Wails runtime", async () => {
  assert.equal(droppedEvent, "files:dropped");
  let handler;
  let name;
  const got = [];
  const off = await subscribeDropped(async (n, h) => ((name = n), (handler = h), () => got.push("off")), (p) => got.push(p));
  assert.equal(name, "files:dropped");
  handler(["C:\\a.txt"]);
  assert.deepEqual(got, [["C:\\a.txt"]]);
  off();
  assert.equal(got.at(-1), "off");

  const absent = await subscribeDropped(async () => { throw new Error("Failed to fetch /wails/runtime.js"); }, () => assert.fail("no event"));
  assert.equal(typeof absent, "function");
  absent();
});

test("the DOM drop gives paths only where the webview exposes them", () => {
  assert.deepEqual(pathsFromTransfer({ files: [{ name: "a.pdf" }] }), []);
  assert.deepEqual(pathsFromTransfer({ files: [{ name: "a.pdf", path: "C:\\a.pdf" }, { path: 5 }] }), ["C:\\a.pdf"]);
  assert.deepEqual(pathsFromTransfer(null), []);
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
