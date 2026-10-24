// View logic of the question card, the card «Как понято задание» and the helper page of a turnkey work (TASK-130).
// The core decides statuses, rounds and the third-round rule (ARCH-44); here they are only put into words in an
// impersonal voice. Questions, assumptions, requirements, steps and pitfalls were written by the reader (untrusted):
// pass them through untouched and render them as text nodes only, never as links or markup.

import { stepHelpUnavailableText, turnkeyErrorText } from "./bindings/turnkey.js";
import type { QuestionAnswer, TurnkeyApi, TurnkeyWork, WorkAnswered, WorkCardFields, WorkQuestion } from "./bindings/turnkey.js";
import type { StepResult } from "./workView.js";
import { runeLen } from "./workView.js";

// The limits of the domain (domain/coursework/work): a typed answer, one requirement line, a step question.
export const turnkeyLimits = { answerMax: 1000, requirementMax: 200, stepQuestionMax: 500, maxRounds: 2 } as const;

export const turnkeyTexts = {
  triageCaption: "Разбор задания",
  triageLine: "Задание разбирается",
  retry: "Повторить",
  failedCaption: "Разбор задания",
  failedText: "Задание не удалось разобрать. Требования можно уточнить вручную или повторить позже.",

  questionsCaption: "Нужно уточнить",
  roundLine: (round: number) => `Уточнение ${Math.min(Math.max(round, 1), turnkeyLimits.maxRounds)} из ${turnkeyLimits.maxRounds}`,
  lastRoundNote: "Это последний круг вопросов: дальше задание разбирается с допущениями.",
  defaultOption: "по умолчанию",
  defaultTextLabel: "по умолчанию",
  textHint: "Можно оставить пустым",
  answer: "Ответить",
  skip: "Пропустить: принять значения по умолчанию",
  answeredCaption: "Уже отвечено",
  skippedAnswer: "пропущено",
  defaultAnswer: "по умолчанию",
  why: "Зачем это нужно",

  cardCaption: "Как понято задание",
  cardHint: "Проверьте и при необходимости исправьте: работа пишется по этим требованиям.",
  notSet: "не указано",
  edited: "изменено",
  edit: "Изменить",
  confirm: "Подтвердить",
  save: "Сохранить",
  cancel: "Отмена",
  assumptions: "Принятые допущения",
  assumptionsHint: "Эти допущения сделаны вместо ответов на вопросы.",
  partialCaption: "Работа будет написана частично",
  placeholders: "Что нужно будет дописать самостоятельно",
  reason: "Пояснение",
  lineTooLong: `Строка — не длиннее ${turnkeyLimits.requirementMax} символов.`,

  helperCaption: "Помощник по заданию",
  helperHint: "Задание целиком не написать: ниже план действий, что сдать и на что обратить внимание.",
  steps: "Шаги",
  submit: "Что сдать",
  pitfalls: "Типичные ошибки",
  readyParts: "Что можно подготовить в приложении",
  placeholdersHelper: "Что нужно заполнить самостоятельно",
  helperEmpty: "Подсказок пока нет.",
  askStep: "Спросить по шагу",
  askLabel: "Вопрос по шагу",
  askSend: "Спросить",
  askClose: "Свернуть",
  askEmpty: "Введите вопрос.",
  askTooLong: `Вопрос — не длиннее ${turnkeyLimits.stepQuestionMax} символов.`,
  answerLabel: "Ответ",
} as const;

// ---- Which card ----

export type TurnkeyCardModel =
  | { type: "triage"; caption: string; line: string; problem: string }
  | { type: "failed"; caption: string; text: string }
  | { type: "questions" }
  | { type: "understanding" }
  | { type: "helper" };

/**
 * The card of a work that has no writing yet, or null (the writing cards decide). A reader call that did not go
 * through keeps the work in triage with its problem; «Повторить» asks again.
 */
