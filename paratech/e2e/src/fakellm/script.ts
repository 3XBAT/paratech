// The script of the fake Messages API: rules consumed in order (ADR-0035 section 4). Validation is strict: an unknown
// field, a wrong type or an out-of-range value rejects the whole script, so a typo never silently matches nothing.

export interface Usage {
  input_tokens: number;
  output_tokens: number;
  cache_creation_input_tokens?: number;
  cache_read_input_tokens?: number;
}

export interface ToolCall {
  name: string;
  input: Record<string, unknown>;
  /** tool_use id; default toolu_e2e_<request>_<n>. */
  id?: string;
}

export interface ToolsRule {
  kind: "tools";
  /** Substring of the last user content (text and tool_result text); "" matches any request. */
  match: string;
  reply: ToolCall[];
  /** Optional text block before the tool_use blocks. */
  text?: string;
  usage?: Usage;
}

export interface PlainRule {
  kind: "plain";
  match: string;
  reply: string;
  usage?: Usage;
}

export type Rule = ToolsRule | PlainRule;

export const DefaultUsage: Usage = { input_tokens: 100, output_tokens: 20 };

export const ScriptLimits = {
  rules: 500,
  match: 4096,
  text: 256 * 1024,
  calls: 32,
  inputBytes: 64 * 1024,
  tokens: 10_000_000,
} as const;

const toolName = /^[a-zA-Z0-9_-]{1,64}$/;
const toolId = /^[a-zA-Z0-9_-]{1,128}$/;

export class ScriptError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "ScriptError";
  }
}

/** Validates a script body {"rules": [...]} and returns the rules. */
export function parseScript(body: unknown): Rule[] {
  const top = object(body, "script");
  onlyKeys(top, ["rules"], "script");
  if (!Array.isArray(top.rules)) {
    throw new ScriptError("script.rules: want an array");
  }
  if (top.rules.length > ScriptLimits.rules) {
    throw new ScriptError(`script.rules: at most ${ScriptLimits.rules} rules`);
  }
  return top.rules.map((r, i) => parseRule(r, `rules[${i}]`));
}

export function parseRule(raw: unknown, at = "rule"): Rule {
  const r = object(raw, at);
  const match = string(r.match, `${at}.match`, ScriptLimits.match);
  const usage = r.usage === undefined ? undefined : parseUsage(r.usage, `${at}.usage`);

  if (r.kind === "plain") {
    onlyKeys(r, ["kind", "match", "reply", "usage"], at);
    const reply = string(r.reply, `${at}.reply`, ScriptLimits.text);
    return usage ? { kind: "plain", match, reply, usage } : { kind: "plain", match, reply };
  }
  if (r.kind === "tools") {
    onlyKeys(r, ["kind", "match", "reply", "text", "usage"], at);
    if (!Array.isArray(r.reply) || r.reply.length === 0 || r.reply.length > ScriptLimits.calls) {
      throw new ScriptError(`${at}.reply: want 1..${ScriptLimits.calls} tool calls`);
    }
    const reply = r.reply.map((c, j) => parseCall(c, `${at}.reply[${j}]`));
    const rule: ToolsRule = { kind: "tools", match, reply };
    if (r.text !== undefined) {
      rule.text = string(r.text, `${at}.text`, ScriptLimits.text);
    }
    if (usage) {
      rule.usage = usage;
    }
    return rule;
  }
  throw new ScriptError(`${at}.kind: want "tools" or "plain"`);
}

function parseCall(raw: unknown, at: string): ToolCall {
  const c = object(raw, at);
  onlyKeys(c, ["name", "input", "id"], at);
  if (typeof c.name !== "string" || !toolName.test(c.name)) {
    throw new ScriptError(`${at}.name: want 1-64 characters of a-z, A-Z, 0-9, _ and -`);
  }
  const input = object(c.input, `${at}.input`);
  if (JSON.stringify(input).length > ScriptLimits.inputBytes) {
    throw new ScriptError(`${at}.input: at most ${ScriptLimits.inputBytes} bytes of JSON`);
  }
  const call: ToolCall = { name: c.name, input };
  if (c.id !== undefined) {
    if (typeof c.id !== "string" || !toolId.test(c.id)) {
      throw new ScriptError(`${at}.id: want 1-128 characters of a-z, A-Z, 0-9, _ and -`);
    }
    call.id = c.id;
  }
  return call;
}

function parseUsage(raw: unknown, at: string): Usage {
  const u = object(raw, at);
  onlyKeys(u, ["input_tokens", "output_tokens", "cache_creation_input_tokens", "cache_read_input_tokens"], at);
  const usage: Usage = {
    input_tokens: tokens(u.input_tokens, `${at}.input_tokens`),
    output_tokens: tokens(u.output_tokens, `${at}.output_tokens`),
  };
  if (u.cache_creation_input_tokens !== undefined) {
    usage.cache_creation_input_tokens = tokens(u.cache_creation_input_tokens, `${at}.cache_creation_input_tokens`);
  }
  if (u.cache_read_input_tokens !== undefined) {
    usage.cache_read_input_tokens = tokens(u.cache_read_input_tokens, `${at}.cache_read_input_tokens`);
  }
  return usage;
}

function object(v: unknown, at: string): Record<string, unknown> {
  if (typeof v !== "object" || v === null || Array.isArray(v)) {
    throw new ScriptError(`${at}: want an object`);
  }
  return v as Record<string, unknown>;
}

function onlyKeys(o: Record<string, unknown>, allowed: string[], at: string): void {
  for (const k of Object.keys(o)) {
    if (!allowed.includes(k)) {
      throw new ScriptError(`${at}: unknown field ${JSON.stringify(k.slice(0, 64))}`);
    }
  }
}

function string(v: unknown, at: string, max: number): string {
  if (typeof v !== "string" || v.length > max) {
    throw new ScriptError(`${at}: want a string of at most ${max} characters`);
  }
  return v;
}

function tokens(v: unknown, at: string): number {
  if (typeof v !== "number" || !Number.isInteger(v) || v < 0 || v > ScriptLimits.tokens) {
    throw new ScriptError(`${at}: want an integer 0..${ScriptLimits.tokens}`);
  }
  return v;
}
