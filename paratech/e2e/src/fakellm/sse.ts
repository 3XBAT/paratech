// The SSE form of a rule's message (docs/refs/anthropic.md section 8), as read by hub/internal/adapters/anthropic
// (stream.go readSSE, stepstream.go). Event order: message_start; per content block content_block_start, one or more
// content_block_delta, content_block_stop; message_delta; message_stop.
import { DefaultChunks, DefaultUsage, type FixedRule, type StreamOptions, type Usage } from "./script.js";
import { FakeModel } from "./messages.js";

export interface SseFrame {
  event: string;
  data: Record<string, unknown>;
}

/** `event: <name>\ndata: <json>\n\n`. */
export function encodeFrame(f: SseFrame): string {
  return `event: ${f.event}\ndata: ${JSON.stringify(f.data)}\n\n`;
}

export function isDelta(f: SseFrame): boolean {
  return f.event === "content_block_delta";
}

/**
 * Splits s into at most n non-empty pieces of nearly equal length by code points (a surrogate pair is never cut). An empty
 * string is one empty piece, so a block still has a delta.
 */
export function splitPieces(s: string, n: number): string[] {
  const points = Array.from(s);
  if (points.length === 0) {
    return [""];
  }
  const count = Math.min(n, points.length);
  const pieces: string[] = [];
  for (let i = 0; i < count; i++) {
    const from = Math.floor((i * points.length) / count);
    const to = Math.floor(((i + 1) * points.length) / count);
    pieces.push(points.slice(from, to).join(""));
  }
  return pieces;
}

/** The events of the message a stream rule answers with. */
export function sseFrames(rule: FixedRule, stream: StreamOptions, seq: number, model: string | undefined): SseFrame[] {
  const usage: Usage = rule.usage ?? DefaultUsage;
  const chunks = stream.chunks ?? DefaultChunks;
  const counts = {
    input_tokens: usage.input_tokens,
    cache_creation_input_tokens: usage.cache_creation_input_tokens ?? 0,
    cache_read_input_tokens: usage.cache_read_input_tokens ?? 0,
  };
  const frames: SseFrame[] = [
    {
      event: "message_start",
      data: {
        type: "message_start",
        message: {
          id: `msg_e2e_${seq}`,
          type: "message",
          role: "assistant",
          model: model || FakeModel,
          content: [],
          stop_reason: null,
          stop_sequence: null,
          usage: { ...counts, output_tokens: 1 },
        },
      },
    },
  ];

  let index = 0;
  const block = (start: Record<string, unknown>, deltaType: string, field: string, pieces: string[]): void => {
    frames.push({ event: "content_block_start", data: { type: "content_block_start", index, content_block: start } });
    for (const piece of pieces) {
      frames.push({ event: "content_block_delta", data: { type: "content_block_delta", index, delta: { type: deltaType, [field]: piece } } });
    }
    frames.push({ event: "content_block_stop", data: { type: "content_block_stop", index } });
    index++;
  };

  let stopReason: string;
  if (rule.kind === "tools") {
    if (rule.text !== undefined) {
      block({ type: "text", text: "" }, "text_delta", "text", splitPieces(rule.text, chunks));
    }
    rule.reply.forEach((c, i) => {
      block({ type: "tool_use", id: c.id ?? `toolu_e2e_${seq}_${i + 1}`, name: c.name, input: {} }, "input_json_delta", "partial_json", splitPieces(JSON.stringify(c.input), chunks));
    });
    stopReason = "tool_use";
  } else {
    block({ type: "text", text: "" }, "text_delta", "text", splitPieces(rule.reply, chunks));
    stopReason = "end_turn";
  }

  frames.push({
    event: "message_delta",
    data: { type: "message_delta", delta: { stop_reason: stopReason, stop_sequence: null }, usage: { ...counts, output_tokens: usage.output_tokens } },
  });
  frames.push({ event: "message_stop", data: { type: "message_stop" } });
  return frames;
}

/**
 * Index of the frame before which the connection is cut for `cutAfter`: right after the k-th delta, before the first
 * delta when k is 0, and before message_stop when the message has fewer than k deltas. -1 when there is no cut.
 */
export function cutIndex(frames: readonly SseFrame[], cutAfter: number | undefined): number {
  if (cutAfter === undefined) {
    return -1;
  }
  let seen = 0;
  for (let i = 0; i < frames.length; i++) {
    const f = frames[i];
    if (f && isDelta(f)) {
      if (seen === cutAfter) {
        return i;
      }
      seen++;
      if (seen === cutAfter) {
        return i + 1;
      }
    }
  }
  return frames.length - 1; // message_stop
}
