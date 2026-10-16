// Runs on the modules compiled by `npm run test` (tsconfig.test.json) with the Node test runner.
import { test } from "node:test";
import assert from "node:assert/strict";
import {
  formatAmount,
  localTime,
  quotaControls,
  quotaDisplay,
  quotaRefreshable,
  quotaTexts,
  quotaUsedPercent,
  unitLabel,
} from "../.test-build/quotaView.js";

// Mirrors the JSON of internal/input/wails/connections.go (Quota, QuotaValues).
const values = {
  unit: "tokens", remaining: 1234567, reserved: 1500, limit: 2000000, periodEnd: "2026-11-01T00:00:00Z",
  fetchedAt: "2026-10-15T10:00:00Z",
};
const current = { status: "current", stale: false, checkedAt: "2026-10-15T10:00:00Z", values };
const unchecked = { status: "unchecked", stale: false, checkedAt: "", values: null };
const ready = { hubUrl: "https://hub.example.org", hubKey: "set" };

// The fixed texts of Go (quotaMessages in connections.go).
const goText = {
  ok: "Остаток обновлён.",
  offline: "Не удалось связаться с Hub: проверьте подключение к интернету и адрес Hub.",
  keyRejected: "Hub не принял ключ: он неверный или отозван. Введите действующий ключ.",
  notConfigured: "Адрес Hub не задан: укажите его выше и сохраните.",
  noKey: "Ключ доступа к Hub не задан.",
  failed: "Hub не смог ответить на запрос остатка.",
};

const texts = (d) => d.lines.map((l) => l.text);

test("amounts are whole numbers in groups of three with the unit in the label", () => {
  assert.equal(formatAmount(0), "0");
  assert.equal(formatAmount(999), "999");
  assert.equal(formatAmount(1000), "1 000");
  assert.equal(formatAmount(1234567), "1 234 567");
  assert.equal(unitLabel("tokens"), "токенов");
  assert.equal(unitLabel("requests"), "requests");
  assert.equal(unitLabel("toString"), "toString");
});

test("times are local; empty or broken is a dash", () => {
  assert.equal(localTime("2026-10-15T10:00:00Z"), new Date("2026-10-15T10:00:00Z").toLocaleString("ru-RU"));
  assert.equal(localTime(""), "—");
  assert.equal(localTime("yesterday"), "—");
});

test("a current quota: remaining, limit and reserved, the time and the cache note", () => {
  const d = quotaDisplay(current, ready, { text: goText.ok, ok: true });
  assert.deepEqual(d.amounts, [
    { label: "Осталось, токенов", value: "1 234 567" },
    { label: "Лимит, токенов", value: "2 000 000" },
    { label: "Зарезервировано идущими запросами, токенов", value: "1 500" },
  ]);
  assert.equal(d.updated, "Обновлено: " + localTime(values.fetchedAt));
  assert.equal(d.stale, "");
  assert.equal(d.renews, "Лимит обновится: " + localTime(values.periodEnd));
  assert.equal(d.note, quotaTexts.note);
  assert.match(d.note, /копия для показа/);
  assert.match(d.note, /сервер обработки решает/);
  assert.equal(d.checked, "");
  assert.deepEqual(d.lines, [{ text: goText.ok, tone: "ok" }]);

  // Opened again later in the run: the same values without the answer.
  assert.deepEqual(quotaDisplay(current, ready, null).lines, []);
  // A quota that does not renew.
  assert.equal(quotaDisplay({ ...current, values: { ...values, periodEnd: "" } }, ready, null).renews, "");
  // An unknown unit is shown as sent.
  assert.equal(quotaDisplay({ ...current, values: { ...values, unit: "credits" } }, ready, null).amounts[0].label,
    "Осталось, credits");
});

test("Hub offline: the old value marked stale with its time, and the Go text", () => {
  const offline = { status: "offline", stale: true, checkedAt: "2026-10-15T11:00:00Z", values };
  const d = quotaDisplay(offline, ready, { text: goText.offline, ok: false });
  assert.equal(d.amounts.length, 3);
  assert.equal(d.updated, "Обновлено: " + localTime(values.fetchedAt));
  assert.match(d.stale, /^Устарело/);
  assert.equal(d.note, quotaTexts.note);
  assert.equal(d.checked, "Последняя попытка: " + localTime("2026-10-15T11:00:00Z"));
  assert.deepEqual(d.lines, [{ text: goText.offline, tone: "error" }]);
  // Without the answer (page opened again): a text of the status.
  assert.deepEqual(texts(quotaDisplay(offline, ready, null)), ["Сервер обработки не ответил при последнем обновлении."]);
  // Offline before any value: no numbers, no stale mark.
  const none = quotaDisplay({ ...offline, stale: false, values: null }, ready, { text: goText.offline, ok: false });
  assert.deepEqual(none.amounts, []);
  assert.equal(none.stale, "");
  assert.equal(none.updated, "");
  assert.equal(none.note, "");
});

test("stale is what Go says, not decided here", () => {
  // The same values and status: only the flag of the reply makes them stale.
  const failed = { status: "failed", stale: false, checkedAt: "2026-10-15T11:00:00Z", values };
  assert.equal(quotaDisplay(failed, ready, null).stale, "");
  assert.match(quotaDisplay({ ...failed, stale: true }, ready, null).stale, /^Устарело/);
});

