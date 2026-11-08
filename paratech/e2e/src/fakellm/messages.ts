// Wire format of the fake, as read by hub/internal/adapters/anthropic (wire.go, wire_agent.go) and described in
// docs/refs/anthropic.md sections 3, 4 and 7: the unary (JSON) answer. The SSE form of the same message is in sse.ts.
import { DefaultUsage, type FixedRule, type Rule, type Usage } from "./script.js";

export const FakeModel = "claude-e2e-fake";

/**
 * The text of the last user message: its string content, or its text blocks and the text of its tool_result blocks
 * (string content or text blocks), joined by newlines. "" when there is none.
 */
export function lastUserContent(body: unknown): string {
  const messages = (body as { messages?: unknown })?.messages;
  if (!Array.isArray(messages)) {
    return "";
  }
  for (let i = messages.length - 1; i >= 0; i--) {
    const m = messages[i] as { role?: unknown; content?: unknown };
    if (m?.role === "user") {
      return contentText(m.content);
    }
  }
  return "";
}

function contentText(content: unknown): string {
  if (typeof content === "string") {
    return content;
  }
  if (!Array.isArray(content)) {
    return "";
  }
  const parts: string[] = [];
  for (const block of content as Array<{ type?: unknown; text?: unknown; content?: unknown }>) {
    if (block?.type === "text" && typeof block.text === "string") {
      parts.push(block.text);
    } else if (block?.type === "tool_result") {
      parts.push(contentText(block.content));
    }
  }
  return parts.join("\n");
}

/** Index of the first remaining rule whose match is a substring of text, or -1. */
export function findRule(rules: readonly Rule[], text: string): number {
  return rules.findIndex((r) => text.includes(r.match));
}

/** The 200 body of a Messages API answer for a rule. */
export function messageResponse(rule: FixedRule, seq: number, model: string | undefined): Record<string, unknown> {
  const usage: Usage = rule.usage ?? DefaultUsage;
  let content: Array<Record<string, unknown>>;
  let stopReason: string;
  if (rule.kind === "tools") {
    content = [
      ...(rule.text !== undefined ? [{ type: "text", text: rule.text }] : []),
      ...rule.reply.map((c, i) => ({
        type: "tool_use",
        id: c.id ?? `toolu_e2e_${seq}_${i + 1}`,
        name: c.name,
        input: c.input,
      })),
    ];
    stopReason = "tool_use";
  } else {
    content = [{ type: "text", text: rule.reply }];
    stopReason = "end_turn";
  }
  return {
    id: `msg_e2e_${seq}`,
    type: "message",
    role: "assistant",
    model: model || FakeModel,
    content,
    stop_reason: stopReason,
    stop_sequence: null,
    usage: {
      input_tokens: usage.input_tokens,
      output_tokens: usage.output_tokens,
      cache_creation_input_tokens: usage.cache_creation_input_tokens ?? 0,
      cache_read_input_tokens: usage.cache_read_input_tokens ?? 0,
    },
  };
}

/** The error envelope of the Messages API. */
export function errorResponse(type: string, message: string, seq: number): Record<string, unknown> {
  return { type: "error", error: { type, message }, request_id: `req_e2e_${seq}` };
}