export function turnkeyCardModel(w: TurnkeyWork): TurnkeyCardModel | null {
  if (w.writing !== null) {
    return null;
  }
  switch (w.status) {
    case "triage":
      return { type: "triage", caption: turnkeyTexts.triageCaption, line: turnkeyTexts.triageLine, problem: w.problem === "" ? "" : w.message };
    case "questions":
      return w.questions.length > 0 ? { type: "questions" } : null;
    case "review":
      return w.hasCard ? { type: "understanding" } : null;
    case "helper":
      return { type: "helper" };
    case "failed":
      return { type: "failed", caption: turnkeyTexts.failedCaption, text: turnkeyTexts.failedText };
    default:
      return null;
  }
}

// ---- Questions ----

export interface AnswerDraft {
  /** Index of the chosen option (a choice question). */
  option: number;
  /** The typed text (a text question); "" takes the default. */
  text: string;
}

/** The defaults of the reader: its default option, an empty text. */
export function initialDrafts(questions: readonly WorkQuestion[]): AnswerDraft[] {
  return questions.map((q) => ({ option: q.kind === "choice" && q.default >= 0 && q.default < q.options.length ? q.default : 0, text: "" }));
}

/** One line without control characters, cut to the limit in runes: the core accepts no more. */
export function lineOf(raw: string, max: number): string {
  // eslint-disable-next-line no-control-regex
  const flat = raw.replace(/[\u0000-\u001f\u007f]+/g, " ").trim();
  return [...flat].slice(0, max).join("");
}

/** What AnswerQuestions gets: an option for a choice, a trimmed single-line text for a text question. */
export function toAnswers(questions: readonly WorkQuestion[], drafts: readonly AnswerDraft[]): QuestionAnswer[] {
  return questions.map((q, i) => {
    const d = drafts[i] ?? { option: 0, text: "" };
    return q.kind === "choice"
      ? { question: i, option: d.option, text: "" }
      : { question: i, option: 0, text: lineOf(d.text, turnkeyLimits.answerMax) };
  });
}

/** The text a student left empty shows the default the core will take. */
export function defaultTextHint(q: WorkQuestion): string {
  return q.defaultText === "" ? turnkeyTexts.textHint : `${turnkeyTexts.defaultTextLabel}: ${q.defaultText}`;
}

/** The earlier answers in words: «пропущено», «по умолчанию» or the answer itself. */
export function answeredLine(a: WorkAnswered): string {
  if (a.answer === "") {
    return turnkeyTexts.skippedAnswer;
  }
  return a.default ? `${a.answer} (${turnkeyTexts.defaultAnswer})` : a.answer;
}

/** The last round of questions (the next call is final and drops further questions into assumptions). */
export function isLastRound(w: TurnkeyWork): boolean {
  return w.questionRound >= turnkeyLimits.maxRounds;
}

// ---- Card ----

export type CardKey = keyof WorkCardFields;

export interface CardRow {
  key: CardKey;
  /** Wire name of the field in TurnkeyWork.edited. */
  field: string;
  label: string;
}

export const cardRows: readonly CardRow[] = [
  { key: "workType", field: "work_type", label: "Вид работы" },
  { key: "volume", field: "volume", label: "Объём" },
  { key: "fileFormat", field: "file_format", label: "Формат файла" },
  { key: "formatting", field: "formatting", label: "Оформление" },
  { key: "variant", field: "variant", label: "Вариант" },
  { key: "deadline", field: "deadline", label: "Срок" },
  { key: "sources", field: "sources", label: "Источники" },
];

/** The closed set of work types of the core (domain/coursework/work). */
export const workTypeLabels: Readonly<Record<string, string>> = {
  referat: "Реферат",
  doklad: "Доклад",
  essay: "Эссе",
  answers: "Ответы на вопросы",
  problems: "Решение задач",
  lab_report: "Отчёт по лабораторной",
  presentation: "Презентация",
  other: "Другое",
};

export const workTypeOptions: readonly { value: string; label: string }[] = Object.entries(workTypeLabels).map(([value, label]) => ({ value, label }));

/** The value of a row as shown: the work type by its name, an empty line as «не указано». */
export function cardValue(card: WorkCardFields, key: CardKey): string {
  const v = card[key];
  if (key === "workType") {
    return workTypeLabels[v] ?? (v === "" ? turnkeyTexts.notSet : v);
  }
  return v === "" ? turnkeyTexts.notSet : v;
}