test("failed: the Go text and the stale value if any", () => {
  const failed = { status: "failed", stale: true, checkedAt: "2026-10-15T11:00:00Z", values };
  const d = quotaDisplay(failed, ready, { text: goText.failed, ok: false });
  assert.equal(d.amounts.length, 3);
  assert.match(d.stale, /^Устарело/);
  assert.deepEqual(d.lines, [{ text: goText.failed, tone: "error" }]);
  assert.deepEqual(texts(quotaDisplay({ ...failed, stale: false, values: null }, ready, null)),
    ["Последнее обновление не удалось."]);
});

test("a rejected key: the key is invalid or revoked, no numbers", () => {
  const rejected = { status: "keyRejected", stale: false, checkedAt: "2026-10-15T11:00:00Z", values: null };
  for (const message of [{ text: goText.keyRejected, ok: false }, null]) {
    const d = quotaDisplay(rejected, ready, message);
    assert.deepEqual(d.amounts, []);
    assert.equal(d.updated, "");
    assert.equal(d.note, "");
    assert.equal(d.lines.length, 1);
    assert.match(d.lines[0].text, /неверный или отозван/);
    assert.equal(d.lines[0].tone, "error");
    assert.equal(d.checked, "Последняя попытка: " + localTime("2026-10-15T11:00:00Z"));
  }
});

test("no address: points to the Hub address field", () => {
  const notConfigured = { status: "notConfigured", stale: false, checkedAt: "2026-10-15T11:00:00Z", values: null };
  const noUrl = { hubUrl: "", hubKey: "set" };
  // The Go text already points to the field above.
  assert.deepEqual(texts(quotaDisplay(notConfigured, noUrl, { text: goText.notConfigured, ok: false })),
    [goText.notConfigured]);
  assert.match(texts(quotaDisplay(notConfigured, noUrl, null))[0], /в поле «Адрес сервера» выше/);
  // Not asked yet (the address was removed): the pointer, no "нажмите «Обновить»".
  assert.deepEqual(texts(quotaDisplay(unchecked, noUrl, null)), [quotaTexts.urlPointer]);
  assert.match(quotaTexts.urlPointer, /«Адрес сервера» выше/);
});

test("no key: points to the key slot", () => {
  const noKey = { status: "noKey", stale: false, checkedAt: "2026-10-15T11:00:00Z", values: null };
  const keyless = { hubUrl: "https://hub.example.org", hubKey: "unset" };
  assert.deepEqual(texts(quotaDisplay(noKey, keyless, { text: goText.noKey, ok: false })),
    [goText.noKey, quotaTexts.keyPointer]);
  assert.deepEqual(texts(quotaDisplay(noKey, keyless, null)), ["Ключ доступа не задан.", quotaTexts.keyPointer]);
  assert.match(quotaTexts.keyPointer, /«Новый ключ» выше/);
  // Not asked yet (the key was removed).
  assert.deepEqual(texts(quotaDisplay(unchecked, keyless, null)), [quotaTexts.keyPointer]);
  // Neither: both, in the order of the fields.
  assert.deepEqual(texts(quotaDisplay(unchecked, { hubUrl: "", hubKey: "unset" }, null)),
    [quotaTexts.urlPointer, quotaTexts.keyPointer]);
  // A store that is missing or did not answer is shown by the slot itself; there is no field to point to.
  assert.deepEqual(texts(quotaDisplay(unchecked, { ...keyless, hubKey: "unsupported" }, null)), []);
});

test("not asked yet with an address and a key: the hint to refresh", () => {
  const d = quotaDisplay(unchecked, ready, null);
  assert.deepEqual(d.amounts, []);
  assert.deepEqual(d.lines, [{ text: quotaTexts.notRequested, tone: "muted" }]);
  assert.equal(d.checked, "");
  // A refresh superseded by a key change: the Go text, the quota forgotten.
  const superseded = "Ключ или адрес Hub изменились во время обновления. Обновите ещё раз.";
  assert.deepEqual(quotaDisplay(unchecked, ready, { text: superseded, ok: false }).lines,
    [{ text: superseded, tone: "error" }]);
});

test("the page asks on its own only with an address and a stored key", () => {
  assert.equal(quotaRefreshable(ready), true);
  assert.equal(quotaRefreshable({ ...ready, hubUrl: "" }), false);
  for (const hubKey of ["unset", "unsupported", "unavailable"]) {
    assert.equal(quotaRefreshable({ ...ready, hubKey }), false, hubKey);
  }
});

test("«Обновить» waits for a refresh and for a change of the key or the address", () => {
  const idle = { refreshing: false, keyBusy: false, hubBusy: false };
  assert.deepEqual(quotaControls(idle), { disabled: false, label: "Обновить" });
  assert.deepEqual(quotaControls({ ...idle, refreshing: true }), { disabled: true, label: "Обновляем…" });
  assert.equal(quotaControls({ ...idle, keyBusy: true }).disabled, true);
  assert.equal(quotaControls({ ...idle, hubBusy: true }).disabled, true);
});

test("the bar shows the used share of the limit; no bar without values or a limit", () => {
  assert.equal(quotaUsedPercent(current), 38);
  assert.equal(quotaUsedPercent({ ...current, values: { ...values, remaining: values.limit } }), 0);
  assert.equal(quotaUsedPercent({ ...current, values: { ...values, remaining: 0 } }), 100);
  assert.equal(quotaUsedPercent({ ...current, values: { ...values, remaining: values.limit * 2 } }), 0);
  assert.equal(quotaUsedPercent({ ...current, values: { ...values, limit: 0 } }), null);
  assert.equal(quotaUsedPercent(unchecked), null);
});
