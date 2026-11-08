// The scripted answers of the fake provider for the assignments pack (TASK-381). Only one Hub call exists in this pack: the
// homework draft (desktop/internal/cases/homeworkflow/drafts.go -> prompts.Build, operation homework_draft). Its user message is
//   <untrusted_data>{"lesson_subject":…,"assignment":{"title":"…","description":"…"},"previous_draft":"…"}</untrusted_data>
//   <student_instructions>{"request":"…"}</student_instructions>        (only when the student typed instructions)
// and the reply is the strict JSON of prompts.ParseDraft: {"title": one line, "body": text}. The fake picks a rule by a
// SUBSTRING of that message, the first remaining one, one rule per expected call; an unscripted call fails the scenario.
import type { Rule } from "../../../src/fakellm/index.js";

export interface DraftAnswer {
  /** One line (prompts.MaxTitleRunes at most). */
  title: string;
  /** Text; Markdown headings are fine. Paragraphs are separated by a blank line. */
  body: string;
}

// Go's encoding/json escapes `<`, `>`, `&` and `"` in the data block, so a match built from the title must not hold them.
const jsonSafe = /^[^<>&"\\\p{Cc}\p{Zl}\p{Zp}]+$/u;

/** The text only the request of this assignment holds: the title inside the data block. */
export const assignmentMatch = (assignmentTitle: string): string => {
  if (!jsonSafe.test(assignmentTitle)) {
    throw new Error(`the title ${JSON.stringify(assignmentTitle)} would be escaped in the request: pick a plainer one`);
  }
  return `"assignment":{"title":"${assignmentTitle}"`;
};

const reply = (a: DraftAnswer): string => JSON.stringify({ title: a.title, body: a.body });

/** One draft: the first request of the assignment, with no instructions. */
export function firstDraftRules(assignmentTitle: string, first: DraftAnswer): Rule[] {
  return [{ kind: "plain", match: assignmentMatch(assignmentTitle), reply: reply(first) }];
}

/**
 * Two drafts of one assignment: the first one, and the regeneration with the student's instruction. The second rule matches
 * the instruction text itself, so it answers only the request that carries it; the first rule is consumed by the first request.
 */
export function regenerationRules(assignmentTitle: string, first: DraftAnswer, instruction: string, second: DraftAnswer): Rule[] {
  if (!jsonSafe.test(instruction)) {
    throw new Error(`the instruction ${JSON.stringify(instruction)} would be escaped in the request: pick a plainer one`);
  }
  return [...firstDraftRules(assignmentTitle, first), { kind: "plain", match: instruction, reply: reply(second) }];
}

/** The markers around the student's instructions in the user message (prompts.StudentOpen / StudentClose). */
export const StudentOpen = "<student_instructions>";
export const StudentClose = "</student_instructions>";
