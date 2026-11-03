// TASK-356: «Адрес сервера» sits under a folded «Дополнительно» of the Hub section.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { defaultHubUrl, hubAddressOpen, hubSetupHint } from "../.test-build/connectionsView.js";

const src = (rel) => readFileSync(new URL(rel, import.meta.url), "utf8");
const none = { savedUrl: "", typedUrl: "", errors: [], chosen: null };

test("the core has no built-in Hub address: empty is the default and stays folded", () => {
  assert.equal(defaultHubUrl, "");
  assert.equal(hubAddressOpen(none), false);
  assert.equal(hubAddressOpen({ ...none, savedUrl: "  ", typedUrl: " " }), false);
});

test("a non-default saved or typed address opens the block by itself", () => {
  assert.equal(hubAddressOpen({ ...none, savedUrl: "https://hub.example.org" }), true);
  assert.equal(hubAddressOpen({ ...none, typedUrl: "https://hub.example.org" }), true);
});

test("a refused address opens the block", () => {
  const errors = [{ field: "hubUrl", message: "Адрес Hub: …" }];
  assert.equal(hubAddressOpen({ ...none, errors }), true);
  assert.equal(hubAddressOpen({ ...none, errors: [{ field: "", message: "Не удалось сохранить" }] }), true);
});

test("a choice made by hand wins over the automatic state during the visit", () => {
  const set = { ...none, savedUrl: "https://hub.example.org" };
  assert.equal(hubAddressOpen({ ...set, chosen: false }), false);
  assert.equal(hubAddressOpen({ ...none, chosen: true }), true);
  // Saving an emptied address does not close the block under the student's hands.
  assert.equal(hubAddressOpen({ ...none, chosen: true, savedUrl: "", typedUrl: "" }), true);
});

test("the setup hint names the next step and where the folded address is", () => {
  const both = hubSetupHint({ hubUrl: "", hubKey: "unset" });
  assert.match(both, /«Сохранить ключ»/);
  assert.match(both, /«Дополнительно»/);
  assert.match(hubSetupHint({ hubUrl: "https://hub.example.org", hubKey: "unset" }), /Вставьте его ниже и нажмите «Сохранить ключ»/);
  assert.match(hubSetupHint({ hubUrl: "", hubKey: "set" }), /Откройте раздел «Дополнительно»/);
  assert.equal(hubSetupHint({ hubUrl: "https://hub.example.org", hubKey: "set" }), "");
  assert.equal(hubSetupHint({ hubUrl: "https://hub.example.org", hubKey: "unavailable" }), "");
});

test("the Hub page: the key stays visible, the address is in a native folded block, the hint is by the key", () => {
  const page = src("../src/pages/ConnectionsPage.tsx");
  const keyAt = page.search(/<SecretSlot\s+nested\s+labels=\{hubKeyLabels\}/);
  const detailsAt = page.indexOf('<details className="conn-adv"');
  const fieldAt = page.indexOf('label="Адрес сервера"');
  assert.ok(keyAt > 0 && detailsAt > keyAt && fieldAt > detailsAt, "key, then details, then the address field");
  assert.ok(page.indexOf("</details>") > fieldAt);
  assert.match(page, /<summary>\s*Дополнительно/);
  assert.match(page, /open=\{addressOpen\} onToggle=/);
  // The save button of the address lives inside the block, so it is never hidden from an open field.
  assert.ok(page.indexOf("void saveHub()") > detailsAt && page.indexOf("void saveHub()") < page.indexOf("</details>"));
  assert.match(page, /hubSetupHint\(saved\)/);
});
