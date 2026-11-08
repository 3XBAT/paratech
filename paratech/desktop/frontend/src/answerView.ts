import type { AnswerClaim, AnswerView } from "./bindings/answers.js";

// Pure view logic of the answer card body: texts, the long-claim clamp, what is drawn for a read answer.
// The claims, the missing parts and every quote are model text: callers render them as React text nodes only.

export const answerTexts = {
  loading: "Загрузка ответа…",
  failed: "Ответ не открылся. Попробуйте ещё раз.",
  retry: "Повторить",
  /** In front of a statement the lessons do not support; it must never read as lesson material. */
  notFromLessons: "Не из материалов пар:",
  missing: "В материалах не нашлось:",
  /** No claim, nothing missing and no source: the way on is in the words. */
  empty: "В материалах пар ответа нет. Попробуйте уточнить вопрос или выбрать другой предмет.",
  /** No claim but some sources: the question is only mentioned there. */
  mentioned: "Прямого ответа нет, но вопрос упоминается здесь:",
  showAll: "Показать полностью",
  collapse: "Свернуть",
} as const;

/** A claim longer than this many UTF-16 units shows its start and a «Показать полностью» toggle. */
export const claimClampLimit = 600;

/** A cut in the middle of a word backs up to a space only when one is this close to the limit. */
const wordBackup = 80;

export interface ClampedClaim {
  text: string;
  clamped: boolean;
}

/**
 * The start of a long text with «…»; a short text is returned as it is. The cut is at the last whitespace within the last
 * 80 units of the limit when there is one (no half word), else at the limit; never inside a surrogate pair.
 */
export function clampClaim(text: string, limit: number = claimClampLimit): ClampedClaim {
  if (text.length <= limit) {
    return { text, clamped: false };
  }
  let end = limit;
  const last = text.charCodeAt(end - 1);
  if (last >= 0xd800 && last <= 0xdbff) {
    end -= 1;
  }
  if (!/\s/.test(text.charAt(end))) {
    for (let i = end - 1; i > 0 && i >= end - wordBackup; i--) {
      if (/\s/.test(text.charAt(i))) {
        end = i;
        break;
      }
    }
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
  /** The line for an empty answer: it says nothing was found only when there is no source either, else the sources are only mentions. */
  emptyNote: string;
}

export function answerParts(view: AnswerView): AnswerParts {
  const claims = view.claims.filter((c) => c.text.trim() !== "");
  const missing = missingLine(view.missing);
  const empty = claims.length === 0 && missing === "";
  const emptyNote = !empty ? "" : view.sources.length > 0 ? answerTexts.mentioned : answerTexts.empty;
  return { claims, missing, empty, emptyNote };
}
