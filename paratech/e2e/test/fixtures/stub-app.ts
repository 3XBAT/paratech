// A stand-in for paratech.exe in the unit tests of launchApp. argv[2] is the behaviour; the rest are the app's flags.
//   ok               all four log lines, then a debug port on 127.0.0.1
//   wildcard         the same, but the debug port listens on 0.0.0.0
//   default-profile  secrets_profile=default instead of isolated
//   crash            writes the first two lines and exits with code 3
//   hang             writes the first two lines and waits
import { appendFileSync, mkdirSync } from "node:fs";
import http from "node:http";
import path from "node:path";

const [, , behaviour, ...flags] = process.argv;
const flag = (name: string): string => flags[flags.indexOf(name) + 1] ?? "";

const logFile = path.join(flag("--data-dir"), "logs", "desktop.log");
mkdirSync(path.dirname(logFile), { recursive: true });
const line = (msg: string) => appendFileSync(logFile, `time=2026-11-04T10:00:00.000+03:00 level=INFO msg="${msg}"\n`);

// What the launcher let through, for the test to read from the redirected stdout.
process.stdout.write(`env-names=${Object.keys(process.env).sort().join(",")}\n`);
process.stdout.write(`argv=${JSON.stringify(flags)}\n`);

line("instance lock acquired");
if (behaviour === "crash") {
  line("credential manager profile secrets_profile=isolated");
  process.exit(3);
}
if (behaviour === "hang") {
  line("credential manager profile secrets_profile=isolated");
  setInterval(() => {}, 1000);
} else {
  line(`credential manager profile secrets_profile=${behaviour === "default-profile" ? "default" : "isolated"}`);
  appendFileSync(logFile, `time=x level=DEBUG msg="credential manager profile id" secrets_profile_id=0123456789abcdef\n`);
  line("e2e mode on");
  line("background work started");

  const server = http.createServer((req, res) => {
    res.setHeader("content-type", "application/json");
    res.end(req.url === "/json/version" ? JSON.stringify({ Browser: "stub", webSocketDebuggerUrl: "ws://stub/devtools" }) : "{}");
  });
  server.listen(Number(flag("--e2e-cdp-port")), behaviour === "wildcard" ? "0.0.0.0" : "127.0.0.1");
}
