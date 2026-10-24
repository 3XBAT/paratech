// View logic of the writing cards of a turnkey work (TASK-132): which card a work shows, the progress line, the plan
// editor and its checks, polling. The core decides statuses and steps (ARCH-44); here they are only put into words in
// an impersonal voice. Topic, titles, paragraphs, notes and sources are model or student text: pass them through
// untouched and render them as text nodes only.

import type { OutlineItem, TurnkeyWork, WorkSection, WorkWriting } from "./bindings/turnkey.js";

export const limits = {
  minSections: 1,
  maxSections: 30,
  minWords: 1,
  maxWords: 20000,
  titleMax: 200,
  noteMax: 300,
} as const;

export const workTexts = {
  caption: "Написание работы",
  startHint: "Требования подтверждены. Работу можно написать целиком: сначала составляется план.",
  start: "Написать работу",
  planning: "Составляется план",
  approveCaption: "План готов, утвердите",
  approveHint: "Разделы можно переименовать, переставить, добавить или убрать.",
  approve: "Утвердить план",
  savePlan: "Сохранить план",
  blockedText: "Не удалось написать раздел. Можно сгенерировать заново.",
  noteLabel: "Дополнительные указания",
  regenerate: "Сгенерировать заново",
  doneCaption: "Работа готова",
  generated: "Сгенерировано — прочитайте перед сдачей",
  verify: "проверить источник",
  revise: "Доработать",
  reviseSection: "Что дорабатывать",
  allSections: "Весь текст",
  editPlan: "Изменить план",
  rewrite: "Написать заново",
  cancel: "Отмена",
  addSection: "Добавить раздел",
  titleLabel: "Название раздела",
  wordsLabel: "Объём, слов",
  moveUp: "Выше",
  moveDown: "Ниже",
  remove: "Убрать раздел",
  sources: "Источники",
  fallbackName: "Работа",
  fileKind: ".docx",
} as const;

export const checkTexts = {
  count: `Нужно от ${limits.minSections} до ${limits.maxSections} разделов.`,
  title: "У каждого раздела должно быть название.",
  words: `Объём раздела — целое число слов от ${limits.minWords} до ${limits.maxWords}.`,
} as const;

// ---- Which card ----

export interface SectionText {
  key: string;
  heading: string;
  paragraphs: string[];
  sources: { text: string; verify: boolean }[];
}

export type WorkCardModel =
  | { type: "none" }
  | { type: "start"; caption: string; hint: string; button: string }
  | { type: "running"; caption: string; line: string }
  | { type: "approve"; caption: string; hint: string }
  | { type: "blocked"; caption: string; text: string; noteLabel: string; button: string }
  | { type: "done"; caption: string; file: { kind: string; name: string }; notice: string; sections: SectionText[] };

/** «Раздел N из M» while the sections are written; N is the section being written, never above M. */
export function progressLine(w: WorkWriting): string {
  if (w.step === "outline" || w.total <= 0) {
    return workTexts.planning;
  }
  const n = w.step === "assemble" ? w.total : Math.min(w.done + 1, w.total);
  return `Раздел ${n} из ${w.total}`;
}

/** The written sections to read: the paragraphs and the sources, as they came. */
export function sectionTexts(sections: readonly WorkSection[]): SectionText[] {
  return sections
    .filter((s) => s.paragraphs.length > 0 || s.sources.length > 0)
    .map((s, i) => ({
      key: `${s.number}:${i}`,
      heading: `${s.number}. ${s.title}`,
      paragraphs: s.paragraphs,
      sources: s.sources.map((r) => ({ text: r.text, verify: r.verify })),
    }));
}

/**
 * The card of a work. A work that is not confirmed and has no writing shows nothing here (its questions, its card and
 * the helper are TASK-130). Writing.sections hold the material of a step that runs: it is never shown as a result.
 */
export function workCardModel(w: TurnkeyWork): WorkCardModel {
  const wr = w.writing;
  if (wr === null) {
    return w.status === "confirmed"
      ? { type: "start", caption: workTexts.caption, hint: workTexts.startHint, button: workTexts.start }
      : { type: "none" };
  }
  switch (wr.step) {
    case "outline":
    case "section":
    case "assemble":
      return { type: "running", caption: workTexts.caption, line: progressLine(wr) };
    case "approve":
      return { type: "approve", caption: workTexts.approveCaption, hint: workTexts.approveHint };
    case "blocked":
      return { type: "blocked", caption: workTexts.caption, text: workTexts.blockedText, noteLabel: workTexts.noteLabel, button: workTexts.regenerate };
    case "none":
      if (wr.result === "") {
        return { type: "none" };
      }
      return {
        type: "done",
        caption: workTexts.doneCaption,
        file: { kind: workTexts.fileKind, name: wr.topic.trim() === "" ? workTexts.fallbackName : wr.topic },
        notice: workTexts.generated,
        sections: sectionTexts(wr.sections),
      };
  }
}

