import http from "node:http";

import { ReasonNoFragment, resolveCited } from "./cited.js";
import { errorResponse, findRule, lastUserContent, messageResponse } from "./messages.js";
import { type FixedRule, parseScript, type Rule, ScriptError, type StreamOptions } from "./script.js";
import { cutIndex, encodeFrame, isDelta, type SseFrame, sseFrames } from "./sse.js";

export const PathScript = "/__e2e/script";
export const PathRequests = "/__e2e/requests";
export const PathUnmatched = "/__e2e/unmatched";
const messagesSuffix = "/v1/messages";

const defaultMaxMessageBytes = 8 << 20; // above the Hub's 1 MiB payload plus prompts
const maxScriptBytes = 1 << 20;
const maxRecordedRaw = 64 << 10;
const loopback = "127.0.0.1";

/** One request the Hub sent to the provider. Headers are never recorded: they carry the provider key. */
export interface RecordedRequest {
  seq: number;
  path: string;
  matched: boolean;
  /** Index of the rule that answered, in the script as set. */
  rule?: number;
  /** Why no rule answered. */
  reason?: string;
  /** What the rules were matched against. */
  lastUser: string;
  /** The parsed JSON body, or the raw text (cut at 64 KiB) when it was not JSON. */
  body: unknown;
}

export interface FakeLLM {
  /** Base URL for HUB_ANTHROPIC_BASE_URL. */
  readonly url: string;
  /** Replaces the script and clears the recorded requests: a script starts a scenario. */
  setScript(rules: Rule[]): void;
  requests(): RecordedRequest[];
  unmatched(): RecordedRequest[];
  /** How many rules are not consumed yet. */
  remaining(): number;
  stop(): Promise<void>;
}

export interface FakeLLMOptions {
  maxMessageBytes?: number;
}

/** Starts the fake Messages API on a free port of 127.0.0.1. */
export async function startFakeLLM(opts: FakeLLMOptions = {}): Promise<FakeLLM> {
  const maxMessageBytes = opts.maxMessageBytes ?? defaultMaxMessageBytes;
  let rules: Array<{ rule: Rule; index: number }> = [];
  let records: RecordedRequest[] = [];
  let seq = 0;

  const setScript = (next: Rule[]): void => {
    const parsed = parseScript({ rules: next });
    rules = parsed.map((rule, index) => ({ rule, index }));
    records = [];
  };

  const answerMessages = (path: string, raw: string, res: http.ServerResponse): void => {
    seq++;
    let body: unknown;
    try {
      body = JSON.parse(raw);
    } catch {
      records.push({ seq, path, matched: false, reason: "malformed JSON", lastUser: "", body: raw.slice(0, maxRecordedRaw) });
      send(res, 400, errorResponse("invalid_request_error", "e2e fake: the body is not JSON", seq));
      return;
    }
    const lastUser = lastUserContent(body);
    const at = findRule(
      rules.map((r) => r.rule),
      lastUser,
    );
    if (at < 0) {
      records.push({ seq, path, matched: false, reason: "unscripted LLM call", lastUser, body });
      send(res, 500, errorResponse("api_error", "e2e fake: unscripted LLM call", seq));
      return;
    }
    const chosen = rules[at];
    if (!chosen) {
      return;
    }
    // A cited rule needs a fragment in the request; without one it is not consumed.
    let fixed: FixedRule;
    if (chosen.rule.kind === "cited") {
      const resolved = resolveCited(chosen.rule, lastUser);
      if (!resolved) {
        records.push({ seq, path, matched: false, reason: ReasonNoFragment, lastUser, body });
        send(res, 500, errorResponse("api_error", `e2e fake: ${ReasonNoFragment}`, seq));
        return;
      }
      fixed = resolved;
    } else {
      fixed = chosen.rule;
    }
    rules.splice(at, 1);
    records.push({ seq, path, matched: true, rule: chosen.index, lastUser, body });

    const model = typeof (body as { model?: unknown })?.model === "string" ? (body as { model: string }).model : undefined;
    const stream = (body as { stream?: unknown })?.stream === true ? fixed.stream : undefined;
    const delayMs = chosen.rule.delayMs ?? 0;
    const id = seq;
    void (async () => {
      if (!(await wait(res, delayMs))) {
        return;
      }
      if (stream) {
        await sendStream(res, sseFrames(fixed, stream, id, model), stream);
        return;
      }
      send(res, 200, messageResponse(fixed, id, model));
    })();
  };

  let port = 0; // known after listen; requests only arrive after it
  const server = http.createServer((req, res) => {
    // DNS-rebinding guard: a web page that resolves its own name to 127.0.0.1 sends another Host. The Hub and the specs
    // use the literal 127.0.0.1 URL.
    const host = req.headers.host;
    if (host !== `${loopback}:${port}` && host !== `localhost:${port}`) {
      req.resume();
      send(res, 403, { error: "e2e fake: Host must be 127.0.0.1 or localhost with the fake's port" });
      return;
    }
    const path = (req.url ?? "/").split("?")[0] ?? "/";
    const method = req.method ?? "GET";

    if (path === PathRequests || path === PathUnmatched) {
      req.resume();
      if (method !== "GET") {
        send(res, 405, { error: "want GET" });
        return;
      }
      send(res, 200, path === PathRequests ? records : records.filter((r) => !r.matched));
      return;
    }
    if (path === PathScript) {
      if (method !== "POST") {
        req.resume();
        send(res, 405, { error: "want POST" });
        return;
      }
      readBody(req, maxScriptBytes, (raw) => {
        if (raw === undefined) {
          send(res, 413, { error: `script over ${maxScriptBytes} bytes` });
          return;
        }
        try {
          const parsed = parseScript(JSON.parse(raw));
          setScript(parsed);
          send(res, 200, { rules: parsed.length });
        } catch (err) {
          send(res, 400, { error: err instanceof ScriptError ? err.message : "script: the body is not JSON" });
        }
      });
      return;
    }
    if (method === "POST" && path.endsWith(messagesSuffix)) {
      readBody(req, maxMessageBytes, (raw) => {
        if (raw === undefined) {
          seq++;
          records.push({ seq, path, matched: false, reason: "body too large", lastUser: "", body: "" });
          send(res, 413, errorResponse("request_too_large", "e2e fake: request too large", seq));
          return;
        }
        answerMessages(path, raw, res);
      });
      return;
    }
    // Not a control endpoint and not POST …/v1/messages: recorded as unmatched too, so a provider endpoint the Hub
    // starts calling (count_tokens, batches...) shows up in /__e2e/unmatched instead of hiding behind a 404.
    readBody(req, maxRecordedRaw, (raw) => {
      seq++;
      records.push({ seq, path, matched: false, reason: `unknown endpoint ${method} ${path}`, lastUser: "", body: parseOrRaw(raw ?? "") });
      send(res, 404, errorResponse("not_found_error", "e2e fake: unknown path", seq));
    });
  });

  await new Promise<void>((resolve, reject) => {
    server.once("error", reject);
    server.listen({ host: loopback, port: 0 }, () => resolve());
  });
  const addr = server.address();
  port = typeof addr === "object" && addr ? addr.port : 0;

  return {
    url: `http://${loopback}:${port}`,
    setScript,
    requests: () => structuredClone(records),
    unmatched: () => structuredClone(records.filter((r) => !r.matched)),
    remaining: () => rules.length,
    stop: () =>
      new Promise<void>((resolve) => {
        server.close(() => resolve());
        server.closeAllConnections();
      }),
  };
}

