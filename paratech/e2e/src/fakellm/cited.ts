// The `cited` rule: an answer of the Hub operation ANSWER (hub/internal/domain/llm/services/answer). The Hub sends one
// user message, the JSON object {"question": "...", "fragments": [{"chunk_id": "...", "text": "..."}]} (Input.ModelData),
// and drops every citation whose chunk_id was not sent or whose quote does not occur in that fragment. A static script
// cannot know either, so the fake reads them from the request.
import { DefaultWords, type CitedRule, type PlainRule } from "./script.js";

export const ReasonNoFragment = "cited: no fragment in the request";

/** The plain rule a cited rule becomes for this request, or undefined when the request holds no usable fragment. */
export function resolveCited(rule: CitedRule, lastUser: string): PlainRule | undefined {
  const fragment = firstFragment(lastUser);
  if (!fragment) {
    return undefined;
  }
  const quote = firstWords(fragment.text, rule.words ?? DefaultWords);
  const reply = JSON.stringify({
    claims: [{ text: rule.claim, cites: [{ chunk_id: fragment.chunkId, quote }] }],
    missing: rule.missing ?? [],
  });
  const resolved: PlainRule = { kind: "plain", match: rule.match, reply };
  if (rule.usage) {
    resolved.usage = rule.usage;
  }
  return resolved;
}

function firstFragment(lastUser: string): { chunkId: string; text: string } | undefined {
  let data: unknown;
  try {
    data = JSON.parse(lastUser);
  } catch {
    return undefined;
  }
  const fragments = (data as { fragments?: unknown } | null)?.fragments;
  if (!Array.isArray(fragments)) {
    return undefined;
  }
  const first = fragments[0] as { chunk_id?: unknown; text?: unknown } | null | undefined;
  if (typeof first?.chunk_id !== "string" || first.chunk_id === "" || typeof first.text !== "string" || first.text.trim() === "") {
    return undefined;
  }
  return { chunkId: first.chunk_id, text: first.text };
}

/** The first `words` whitespace-separated words of text, as one verbatim substring (inner spacing kept). */
export function firstWords(text: string, words: number): string {
  const found = [...text.matchAll(/\S+/g)].slice(0, words);
  const head = found[0];
  const tail = found[found.length - 1];
  if (!head || !tail) {
    return "";
  }
  return text.slice(head.index, (tail.index ?? 0) + tail[0].length);
}