export function isFieldEdited(w: TurnkeyWork, row: CardRow): boolean {
  return w.edited.includes(row.field);
}

/** The card in the editor: every requirement is one line. */
export function cardDraft(card: WorkCardFields): WorkCardFields {
  return { ...card };
}

/** The draft as the core gets it: trimmed lines. */
export function cleanCard(draft: WorkCardFields): WorkCardFields {
  return {
    workType: draft.workType,
    volume: lineOf(draft.volume, Number.MAX_SAFE_INTEGER),
    fileFormat: lineOf(draft.fileFormat, Number.MAX_SAFE_INTEGER),
    formatting: lineOf(draft.formatting, Number.MAX_SAFE_INTEGER),
    variant: lineOf(draft.variant, Number.MAX_SAFE_INTEGER),
    deadline: lineOf(draft.deadline, Number.MAX_SAFE_INTEGER),
    sources: lineOf(draft.sources, Number.MAX_SAFE_INTEGER),
  };
}

export interface CardCheck {
  ok: boolean;
  problem: string;
  bad: Partial<Record<CardKey, boolean>>;
}

/** The light client check; the core checks again. */
export function checkCard(draft: WorkCardFields): CardCheck {
  const bad: Partial<Record<CardKey, boolean>> = {};
  const clean = cleanCard(draft);
  for (const row of cardRows) {
    if (row.key === "workType") {
      bad.workType = workTypeLabels[clean.workType] === undefined;
    } else {
      bad[row.key] = runeLen(clean[row.key]) > turnkeyLimits.requirementMax;
    }
  }
  const lineBad = cardRows.some((r) => r.key !== "workType" && bad[r.key]);
  return { ok: !Object.values(bad).some(Boolean), problem: lineBad ? turnkeyTexts.lineTooLong : "", bad };
}

/** The draft differs from the card as saved. */
export function isCardEdited(draft: WorkCardFields, card: WorkCardFields): boolean {
  const a = cleanCard(draft);
  return cardRows.some((r) => a[r.key] !== card[r.key]);
}

/** «Подтвердить»: EditUnderstanding if edited, then ConfirmRequirements; the same reply rules as the writing steps. */
export async function confirmCard(api: TurnkeyApi, workId: string, draft: WorkCardFields, edited: boolean): Promise<StepResult> {
  if (edited) {
    try {
      await api.editUnderstanding(workId, cleanCard(draft));
    } catch (error) {
      return { ok: false, error, fresh: null };
    }
  }
  try {
    return { ok: true, work: await api.confirmRequirements(workId) };
  } catch (error) {
    let fresh: TurnkeyWork | null = null;
    if (edited) {
      try {
        fresh = await api.work(workId);
      } catch {
        fresh = null;
      }
    }
    return { ok: false, error, fresh };
  }
}

/** Partial: the work is written with placeholders; the feasibility reason explains it. */
export function isPartial(w: TurnkeyWork): boolean {
  return w.partial || w.feasibility === "partial";
}

// ---- Helper ----

export type StepOutcome = { kind: "answer"; text: string } | { kind: "unavailable"; text: string } | { kind: "error"; text: string };

/** «Спросить по шагу»: the answer, the calm «пока недоступны» (no backend yet) or a failure. */
export async function askStepOutcome(api: TurnkeyApi, workId: string, step: number, question: string): Promise<StepOutcome> {
  try {
    return { kind: "answer", text: await api.askStep(workId, step, question) };
  } catch (e) {
    const text = turnkeyErrorText(e);
    return text === stepHelpUnavailableText ? { kind: "unavailable", text } : { kind: "error", text };
  }
}

/** A question about a step: trimmed, one line; "" and too long are refused before the call. */
export function stepQuestionCheck(raw: string): { ok: boolean; text: string; problem: string } {
  const text = lineOf(raw, Number.MAX_SAFE_INTEGER);
  if (text === "") {
    return { ok: false, text, problem: turnkeyTexts.askEmpty };
  }
  if (runeLen(text) > turnkeyLimits.stepQuestionMax) {
    return { ok: false, text, problem: turnkeyTexts.askTooLong };
  }
  return { ok: true, text, problem: "" };
}
