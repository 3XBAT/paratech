import assert from "node:assert/strict";
import { describe, test } from "node:test";

import { checkLoopbackOnly, isLoopbackAddress, parseNetstatListeners } from "../src/app/netstat.js";

const english = [
  "",
  "Active Connections",
  "",
  "  Proto  Local Address          Foreign Address        State           PID",
  "  TCP    0.0.0.0:135            0.0.0.0:0              LISTENING       1032",
  "  TCP    127.0.0.1:9222         0.0.0.0:0              LISTENING       5544",
  "  TCP    127.0.0.1:50112        127.0.0.1:9222         ESTABLISHED     7788",
  "  TCP    192.168.1.5:50113      93.184.216.34:443      TIME_WAIT       0",
  "",
].join("\r\n");

// Russian Windows: the column titles and the state word are translated; the numbers are not.
const russian = [
  "",
  "Активные подключения",
  "",
  "  Имя    Локальный адрес        Внешний адрес          Состояние       PID",
  "  TCP    127.0.0.1:9222         0.0.0.0:0              ОЖИДАНИЕ        5544",
  "  TCP    127.0.0.1:50112        127.0.0.1:9222         УСТАНОВЛЕНО     7788",
  "",
].join("\r\n");

const ipv6 = [
  "  Proto  Local Address          Foreign Address        State           PID",
  "  TCP    [::1]:9222             [::]:0                 LISTENING       5544",
  "  TCP    [::]:445               [::]:0                 LISTENING       4",
  "  TCP    [fe80::1%4]:9222       [::]:0                 LISTENING       5544",
].join("\r\n");

describe("netstat parsing", () => {
  test("English: only the listening rows", () => {
    assert.deepEqual(parseNetstatListeners(english), [
      { address: "0.0.0.0", port: 135, pid: 1032 },
      { address: "127.0.0.1", port: 9222, pid: 5544 },
    ]);
  });

  test("Russian: the state word does not matter, the numbers do", () => {
    assert.deepEqual(parseNetstatListeners(russian), [{ address: "127.0.0.1", port: 9222, pid: 5544 }]);
  });

  test("IPv6 addresses lose their brackets", () => {
    assert.deepEqual(
      parseNetstatListeners(ipv6).map((l) => `${l.address}|${l.port}`),
      ["::1|9222", "::|445", "fe80::1%4|9222"],
    );
  });

  test("garbage and headers give no rows", () => {
    assert.deepEqual(parseNetstatListeners("Active Connections\r\n\r\n  Proto  Local Address\r\nTCP x y z"), []);
    assert.deepEqual(parseNetstatListeners(""), []);
  });
});

describe("loopback-only check", () => {
  test("a listener on 127.0.0.1 only passes (English and Russian)", () => {
    for (const out of [english, russian]) {
      const check = checkLoopbackOnly(out, 9222);
      assert.equal(check.ok, true);
      assert.equal(check.listeners.length, 1);
    }
  });

  test("::1 passes, [::] on the same port fails", () => {
    assert.equal(checkLoopbackOnly("  TCP    [::1]:9222   [::]:0   LISTENING   5544", 9222).ok, true);
    const wild = checkLoopbackOnly("  TCP    127.0.0.1:9222   0.0.0.0:0   LISTENING   5544\r\n  TCP    [::]:9222   [::]:0   LISTENING   5544", 9222);
    assert.deepEqual(wild.ok, false);
    assert.equal(!wild.ok && wild.reason, "not-loopback");
  });

  test("0.0.0.0, a LAN address or a link-local address fails", () => {
    for (const addr of ["0.0.0.0", "192.168.1.5", "[fe80::1%4]"]) {
      const check = checkLoopbackOnly(`  TCP    ${addr}:9222   0.0.0.0:0   LISTENING   5544`, 9222);
      assert.equal(check.ok, false, addr);
    }
  });

  test("no listener on the port fails (the check cannot vouch for it)", () => {
    const check = checkLoopbackOnly(english, 9333);
    assert.equal(check.ok, false);
    assert.equal(!check.ok && check.reason, "not-listening");
  });

  test("the other ports do not count", () => {
    const out = english + "  TCP    0.0.0.0:9223   0.0.0.0:0   LISTENING   1\r\n";
    assert.equal(checkLoopbackOnly(out, 9222).ok, true);
  });

  test("loopback addresses are exactly 127.0.0.1 and ::1", () => {
    assert.equal(isLoopbackAddress("127.0.0.1"), true);
    assert.equal(isLoopbackAddress("::1"), true);
    for (const a of ["0.0.0.0", "::", "127.0.0.2", "localhost", "10.0.0.1"]) {
      assert.equal(isLoopbackAddress(a), false, a);
    }
  });
});
