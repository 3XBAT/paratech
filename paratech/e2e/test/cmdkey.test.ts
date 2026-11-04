import assert from "node:assert/strict";
import { describe, test } from "node:test";

import {
  type CmdkeyRunner,
  deleteProfileCredentials,
  isValidProfileId,
  parseCmdkeyTargets,
  parseSecretsProfileId,
  profileTargetPrefix,
  profileTargets,
} from "../src/sandbox/cmdkey.js";

const id = "0123456789abcdef";
const otherId = "fedcba9876543210";

// Obviously fake names; the layout of `cmdkey /list` is the real one.
const english = [
  "",
  "Currently stored credentials:",
  "",
  "    Target: MicrosoftAccount:target=SSO_POP_Device",
  "    Type: Generic ",
  "    User: someone",
  "    Saved for this logon only",
  "    ",
  `    Target: LegacyGeneric:target=paratech-${id}/hub/api-key`,
  "    Type: Generic ",
  "    Local machine persistence",
  "    ",
  `    Target: LegacyGeneric:target=paratech-${otherId}/hub/api-key`,
  "    Type: Generic ",
  "    ",
  "    Target: LegacyGeneric:target=paratech/google/client-secret",
  "    Type: Generic ",
  "    ",
  `    Target: LegacyGeneric:target=paratech-${id}x/mail/password`,
  `    Target: LegacyGeneric:target=xparatech-${id}/mail/password`,
  `    Target: LegacyGeneric:target=paratech-${id}/`,
  `    Target: LegacyGeneric:target=paratech-${id}/mail/password`,
  "",
].join("\r\n");

const russian = [
  "",
  "Сохраненные учетные данные:",
  "",
  "    Объект: MicrosoftAccount:target=SSO_POP_Device",
  "    Тип: Универсальный ",
  "    Пользователь: someone",
  "    Только для этого входа",
  "    ",
  `    Объект: LegacyGeneric:target=paratech-${id}/google/token`,
  "    Тип: Универсальный ",
  "    Локальный компьютер",
  "    ",
  `    Объект: LegacyGeneric:target=paratech-${otherId}/google/token`,
  "    ",
  "    Объект: LegacyGeneric:target=paratech/hub/api-key",
  "",
].join("\r\n");

describe("cmdkey /list parsing", () => {
  test("English output: targets after target=", () => {
    const targets = parseCmdkeyTargets(english);
    assert.ok(targets.includes(`paratech-${id}/hub/api-key`));
    assert.ok(targets.includes("paratech/google/client-secret"));
    assert.ok(targets.includes("SSO_POP_Device"));
  });

  test("Russian output: the localized label does not matter", () => {
    const targets = parseCmdkeyTargets(russian);
    assert.ok(targets.includes(`paratech-${id}/google/token`));
    assert.ok(targets.includes("paratech/hub/api-key"));
  });

  test("a label without the target= token is read as «label: value»", () => {
    assert.deepEqual(parseCmdkeyTargets(`    Объект: paratech-${id}/x\r\n`), [`paratech-${id}/x`]);
  });
});

describe("which targets belong to the profile", () => {
  test("only the exact prefix paratech-<id>/ with a name after it, English", () => {
    assert.deepEqual(profileTargets(english, id).sort(), [`paratech-${id}/hub/api-key`, `paratech-${id}/mail/password`].sort());
  });

  test("only the exact prefix, Russian", () => {
    assert.deepEqual(profileTargets(russian, id), [`paratech-${id}/google/token`]);
    assert.deepEqual(profileTargets(russian, otherId), [`paratech-${otherId}/google/token`]);
  });

  test("another profile, the owner's paratech/… entries and look-alikes are left alone", () => {
    for (const t of profileTargets(english, id)) {
      assert.ok(t.startsWith(`paratech-${id}/`), t);
    }
    const all = profileTargets(english, id).join("\n");
    assert.ok(!all.includes(otherId) && !all.includes("paratech/google") && !all.includes(`${id}x`) && !all.includes("xparatech"));
  });

  test("a target with a quote or a control character is skipped", () => {
    const hostile = `    Target: LegacyGeneric:target=paratech-${id}/a" /delete:paratech/hub\r\n`;
    assert.deepEqual(profileTargets(hostile, id), []);
  });

  test("a malformed id is refused", () => {
    for (const bad of ["", "abc", "x/../y", "0123 456789", "paratech/", `${id}\n`]) {
      assert.equal(isValidProfileId(bad), false, bad);
      assert.throws(() => profileTargetPrefix(bad));
    }
    assert.equal(isValidProfileId(id), true);
    assert.equal(isValidProfileId(undefined), false);
  });
});

describe("deleting the profile's entries", () => {
  function fakeRunner(listing: string, failOn?: string) {
    const calls: string[][] = [];
    const run: CmdkeyRunner = (args) => {
      calls.push(args);
      if (args[0] === "/list") {
        return { code: 0, stdout: listing };
      }
      return { code: args[0] === `/delete:${failOn}` ? 1 : 0, stdout: "" };
    };
    return { run, calls };
  }

  test("deletes exactly the targets of the profile", () => {
    const { run, calls } = fakeRunner(english);
    const res = deleteProfileCredentials(id, run);
    const deletes = calls.filter((c) => c[0]?.startsWith("/delete:")).map((c) => c[0]);
    assert.deepEqual(deletes.sort(), [`/delete:paratech-${id}/hub/api-key`, `/delete:paratech-${id}/mail/password`].sort());
    assert.equal(res.failed.length, 0);
    assert.equal(res.deleted.length, 2);
    assert.ok(calls.every((c) => c.length === 1), "one argument per cmdkey call, no shell");
  });

  test("an unknown or malformed id deletes nothing and does not even list", () => {
    for (const bad of [undefined, "", "short", "a/b/c/d/e"]) {
      const { run, calls } = fakeRunner(english);
      assert.deepEqual(deleteProfileCredentials(bad, run), { deleted: [], failed: [] });
      assert.deepEqual(calls, []);
    }
  });

  test("a failed delete is reported, the others still go", () => {
    const { run } = fakeRunner(english, `paratech-${id}/hub/api-key`);
    const res = deleteProfileCredentials(id, run);
    assert.deepEqual(res.failed, [`paratech-${id}/hub/api-key`]);
    assert.deepEqual(res.deleted, [`paratech-${id}/mail/password`]);
  });

  test("a failed listing deletes nothing", () => {
    const calls: string[][] = [];
    const res = deleteProfileCredentials(id, (args) => {
      calls.push(args);
      return { code: 1, stdout: "" };
    });
    assert.deepEqual(res.deleted, []);
    assert.deepEqual(res.failed, ["cmdkey /list"]);
    assert.equal(calls.length, 1);
  });
});

describe("secrets_profile_id in the debug log", () => {
  test("is read from the real log line", () => {
    const log = `time=2026-11-04T10:00:00.000+03:00 level=DEBUG msg="credential manager profile id" secrets_profile_id=${id}\n`;
    assert.equal(parseSecretsProfileId(log), id);
  });

  test("absent or malformed gives undefined", () => {
    assert.equal(parseSecretsProfileId("level=INFO msg=hello"), undefined);
    assert.equal(parseSecretsProfileId("secrets_profile_id=ab"), undefined);
    assert.equal(parseSecretsProfileId('secrets_profile_id="a b c d e f g"'), undefined);
  });
});
