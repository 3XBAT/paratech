// BUG-20261108-teachers: the blocks of the subject page are siblings; two with the same key made React keep stale copies,
// so «Преподаватели» was drawn 4-5 times. Pages have no DOM harness, so the keys are read from source.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const page = readFileSync(new URL("../src/pages/SubjectsPage.tsx", import.meta.url), "utf8");

test("the blocks of the subject page have distinct keys", () => {
  const start = page.indexOf("<SubjectHead");
  const end = page.indexOf("<SubjectTabs", start);
  assert.ok(start > 0 && end > start);
  const tail = page.slice(start, page.indexOf("/>", end));
  const keys = [...tail.matchAll(/<(TeachersBlock|CallLinkBlock|SubjectTabs)\b[^]*?key=\{([^}]*(?:\}[^}]*)?)\}/g)].map((m) => [m[1], m[2]]);
  assert.deepEqual(keys.map((k) => k[0]), ["TeachersBlock", "CallLinkBlock", "SubjectTabs"]);
  assert.equal(new Set(keys.map((k) => k[1])).size, keys.length, `keys must differ: ${JSON.stringify(keys)}`);
});

test("no two siblings on the page share the bare subject id as key", () => {
  assert.ok((page.match(/key=\{subject\.id\}/g) ?? []).length <= 1);
});