// ---- Polling and stale replies ----

export const pollMs = 3000;
export const retryMs = 15000;

/** A step of the writing is running: the card re-reads the work until it is not. */
export function isRunning(w: TurnkeyWork | null): boolean {
  const step = w?.writing?.step;
  return step === "outline" || step === "section" || step === "assemble";
}

/** The wait before the next read, or null to stop. Nothing is read again once a loaded work does not run. */
export function pollDelay(w: TurnkeyWork | null, failed: boolean): number | null {
  if (w !== null && !isRunning(w)) {
    return null;
  }
  return failed ? retryMs : pollMs;
}

/** The newest of two replies: an older version never replaces a newer one. */
export function acceptWork(current: TurnkeyWork | null, next: TurnkeyWork): TurnkeyWork {
  return current !== null && next.version < current.version ? current : next;
}

// ---- Plan editor ----

/** A section in the editor; words stay text while typed. */
export interface EditorItem {
  /** 0: a new section. */
  number: number;
  title: string;
  words: string;
}

export const newSectionWords = "300";

export function editorFromSections(sections: readonly WorkSection[]): EditorItem[] {
  return sections.map((s) => ({ number: s.number, title: s.title, words: String(s.words) }));
}

export function addItem(items: readonly EditorItem[]): EditorItem[] {
  return items.length >= limits.maxSections ? [...items] : [...items, { number: 0, title: "", words: newSectionWords }];
}

export function removeItem(items: readonly EditorItem[], index: number): EditorItem[] {
  return items.filter((_, i) => i !== index);
}

/** Moves an item one place up (-1) or down (1); at the edge nothing moves. */
export function moveItem(items: readonly EditorItem[], index: number, dir: -1 | 1): EditorItem[] {
  const to = index + dir;
  if (index < 0 || index >= items.length || to < 0 || to >= items.length) {
    return [...items];
  }
  const out = [...items];
  [out[index], out[to]] = [out[to], out[index]];
  return out;
}

function wordsOf(raw: string): number | null {
  const t = raw.trim();
  if (!/^\d{1,6}$/.test(t)) {
    return null;
  }
  const n = Number(t);
  return n >= limits.minWords && n <= limits.maxWords ? n : null;
}

export interface EditorCheck {
  ok: boolean;
  /** The first problem in words; "" when ok. */
  problem: string;
  badTitles: boolean[];
  badWords: boolean[];
}

/** The light client check; the core checks again. */
export function checkEditor(items: readonly EditorItem[]): EditorCheck {
  const badTitles = items.map((i) => i.title.trim() === "" || i.title.length > limits.titleMax);
  const badWords = items.map((i) => wordsOf(i.words) === null);
  let problem = "";
  if (items.length < limits.minSections || items.length > limits.maxSections) {
    problem = checkTexts.count;
  } else if (badTitles.some(Boolean)) {
    problem = checkTexts.title;
  } else if (badWords.some(Boolean)) {
    problem = checkTexts.words;
  }
  return { ok: problem === "", problem, badTitles, badWords };
}

/** What EditOutline gets: the order of the editor, number 0 for a new section, trimmed titles. */
export function toOutlineItems(items: readonly EditorItem[]): OutlineItem[] {
  return items.map((i) => ({ number: i.number, title: i.title.trim(), words: wordsOf(i.words) ?? 0 }));
}

/** The editor differs from the saved plan, so EditOutline must be called before the next step. */
export function isEdited(items: readonly EditorItem[], sections: readonly WorkSection[]): boolean {
  if (items.length !== sections.length) {
    return true;
  }
  return items.some((it, i) => {
    const s = sections[i];
    return it.number !== s.number || it.title.trim() !== s.title.trim() || wordsOf(it.words) !== s.words;
  });
}

// ---- Revision ----

export interface ReviseOption {
  /** 0: every section. */
  value: number;
  label: string;
}

export function reviseOptions(sections: readonly WorkSection[]): ReviseOption[] {
  return [{ value: 0, label: workTexts.allSections }, ...sections.map((s) => ({ value: s.number, label: `${s.number}. ${s.title}` }))];
}

/** The note for Regenerate: trimmed and cut to the limit. */
export function noteOf(raw: string): string {
  return raw.trim().slice(0, limits.noteMax);
}
