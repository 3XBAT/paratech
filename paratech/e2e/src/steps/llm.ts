// Spec-side client of the fake Messages API (src/fakellm) and the «unscripted LLM call» rule: a scenario fails on the
// FIRST provider call no rule answered (the Hub retries the 500, so one call shows up several times).
import type { RecordedRequest, Rule } from "../fakellm/index.js";
import { PathRequests, PathScript, PathUnmatched } from "../fakellm/index.js";
import { scrubSecrets } from "../report/scrub.js";
import { envError } from "./env.js";

export type { RecordedRequest, Rule };

export interface Llm {
  /**
   * Replaces the script and clears the recordings: call it at the start of a scenario. With the run's guard it first
   * throws UnscriptedLlmError for an unscripted call not yet reported, which the clearing would otherwise lose.
   */
  script(rules: Rule[]): Promise<void>;
  /** Every Hub→provider request since the script was set, matched or not. */
  requests(): Promise<RecordedRequest[]>;
  /** The requests no rule answered. */
  unmatched(): Promise<RecordedRequest[]>;
  /** Throws UnscriptedLlmError when any request went unanswered. */
  assertNoUnmatched(): Promise<void>;
}

const shortLength = 120;

/** What the call was asking, short and without secrets. */
export function describeCall(r: RecordedRequest): string {
  const asked = r.lastUser.replace(/\s+/g, " ").trim();
  let text: string;
  if (r.reason === "unscripted LLM call") {
    text = asked
      ? `no rule matches the last user message «${asked.length > shortLength ? `${asked.slice(0, shortLength)}…` : asked}»`
      : "no rule matches a request without a user message";
  } else {
    text = `${r.reason ?? "unmatched"} (${r.path})`;
  }
  return scrubSecrets(text);
}

export function unscriptedMessage(calls: readonly RecordedRequest[]): string {
  const first = calls[0];
  if (!first) {
    return "unscripted LLM call";
  }
  const more = calls.length > 1 ? ` (${calls.length} unanswered records, the Hub retries)` : "";
  return `unscripted LLM call: ${describeCall(first)}${more}`;
}

export class UnscriptedLlmError extends Error {
  readonly calls: readonly RecordedRequest[];

  constructor(calls: readonly RecordedRequest[]) {
    super(unscriptedMessage(calls));
    this.name = "UnscriptedLlmError";
    this.calls = calls;
  }
}

/**
 * The client of the fake at baseUrl (PARATECH_E2E_FAKE_URL). `guard` is the one step() and the test fixture use
 * (unscriptedGuard()), so a call it reports from script() is not reported again.
 */
export function createLlm(baseUrl: string, timeoutMs = 10_000, guard?: UnscriptedGuard): Llm {
  const base = baseUrl.replace(/\/+$/, "");

  const call = async (path: string, init?: RequestInit): Promise<Response> => {
    try {
      return await fetch(base + path, { ...init, signal: AbortSignal.timeout(timeoutMs) });
    } catch (err) {
      throw envError(scrubSecrets(`фейковый LLM не отвечает (${path}): ${err instanceof Error ? err.message : String(err)}`));
    }
  };

  const list = async (path: string): Promise<RecordedRequest[]> => {
    const res = await call(path);
    if (!res.ok) {
      throw new Error(`fake LLM ${path}: HTTP ${res.status}`);
    }
    return (await res.json()) as RecordedRequest[];
  };

  const llm: Llm = {
    async script(rules) {
      const lost = await guard?.check();
      if (lost) {
        throw lost;
      }
      const res = await call(PathScript, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ rules }),
      });
      if (!res.ok) {
        const detail = (await res.json().catch(() => ({}))) as { error?: string };
        throw new Error(`fake LLM refused the script: ${scrubSecrets(detail.error ?? `HTTP ${res.status}`)}`);
      }
    },
    requests: () => list(PathRequests),
    unmatched: () => list(PathUnmatched),
    async assertNoUnmatched() {
      const calls = await list(PathUnmatched);
      if (calls.length > 0) {
        throw new UnscriptedLlmError(calls);
      }
    },
  };
  return llm;
}

/**
 * Reports an unscripted call once: check() returns an error only for records newer than the last one it reported, so
 * the step that failed, the steps around it and the test teardown do not each raise it again.
 */
export class UnscriptedGuard {
  private reported = 0;

  constructor(private readonly llm: Llm) {}

  /**
   * Ignores every record that exists now: at the start of a test, so leftovers of the previous test (its retries
   * arrive late; a new worker starts after a failure) are not blamed on this one.
   */
  async baseline(): Promise<void> {
    const seen = await this.llm.unmatched();
    this.reported = Math.max(this.reported, ...seen.map((r) => r.seq));
  }

  async check(): Promise<UnscriptedLlmError | undefined> {
    const fresh = (await this.llm.unmatched()).filter((r) => r.seq > this.reported);
    if (fresh.length === 0) {
      return undefined;
    }
    this.reported = Math.max(...fresh.map((r) => r.seq));
    return new UnscriptedLlmError(fresh);
  }
}
