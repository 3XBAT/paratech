// A stand-in for hub.exe in the unit tests of startHub: it answers "keys create" and serves /readyz, and it prints
// every secret it was given, so the tests can prove the harness scrubs them. argv[2] is "ok" or "crash".
import http from "node:http";

// Same value as in hub-harness.test.ts; not imported from here, because importing this file runs it.
const StubApiKey = "pthub_stub0001_s3cr3tvalue0001";

const [, , behaviour, ...hubArgs] = process.argv;
const env = process.env;

if (hubArgs[0] === "keys" && hubArgs[1] === "create") {
  process.stderr.write(`keys: provider key in env: ${env.HUB_ANTHROPIC_API_KEY ?? "(none)"}\n`);
  process.stdout.write(`API key stub0001 issued for "${hubArgs[3]}".\nIt is shown only this once:\n${StubApiKey}\n`);
  process.exit(0);
}

process.stderr.write(`provider key=${env.HUB_ANTHROPIC_API_KEY}\n`);
process.stderr.write(`seal=${env.HUB_AGENT_SEAL_KEYS} api=${StubApiKey}\n`);
// A secret cut across two writes still has to be scrubbed.
const key = env.HUB_ANTHROPIC_API_KEY ?? "";
process.stderr.write(`split=${key.slice(0, 5)}`);
process.stderr.write(`${key.slice(5)}\n`);

// Russian text whose bytes are cut inside a two-byte char (and the key inside), the second half written later so the
// parent reads it as a separate chunk.
const russian = Buffer.from(`русский ${key} текст\n`, "utf8");
const cut = 3; // "р" is bytes 0-1, so byte 3 is inside "у"
process.stderr.write(russian.subarray(0, cut));
await new Promise((r) => setTimeout(r, 50));
process.stderr.write(russian.subarray(cut));

process.stderr.write(`quota=${env.HUB_QUOTA_LIMIT} stream=${env.HUB_PROVIDER_STREAM ?? "(unset)"} base=${env.HUB_ANTHROPIC_BASE_URL}\n`);
process.stderr.write(`env-names=${Object.keys(env).sort().join(",")}\n`);
process.stderr.write("end-of-startup\n"); // the tests wait for it: every line above is complete by then

if (behaviour === "crash") {
  process.exit(3);
}

const [host, port] = (env.HUB_LISTEN_ADDR ?? "").split(":");
http
  .createServer((req, res) => {
    res.writeHead(req.url === "/readyz" ? 200 : 404).end("ok\n");
  })
  .listen({ host, port: Number(port) });