/** Reads the body up to max bytes; undefined when it is larger (the rest is drained, not kept). */
function readBody(req: http.IncomingMessage, max: number, done: (raw: string | undefined) => void): void {
  const chunks: Buffer[] = [];
  let size = 0;
  let finished = false;
  const finish = (raw: string | undefined): void => {
    if (!finished) {
      finished = true;
      done(raw);
    }
  };
  req.on("data", (c: Buffer) => {
    size += c.length;
    if (size <= max) {
      chunks.push(c);
    }
  });
  req.on("end", () => finish(size > max ? undefined : Buffer.concat(chunks).toString("utf8")));
  req.on("error", () => finish(undefined));
}

function parseOrRaw(raw: string): unknown {
  try {
    return JSON.parse(raw);
  } catch {
    return raw;
  }
}

const gone = (res: http.ServerResponse): boolean => res.socket === null || res.socket.destroyed;

/** Waits ms; false when the client went away first (the Hub gave up, «Стоп»), so nothing is written afterwards. */
function wait(res: http.ServerResponse, ms: number): Promise<boolean> {
  if (gone(res)) {
    return Promise.resolve(false);
  }
  if (ms <= 0) {
    return Promise.resolve(true);
  }
  return new Promise((resolve) => {
    const onClose = (): void => {
      clearTimeout(timer);
      resolve(false);
    };
    const timer = setTimeout(() => {
      res.off("close", onClose);
      resolve(true);
    }, ms);
    res.once("close", onClose);
  });
}

/** Writes one chunk; false when the connection is gone. */
function write(res: http.ServerResponse, chunk: string): Promise<boolean> {
  if (gone(res)) {
    return Promise.resolve(false);
  }
  return new Promise((resolve) => {
    const onClose = (): void => resolve(false);
    res.once("close", onClose);
    res.write(chunk, (err) => {
      res.off("close", onClose);
      resolve(!err);
    });
  });
}

/** Sends the events one by one; destroys the socket instead of the frame at the cut index (a broken network). */
async function sendStream(res: http.ServerResponse, frames: readonly SseFrame[], opts: StreamOptions): Promise<void> {
  const cut = cutIndex(frames, opts.cutAfter);
  const chunkDelayMs = opts.chunkDelayMs ?? 0;
  res.writeHead(200, { "content-type": "text/event-stream", "cache-control": "no-cache" });
  res.flushHeaders();
  let deltas = 0;
  for (const [i, frame] of frames.entries()) {
    if (i === cut) {
      res.destroy();
      return;
    }
    if (isDelta(frame)) {
      if (deltas > 0 && !(await wait(res, chunkDelayMs))) {
        return;
      }
      deltas++;
    }
    if (!(await write(res, encodeFrame(frame)))) {
      return;
    }
  }
  res.end();
}

function send(res: http.ServerResponse, status: number, body: unknown): void {
  const data = JSON.stringify(body);
  res.writeHead(status, { "content-type": "application/json", "content-length": Buffer.byteLength(data) });
  res.end(data);
}
