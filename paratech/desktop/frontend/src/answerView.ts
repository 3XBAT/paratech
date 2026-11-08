import type { AnswerClaim, AnswerView } from "./bindings/answers.js";

// Pure view logic of the answer card body: texts, the long-claim clamp, what is drawn for a read answer.
// The claims, the missing parts and every quote are model text: callers render them as React text nodes only.

export const answerTexts = {
  loading: "Загрузка ответа…",
  failed: "Ответ не загрузился.",
  /** In front of a statement the lessons do not support; it must never read as lesson material. */
  notFromLessons: "Не из материалов пар:",
  missing: "В материалах не нашлось:",
  empty: "В материалах ничего не нашлось.",
  showAll: "Показать полностью",
  collapse: "Свернуть",
} as const;

/** A claim longer than this many UTF-16 units shows its start and a «Показать полностью» toggle. */
export const claimClampLimit = 600;

export interface ClampedClaim {
  text: string;
  clamped: boolean;
}

/** The start of a long text (never cut inside a surrogate pair) with «…»; a short text is returned as it is. */
export function clampClaim(text: string, limit: number = claimClampLimit): ClampedClaim {
  if (text.length <= limit) {
    return { text, clamped: false };
  }
  let end = limit;
  const last = text.charCodeAt(end - 1);
  if (last >= 0xd800 && last <= 0xdbff) {
    end -= 1;
  }
  return { text: `${text.slice(0, end).trimEnd()}…`, clamped: true };
}

/** The parts the lessons do not answer as one line, or "" when there are none. Blank items are dropped. */
export function missingLine(missing: readonly string[]): string {
  const items = missing.map((m) => m.trim()).filter((m) => m !== "");
  return items.length === 0 ? "" : `${answerTexts.missing} ${items.join("; ")}`;
}

export interface AnswerParts {
  /** Claims with some text, in the core's order. */
  claims: AnswerClaim[];
  /** "" when nothing is missing. */
  missing: string;
  /** No claim and nothing missing: the lessons said nothing. */
  empty: boolean;
}

export function answerParts(view: AnswerView): AnswerParts {
  const claims = view.claims.filter((c) => c.text.trim() !== "");
  const missing = missingLine(view.missing);
  return { claims, missing, empty: claims.length === 0 && missing === "" };
}
