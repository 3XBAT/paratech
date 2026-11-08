// The scripted side of the MODEL for the agent pack (TASK-380). The real Hub runs in front of the fake provider; a rule plays one
// answer of the model, here always the planner's `reply` tool. What the pack judges is what the app sent to the model and what the
// panel shows, never the model's own wording (that is the LIVE set, TASK-382).
//
// A request of the student reaches the fake with the student's text as the last user content, so a rule matches on a distinctive
// substring of it; the next request of the same turn (after a tool) carries the tool_result as the last user content.
import type { Rule } from "../../../src/fakellm/index.js";

/** The tool catalog the pack was written for (`values.CatalogVersion` of the desktop, hub/pkg/prompts/agent/panel/v9/tools.json). */
export const CatalogVersion = 9;

/** The tool names of catalog v9, sorted. A request with another set means the pack must be rewritten for the new catalog. */
export const CatalogV9Tools: readonly string[] = [
  "answer_question",
  "ask_questions",
  "cannot_do",
  "create_assignment",
  "create_folder",
  "create_subject",
  "delivery_status",
  "edit_due",
  "find_homework",
  "find_lessons",
  "inspect_upload",
  "job_status",
  "link_material",
  "list_assignments",
  "list_subjects",
  "make_lecture_text",
  "make_summary",
  "place_upload",
  "propose_deadline",
  "propose_delete_recording",
  "propose_send",
  "reply",
  "search",
  "set_lesson_info",
  "show_plan",
  "triage_assignment",
  "write_work",
].sort();

/** The tool names a planner request to the provider carries (`tools[].name`), sorted; [] for a request without tools. */
export function toolNamesOf(body: unknown): string[] {
  const tools = (body as { tools?: unknown } | null)?.tools;
  if (!Array.isArray(tools)) {
    return [];
  }
  return tools.map((t) => String((t as { name?: unknown })?.name ?? "")).sort();
}

/** Why a planner request does not carry the v9 catalog, or undefined when it does. */
export function catalogProblem(body: unknown): string | undefined {
  const names = toolNamesOf(body);
  if (names.length === 0) {
    return "в запросе к модели нет списка инструментов (tools)";
  }
  const extra = names.filter((n) => !CatalogV9Tools.includes(n));
  const missing = CatalogV9Tools.filter((n) => !names.includes(n));
  if (extra.length === 0 && missing.length === 0) {
    return undefined;
  }
  return (
    `приложение прислало другой каталог инструментов, чем v${CatalogVersion}, под который написан пак агента (TASK-380): ` +
    `лишние [${extra.join(", ")}], нет [${missing.join(", ")}]; перепишите сценарии под новый каталог`
  );
}

export interface ReplyOptions {
  delayMs?: number;
  stream?: { chunks?: number; chunkDelayMs?: number; cutAfter?: number };
}

/** The model answers a student's request with the `reply` tool: one rule, consumed by the first request that contains `match`. */
export function replyRule(match: string, text: string, opts: ReplyOptions = {}): Rule {
  const rule: Rule = { kind: "tools", match, reply: [{ name: "reply", input: { text } }] };
  if (opts.delayMs !== undefined) {
    rule.delayMs = opts.delayMs;
  }
  if (opts.stream !== undefined) {
    rule.stream = opts.stream;
  }
  return rule;
}

/** The Hub's chat_summary operation (ADR-0028): its untrusted data is {"summary":…,"pairs":[…]}, the answer {"summary":…}. */
export const ChatSummaryMatch = '"pairs":[';

export function chatSummaryRule(summary: string, delayMs = 0): Rule {
  const rule: Rule = { kind: "plain", match: ChatSummaryMatch, reply: JSON.stringify({ summary }) };
  if (delayMs > 0) {
    rule.delayMs = delayMs;
  }
  return rule;
}

const sentences = [
  "Предел описывает, к какому значению приближается величина, когда аргумент подходит к заданной точке",
  "Для проверки берут сколь угодно малое число и ищут окрестность, в которой значения функции остаются рядом с пределом",
  "Если такая окрестность находится для любого малого числа, говорят, что предел существует",
  "Односторонние пределы справа и слева могут различаться, и тогда общего предела нет",
  "Непрерывность функции в точке означает, что предел совпадает со значением функции",
  "Бесконечно малые величины удобно сравнивать между собой по скорости стремления к нулю",
];

/** Harmless Russian text of about `runes` characters (no quotes, backslashes or control characters: it goes through JSON unchanged). */
export function filler(runes: number, seed = 0): string {
  const out: string[] = [];
  let total = 0;
  for (let i = 0; total < runes; i++) {
    const s = `${sentences[(i + seed) % sentences.length]} (${seed + i + 1}).`;
    out.push(s);
    total += [...s].length + 1;
  }
  return out.join(" ");
}
