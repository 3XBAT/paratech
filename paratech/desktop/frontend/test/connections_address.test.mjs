// TASK-356: «Адрес сервера» sits in a folded block of the Hub section; the core has no built-in address.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { emptyHubUrl, hubAddressOpen, hubSetupHint, overviewTexts } from "../.test-build/connectionsView.js";

const src = (rel) => readFileSync(new URL(rel, import.meta.url), "utf8");
const url = "https://hub.example.org";
const works = { savedUrl: url, typedUrl: url, errors: [], quotaStatus: "current", chosen: null };

test("an address that is saved and works stays folded", () => {
  assert.equal(emptyHubUrl, "");
  assert.equal(hubAddressOpen(works), false);
  assert.equal(hubAddressOpen({ ...works, quotaStatus: "unchecked" }), false);
});

test("an empty saved address is a missing value: the block opens by itself", () => {
  assert.equal(hubAddressOpen({ ...works, savedUrl: "", typedUrl: "", quotaStatus: "noKey" }), true);
  assert.equal(hubAddressOpen({ ...works, savedUrl: "  ", typedUrl: " " }), true);
});

test("a form being edited stays open, also when the field is emptied", () => {
  assert.equal(hubAddressOpen({ ...works, typedUrl: "https://other.example.org" }), true);
  assert.equal(hubAddressOpen({ ...works, typedUrl: "" }), true);
  assert.equal(hubAddressOpen({ ...works, typedUrl: ` ${url} ` }), false);
});

test("a refused address opens the block", () => {
  const errors = [{ field: "hubUrl", message: "Адрес Hub: …" }];
  assert.equal(hubAddressOpen({ ...works, errors }), true);
  assert.equal(hubAddressOpen({ ...works, errors: [{ field: "", message: "Не удалось сохранить" }] }), true);
});

test("the block opens when the Hub did not answer, but not for key problems", () => {
  for (const quotaStatus of ["offline", "notConfigured", "failed"]) {
    assert.equal(hubAddressOpen({ ...works, quotaStatus }), true, quotaStatus);
  }
  for (const quotaStatus of ["noKey", "keyRejected", "current", "unchecked"]) {
    assert.equal(hubAddressOpen({ ...works, quotaStatus }), false, quotaStatus);
  }
});

test("a choice made by hand wins over the automatic state during the visit", () => {
  assert.equal(hubAddressOpen({ ...works, chosen: true }), true);
  assert.equal(hubAddressOpen({ ...works, savedUrl: "", typedUrl: "", chosen: false }), false);
  assert.equal(hubAddressOpen({ ...works, quotaStatus: "offline", chosen: false }), false);
  // Saving an emptied address does not close the block under the student's hands.
  assert.equal(hubAddressOpen({ ...works, savedUrl: "", typedUrl: "", chosen: true }), true);
});

test("the setup hint: consequence first, then the action, and where the address field is", () => {
  const both = hubSetupHint({ hubUrl: "", hubKey: "unset" });
  assert.match(both, /^Нужны ключ доступа и адрес сервера — их выдаёт автор приложения\./);
  assert.match(both, /«Сохранить ключ»/);
  assert.match(both, /поле «Адрес сервера» ниже/);

  const noUrl = hubSetupHint({ hubUrl: "", hubKey: "set" });
  assert.match(noUrl, /^Не указан адрес сервера: без него конспекты и поиск заданий не работают\./);
  assert.match(noUrl, /поле «Адрес сервера» ниже/);

  const noKey = hubSetupHint({ hubUrl: url, hubKey: "unset" });
  assert.match(noKey, /^Без ключа конспекты и поиск заданий не работают\./);
  assert.match(noKey, /«Сохранить ключ»/);
  assert.doesNotMatch(noKey, /Ключ не задан/);

  assert.equal(hubSetupHint({ hubUrl: url, hubKey: "set" }), "");
  assert.equal(hubSetupHint({ hubUrl: url, hubKey: "unavailable" }), "");
  for (const t of [both, noUrl, noKey]) {
    assert.doesNotMatch(t, /Дополнительно/);
  }
});

test("the Hub texts: one description line, the key and the address come from the app author", () => {
  assert.equal(overviewTexts.hubDesc, "Ключ доступа, адрес сервера и лимит обработки");
  assert.match(overviewTexts.hubHint, /^Ключ доступа и адрес сервера выдаёт автор приложения\. Это не ключ Claude или ChatGPT\./);
  assert.match(overviewTexts.hubHint, /Звук не отправляется/);
  assert.equal([...overviewTexts.hubHint.matchAll(/расшифровк/gi)].length, 1);
});

test("the Hub page: the hint above the key, the address in a native folded block with a chevron", () => {
  const page = src("../src/pages/ConnectionsPage.tsx");
  const hintAt = page.indexOf("{setupHint !== \"\"");
  const keyAt = page.search(/<SecretSlot\s+nested\s+labels=\{hubKeyLabels\}/);
  const detailsAt = page.indexOf('<details className="conn-adv"');
  const fieldAt = page.indexOf('label="Адрес сервера"');
  assert.ok(hintAt > 0 && keyAt > hintAt && detailsAt > keyAt && fieldAt > detailsAt, "hint, key, details, address field");
  assert.ok(page.indexOf("</details>") > fieldAt);
  // The hint follows the divider directly and is not cut off from the key by another one.
  assert.match(page, /<hr className="sep" \/>\s*\{setupHint !== ""[^\n]*\n\s*<SecretSlot/);
  assert.match(page, /<summary>\s*<Icon name="chevr" className="conn-adv-chev" \/>\s*Адрес сервера\s*<\/summary>/);
  assert.doesNotMatch(page, /<summary>[^<]*Дополнительно/);
  assert.match(page, /open=\{addressOpen\} onToggle=/);
  assert.match(page, /quotaStatus: saved\.quota\.status/);
  // The save button of the address lives inside the block, so it is never hidden from an open field.
  assert.ok(page.indexOf("void saveHub()") > detailsAt && page.indexOf("void saveHub()") < page.indexOf("</details>"));
  assert.match(page, /Сохранить адрес\s*<\/Button>/);
  assert.match(page, /hubSetupHint\(saved\)/);
  // A new visit forgets the hand-made choice together with the open section.
  assert.match(page, /setOpen\(null\);\s*setAddrChosen\(null\);/);
});

test("the chevron is drawn by the summary: native marker hidden, turns when open", () => {
  const css = src("../src/styles.css");
  assert.match(css, /\.conn-adv > summary \{[^}]*list-style: none;[^}]*color: var\(--text\)/);
  assert.match(css, /\.conn-adv > summary::-webkit-details-marker \{ display: none; \}/);
  assert.match(css, /\.conn-adv\[open\] > summary \.i \{ transform: rotate\(90deg\); \}/);
  assert.match(css, /\.conn-adv > summary \.i \{[^}]*color: var\(--muted\)/);
});
